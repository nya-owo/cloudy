package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// uploadTestApp is a signed-in app whose uploads wait until cancelled.
func uploadTestApp(t *testing.T) *app {
	t.Helper()
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.path = filepath.Join(t.TempDir(), "settings.json")
	a.profile = &netease.Profile{UserID: 1, Nickname: "小明"}
	a.up.user = 1
	old := uploadToCloud
	uploadToCloud = func(ctx context.Context, path string, meta netease.CloudUploadMeta, progress func(sent, total int64)) (int64, error) {
		<-ctx.Done()
		return 0, ctx.Err()
	}
	oldFetch := fetchCloud
	fetchCloud = func() (*netease.CloudDrive, error) { return &netease.CloudDrive{}, nil }
	t.Cleanup(func() { uploadToCloud, fetchCloud = old, oldFetch })
	return a
}

// Songs go up one at a time, those after waiting in order, and a song asked
// for twice is listed once.
func TestUploadQueue(t *testing.T) {
	a := uploadTestApp(t)
	songs := []cloudUpload{
		{Path: `C:\Music\a.mp3`, Title: "甲", Artist: "歌手", Size: 1 << 20},
		{Path: `C:\Music\b.flac`, Title: "乙", Size: 2 << 20},
	}
	a.queueUploads(songs, 0)
	if a.up.queue.running == nil || a.up.queue.running.Title != "甲" || len(a.up.queue.waiting) != 1 {
		t.Fatalf("job %+v queue %+v", a.up.queue.running, a.up.queue.waiting)
	}
	a.queueUploads([]cloudUpload{{Path: `c:\music\B.FLAC`}}, 0)
	if len(a.up.queue.waiting) != 1 || a.toast != "这些歌曲已经在上传列表里" {
		t.Fatalf("queued twice: %+v, toast %q", a.up.queue.waiting, a.toast)
	}

	// The one going up fails: it is kept to try again, and the next starts.
	a.finishUpload(errors.New("netease: 上传文件失败"))
	if len(a.up.queue.failed) != 1 || a.up.queue.failed[0].reason != "上传文件失败" {
		t.Fatalf("failed %+v", a.up.queue.failed)
	}
	if a.up.queue.running == nil || a.up.queue.running.Title != "乙" {
		t.Fatalf("next not started: %+v", a.up.queue.running)
	}

	// Kept across launches: what went up as the app quit, and what failed.
	b := uploadTestApp(t)
	b.settings.path = a.settings.path
	b.restoreUploadQueue()
	if b.up.queue.running == nil || b.up.queue.running.Title != "乙" || len(b.up.queue.failed) != 1 {
		t.Fatalf("restored job %+v failed %+v", b.up.queue.running, b.up.queue.failed)
	}
	b.cancelUpload()

	// Tried again, the failed one waits behind the one going up.
	a.retryUploads()
	if len(a.up.queue.failed) != 0 || len(a.up.queue.waiting) != 1 || a.up.queue.waiting[0].Title != "甲" {
		t.Fatalf("retry: failed %+v queue %+v", a.up.queue.failed, a.up.queue.waiting)
	}

	// Done, the drive is read again.
	a.finishUpload(nil)
	if a.toast != "已上传到云盘：乙" || a.up.queue.running == nil {
		t.Fatalf("toast %q job %+v", a.toast, a.up.queue.running)
	}
	a.cancelUpload()
	if a.up.queue.running != nil || !a.cloud.loading {
		t.Fatalf("the drive was not read again: job %+v cloud %+v", a.up.queue.running, a.cloud)
	}
}

// Files the drive does not take are left out, and said so.
func TestUploadSkipsOtherFiles(t *testing.T) {
	if uploadable("cover.jpg") || !uploadable(`D:\歌\x.FLAC`) {
		t.Fatal("uploadable")
	}
	a := uploadTestApp(t)
	a.queueUploads(nil, 2)
	if a.toast != "所选文件不是云盘支持的音乐" {
		t.Fatalf("toast %q", a.toast)
	}
}

