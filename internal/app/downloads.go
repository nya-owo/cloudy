package app

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/local"
	"netease-music-mygo/internal/netease"
)

// fetchDownload saves a song; tests replace it.
var fetchDownload = download.Fetch

// chooseFolder asks for a folder, under title; tests replace it.
var chooseFolder = func(parent *mygo.Window, title, start string) (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:            parent,
		Title:             title,
		DefaultPath:       start,
		ButtonLabel:       "选择",
		Directory:         true,
		CreateDirectories: true,
	})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

// chooseDownloadFolder asks for the folder downloaded songs are kept in;
// tests replace it.
var chooseDownloadFolder = func(parent *mygo.Window, start string) (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:            parent,
		Title:             "选择下载歌曲文件夹",
		DefaultPath:       start,
		ButtonLabel:       "选择",
		Directory:         true,
		CreateDirectories: true,
	})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

// songKey names a download by its song.
type songKey struct{}

func (songKey) key(tr netease.Track) string { return strconv.FormatInt(tr.ID, 10) }

// defaultDownloadFolder is where songs go until the listener names another
// folder: Cloudy Downloads among their music.
func (a *app) defaultDownloadFolder() string {
	dir, err := mygo.App.Path(mygo.PathMusic)
	if err != nil || dir == "" {
		dir = os.Getenv("USERPROFILE")
	}
	if dir == "" {
		dir = "."
	}
	return filepath.Join(dir, "Cloudy Downloads")
}

// downloadFolder is where downloaded songs are kept.
func (a *app) downloadFolder() string {
	if dir := strings.TrimSpace(a.settings.DownloadFolder); dir != "" {
		return dir
	}
	return a.defaultDownloadFolder()
}

// downloadStore is where the index of downloaded songs is kept.
func (a *app) downloadStore() download.Store {
	if a.dl.index == "" {
		if dir, err := mygo.App.Path(mygo.PathUserData); err == nil {
			a.dl.index = filepath.Join(dir, "downloads.json")
		}
	}
	return download.Store{Path: a.dl.index}
}

// ensureDownloadFolder makes the folder downloaded songs are kept in, and
// returns it.
func (a *app) ensureDownloadFolder() (string, error) {
	dir := a.downloadFolder()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return dir, err
	}
	return dir, nil
}

// setDownloadFolder changes where downloaded songs go. An empty path goes
// back to Cloudy Downloads among the listener's music.
func (a *app) setDownloadFolder(dir string) {
	dir = strings.Trim(strings.TrimSpace(dir), `"`)
	if dir != "" {
		if !filepath.IsAbs(dir) {
			if abs, err := filepath.Abs(dir); err == nil {
				dir = abs
			}
		}
		dir = filepath.Clean(dir)
		if folderRoot(dir) {
			a.toast = "不能把整块磁盘当作下载文件夹"
			a.dl.pathDraft = a.downloadFolder()
			a.redraw()
			return
		}
	}
	a.settings.DownloadFolder = dir
	a.settings.save()
	a.dl.pathDraft = a.downloadFolder()
	a.dl.loaded, a.dl.items, a.dl.err = false, nil, ""
	a.loadDownloads(true)
	a.redraw()
}

// loadDownloads reads what was saved, and, when asked, reads the folder too
// for files put there by hand. A missing or broken index is rebuilt from the
// folder rather than taking the songs away.
func (a *app) loadDownloads(scan bool) {
	if a.dl.loaded && !scan {
		return
	}
	dir, err := a.ensureDownloadFolder()
	if err != nil {
		a.dl.loaded, a.dl.err = true, "无法打开下载文件夹"
		return
	}
	if a.dl.artDir == "" {
		if cache, e := mygo.App.Path(mygo.PathCache); e == nil {
			a.dl.artDir = filepath.Join(cache, "download-covers")
		}
	}
	a.dl.err = ""
	store := a.downloadStore()
	_, statErr := os.Stat(store.Path)
	missing := os.IsNotExist(statErr)
	items, err := store.Load()
	broken := err != nil && !os.IsNotExist(err)
	if broken {
		items = nil
	}
	if scan || broken || missing {
		a.scanDownloadsLater(dir, items)
		return
	}
	items = existingDownloads(items)
	sortDownloads(items)
	a.dl.items = items
	a.noteDownloads()
	a.dl.loaded = true
	a.fixDownloads()
}

