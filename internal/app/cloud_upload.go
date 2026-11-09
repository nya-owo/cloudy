package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/local"
	"netease-music-mygo/internal/netease"
)

// Songs from the listener's computer go up to their cloud drive one at a
// time, as downloads come down: one uploading, those waiting behind it, and
// those that failed, kept across launches to be tried again.

// uploadToCloud sends a file to the cloud drive; tests replace it.
var uploadToCloud = netease.CloudUpload

// chooseUploadFiles asks for the songs to upload; tests replace it.
var chooseUploadFiles = func(parent *mygo.Window) ([]string, error) {
	return mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:      parent,
		Title:       "选择要上传到云盘的音乐",
		ButtonLabel: "上传",
		Multiple:    true,
		Filters:     []mygo.FileFilter{{Name: "音乐", Extensions: netease.CloudUploadExts}},
	})
}

// cloudUpload is a song waiting to go up, or that went up and failed, as its
// file's tags name it.
type cloudUpload struct {
	Path   string `json:"path"`
	Title  string `json:"title,omitempty"`
	Artist string `json:"artist,omitempty"`
	Album  string `json:"album,omitempty"`
	Size   int64  `json:"size,omitempty"`
	Reason string `json:"reason,omitempty"` // why it failed, for one that did
}

// name is what the upload is listed as.
func (u cloudUpload) name() string {
	if u.Title != "" {
		return u.Title
	}
	base := filepath.Base(u.Path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// uploadJob is the song going up now, and how far it has come.
type uploadJob struct {
	item cloudUpload
	sent int64
	all  int64
}

// uploadKey compares paths as the system does: without regard to case on
// Windows.
func uploadKey(path string) string {
	path = filepath.Clean(path)
	if filepath.Separator == '\\' {
		return strings.ToLower(path)
	}
	return path
}

// readUpload reads what a file's tags say of it, for the list and for the
// drive.
func readUpload(path string) (cloudUpload, error) {
	info, err := os.Stat(path)
	if err != nil {
		return cloudUpload{}, err
	}
	if info.IsDir() {
		return cloudUpload{}, errors.New("is a folder")
	}
	u := cloudUpload{Path: path, Size: info.Size()}
	if s, err := local.Read(path); err == nil {
		u.Title, u.Artist, u.Album = s.Title, s.Artist, s.Album
	}
	return u, nil
}

// uploadable reports whether the cloud drive takes a file of path's kind.
func uploadable(path string) bool {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	return slices.Contains(netease.CloudUploadExts, ext)
}

// pickUploads asks for songs, and queues those chosen. The dialog waits for
// the main thread, which shows it.
func (a *app) pickUploads() {
	if a.profile == nil {
		a.toast = "登录后才能上传到云盘"
		a.redraw()
		return
	}
	win := a.win
	go func() {
		paths, err := chooseUploadFiles(win)
		if err != nil || len(paths) == 0 {
			return
		}
		var items []cloudUpload
		skipped := 0
		for _, p := range paths {
			if !uploadable(p) {
				skipped++
				continue
			}
			u, err := readUpload(p)
			if err != nil {
				skipped++
				continue
			}
			items = append(items, u)
		}
		a.post(func() { a.queueUploads(items, skipped) })
	}()
}

// uploadPending reports whether path waits or goes up now.
func (a *app) uploadPending(path string) bool {
	key := uploadKey(path)
	if a.uploadJob != nil && uploadKey(a.uploadJob.item.Path) == key {
		return true
	}
	for _, u := range a.uploadQueue {
		if uploadKey(u.Path) == key {
			return true
		}
	}
	return false
}

// queueUploads adds songs to what goes up, those queued already once.
func (a *app) queueUploads(items []cloudUpload, skipped int) {
	added := 0
	for _, u := range items {
		if a.uploadPending(u.Path) {
			continue
		}
		u.Reason = ""
		a.dropFailedUploads(u.Path)
		a.uploadQueue = append(a.uploadQueue, u)
		added++
	}
	switch {
	case added == 1 && skipped == 0:
		a.toast = "已加入上传：" + items[len(items)-1].name()
	case added > 0 && skipped > 0:
		a.toast = "已加入上传 " + strconv.Itoa(added) + " 首，" + strconv.Itoa(skipped) + " 个文件不是云盘支持的音乐"
	case added > 0:
		a.toast = "已加入上传 " + strconv.Itoa(added) + " 首"
	case skipped > 0:
		a.toast = "所选文件不是云盘支持的音乐"
	default:
		a.toast = "这些歌曲已经在上传列表里"
	}
	a.pumpUpload()
	a.saveUploadQueue()
	a.redraw()
}

// pumpUpload starts the next song queued, unless one is going up.
func (a *app) pumpUpload() {
	if a.uploadJob != nil || len(a.uploadQueue) == 0 {
		if a.uploadJob == nil && a.uploadedSome {
			// The drive changed: read it again once all went up.
			a.uploadedSome = false
			a.loadCloud(true)
		}
		return
	}
	u := a.uploadQueue[0]
	a.uploadQueue = a.uploadQueue[1:]
	a.uploadJob = &uploadJob{item: u, all: u.Size}
	a.uploadGen++
	gen := a.uploadGen
	ctx, cancel := context.WithCancel(context.Background())
	a.uploadCancel = cancel
	meta := netease.CloudUploadMeta{Title: u.Title, Artist: u.Artist, Album: u.Album}
	go func() {
		_, err := uploadToCloud(ctx, u.Path, meta, func(sent, total int64) {
			a.post(func() {
				if gen != a.uploadGen || a.uploadJob == nil {
					return
				}
				a.uploadJob.sent, a.uploadJob.all = sent, total
			})
		})
		a.post(func() {
			if gen != a.uploadGen {
				return
			}
			a.finishUpload(err)
		})
	}()
}

// finishUpload says how the song went, and starts the next one queued.
func (a *app) finishUpload(err error) {
	var u cloudUpload
	if a.uploadJob != nil {
		u = a.uploadJob.item
	}
	a.uploadJob, a.uploadCancel = nil, nil
	switch {
	case err == nil:
		a.toast = "已上传到云盘：" + u.name()
		a.uploadedSome = true
	case errors.Is(err, context.Canceled):
		a.toast = "已取消上传"
	case errors.Is(err, os.ErrNotExist):
		a.toast = "上传失败：找不到文件"
		a.failUpload(u, "找不到文件")
	case errors.Is(err, netease.ErrCloudFull):
		a.toast = "云盘空间不足"
		a.failUpload(u, "云盘空间不足")
	default:
		a.toast = "上传失败：" + u.name()
		a.failUpload(u, uploadReason(err))
	}
	a.pumpUpload()
	a.saveUploadQueue()
	a.redraw()
}

// uploadReason is why an upload failed, short enough for its row.
func uploadReason(err error) string {
	msg := err.Error()
	msg = strings.TrimPrefix(msg, "netease: ")
	if r := []rune(msg); len(r) > 60 {
		msg = string(r[:60]) + "…"
	}
	return msg
}

// failUpload keeps a song that failed to go up, for trying again.
func (a *app) failUpload(u cloudUpload, reason string) {
	u.Reason = reason
	a.dropFailedUploads(u.Path)
	a.uploadFailed = append(a.uploadFailed, u)
}

// dropFailedUploads forgets that path failed.
func (a *app) dropFailedUploads(path string) {
	key := uploadKey(path)
	a.uploadFailed = slices.DeleteFunc(a.uploadFailed, func(f cloudUpload) bool { return uploadKey(f.Path) == key })
}

// retryUploads queues again the failed songs of paths, or all of them.
func (a *app) retryUploads(paths ...string) {
	want := map[string]bool{}
	for _, p := range paths {
		want[uploadKey(p)] = true
	}
	var again, kept []cloudUpload
	for _, f := range a.uploadFailed {
		if len(want) == 0 || want[uploadKey(f.Path)] {
			f.Reason = ""
			again = append(again, f)
		} else {
			kept = append(kept, f)
		}
	}
	a.uploadFailed = kept
	for _, u := range again {
		if !a.uploadPending(u.Path) {
			a.uploadQueue = append(a.uploadQueue, u)
		}
	}
	if len(again) > 0 {
		a.toast = "重新上传 " + strconv.Itoa(len(again)) + " 首"
	}
	a.pumpUpload()
	a.saveUploadQueue()
	a.redraw()
}

// cancelUpload stops the song going up and goes on with the queue.
func (a *app) cancelUpload() {
	if a.uploadJob == nil {
		return
	}
	if a.uploadCancel != nil {
		a.uploadCancel()
	}
	a.uploadGen++
	a.uploadJob, a.uploadCancel = nil, nil
	a.toast = "已取消上传"
	a.pumpUpload()
	a.saveUploadQueue()
	a.redraw()
}

// queuedUploads is the upload queue as it is kept across launches.
type queuedUploads struct {
	Queue  []cloudUpload `json:"queue"`
	Failed []cloudUpload `json:"failed,omitempty"`
}

// uploadQueuePath is where the queue is kept, beside the settings: none for
// settings that were never loaded, as in tests.
func (a *app) uploadQueuePath() string {
	if a.settings.path == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(a.settings.path), "upload-queue.json")
}

// saveUploadQueue keeps the queue, so that what waits, or went up as the app
// quit, goes up once it opens again.
func (a *app) saveUploadQueue() {
	path := a.uploadQueuePath()
	if path == "" {
		return
	}
	q := queuedUploads{Failed: a.uploadFailed}
	if a.uploadJob != nil {
		q.Queue = append(q.Queue, a.uploadJob.item)
	}
	q.Queue = append(q.Queue, a.uploadQueue...)
	if len(q.Queue) == 0 && len(q.Failed) == 0 {
		os.Remove(path)
		return
	}
	b, err := json.Marshal(q)
	if err != nil {
		return
	}
	writeFileAtomic(path, b, 0o600)
}

// restoreUploadQueue picks the queue up where the app left it, once the
// listener's session is back: the drive is theirs.
func (a *app) restoreUploadQueue() {
	if a.uploadsRestored {
		return
	}
	a.uploadsRestored = true
	path := a.uploadQueuePath()
	if path == "" {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var q queuedUploads
	if json.Unmarshal(b, &q) != nil {
		return
	}
	a.uploadFailed = q.Failed
	for _, u := range q.Queue {
		if !a.uploadPending(u.Path) {
			a.uploadQueue = append(a.uploadQueue, u)
		}
	}
	if len(q.Queue) > 0 {
		a.toast = "继续上传上次没完成的歌曲"
	}
	a.pumpUpload()
	a.redraw()
}
