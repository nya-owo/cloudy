package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/netease"
)

func testDownloadApp(t *testing.T) *app {
	t.Helper()
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.DownloadFolder = filepath.Join(t.TempDir(), "Downloads")
	a.dl.index = filepath.Join(t.TempDir(), "downloads.json")
	a.dl.loaded = true
	return a
}

// A song downloaded from NetEase plays from the file, though the account
// could no longer stream it.
func TestDownloadedTracksPlayLocally(t *testing.T) {
	a := newTestApp()
	tr := netease.Track{
		ID:        7,
		Name:      "下载测试",
		Artists:   []netease.Artist{{Name: "测试歌手"}},
		Fee:       1,
		Privilege: &netease.Privilege{St: 0, Pl: 0},
	}
	a.dl.items = []download.Item{{Track: tr, Path: filepath.Join("C:", "Music", "下载测试.mp3")}}
	tracks := a.downloadTracks()
	if len(tracks) != 1 {
		t.Fatalf("tracks %+v", tracks)
	}
	if !tracks[0].Playable() || tracks[0].LocalPath == "" || tracks[0].ID != 7 {
		t.Fatalf("track %+v", tracks[0])
	}
}

// A song without the right to play it all is not downloaded.
func TestDownloadWithoutPermission(t *testing.T) {
	a := testDownloadApp(t)
	a.startDownload(netease.Track{
		ID:        8,
		Name:      "会员歌曲",
		Privilege: &netease.Privilege{St: 0, Pl: 0},
	})
	if a.dl.queue.running != nil || len(a.dl.queue.waiting) != 0 {
		t.Fatal("queued a song without permission")
	}
	if a.toast != "这首歌暂时没有下载权限" {
		t.Errorf("toast %q", a.toast)
	}
}

// The download in a song's right-click menu starts, and the same song is not
// queued twice.
func TestDownloadFromSongMenu(t *testing.T) {
	a := testDownloadApp(t)
	old := fetchDownload
	fetchDownload = func(ctx context.Context, tr netease.Track, level, dir string, progress download.Progress) (download.Item, error) {
		<-ctx.Done()
		return download.Item{}, ctx.Err()
	}
	defer func() { fetchDownload = old }()

	a.playlists[42] = &netease.Playlist{ID: 42, Name: "测试歌单", Tracks: []netease.Track{{
		ID: 2, Name: "宝船", Artists: []netease.Artist{{Name: "DOUDOU"}}, Duration: 200000,
	}}}
	a.route = route{page: pagePlaylist, id: 42}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.RightClick("宝船"); err != nil {
		t.Fatal(err)
	}
	if err := chooseItem(tt, "下载到本机"); err != nil {
		t.Fatal(err)
	}
	if a.dl.queue.running == nil || a.dl.queue.running.ID != 2 {
		t.Fatalf("job %+v, toast %q, queue %+v, downloads %+v", a.dl.queue.running, a.toast, a.dl.queue.waiting, a.dl.items)
	}
	a.startDownload(netease.Track{ID: 2, Name: "宝船"})
	if a.toast != "这首歌正在下载" {
		t.Errorf("toast %q", a.toast)
	}
	a.cancelDownload()
}