// scanDownloadsLater reads the folder away from the window, which shows
// what the index knew meanwhile, and how far the reading has come: a
// folder of thousands of songs, NetEase's own, takes a minute.
func (a *app) scanDownloadsLater(dir string, indexed []download.Item) {
	a.dl.scanGen++
	gen := a.dl.scanGen
	known := existingDownloads(slices.Clone(indexed))
	sortDownloads(known)
	a.dl.items, a.dl.loaded = known, true
	a.noteDownloads()
	count := &scanCount{}
	a.dl.scanned = count
	artDir := a.dl.artDir
	run := func() ([]download.Item, error) {
		return scanDownloadFolder(dir, indexed, artDir, func(n, all int) {
			count.all.Store(int64(all))
			count.done.Store(int64(n))
		})
	}
	finish := func(items []download.Item, err error) {
		if gen != a.dl.scanGen {
			return // another folder, or another scan, since
		}
		a.dl.scanning = 0
		if err != nil {
			a.dl.err = "无法读取下载文件夹"
		}
		// What finished downloading while the folder was read stays.
		have := make(map[string]bool, len(items))
		for _, it := range items {
			have[downloadPathKey(it.Path)] = true
		}
		for _, it := range a.dl.items {
			if !have[downloadPathKey(it.Path)] && pathWithin(dir, it.Path) {
				items = append(items, it)
			}
		}
		sortDownloads(items)
		a.dl.items = items
		a.noteDownloads()
		a.fixDownloads()
		if err := a.saveDownloads(); err != nil {
			a.dl.err = "无法保存下载记录"
		}
	}
	a.dl.scanning = gen
	if a.win == nil {
		// No window to come back to (tests): at once.
		finish(run())
		return
	}
	go func() {
		items, err := run()
		a.post(func() { finish(items, err) })
	}()
}

// scanningDownloads reports whether the folder is being read, and how many
// of how many songs are.
func (a *app) scanningDownloads() (bool, int, int) {
	if a.dl.scanning == 0 || a.dl.scanning != a.dl.scanGen || a.dl.scanned == nil {
		return false, 0, 0
	}
	return true, int(a.dl.scanned.done.Load()), int(a.dl.scanned.all.Load())
}

// scanDownloadFolder reads the songs in dir, taking the details of those the
// index knows and reading the rest from their tags or names, one file at a
// time, its cover kept in artDir. It runs away from the window: it touches
// nothing of the app's. progress hears how many of how many it has read.
func scanDownloadFolder(dir string, indexed []download.Item, artDir string, progress func(done, all int)) ([]download.Item, error) {
	byPath := make(map[string]download.Item, len(indexed))
	for _, it := range indexed {
		byPath[downloadPathKey(it.Path)] = it
	}
	paths, err := local.Paths(dir)
	if err != nil {
		return existingDownloads(slices.Clone(indexed)), err
	}
	out := make([]download.Item, 0, len(paths)+len(indexed))
	seen := make(map[string]bool, len(paths))
	for n, path := range paths {
		if progress != nil && n%16 == 0 {
			progress(n, len(paths))
		}
		key := downloadPathKey(path)
		it, ok := byPath[key]
		if ok && downloadComplete(it.Track) {
			// The index says all there is: the file need not be read.
			seen[key] = true
			it.Path = path
		} else {
			s, err := local.Read(path)
			if err != nil {
				continue
			}
			seen[key] = true
			if !ok {
				it = download.Item{Track: downloadFileTrack(*s, artDir), Path: path}
				// Downloaded by Cloudy, it says which song it is.
				it.Tagged = s.NeteaseID > 0
			} else {
				it.Path = path
				it.Track = downloadDetails(it.Track, *s)
			}
		}
		if info, err := os.Stat(path); err == nil {
			if it.Size == 0 {
				it.Size = info.Size()
			}
			if it.At.IsZero() {
				it.At = info.ModTime()
			}
		}
		out = append(out, it)
	}
	if progress != nil {
		progress(len(paths), len(paths))
	}
	// A file the scanner could not read still belongs to the listener if
	// the index knows it: keep it listed, so playback can report the fault.
	for _, it := range indexed {
		key := downloadPathKey(it.Path)
		if seen[key] {
			continue
		}
		if _, err := os.Stat(it.Path); err == nil && pathWithin(dir, it.Path) {
			out = append(out, it)
		}
	}
	return out, nil
}