// The cloud page, empty or not, offers the upload and its list, which lists
// what goes up and cancels what is chosen.
func TestUploadList(t *testing.T) {
	a := uploadTestApp(t)
	a.cloud.drive = &netease.CloudDrive{}
	a.route = route{page: pageCloud}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	for _, s := range []string{"我的音乐云盘", "上传音乐", "上传列表"} {
		if !tt.HasText(s) {
			t.Fatalf("no %q: %v", s, tt.Texts())
		}
	}
	a.queueUploads([]cloudUpload{
		{Path: `C:\Music\a.mp3`, Title: "甲", Size: 1 << 20},
		{Path: `C:\Music\b.mp3`, Title: "乙", Size: 1 << 20},
	}, 0)
	a.up.queue.done, a.up.queue.size = 1<<19, 1<<20
	tt.Frame()
	if err := tt.Click("上传列表 2 · 50%"); err != nil {
		t.Fatalf("%v: %v", err, tt.Texts())
	}
	tt.Frame()
	for _, s := range []string{"正在上传 1 首 · 等待 1 首", "甲", "乙", "等待中 · 第 1 位"} {
		if !tt.HasText(s) {
			t.Fatalf("no %q: %v", s, tt.Texts())
		}
	}
	if err := tt.Click("全选"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("取消所选"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.up.queue.running != nil || len(a.up.queue.waiting) != 0 || !tt.HasText("上传列表是空的") {
		t.Fatalf("job %+v queue %+v: %v", a.up.queue.running, a.up.queue.waiting, tt.Texts())
	}
}

// Signing out, or in as another, stops the uploads: they would land on the
// other account's drive. They are kept for the account they were for, and
// go up again once it signs back in, not for another, nor for nobody.
func TestUploadsStayWithTheirAccount(t *testing.T) {
	a := uploadTestApp(t)
	a.queueUploads([]cloudUpload{
		{Path: `C:\Music\a.mp3`, Title: "甲", Size: 1 << 20},
		{Path: `C:\Music\b.flac`, Title: "乙", Size: 2 << 20},
	}, 0)
	a.uploadsFor(nil)
	if a.up.queue.running != nil || len(a.up.queue.waiting) != 0 || a.up.user != 0 {
		t.Fatalf("still uploading after signing out: job %+v queue %+v", a.up.queue.running, a.up.queue.waiting)
	}
	if a.toast != "账号已切换，已停止上传" {
		t.Fatalf("toast %q", a.toast)
	}

	a.uploadsFor(&netease.Profile{UserID: 2})
	if a.up.queue.running != nil || len(a.up.queue.waiting) != 0 {
		t.Fatalf("another account took the queue: job %+v queue %+v", a.up.queue.running, a.up.queue.waiting)
	}

	a.uploadsFor(&netease.Profile{UserID: 1})
	if a.up.queue.running == nil || a.up.queue.running.Title != "甲" || len(a.up.queue.waiting) != 1 {
		t.Fatalf("not restored for its account: job %+v queue %+v", a.up.queue.running, a.up.queue.waiting)
	}
}

// A queue kept by an earlier version, in one file for any account, is taken
// up by the first account that signs in, and not while nobody is signed in.
func TestUploadsOldQueueFile(t *testing.T) {
	a := uploadTestApp(t)
	a.up.user = 0
	old := a.oldUploadQueuePath()
	if err := os.WriteFile(old, []byte(`{"queue":[{"path":"C:\\Music\\a.mp3","title":"甲","size":1048576}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	a.uploadsFor(nil)
	if a.up.queue.running != nil {
		t.Fatal("restored with nobody signed in")
	}
	a.uploadsFor(&netease.Profile{UserID: 5})
	if a.up.queue.running == nil || a.up.queue.running.Title != "甲" {
		t.Fatalf("not restored: %+v", a.up.queue.running)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("the old file is still there, for the next account to take")
	}
	if _, err := os.Stat(a.uploadQueuePath()); err != nil {
		t.Fatal("not kept for the account:", err)
	}
}
