package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/local"
	"netease-music-mygo/internal/netease"
)

// localLibrary is the listener's own music, in the folders they chose
// (settings.LocalFolders): read once, kept in an index of its own, and read
// again as the folders change.
type localLibrary struct {
	items  []download.Item
	tracks []netease.Track // items as the player queues them
	loaded bool
	err    string
	// The folders being read: scanning is the scan's number while it runs,
	// scanGen the last begun; scanned says how far it has read.
	scanning, scanGen int
	scanned           *scanCount
	index             string // where the index is kept
	artDir            string // where the covers of the files are kept
	unwatch           []func()
	changed           *time.Timer // a change in a folder, waiting for the rest
}

// localSettle is how long a folder has been still before it is read again:
// a copy of an album changes it many times over.
const localSettle = 2 * time.Second

// libraryStore is where the library's index is kept.
func (a *app) libraryStore() download.Store {
	if a.lib.index == "" {
		if dir, err := mygo.App.Path(mygo.PathUserData); err == nil {
			a.lib.index = filepath.Join(dir, "local-music.json")
		}
	}
	return download.Store{Path: a.lib.index}
}

// libraryArt is where the covers of the library's files are kept.
func (a *app) libraryArt() string {
	if a.lib.artDir == "" {
		if cache, err := mygo.App.Path(mygo.PathCache); err == nil {
			a.lib.artDir = filepath.Join(cache, "local-covers")
		}
	}
	return a.lib.artDir
}

// loadLibrary shows what the index knew, and reads the folders again, away
// from the window, for what changed while Cloudy was closed; it watches
// them from then on. Once loaded, only scan reads them again.
func (a *app) loadLibrary(scan bool) {
	if a.lib.loaded && !scan {
		return
	}
	first := !a.lib.loaded
	if first {
		items, _ := a.libraryStore().Load()
		a.setLibrary(existingDownloads(items))
		a.lib.loaded = true
		a.watchLibrary()
	}
	a.scanLibraryLater()
}

// setLibrary shows items, sorted as the downloads are: newest first.
func (a *app) setLibrary(items []download.Item) {
	sortDownloads(items)
	a.lib.items = items
	tracks := make([]netease.Track, 0, len(items))
	for _, it := range items {
		tr := it.Track
		tr.LocalPath, tr.Privilege, tr.Fee, tr.ProgramID, tr.Reason = it.Path, nil, 0, 0, ""
		tracks = append(tracks, tr)
	}
	a.lib.tracks = tracks
}

// scanLibraryLater reads the folders away from the window.
func (a *app) scanLibraryLater() {
	a.lib.scanGen++
	gen := a.lib.scanGen
	dirs := slices.Clone(a.settings.LocalFolders)
	indexed := slices.Clone(a.lib.items)
	count := &scanCount{}
	a.lib.scanned = count
	art := a.libraryArt()
	run := func() ([]download.Item, error) {
		return scanLibrary(dirs, indexed, art, func(n, all int) {
			count.all.Store(int64(all))
			count.done.Store(int64(n))
		})
	}
	finish := func(items []download.Item, err error) {
		if gen != a.lib.scanGen {
			return // other folders, or another scan, since
		}
		a.lib.scanning = 0
		a.lib.err = ""
		if err != nil {
			a.lib.err = "有文件夹无法读取：" + err.Error()
		}
		a.setLibrary(items)
		a.libraryStore().Save(items)
		a.redraw()
	}
	a.lib.scanning = gen
	if a.win == nil {
		finish(run()) // no window to come back to (tests)
		return
	}
	go func() {
		items, err := run()
		a.post(func() { finish(items, err) })
	}()
	a.redraw()
}