// downloadComplete reports whether an index entry has all downloadDetails
// would fill from the file.
func downloadComplete(tr netease.Track) bool {
	return tr.Name != "" && len(tr.Artists) > 0 && tr.Album.Name != "" && tr.Duration > 0
}

// downloadDetails fills what an index entry left empty from the file's tags.
func downloadDetails(tr netease.Track, s local.Song) netease.Track {
	if tr.Name == "" {
		tr.Name = s.Title
	}
	if len(tr.Artists) == 0 {
		artist := strings.TrimSpace(s.Artist)
		if artist == "" {
			artist = "未知歌手"
		}
		tr.Artists = []netease.Artist{{Name: artist}}
	}
	if tr.Album.Name == "" {
		tr.Album.Name = s.Album
	}
	if tr.Duration <= 0 {
		tr.Duration = s.Duration.Milliseconds()
	}
	return tr
}

// existingDownloads keeps those of items whose file is still there.
func existingDownloads(items []download.Item) []download.Item {
	out := items[:0]
	for _, it := range items {
		if _, err := os.Stat(it.Path); err == nil {
			out = append(out, it)
		}
	}
	return out
}

func sortDownloads(items []download.Item) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].At.Equal(items[j].At) {
			return items[i].Path < items[j].Path
		}
		return items[i].At.After(items[j].At)
	})
}

