package app

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/netease"
)

// Under the cover background the page is translucent: the floating bars'
// glass takes what shows through as translucent, not as black.
func TestFloatGlassUnderCoverBackground(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	if !a.floatGlass(false).translucent {
		t.Error("the bar's glass reads the cover background as opaque")
	}
	a.settings.SidebarGlass, a.settings.MainGlass = false, false
	if a.floatGlass(false).translucent {
		t.Error("over the plain page, the bar's glass is translucent")
	}
}

// A long menu, as of many playlists, keeps in the window and scrolls.
func TestLongMenuKeepsInWindow(t *testing.T) {
	group := make([]menuAction, 60)
	m := paneMenu{groups: [][]menuAction{group}}
	if h := m.height(700); h > 700-16 {
		t.Errorf("a menu of 60 rows is %v tall in a window of 700", h)
	}
	short := paneMenu{groups: [][]menuAction{group[:3]}}
	if h := short.height(700); h != 12+3*menuRowHeight {
		t.Errorf("a short menu is %v tall", h)
	}
}

// Sorted by album, an album's songs keep together, and songs of none come
// last.
func TestSortByAlbum(t *testing.T) {
	song := func(name, album, artist string) netease.Track {
		return netease.Track{Name: name, Album: netease.Album{Name: album}, Artists: []netease.Artist{{Name: artist}}}
	}
	songs := []netease.Track{song("c", "B", "x"), song("z", "", "x"), song("a", "A", "y"), song("b", "B", "x"), song("d", "A", "x")}
	slices.SortStableFunc(songs, byAlbum)
	var got []string
	for _, s := range songs {
		got = append(got, s.Name)
	}
	if want := []string{"d", "a", "b", "c", "z"}; !slices.Equal(got, want) {
		t.Errorf("sorted %v, want %v", got, want)
	}
	if plSorts[4] != "按专辑" {
		t.Errorf("sorts %v", plSorts)
	}
}

// The player hears where the downloaded songs are as the list changes.
func TestDownloadsTellThePlayer(t *testing.T) {
	a := newTestApp()
	a.dl.items = []download.Item{{Track: netease.Track{ID: 7}, Path: `C:\m\7.mp3`}}
	a.noteDownloads()
	if a.downloadedFile(7) != `C:\m\7.mp3` || a.downloadedFile(8) != "" {
		t.Fatal("the player does not know the downloads")
	}
	a.dl.items = nil
	a.noteDownloads()
	if a.downloadedFile(7) != "" {
		t.Fatal("a deleted download still plays from its file")
	}
}

// Signing out asks first.
func TestSignOutAsks(t *testing.T) {
	a := newTestApp()
	a.profile = &netease.Profile{UserID: 1, Nickname: "n"}
	a.askSignOut()
	if !a.signingOut || a.profile == nil {
		t.Fatal("signed out without asking")
	}
}

// Files and folders dropped become songs; what is not a song is counted.
func TestDroppedTracks(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "player", "testdata", "seek.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "album")
	os.MkdirAll(sub, 0o755)
	one := filepath.Join(dir, "one.mp3")
	os.WriteFile(one, b, 0o644)
	os.WriteFile(filepath.Join(sub, "two.mp3"), b, 0o644)
	text := filepath.Join(dir, "notes.txt")
	os.WriteFile(text, []byte("x"), 0o644)
	tracks, other := droppedTracks([]string{one, sub, text}, t.TempDir())
	if len(tracks) != 2 || other != 1 {
		t.Fatalf("%d songs, %d others", len(tracks), other)
	}
	if tracks[0].LocalPath != one || tracks[0].ID >= 0 {
		t.Errorf("first song %+v", tracks[0])
	}
}

// The cache goes in a folder of its own inside the one chosen, which is
// never itself emptied of songs.
func TestCacheFolderOfItsOwn(t *testing.T) {
	dir := t.TempDir()
	if got := songCacheDir(dir); got != filepath.Join(dir, cacheFolderName) {
		t.Errorf("cache in %q", got)
	}
}