// A click on a downloaded song plays its file, and the page shows the songs
// saved even when NetEase would no longer say they are playable.
func TestDownloadedSongPlays(t *testing.T) {
	path, err := filepath.Abs(filepath.Join("..", "player", "testdata", "seek.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	a := testDownloadApp(t)
	a.dl.items = []download.Item{{
		Track: netease.Track{
			ID: 9, Name: "下载测试", Duration: 1000,
			Artists:   []netease.Artist{{Name: "测试歌手"}},
			Fee:       1,
			Privilege: &netease.Privilege{St: 0, Pl: 0},
		},
		Path: path,
		At:   time.Now(),
	}}
	a.open(route{page: pageDownloads})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("下载测试"); err != nil {
		t.Fatal(err)
	}
	cur := a.pl.Current()
	if cur == nil || cur.LocalPath != path {
		t.Fatalf("playing %+v", cur)
	}
}

// Reading the downloads folder again picks up a file put there by hand, and
// keeps its details in the index.
func TestDownloadsScanFindsFile(t *testing.T) {
	a := testDownloadApp(t)
	src, err := filepath.Abs(filepath.Join("..", "player", "testdata", "seek.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(a.settings.DownloadFolder, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(a.settings.DownloadFolder, "手动 - 测试.mp3")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	a.dl.artDir = t.TempDir()
	a.dl.loaded = false
	a.loadDownloads(true)
	if len(a.dl.items) != 1 || a.dl.items[0].Path != path {
		t.Fatalf("downloads %+v", a.dl.items)
	}
	if _, err := os.Stat(a.dl.index); err != nil {
		t.Errorf("index: %v", err)
	}
}

// Switching folders shows the songs of the new folder, not those indexed
// under the old one.
func TestDownloadFolderSwitchReadsOnlyNewFolder(t *testing.T) {
	a := testDownloadApp(t)
	src, err := filepath.Abs(filepath.Join("..", "player", "testdata", "seek.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	dirA, dirB := filepath.Join(t.TempDir(), "a"), filepath.Join(t.TempDir(), "b")
	for _, dir := range []string{dirA, dirB} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	pathA, pathB := filepath.Join(dirA, "歌手甲 - 第一首.mp3"), filepath.Join(dirB, "歌手乙 - 第二首.mp3")
	if err := os.WriteFile(pathA, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pathB, b, 0o644); err != nil {
		t.Fatal(err)
	}
	a.setDownloadFolder(dirA)
	if len(a.dl.items) != 1 || a.dl.items[0].Path != pathA {
		t.Fatalf("first folder: %+v", a.dl.items)
	}
	a.setDownloadFolder(dirB)
	if len(a.dl.items) != 1 || a.dl.items[0].Path != pathB {
		t.Fatalf("second folder: %+v", a.dl.items)
	}
	a.loadDownloads(true)
	if len(a.dl.items) != 1 || a.dl.items[0].Path != pathB {
		t.Fatalf("after rescan: %+v", a.dl.items)
	}
}

// The download folder is the listener's to choose, typed with quotes as
// Explorer copies it, and going back to empty returns to Cloudy Downloads.
func TestDownloadFolderSet(t *testing.T) {
	a := testDownloadApp(t)
	dir := filepath.Join(t.TempDir(), "downloads")
	a.setDownloadFolder(`"` + dir + `"`)
	if a.settings.DownloadFolder != dir || a.downloadFolder() != dir {
		t.Fatalf("folder %q, setting %q", a.downloadFolder(), a.settings.DownloadFolder)
	}
	if a.dl.pathDraft != dir {
		t.Errorf("the input still shows %q", a.dl.pathDraft)
	}
	// A whole disk is not a folder to fill with songs.
	a.setDownloadFolder(filepath.VolumeName(dir) + string(filepath.Separator))
	if a.settings.DownloadFolder != dir || a.dl.pathDraft != dir {
		t.Errorf("a volume root was taken: %q, input %q", a.settings.DownloadFolder, a.dl.pathDraft)
	}
	a.setDownloadFolder("")
	if a.settings.DownloadFolder != "" || a.downloadFolder() != a.defaultDownloadFolder() {
		t.Errorf("cleared to %q, setting %q", a.downloadFolder(), a.settings.DownloadFolder)
	}
}

// A song already on disk offers to delete its file, not to download it again.
func TestDownloadedSongMenu(t *testing.T) {
	a := testDownloadApp(t)
	if err := os.MkdirAll(a.settings.DownloadFolder, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(a.settings.DownloadFolder, "已经下载.mp3")
	if err := os.WriteFile(path, []byte("ID3"), 0o644); err != nil {
		t.Fatal(err)
	}
	tr := netease.Track{ID: 7, Name: "已经下载"}
	a.dl.items = []download.Item{{Track: tr, Path: path}}
	check := func(groups [][]menuAction) (download, remove bool) {
		for _, group := range groups {
			for _, act := range group {
				download = download || act.label == "下载到本机"
				remove = remove || act.label == "从本地删除"
			}
		}
		return
	}
	if download, remove := check(a.songActions(&tr, 0)); download || !remove {
		t.Fatalf("online menu: download %v, delete %v", download, remove)
	}
	local := tr
	local.LocalPath = path
	if download, remove := check(a.downloadedSongActions(&local)); download || !remove {
		t.Fatalf("local menu: download %v, delete %v", download, remove)
	}
}

// Deleting a downloaded song removes its file and its index entry.
func TestDeleteDownload(t *testing.T) {
	a := testDownloadApp(t)
	if err := os.MkdirAll(a.settings.DownloadFolder, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(a.settings.DownloadFolder, "要删除.mp3")
	if err := os.WriteFile(path, []byte("ID3"), 0o644); err != nil {
		t.Fatal(err)
	}
	tr := netease.Track{ID: 8, Name: "要删除"}
	a.dl.items = []download.Item{{Track: tr, Path: path}}
	a.confirmDeleteDownload(tr)
	if !a.dl.deletion.open {
		t.Fatal("no delete confirmation")
	}
	a.deleteDownload()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file still there: %v", err)
	}
	if len(a.dl.items) != 0 {
		t.Fatalf("downloads %+v", a.dl.items)
	}
}

// Downloaded songs can be chosen in the page and deleted together.
func TestDownloadsBatchDelete(t *testing.T) {
	a := testDownloadApp(t)
	if err := os.MkdirAll(a.settings.DownloadFolder, 0o755); err != nil {
		t.Fatal(err)
	}
	var paths []string
	for i, name := range []string{"一", "二"} {
		path := filepath.Join(a.settings.DownloadFolder, name+".mp3")
		if err := os.WriteFile(path, []byte("ID3"), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
		a.dl.items = append(a.dl.items, download.Item{Track: netease.Track{ID: int64(i + 1), Name: name}, Path: path})
	}
	a.open(route{page: pageDownloads})
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := chooseItem(tt, "更多", "批量删除"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.batch.on || !a.batch.downloads {
		t.Fatalf("batch %+v", a.batch)
	}
	if err := tt.Click("全选"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.downloadBatchCount() != 2 {
		t.Fatalf("chosen %d", a.downloadBatchCount())
	}
	if err := tt.Click("删除选中"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.dl.deletion.open {
		t.Fatal("no delete confirmation")
	}
	if err := tt.Click("删除"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, path := range paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s still there: %v", path, err)
		}
	}
	if len(a.dl.items) != 0 || a.batch.on {
		t.Fatalf("downloads %+v, batch %+v", a.dl.items, a.batch)
	}
}