// downloadPathKey compares paths as the system does: Windows ignores case,
// the others do not.
func downloadPathKey(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

// pathWithin reports whether path is in dir or one of its folders.
func pathWithin(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// saveDownloads keeps the index, and says whether it could.
func (a *app) saveDownloads() error {
	return a.downloadStore().Save(a.dl.items)
}

// downloadTracks are the downloaded songs as the player queues them: each
// one plays from its file, whatever NetEase would say of the song now.
func (a *app) downloadTracks() []netease.Track {
	tracks := make([]netease.Track, 0, len(a.dl.items))
	for _, it := range a.dl.items {
		tr := it.Track
		tr.LocalPath, tr.Privilege, tr.Fee, tr.ProgramID, tr.Reason = it.Path, nil, 0, 0, ""
		tracks = append(tracks, tr)
	}
	return tracks
}

// downloadBytes is how much the downloaded songs take.
func (a *app) downloadBytes() int64 {
	var total int64
	for _, it := range a.dl.items {
		if it.Size > 0 {
			total += it.Size
			continue
		}
		if info, err := os.Stat(it.Path); err == nil {
			total += info.Size()
		}
	}
	return total
}

// noteDownloads tells the player where the downloaded songs are, after
// a.dl.items changed: a song downloaded plays from its file wherever it is
// started from.
func (a *app) noteDownloads() {
	files := make(map[int64]string, len(a.dl.items))
	for _, it := range a.dl.items {
		if id := it.Track.ID; id > 0 && files[id] == "" {
			files[id] = it.Path
		}
	}
	a.dl.files.Store(files)
}

// noteDownloadsAtStart reads the index of downloads off the window, for the
// player to know them before the downloads page is first opened. What the
// page reads, if it comes first, stays.
func (a *app) noteDownloadsAtStart() {
	store := a.downloadStore()
	go func() {
		items, err := store.Load()
		if err != nil {
			return
		}
		files := make(map[int64]string, len(items))
		for _, it := range items {
			if id := it.Track.ID; id > 0 && files[id] == "" {
				files[id] = it.Path
			}
		}
		a.dl.files.CompareAndSwap(nil, files)
	}()
}

// downloadedFile is where the song with id was downloaded to, or "". It is
// safe on any goroutine.
func (a *app) downloadedFile(id int64) string {
	if files, ok := a.dl.files.Load().(map[int64]string); ok {
		return files[id]
	}
	return ""
}

// downloaded reports whether the song is already saved.
func (a *app) downloaded(id int64) bool {
	for _, it := range a.dl.items {
		if it.Track.ID == id {
			if _, err := os.Stat(it.Path); err == nil {
				return true
			}
		}
	}
	return false
}

// startDownload adds a song to what is downloaded, or says why it cannot.
func (a *app) startDownload(tr netease.Track) {
	if tr.LocalPath != "" || tr.ProgramID != 0 {
		return
	}
	if !tr.Playable() {
		a.toast = "这首歌暂时没有下载权限"
		a.redraw()
		return
	}
	a.loadDownloads(false)
	if a.downloaded(tr.ID) {
		a.toast = "这首歌已经下载过了"
		a.redraw()
		return
	}
	q := &a.dl.queue
	if cur, ok := q.current(); ok && cur.ID == tr.ID {
		a.toast = "这首歌正在下载"
		a.redraw()
		return
	}
	if !q.add(tr) {
		a.toast = "这首歌已经在下载队列里"
		a.redraw()
		return
	}
	a.toast = "已加入下载：" + tr.Name
	a.pumpDownload()
	a.saveDownloadQueue()
	a.redraw()
}

// pumpDownload starts the next song queued, unless one is downloading.
func (a *app) pumpDownload() {
	q := &a.dl.queue
	tr, ok := q.next()
	if !ok {
		return
	}
	dir, err := a.ensureDownloadFolder()
	if err != nil {
		a.toast = "无法创建下载文件夹"
		q.waiting = nil
		return
	}
	level := a.downloadLevel()
	q.run(tr, a.post, func(ctx context.Context, progress func(done, size int64)) func() {
		item, err := fetchDownload(ctx, tr, level, dir, progress)
		if err == nil {
			item.Tagged = tagDownload(item)
		}
		return func() { a.finishDownload(item, err) }
	})
}

// finishDownload lists what arrived, or says why it did not, and starts the
// next one queued.
func (a *app) finishDownload(item download.Item, err error) {
	tr := a.dl.queue.finish()
	switch {
	case err == nil:
		a.dl.items = append([]download.Item{item}, a.dl.items...)
		a.noteDownloads()
		sortDownloads(a.dl.items)
		a.dl.loaded = true
		if saveErr := a.saveDownloads(); saveErr != nil {
			a.toast = "已下载：" + item.Track.Name + "，但下载记录没有保存"
		} else {
			a.toast = "已下载：" + item.Track.Name
		}
	case errors.Is(err, context.Canceled):
		a.toast = "已取消下载"
	case errors.Is(err, netease.ErrNoCopyright):
		a.toast = "这首歌暂无版权，不能下载"
		a.dl.queue.fail(tr, "暂无版权")
	case errors.Is(err, netease.ErrUnavailable):
		a.toast = "这首歌暂时不能下载"
		a.dl.queue.fail(tr, "没有下载权限")
	default:
		a.toast = "下载失败：" + err.Error()
		a.dl.queue.fail(tr, err.Error())
	}
	a.pumpDownload()
	a.saveDownloadQueue()
	a.redraw()
}

// cancelDownload stops the song being saved and goes on with the queue.
func (a *app) cancelDownload() {
	if !a.dl.queue.stop() {
		return
	}
	a.toast = "已取消下载"
	a.pumpDownload()
	a.saveDownloadQueue()
	a.redraw()
}

// pickDownloadFolder asks for another folder, and reads it once chosen.
func (a *app) pickDownloadFolder() {
	start := a.downloadFolder()
	go func() {
		dir, err := chooseDownloadFolder(a.win, start)
		if err != nil || dir == "" {
			return
		}
		a.post(func() { a.setDownloadFolder(dir) })
	}()
}

func (a *app) openDownloadFolder() {
	dir, err := a.ensureDownloadFolder()
	if err == nil {
		err = mygo.Shell.OpenPath(dir)
	}
	if err != nil {
		a.toast = "无法打开下载文件夹"
	}
}

// folderRoot reports whether dir is a whole volume: reading or filling one
// of those is never what the listener meant.
func folderRoot(dir string) bool {
	if vol := filepath.VolumeName(dir); vol != "" {
		return filepath.Clean(dir) == filepath.Clean(vol+string(filepath.Separator))
	}
	return filepath.Clean(dir) == string(filepath.Separator)
}

// downloadFileTrack turns a song put in the downloads folder by hand into
// one the player queues, with the tags its file carries.
func (a *app) downloadFileTrack(s local.Song) netease.Track {
	return downloadFileTrack(s, a.dl.artDir)
}

// downloadFileTrack is the song a file is, its cover kept in artDir.
func downloadFileTrack(s local.Song, artDir string) netease.Track {
	tr := netease.Track{
		ID:        downloadFileID(s.Path),
		Name:      s.Title,
		Duration:  s.Duration.Milliseconds(),
		LocalPath: s.Path,
	}
	if s.NeteaseID > 0 {
		// Downloaded by Cloudy: the NetEase song, with its lyrics and menu.
		tr.ID = s.NeteaseID
	}
	if tr.Name == "" {
		tr.Name = strings.TrimSuffix(filepath.Base(s.Path), filepath.Ext(s.Path))
	}
	artist := strings.TrimSpace(s.Artist)
	if artist == "" {
		artist = "未知歌手"
	}
	tr.Artists = []netease.Artist{{Name: artist}}
	tr.Album.Name = strings.TrimSpace(s.Album)
	if tr.Album.Name == "" {
		tr.Album.Name = "未知专辑"
	}
	if cover := downloadCover(s, artDir); cover != "" {
		tr.Album.PicURL = cover
	}
	return tr
}

// downloadFileID names a file apart from NetEase's songs: a hash of its
// path, below zero, the same each time the folder is read.
func downloadFileID(path string) int64 {
	h := fnv.New64a()
	h.Write([]byte(strings.ToLower(filepath.Clean(path))))
	return -int64(h.Sum64()&0x3fffffffffffffff) - 1
}

// downloadCover keeps a song's embedded cover as a file, so that the cover
// cache and the system's now playing read it as they do any other.
func (a *app) downloadCover(s local.Song) string { return downloadCover(s, a.dl.artDir) }

// downloadCover keeps s's embedded cover in dir, or the cache's folder for
// them when dir is empty, and returns its path.
func downloadCover(s local.Song, dir string) string {
	if len(s.Picture) == 0 {
		return ""
	}
	if dir == "" {
		cache, err := mygo.App.Path(mygo.PathCache)
		if err != nil {
			return ""
		}
		dir = filepath.Join(cache, "download-covers")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	h := fnv.New64a()
	h.Write(s.Picture)
	h.Write([]byte(s.PictureMIME))
	ext := ".jpg"
	switch {
	case strings.Contains(strings.ToLower(s.PictureMIME), "png"):
		ext = ".png"
	case strings.Contains(strings.ToLower(s.PictureMIME), "webp"):
		ext = ".webp"
	}
	path := filepath.Join(dir, fmt.Sprintf("%x%s", h.Sum64(), ext))
	if _, err := os.Stat(path); err == nil {
		return path
	}
	if err := os.WriteFile(path, s.Picture, 0o600); err != nil {
		return ""
	}
	return path
}

// downloadedSongActions lists what can be done with a song whose file is
// here: its title or path copied, and its file shown in the file manager.
func (a *app) downloadedSongActions(tr *netease.Track) [][]menuAction {
	song := *tr
	groups := [][]menuAction{
		{{label: "下一首播放", icon: iconNext, do: func() { a.playNext([]netease.Track{song}) }}},
		{
			{label: "复制歌名", icon: iconCopy, do: func() { copyText(tr.Name) }},
			{label: "复制文件路径", icon: iconCopy, do: func() { copyText(tr.LocalPath) }},
		},
		{{label: "在文件夹中显示", icon: iconFolder, do: func() { mygo.Shell.ShowItemInFolder(tr.LocalPath) }}},
	}
	// Only what Cloudy downloaded: the listener's own music stays theirs.
	if a.inDownloads(tr.LocalPath) {
		groups = append(groups, []menuAction{{label: "从本地删除", icon: iconTrash, do: func() { a.confirmDeleteDownload(*tr) }}})
	}
	return groups
}