// scanLibrary reads the songs in dirs and the folders below them. A file the
// index knows, of the same size and time, is not read again. The first
// folder that cannot be read is said, the others read all the same.
func scanLibrary(dirs []string, indexed []download.Item, artDir string, progress func(done, all int)) ([]download.Item, error) {
	byPath := make(map[string]download.Item, len(indexed))
	for _, it := range indexed {
		byPath[downloadPathKey(it.Path)] = it
	}
	var paths []string
	var failed error
	seen := map[string]bool{}
	for _, dir := range dirs {
		found, err := local.Paths(dir)
		if err != nil {
			if failed == nil {
				failed = err
			}
			continue
		}
		for _, p := range found {
			if key := downloadPathKey(p); !seen[key] {
				seen[key] = true
				paths = append(paths, p)
			}
		}
	}
	out := make([]download.Item, 0, len(paths))
	for n, path := range paths {
		if progress != nil && n%16 == 0 {
			progress(n, len(paths))
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if it, ok := byPath[downloadPathKey(path)]; ok && it.Numbered && it.Size == info.Size() && it.At.Equal(info.ModTime()) {
			it.Path = path
			out = append(out, it)
			continue
		}
		s, err := local.Read(path)
		if err != nil {
			continue
		}
		out = append(out, download.Item{Track: downloadFileTrack(*s, artDir), Path: path, Size: info.Size(), At: info.ModTime(), Numbered: true})
	}
	if progress != nil {
		progress(len(paths), len(paths))
	}
	return out, failed
}

// scanningLibrary reports whether the folders are being read, and how far.
func (a *app) scanningLibrary() (bool, int, int) {
	if a.lib.scanning == 0 || a.lib.scanned == nil {
		return false, 0, 0
	}
	return true, int(a.lib.scanned.done.Load()), int(a.lib.scanned.all.Load())
}

// addLibraryFolder adds dir to the folders of the library: a folder in one
// of them is in it already, and one that holds some of them takes their
// place.
func (a *app) addLibraryFolder(dir string) {
	dir = strings.Trim(strings.TrimSpace(dir), `"`)
	if dir == "" {
		return
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	dir = filepath.Clean(dir)
	if folderRoot(dir) {
		a.toast = "不能把整块磁盘当作音乐文件夹，请选择其中的文件夹"
		a.redraw()
		return
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		a.toast = "无法打开这个文件夹"
		a.redraw()
		return
	}
	var kept []string
	for _, have := range a.settings.LocalFolders {
		switch {
		case pathWithin(have, dir):
			a.toast = "这个文件夹已经在本地音乐里了"
			a.redraw()
			return
		case pathWithin(dir, have):
			// Held by the one added: it goes.
		default:
			kept = append(kept, have)
		}
	}
	a.settings.LocalFolders = append(kept, dir)
	a.changed()
	a.toast = "已添加文件夹，正在读取：" + filepath.Base(dir)
	a.libraryFoldersChanged()
}

// removeLibraryFolder takes dir out of the library. Its files stay where
// they are.
func (a *app) removeLibraryFolder(dir string) {
	a.settings.LocalFolders = slices.DeleteFunc(slices.Clone(a.settings.LocalFolders), func(d string) bool {
		return downloadPathKey(d) == downloadPathKey(dir)
	})
	a.changed()
	a.toast = "已从本地音乐移除：" + filepath.Base(dir) + "（文件不会被删除）"
	a.libraryFoldersChanged()
}

// libraryFoldersChanged reads the folders again, and watches those there
// are now.
func (a *app) libraryFoldersChanged() {
	if !a.lib.loaded {
		a.loadLibrary(false)
		a.redraw()
		return
	}
	a.watchLibrary()
	a.scanLibraryLater()
	a.redraw()
}

// pickLibraryFolder asks for a folder to add to the library.
func (a *app) pickLibraryFolder() {
	start := ""
	if n := len(a.settings.LocalFolders); n > 0 {
		start = a.settings.LocalFolders[n-1]
	} else if music, err := mygo.App.Path(mygo.PathMusic); err == nil {
		start = music
	}
	go func() {
		dir, err := chooseFolder(a.win, "添加本地音乐文件夹", start)
		if err != nil || dir == "" {
			return
		}
		a.post(func() { a.addLibraryFolder(dir) })
	}()
}

// watchLibrary watches the library's folders, where the system can: a
// change reads them again once they have been still for localSettle.
func (a *app) watchLibrary() {
	for _, off := range a.lib.unwatch {
		off()
	}
	a.lib.unwatch = nil
	if a.win == nil {
		return // tests
	}
	for _, dir := range a.settings.LocalFolders {
		if off := watchFolder(dir, func() { a.post(a.libraryChanged) }); off != nil {
			a.lib.unwatch = append(a.lib.unwatch, off)
		}
	}
}

// libraryChanged hears that a folder of the library changed.
func (a *app) libraryChanged() {
	if a.lib.changed != nil {
		a.lib.changed.Reset(localSettle)
		return
	}
	a.lib.changed = time.AfterFunc(localSettle, func() {
		a.post(func() {
			a.lib.changed = nil
			a.scanLibraryLater()
		})
	})
}

// inDownloads reports whether path is in the downloads folder: a file of
// the listener's own music elsewhere is never deleted by Cloudy.
func (a *app) inDownloads(path string) bool {
	return pathWithin(a.downloadFolder(), path)
}

// loadLibraryLater reads the library soon after the window opens, for its
// folders to be watched from the start: not at once, which the window's
// first frames have the computer for.
func (a *app) loadLibraryLater() {
	if len(a.settings.LocalFolders) == 0 {
		return
	}
	time.AfterFunc(3*time.Second, func() { a.post(func() { a.loadLibrary(false) }) })
}