// A song dropped plays at once, with the queue kept after it: alone in
// its place, there was nothing to skip to.
func TestDroppedSongKeepsTheQueue(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "player", "testdata", "seek.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	// Not t.TempDir: the player keeps the song playing open.
	dir, err := os.MkdirTemp("", "cloudy-drop-")
	if err != nil {
		t.Fatal(err)
	}
	var queued []netease.Track
	for _, name := range []string{"a.mp3", "b.mp3", "dropped.mp3"} {
		path := filepath.Join(dir, name)
		os.WriteFile(path, b, 0o644)
		queued = append(queued, netease.Track{ID: downloadFileID(path), Name: name, LocalPath: path, Duration: 1000})
	}
	a := newTestApp()
	a.pl.PlayQueue(queued[:2], 0)
	a.playDropped(queued[2:], 0)
	q := a.pl.Queue()
	if len(q) != 3 || a.pl.Current() == nil || a.pl.Current().Name != "dropped.mp3" {
		t.Fatalf("queue %d, playing %+v", len(q), a.pl.Current())
	}
	if next := a.pl.NextIndex(); next < 0 || q[next].Name != "b.mp3" {
		t.Fatalf("next is %d", next)
	}
}

// Sorted by album, each album has a title over its songs, and the songs'
// rows are after them.
func TestAlbumGroups(t *testing.T) {
	song := func(name, album string, id int64, artist string) netease.Track {
		return netease.Track{Name: name, Album: netease.Album{Name: album, ID: id}, Artists: []netease.Artist{{Name: artist}}, Duration: 60000}
	}
	songs := []netease.Track{song("1", "X", 5, "p"), song("2", "X", 5, "q"), song("3", "X", 0, "r"), song("4", "Y", 0, "r")}
	slices.SortStableFunc(songs, byAlbum)
	g := albumGroups(songs)
	if len(g) != 3 || g[0].n+g[1].n != 3 || g[2].name != "Y" {
		t.Fatalf("groups %+v", g)
	}
	if g[1].id == 5 && g[1].artist != "多位歌手" || g[0].id == 5 && g[0].artist != "多位歌手" {
		t.Errorf("an album of two artists says %+v", g)
	}
}

// An album chosen in the album index is all the list shows; 全部专辑 shows
// them all again.
func TestAlbumChosenInIndex(t *testing.T) {
	a := newTestApp()
	a.route = route{page: pageDownloads}
	song := func(name, album string) netease.Track {
		return netease.Track{Name: name, Album: netease.Album{Name: album}, Artists: []netease.Artist{{Name: "r"}}}
	}
	tracks := []netease.Track{song("1", "X"), song("2", "Y"), song("3", "X")}
	a.listShown(tracks)
	v := &a.plView
	v.sort = sortAlbum
	if got := a.listShown(tracks); len(got) != 3 || len(v.all) != 2 {
		t.Fatalf("%d songs, %d albums", len(got), len(v.all))
	}
	a.chooseAlbum(v, v.all[0].key)
	got := a.listShown(tracks)
	if len(got) != 2 || got[0].Album.Name != "X" || len(v.groups) != 1 || v.groups[0].start != 0 || len(v.all) != 2 {
		t.Fatalf("kept to X: %d songs, groups %+v", len(got), v.groups)
	}
	a.chooseAlbum(v, "")
	if got := a.listShown(tracks); len(got) != 3 {
		t.Fatalf("all again: %d", len(got))
	}
}

// A long list in a menu, as of playlists, has a search, which keeps what
// it finds.
func TestMenuSearch(t *testing.T) {
	var lists []menuAction
	for i := range 12 {
		lists = append(lists, menuAction{label: fmt.Sprint("list ", i), title: fmt.Sprintf("歌单%d", i)})
	}
	m := paneMenu{groups: [][]menuAction{{{label: "新建歌单…"}}, lists}}
	if !m.searchable() {
		t.Fatal("13 rows and no search")
	}
	if got := matching(m.groups, "歌单1"); len(got) != 1 || len(got[0]) != 3 {
		t.Fatalf("found %v", got)
	}
	if (&paneMenu{groups: [][]menuAction{lists[:3]}}).searchable() {
		t.Error("3 rows have a search")
	}
}

