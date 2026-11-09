package app

import (
	"context"
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
	Reason string `json:"reason,omitempty"` // why it failed, in the queue's file
}

// name is what the upload is listed as.
func (u cloudUpload) name() string {
	if u.Title != "" {
		return u.Title
	}
	base := filepath.Base(u.Path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// fileKey names an upload by its file.
type fileKey struct{}

func (fileKey) key(u cloudUpload) string { return uploadKey(u.Path) }

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
	return a.up.queue.pending(uploadKey(path))
}

// queueUploads adds songs to what goes up, those queued already once.
func (a *app) queueUploads(items []cloudUpload, skipped int) {
	added := 0
	for _, u := range items {
		u.Reason = ""
		if a.up.queue.add(u) {
			added++
		}
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
	q := &a.up.queue
	u, ok := q.next()
	if !ok {
		if q.running == nil && a.up.sentSome {
			// The drive changed: read it again once all went up.
			a.up.sentSome = false
			a.loadCloud(true)
		}
		return
	}
	meta := netease.CloudUploadMeta{Title: u.Title, Artist: u.Artist, Album: u.Album}
	q.run(u, a.post, func(ctx context.Context, progress func(done, size int64)) func() {
		_, err := uploadToCloud(ctx, u.Path, meta, progress)
		return func() { a.finishUpload(err) }
	})
	q.size = u.Size
}

// finishUpload says how the song went, and starts the next one queued.
func (a *app) finishUpload(err error) {
	q := &a.up.queue
	u := q.finish()
	switch {
	case err == nil:
		a.toast = "已上传到云盘：" + u.name()
		a.up.sentSome = true
	case errors.Is(err, context.Canceled):
		a.toast = "已取消上传"
	case errors.Is(err, os.ErrNotExist):
		a.toast = "上传失败：找不到文件"
		q.fail(u, "找不到文件")
	case errors.Is(err, netease.ErrCloudFull):
		a.toast = "云盘空间不足"
		q.fail(u, "云盘空间不足")
	default:
		a.toast = "上传失败：" + u.name()
		q.fail(u, uploadReason(err))
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

// retryUploads queues again the failed songs of paths, or all of them.
func (a *app) retryUploads(paths ...string) {
	keys := make([]string, len(paths))
	for i, p := range paths {
		keys[i] = uploadKey(p)
	}
	a.retryUploadKeys(keys...)
}

func (a *app) retryUploadKeys(keys ...string) {
	if n := a.up.queue.retry(keys...); n > 0 {
		a.toast = "重新上传 " + strconv.Itoa(n) + " 首"
	}
	a.pumpUpload()
	a.saveUploadQueue()
	a.redraw()
}

// cancelUpload stops the song going up and goes on with the queue.
func (a *app) cancelUpload() {
	if !a.up.queue.stop() {
		return
	}
	a.toast = "已取消上传"
	a.pumpUpload()
	a.saveUploadQueue()
	a.redraw()
}

// uploadQueuePath is where the queue of the account signed in is kept,
// beside the settings: none while nobody is signed in, or for settings that
// were never loaded, as in tests. Each account has its own, as the songs
// go up to its drive.
func (a *app) uploadQueuePath() string {
	if a.settings.path == "" || a.up.user == 0 {
		return ""
	}
	return filepath.Join(filepath.Dir(a.settings.path), "upload-queue-"+strconv.FormatInt(a.up.user, 10)+".json")
}

// oldUploadQueuePath is where versions up to 1.2.6 kept the queue, for
// whoever was signed in.
func (a *app) oldUploadQueuePath() string {
	return filepath.Join(filepath.Dir(a.settings.path), "upload-queue.json")
}

// saveUploadQueue keeps the queue, so that what waits, or went up as the app
// quit, goes up once it opens again. The songs that failed are kept with
// why, in their Reason.
func (a *app) saveUploadQueue() {
	var failed []cloudUpload
	for _, f := range a.up.queue.failed {
		u := f.item
		u.Reason = f.reason
		failed = append(failed, u)
	}
	saveQueueFile(a.uploadQueuePath(), a.up.queue.kept(), failed)
}

// restoreUploadQueue picks the queue of the account signed in up where the
// app left it, once the listener's session is back: the drive is theirs. A
// queue kept by an earlier version, for no account in particular, goes to
// the first account that signs in.
func (a *app) restoreUploadQueue() {
	q := &a.up.queue
	path := a.uploadQueuePath()
	if q.restored || path == "" {
		return
	}
	q.restored = true
	kept, ok := readQueueFile[cloudUpload, cloudUpload](path)
	if !ok {
		old := a.oldUploadQueuePath()
		if kept, ok = readQueueFile[cloudUpload, cloudUpload](old); !ok {
			return
		}
		os.Remove(old)
	}
	var failed []failedJob[cloudUpload]
	for _, u := range kept.Failed {
		reason := u.Reason
		u.Reason = ""
		failed = append(failed, failedJob[cloudUpload]{u, reason})
	}
	q.restore(kept.Queue, failed)
	if len(kept.Queue) > 0 {
		a.toast = "继续上传上次没完成的歌曲"
	}
	a.pumpUpload()
	a.saveUploadQueue()
	a.redraw()
}

// uploadsFor gives the queue to the account now signed in, prof, or to
// nobody. The songs of the account that was signed in stop going up, as
// they would land on another's drive: they are kept for it, and go up
// again once it signs back in.
func (a *app) uploadsFor(prof *netease.Profile) {
	var id int64
	if prof != nil {
		id = prof.UserID
	}
	if id != a.up.user {
		if a.up.user != 0 {
			a.leaveUploads()
		}
		a.up.user = id
	}
	a.restoreUploadQueue()
}

// leaveUploads stops the upload under way and puts the queue away for the
// account it was for.
func (a *app) leaveUploads() {
	busy := a.up.queue.count() > 0
	a.saveUploadQueue()
	a.up.queue.reset()
	a.up.sentSome = false
	if busy {
		a.toast = "账号已切换，已停止上传"
	}
}
