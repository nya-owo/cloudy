package app

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/netease"
)

// A playlist's three dots hold its batch actions and share links.
func TestPlaylistDetailsMenu(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.profile = &netease.Profile{UserID: 1, Nickname: "nya"}
	pl := &netease.Playlist{
		ID: 42, Name: "热歌榜", TrackCount: 2,
		Tracks: []netease.Track{{ID: 1, Name: "一"}, {ID: 2, Name: "二"}},
	}
	pl.Creator.UserID, pl.Creator.Nickname = 99, "网易云音乐"
	a.playlists[42] = pl
	a.route = route{page: pagePlaylist, id: 42}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("歌单详情"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("批量操作") || !tt.HasText("分享") {
		t.Fatalf("details menu: %q", tt.Texts())
	}
	if err := chooseItem(tt, "批量操作"); err != nil {
		t.Fatal(err)
	}
	if !a.batch.on || a.batch.playlist != 42 {
		t.Fatalf("batch did not start: %+v", a.batch)
	}
	a.cancelBatch()
	tt.Frame()
	if err := tt.Click("歌单详情"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := chooseItem(tt, "分享"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("复制歌单链接") || !tt.HasText("在浏览器中打开") {
		t.Fatalf("share menu: %q", tt.Texts())
	}
}

// Batch selection happens in the playlist's own rows, starts with none
// chosen, and can choose all or none.
func TestPlaylistBatchInPage(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.profile = &netease.Profile{UserID: 1}
	pl := &netease.Playlist{ID: 42, Name: "热歌榜", TrackCount: 2, Tracks: []netease.Track{{ID: 1, Name: "一"}, {ID: 2, Name: "二"}}}
	pl.Creator.UserID, pl.Creator.Nickname = 99, "网易云音乐"
	a.playlists[42] = pl
	a.route = route{page: pagePlaylist, id: 42}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("歌单详情"); err != nil {
		t.Fatal(err)
	}
	if err := chooseItem(tt, "批量操作"); err != nil {
		t.Fatal(err)
	}
	if !a.batch.on || a.batchCount(pl) != 0 {
		t.Fatalf("batch on %v, chosen %d", a.batch.on, a.batchCount(pl))
	}
	if !tt.HasText("全选") || !tt.HasText("下载") {
		t.Fatalf("batch bar texts %q", tt.Texts())
	}
	if err := tt.Click("一"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.batch.selected[1] || a.batchCount(pl) != 1 {
		t.Fatalf("row selection: %+v", a.batch.selected)
	}
	if err := tt.Click("全选"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.batchAll(pl) || a.batchCount(pl) != 2 {
		t.Fatalf("select all: chosen %d", a.batchCount(pl))
	}
	if err := tt.Click("取消全选"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.batchCount(pl) != 0 {
		t.Fatalf("clear all: chosen %d", a.batchCount(pl))
	}
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.batch.on {
		t.Fatal("batch stayed on")
	}
}

// Choosing songs in the playlist and then Download downloads just those.
func TestPlaylistBatchInlineDownload(t *testing.T) {
	a := testDownloadApp(t)
	a.profile = &netease.Profile{UserID: 1}
	pl := &netease.Playlist{ID: 42, Name: "热歌榜", TrackCount: 2, Tracks: []netease.Track{{ID: 1, Name: "一"}, {ID: 2, Name: "二"}}}
	pl.Creator.UserID = 99
	a.playlists[42] = pl
	a.route = route{page: pagePlaylist, id: 42}
	old := fetchDownload
	fetchDownload = func(ctx context.Context, tr netease.Track, level, dir string, progress download.Progress) (download.Item, error) {
		<-ctx.Done()
		return download.Item{}, ctx.Err()
	}
	defer func() { fetchDownload = old }()
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("歌单详情"); err != nil {
		t.Fatal(err)
	}
	if err := chooseItem(tt, "批量操作"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("一"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("下载"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.batch.on {
		t.Fatal("batch stayed on after download")
	}
	if a.downloadQ.running == nil || a.downloadQ.running.ID != 1 {
		t.Fatalf("job %+v", a.downloadQ.running)
	}
	a.cancelDownload()
}

// A batch like sends one request per unliked song and fills their hearts.
func TestPlaylistBatchLikes(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.profile = &netease.Profile{UserID: 1}
	a.liked = map[int64]bool{1: true}
	oldPause, oldLike := batchLikePause, likeSong
	batchLikePause = 0
	calls := make(chan int64, 2)
	likeSong = func(id int64, like bool) error { calls <- id; return nil }
	defer func() {
		batchLikePause, likeSong = oldPause, oldLike
	}()
	a.likeAll([]netease.Track{{ID: 1}, {ID: 2}, {ID: 2}, {ID: 3}, {ID: 4, LocalPath: `C:\4.mp3`}})
	got := make([]int64, 0, 2)
	for len(got) < 2 {
		select {
		case id := <-calls:
			got = append(got, id)
		case <-time.After(time.Second):
			t.Fatalf("liked %v, want 2 and 3", got)
		}
	}
	if got[0] != 2 || got[1] != 3 {
		t.Fatalf("liked %v, want 2 and 3", got)
	}
	if !a.liked[2] || !a.liked[3] || a.liked[4] {
		t.Fatalf("liked map %v", a.liked)
	}
}

// The liked songs page has the details button, and its batch bar can unlike.
func TestLikedPlaylistBatch(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.profile = &netease.Profile{UserID: 1}
	pl := &netease.Playlist{ID: 99, Name: "我喜欢的音乐", SpecialType: 5, TrackCount: 2,
		Tracks: []netease.Track{{ID: 1, Name: "一"}, {ID: 2, Name: "二"}}}
	pl.Creator.UserID = 1
	a.playlists[99] = pl
	a.liked = map[int64]bool{1: true, 2: true}
	a.route = route{page: pagePlaylist, id: 99}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("歌单详情"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := chooseItem(tt, "批量操作"); err != nil {
		t.Fatal(err)
	}
	if !a.batch.on {
		t.Fatal("liked songs did not enter batch")
	}
	if !tt.HasText("取消喜欢") {
		t.Fatalf("batch bar %q", tt.Texts())
	}
	a.cancelBatch()
}

// A batch unlike sends one request per liked song and clears their hearts.
func TestPlaylistBatchUnlike(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.profile = &netease.Profile{UserID: 1}
	a.liked = map[int64]bool{1: true, 2: true}
	oldPause, oldLike := batchLikePause, likeSong
	batchLikePause = 0
	calls := make(chan bool, 2)
	likeSong = func(id int64, like bool) error { calls <- like; return nil }
	defer func() { batchLikePause, likeSong = oldPause, oldLike }()
	a.unlikeAll([]netease.Track{{ID: 1}, {ID: 2}, {ID: 3}})
	for i := 0; i < 2; i++ {
		select {
		case like := <-calls:
			if like {
				t.Fatal("unlike sent a like")
			}
		case <-time.After(time.Second):
			t.Fatal("unlike did not run")
		}
	}
	if a.liked[1] || a.liked[2] || a.liked[3] {
		t.Fatalf("liked map %v", a.liked)
	}
}

// A batch download queues the playable songs it does not have yet.
func TestPlaylistBatchDownloads(t *testing.T) {
	a := testDownloadApp(t)
	old := fetchDownload
	fetchDownload = func(ctx context.Context, tr netease.Track, level, dir string, progress download.Progress) (download.Item, error) {
		<-ctx.Done()
		return download.Item{}, ctx.Err()
	}
	defer func() { fetchDownload = old }()
	a.downloadAll([]netease.Track{
		{ID: 1, Name: "一"},
		{ID: 2, Name: "二", Privilege: &netease.Privilege{St: 0, Pl: 0}},
		{ID: 3, Name: "三"},
	})
	if a.downloadQ.running == nil || a.downloadQ.running.ID != 1 {
		t.Fatalf("job %+v", a.downloadQ.running)
	}
	if len(a.downloadQ.waiting) != 1 || a.downloadQ.waiting[0].ID != 3 {
		t.Fatalf("queue %+v", a.downloadQ.waiting)
	}
	a.cancelDownload()
}

// With NM_FRAMES set, saves a playlist while its songs are being chosen.
func TestPlaylistBatchFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip("set NM_FRAMES to a directory to save frames")
	}
	a := newTestApp()
	a.settings = defaultSettings()
	a.profile = &netease.Profile{UserID: 1}
	pl := &netease.Playlist{ID: 42, Name: "热歌榜", TrackCount: 3, Tracks: []netease.Track{
		{ID: 1, Name: "Hong Kong", Artists: []netease.Artist{{Name: "The Weeknd"}}, Album: netease.Album{Name: "Hong Kong"}, Duration: 217000},
		{ID: 2, Name: "我不难过", Artists: []netease.Artist{{Name: "孙燕姿"}}, Album: netease.Album{Name: "未完成"}, Duration: 320000},
		{ID: 3, Name: "明知故犯", Artists: []netease.Artist{{Name: "Max李玄"}}, Album: netease.Album{Name: "失温"}, Duration: 166000},
	}}
	pl.Creator.UserID, pl.Creator.Nickname = 99, "网易云音乐"
	a.playlists[42] = pl
	a.open(route{page: pagePlaylist, id: 42})
	a.startBatch(pl)
	a.batch.selected[2] = true
	for _, dark := range []bool{false, true} {
		for _, width := range []int{1180, 880} {
			tt := ui.NewTester(a.view, width, 780)
			tt.SetDark(dark)
			tt.Advance(500 * time.Millisecond)
			tt.Frame()
			appearance := "light"
			if dark {
				appearance = "dark"
			}
			f, err := os.Create(filepath.Join(dir, fmt.Sprintf("batch-%s-%d.png", appearance, width)))
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
}