// The library reads the songs in its folders and below them, and does not
// read again a file that did not change.
func TestLibraryScan(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "player", "testdata", "seek.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "album"), 0o755)
	os.WriteFile(filepath.Join(dir, "a.mp3"), b, 0o644)
	os.WriteFile(filepath.Join(dir, "album", "b.mp3"), b, 0o644)
	os.WriteFile(filepath.Join(dir, "c.txt"), b, 0o644)
	items, err := scanLibrary([]string{dir, filepath.Join(dir, "album")}, nil, t.TempDir(), nil)
	if err != nil || len(items) != 2 {
		t.Fatalf("%d songs, %v", len(items), err)
	}
	items[0].Track.Name = "kept"
	again, _ := scanLibrary([]string{dir}, items, t.TempDir(), nil)
	if len(again) != 2 || again[0].Track.Name != "kept" && again[1].Track.Name != "kept" {
		t.Fatal("a file that did not change was read again")
	}
	if _, err := scanLibrary([]string{filepath.Join(dir, "gone")}, nil, "", nil); err == nil {
		t.Error("a missing folder said nothing")
	}
}

// Folders added to the library: one in another is in it already, one that
// holds others takes their place, and a whole disk is refused.
func TestLibraryFolders(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	os.MkdirAll(sub, 0o755)
	a.addLibraryFolder(sub)
	a.addLibraryFolder(sub)
	if len(a.settings.LocalFolders) != 1 {
		t.Fatalf("folders %v", a.settings.LocalFolders)
	}
	a.addLibraryFolder(dir)
	if len(a.settings.LocalFolders) != 1 || a.settings.LocalFolders[0] != dir {
		t.Fatalf("folders %v", a.settings.LocalFolders)
	}
	a.addLibraryFolder(filepath.VolumeName(dir) + string(filepath.Separator))
	if len(a.settings.LocalFolders) != 1 {
		t.Fatal("a whole disk was added")
	}
	a.removeLibraryFolder(dir)
	if len(a.settings.LocalFolders) != 0 {
		t.Fatal("not removed")
	}
}

// Sorted by artist, each artist has a title over their songs.
func TestArtistGroups(t *testing.T) {
	a := newTestApp()
	a.route = route{page: pageLocal}
	song := func(name, artist, album string) netease.Track {
		return netease.Track{Name: name, Album: netease.Album{Name: album}, Artists: []netease.Artist{{Name: artist}}}
	}
	tracks := []netease.Track{song("1", "B", "x"), song("2", "A", "y"), song("3", "B", "z")}
	a.listShown(tracks)
	a.plView.sort = sortArtist
	a.listShown(tracks)
	g := a.plView.groups
	if len(g) != 2 || g[0].name != "A" || g[1].n != 2 || g[1].artist != "2 张专辑" {
		t.Fatalf("groups %+v", g)
	}
}

// A song of the listener's own music is never deleted from its menu: only
// what Cloudy downloaded is.
func TestOwnMusicNotDeleted(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.DownloadFolder = t.TempDir()
	own := netease.Track{ID: -5, Name: "x", LocalPath: filepath.Join(t.TempDir(), "x.mp3")}
	for _, g := range a.songActions(&own, 0) {
		for _, act := range g {
			if act.label == "从本地删除" {
				t.Fatal("the listener's own file can be deleted")
			}
		}
	}
	mine := netease.Track{ID: -6, Name: "y", LocalPath: filepath.Join(a.settings.DownloadFolder, "y.mp3")}
	found := false
	for _, g := range a.songActions(&mine, 0) {
		for _, act := range g {
			found = found || act.label == "从本地删除"
		}
	}
	if !found {
		t.Error("a downloaded file cannot be deleted")
	}
}

// The 本地音乐 page lists what its folders hold, and asks for a folder
// while it has none.
func TestLocalPage(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "player", "testdata", "seek.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	a := newTestApp()
	a.settings = defaultSettings()
	a.lib.index = filepath.Join(t.TempDir(), "local.json")
	a.lib.artDir = t.TempDir()
	tt := ui.NewTester(a.view, 1200, 760)
	a.open(route{page: pageLocal})
	tt.Frame()
	if !tt.HasText("还没有添加本地音乐文件夹") {
		t.Fatal("no folder, and the page does not ask for one")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "歌.mp3"), b, 0o644)
	a.addLibraryFolder(dir)
	tt.Frame()
	tt.Frame()
	if !tt.HasText("本地音乐") || len(a.lib.tracks) != 1 {
		t.Fatalf("%d songs listed", len(a.lib.tracks))
	}
	a.plView.sort = sortArtist
	tt.Frame()
	if !tt.HasText("1 张专辑 · 1 首 · 1 分钟") {
		t.Error("no title over the artist's songs")
	}
}
