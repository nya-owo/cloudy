package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/netease"
)

// What the download and upload queues hold, read the same way before and
// after the queues share their code.

func dlRunning(a *app) (netease.Track, bool) { return a.downloadQ.current() }

func dlWaiting(a *app) []int64 {
	var ids []int64
	for _, tr := range a.downloadQ.waiting {
		ids = append(ids, tr.ID)
	}
	return ids
}

func dlFailed(a *app) map[int64]string {
	out := map[int64]string{}
	for _, f := range a.downloadQ.failed {
		out[f.item.ID] = f.reason
	}
	return out
}

func upRunning(a *app) (cloudUpload, bool) { return a.uploadQ.current() }

func upWaiting(a *app) []string {
	var out []string
	for _, u := range a.uploadQ.waiting {
		out = append(out, u.Title)
	}
	return out
}

func upFailed(a *app) map[string]string {
	out := map[string]string{}
	for _, f := range a.uploadQ.failed {
		out[f.item.Title] = f.reason
	}
	return out
}

// queueDownloadApp is an app whose downloads wait until cancelled, with its
// queue kept in a file.
func queueDownloadApp(t *testing.T) *app {
	t.Helper()
	a := testDownloadApp(t)
	a.settings.path = filepath.Join(t.TempDir(), "settings.json")
	old := fetchDownload
	fetchDownload = func(ctx context.Context, tr netease.Track, level, dir string, progress download.Progress) (download.Item, error) {
		<-ctx.Done()
		return download.Item{}, ctx.Err()
	}
	t.Cleanup(func() { fetchDownload = old })
	return a
}

func qsong(id int64, name string) netease.Track {
	return netease.Track{ID: id, Name: name, Duration: 200000}
}

func sameIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Songs download one at a time in order; one that fails is kept with why,
// the next starts, and trying again queues it at the end.
func TestDownloadQueueFailAndRetry(t *testing.T) {
	a := queueDownloadApp(t)
	for _, tr := range []netease.Track{qsong(1, "一"), qsong(2, "二"), qsong(3, "三")} {
		a.startDownload(tr)
	}
	if tr, ok := dlRunning(a); !ok || tr.ID != 1 || !sameIDs(dlWaiting(a), []int64{2, 3}) {
		t.Fatalf("running %v %v, waiting %v", tr.ID, ok, dlWaiting(a))
	}
	a.finishDownload(download.Item{}, errors.New("断网了"))
	if dlFailed(a)[1] != "断网了" || a.toast != "下载失败：断网了" {
		t.Fatalf("failed %v, toast %q", dlFailed(a), a.toast)
	}
	if tr, _ := dlRunning(a); tr.ID != 2 || !sameIDs(dlWaiting(a), []int64{3}) {
		t.Fatalf("next not started: running %d waiting %v", tr.ID, dlWaiting(a))
	}
	a.finishDownload(download.Item{}, netease.ErrUnavailable)
	if dlFailed(a)[2] != "没有下载权限" {
		t.Fatalf("failed %v", dlFailed(a))
	}

	a.retryDownloads(1)
	if len(dlFailed(a)) != 1 || !sameIDs(dlWaiting(a), []int64{1}) || a.toast != "重新下载 1 首" {
		t.Fatalf("retry one: failed %v waiting %v toast %q", dlFailed(a), dlWaiting(a), a.toast)
	}
	a.retryDownloads()
	if len(dlFailed(a)) != 0 || !sameIDs(dlWaiting(a), []int64{1, 2}) {
		t.Fatalf("retry all: failed %v waiting %v", dlFailed(a), dlWaiting(a))
	}

	// Asked for again, a song that failed is queued, not listed twice.
	a.finishDownload(download.Item{}, errors.New("x")) // 3 fails, 1 starts
	a.startDownload(qsong(3, "三"))
	if _, failed := dlFailed(a)[3]; failed || !sameIDs(dlWaiting(a), []int64{2, 3}) {
		t.Fatalf("asked again: failed %v waiting %v", dlFailed(a), dlWaiting(a))
	}
	a.clearDownloadQueue()
}

// One song taken out: the one downloading is cancelled and the next
// starts, one waiting leaves the queue, one that failed is forgotten.
func TestDownloadQueueRemove(t *testing.T) {
	a := queueDownloadApp(t)
	for _, tr := range []netease.Track{qsong(1, "一"), qsong(2, "二"), qsong(3, "三"), qsong(4, "四")} {
		a.startDownload(tr)
	}
	a.finishDownload(download.Item{}, errors.New("x")) // 1 fails, 2 starts
	a.removeDownload(3)
	if !sameIDs(dlWaiting(a), []int64{4}) || a.toast != "已从下载队列移除：三" {
		t.Fatalf("waiting %v toast %q", dlWaiting(a), a.toast)
	}
	a.removeDownload(2)
	if tr, _ := dlRunning(a); tr.ID != 4 || a.toast != "已取消下载" {
		t.Fatalf("running %d toast %q", tr.ID, a.toast)
	}
	a.removeDownload(1)
	if len(dlFailed(a)) != 0 {
		t.Fatalf("failed %v", dlFailed(a))
	}
	a.clearDownloadQueue()
	if _, ok := dlRunning(a); ok || len(dlWaiting(a)) != 0 || a.toast != "已清空下载队列" {
		t.Fatalf("not cleared: waiting %v toast %q", dlWaiting(a), a.toast)
	}
}

