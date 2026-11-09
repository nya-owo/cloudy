package app

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

func queueTestApp() *app {
	a := newTestApp()
	a.settings = defaultSettings()
	a.downloadQ.running = &netease.Track{ID: 1, Name: "正在下载", Artists: []netease.Artist{{Name: "歌手甲"}}, Duration: 200000}
	a.downloadQ.done, a.downloadQ.size = 50, 100
	a.downloadQ.waiting = []netease.Track{
		{ID: 2, Name: "等待一", Artists: []netease.Artist{{Name: "歌手乙"}}, Duration: 210000},
		{ID: 3, Name: "等待二", Artists: []netease.Artist{{Name: "歌手丙"}}, Duration: 220000},
	}
	a.openDownloadQueue()
	return a
}

// The queue window lists what downloads and what waits, and cancels the
// chosen songs in one go.
func TestDownloadQueueWindow(t *testing.T) {
	a := queueTestApp()
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if !tt.HasText("下载队列") || !tt.HasText("正在下载") || !tt.HasText("等待中") {
		t.Fatalf("queue texts %q", tt.Texts())
	}
	if err := tt.Click("全选"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if chosen := a.queueView.chosen(a.downloadQueueEntries()); chosen != 3 {
		t.Fatalf("chosen %d", chosen)
	}
	if err := tt.Click("取消所选下载"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.downloadQ.running != nil || len(a.downloadQ.waiting) != 0 {
		t.Fatalf("job %+v, queue %+v", a.downloadQ.running, a.downloadQ.waiting)
	}
	if !tt.HasText("下载队列是空的") {
		t.Fatalf("empty queue texts %q", tt.Texts())
	}
	if err := tt.Click("关闭"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.queueView.open {
		t.Fatal("queue window stayed open")
	}
}

// With NM_FRAMES set, saves the queue window in both appearances.
func TestDownloadQueueFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip("set NM_FRAMES to a directory to save frames")
	}
	a := queueTestApp()
	for _, dark := range []bool{false, true} {
		tt := ui.NewTester(a.view, 1180, 780)
		tt.SetDark(dark)
		tt.Advance(500 * time.Millisecond)
		tt.Frame()
		name := "queue-light.png"
		if dark {
			name = "queue-dark.png"
		}
		f, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tt.Image()); err != nil {
			f.Close()
			t.Fatal(err)
		}
		f.Close()
	}
}