// The queue kept across launches is a file the earlier versions wrote too:
// the song downloading first, then those waiting, and those that failed
// with why. The next launch reads it and starts again.
func TestDownloadQueueFile(t *testing.T) {
	a := queueDownloadApp(t)
	a.startDownload(qsong(1, "一"))
	a.startDownload(qsong(2, "二"))
	a.startDownload(qsong(3, "三"))
	a.finishDownload(download.Item{}, errors.New("坏了")) // 1 fails, 2 starts

	b, err := os.ReadFile(a.downloadQueuePath())
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Queue  []struct{ ID int64 } `json:"queue"`
		Failed []struct {
			Track  struct{ ID int64 } `json:"track"`
			Reason string             `json:"reason"`
		} `json:"failed"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Queue) != 2 || file.Queue[0].ID != 2 || file.Queue[1].ID != 3 ||
		len(file.Failed) != 1 || file.Failed[0].Track.ID != 1 || file.Failed[0].Reason != "坏了" {
		t.Fatalf("kept %s", b)
	}
	a.clearDownloadQueue()

	old := `{"queue":[{"id":7,"name":"七"},{"id":8,"name":"八"}],"failed":[{"track":{"id":9,"name":"九"},"reason":"没有下载权限"}]}`
	c := queueDownloadApp(t)
	if err := os.WriteFile(c.downloadQueuePath(), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	c.restoreDownloadQueue()
	if tr, ok := dlRunning(c); !ok || tr.ID != 7 || !sameIDs(dlWaiting(c), []int64{8}) || dlFailed(c)[9] != "没有下载权限" {
		t.Fatalf("restored running %d waiting %v failed %v", tr.ID, dlWaiting(c), dlFailed(c))
	}
	if c.toast != "继续下载上次没完成的歌曲" {
		t.Fatalf("toast %q", c.toast)
	}
	c.restoreDownloadQueue()
	if !sameIDs(dlWaiting(c), []int64{8}) {
		t.Fatalf("restored twice: %v", dlWaiting(c))
	}
	c.clearDownloadQueue()
}

// The upload queue's file keeps the failed songs with their reason inline,
// as the earlier versions wrote it, and reads back so.
func TestUploadQueueFile(t *testing.T) {
	a := uploadTestApp(t)
	a.queueUploads([]cloudUpload{
		{Path: `C:\Music\a.mp3`, Title: "甲", Size: 1},
		{Path: `C:\Music\b.mp3`, Title: "乙", Size: 2},
	}, 0)
	a.finishUpload(errors.New("netease: 太大了"))
	b, err := os.ReadFile(a.uploadQueuePath())
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Queue  []cloudUpload `json:"queue"`
		Failed []cloudUpload `json:"failed"`
	}
	if err := json.Unmarshal(b, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Queue) != 1 || file.Queue[0].Title != "乙" || len(file.Failed) != 1 || file.Failed[0].Reason != "太大了" {
		t.Fatalf("kept %s", b)
	}
	a.clearUploadQueue()

	old := `{"queue":[{"path":"C:\\Music\\c.mp3","title":"丙","size":3}],"failed":[{"path":"C:\\Music\\d.mp3","title":"丁","reason":"云盘空间不足"}]}`
	c := uploadTestApp(t)
	if err := os.WriteFile(c.uploadQueuePath(), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	c.restoreUploadQueue()
	if u, ok := upRunning(c); !ok || u.Title != "丙" || upFailed(c)["丁"] != "云盘空间不足" {
		t.Fatalf("restored running %+v failed %v", u, upFailed(c))
	}
	c.clearUploadQueue()
}

// One upload taken out, as one download: cancelled while it goes up, out
// of the queue while it waits, forgotten once it failed; and the chosen
// ones go together.
func TestUploadQueueRemove(t *testing.T) {
	a := uploadTestApp(t)
	a.queueUploads([]cloudUpload{
		{Path: `C:\Music\a.mp3`, Title: "甲"}, {Path: `C:\Music\b.mp3`, Title: "乙"},
		{Path: `C:\Music\c.mp3`, Title: "丙"}, {Path: `C:\Music\d.mp3`, Title: "丁"},
	}, 0)
	a.finishUpload(errors.New("x")) // 甲 fails, 乙 starts
	a.removeUpload(`C:\Music\C.MP3`)
	if w := upWaiting(a); len(w) != 1 || w[0] != "丁" || a.toast != "已从上传列表移除：丙" {
		t.Fatalf("waiting %v toast %q", w, a.toast)
	}
	a.removeUpload(`C:\Music\b.mp3`)
	if u, _ := upRunning(a); u.Title != "丁" || a.toast != "已取消上传" {
		t.Fatalf("running %+v toast %q", u, a.toast)
	}
	a.openUploadQueue()
	a.uploadView.selected[uploadKey(`C:\Music\a.mp3`)] = true
	a.uploadView.selected[uploadKey(`C:\Music\d.mp3`)] = true
	a.cancelSelectedUploads()
	if _, ok := upRunning(a); ok || len(upFailed(a)) != 0 || a.toast != "已取消 2 项上传" {
		t.Fatalf("failed %v toast %q", upFailed(a), a.toast)
	}
}
