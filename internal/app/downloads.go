package app

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"runtime"
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
	if a.downloadIndex == "" {
		if dir, err := mygo.App.Path(mygo.PathUserData); err == nil {
			a.downloadIndex = filepath.Join(dir, "downloads.json")
		}
	}
	return download.Store{Path: a.downloadIndex}
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
			a.downloadPathDraft = a.downloadFolder()
			a.redraw()
			return
		}
	}
	a.settings.DownloadFolder = dir
	a.settings.save()
	a.downloadPathDraft = a.downloadFolder()
	a.downloadsLoaded, a.downloads, a.downloadsErr = false, nil, ""
	a.loadDownloads(true)
	a.redraw()
}

// loadDownloads reads what was saved, and, when asked, reads the folder too
// for files put there by hand. A missing or broken index is rebuilt from the
// folder rather than taking the songs away.
func (a *app) loadDownloads(scan bool) {
	if a.downloadsLoaded && !scan {
		return
	}
	dir, err := a.ensureDownloadFolder()
	if err != nil {
		a.downloadsLoaded, a.downloadsErr = true, "无法打开下载文件夹"
		return
	}
	if a.downloadArtDir == "" {
		if cache, e := mygo.App.Path(mygo.PathCache); e == nil {
			a.downloadArtDir = filepath.Join(cache, "download-covers")
		}
	}
	a.downloadsErr = ""
	store := a.downloadStore()
	_, statErr := os.Stat(store.Path)
	missing := os.IsNotExist(statErr)
	items, err := store.Load()
	broken := err != nil && !os.IsNotExist(err)
	if broken {
		items = nil
	}
	if scan || broken || missing {
		items, err = a.scanDownloadFolder(dir, items)
		if err != nil {
			a.downloadsErr = "无法读取下载文件夹"
		}
	} else {
		items = existingDownloads(items)
	}
	sortDownloads(items)
	a.downloads = items
	a.downloadsLoaded = true
	a.fixDownloads()
	if scan || broken || missing {
		if err := a.saveDownloads(); err != nil {
			a.downloadsErr = "无法保存下载记录"
		}
	}
}

// scanDownloadFolder reads the songs in dir, taking the details of those the
// index knows and reading the rest from their tags or names.
func (a *app) scanDownloadFolder(dir string, indexed []download.Item) ([]download.Item, error) {
	byPath := make(map[string]download.Item, len(indexed))
	for _, it := range indexed {
		byPath[downloadPathKey(it.Path)] = it
	}
	songs, err := local.Scan(dir)
	if err != nil {
		return existingDownloads(indexed), err
	}
	out := make([]download.Item, 0, len(songs)+len(indexed))
	seen := make(map[string]bool, len(songs))
	for _, s := range songs {
		key := downloadPathKey(s.Path)
		seen[key] = true
		it, ok := byPath[key]
		if !ok {
			it = download.Item{Track: a.downloadFileTrack(s), Path: s.Path}
			// Downloaded by Cloudy, it says which song it is.
			it.Tagged = s.NeteaseID > 0
		} else {
			it.Path = s.Path
			it.Track = downloadDetails(it.Track, s)
		}
		if info, err := os.Stat(s.Path); err == nil {
			if it.Size == 0 {
				it.Size = info.Size()
			}
			if it.At.IsZero() {
				it.At = info.ModTime()
			}
		}
		out = append(out, it)
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
	return a.downloadStore().Save(a.downloads)
}

// downloadTracks are the downloaded songs as the player queues them: each
// one plays from its file, whatever NetEase would say of the song now.
func (a *app) downloadTracks() []netease.Track {
	tracks := make([]netease.Track, 0, len(a.downloads))
	for _, it := range a.downloads {
		tr := it.Track
		tr.LocalPath, tr.Privilege, tr.Fee, tr.ProgramID, tr.Reason = it.Path, nil, 0, 0, ""
		tracks = append(tracks, tr)
	}
	return tracks
}

// downloadBytes is how much the downloaded songs take.
func (a *app) downloadBytes() int64 {
	var total int64
	for _, it := range a.downloads {
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

// downloaded reports whether the song is already saved.
func (a *app) downloaded(id int64) bool {
	for _, it := range a.downloads {
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
	q := &a.downloadQ
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
	q := &a.downloadQ
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
	tr := a.downloadQ.finish()
	switch {
	case err == nil:
		a.downloads = append([]download.Item{item}, a.downloads...)
		sortDownloads(a.downloads)
		a.downloadsLoaded = true
		if saveErr := a.saveDownloads(); saveErr != nil {
			a.toast = "已下载：" + item.Track.Name + "，但下载记录没有保存"
		} else {
			a.toast = "已下载：" + item.Track.Name
		}
	case errors.Is(err, context.Canceled):
		a.toast = "已取消下载"
	case errors.Is(err, netease.ErrUnavailable):
		a.toast = "这首歌暂时不能下载"
		a.downloadQ.fail(tr, "没有下载权限")
	default:
		a.toast = "下载失败：" + err.Error()
		a.downloadQ.fail(tr, err.Error())
	}
	a.pumpDownload()
	a.saveDownloadQueue()
	a.redraw()
}

// cancelDownload stops the song being saved and goes on with the queue.
func (a *app) cancelDownload() {
	if !a.downloadQ.stop() {
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
	if cover := a.downloadCover(s); cover != "" {
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
func (a *app) downloadCover(s local.Song) string {
	if len(s.Picture) == 0 {
		return ""
	}
	dir := a.downloadArtDir
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
	return [][]menuAction{
		{
			{label: "复制歌名", icon: iconCopy, do: func() { copyText(tr.Name) }},
			{label: "复制文件路径", icon: iconCopy, do: func() { copyText(tr.LocalPath) }},
		},
		{{label: "在文件夹中显示", icon: iconFolder, do: func() { mygo.Shell.ShowItemInFolder(tr.LocalPath) }}},
		{{label: "从本地删除", icon: iconTrash, do: func() { a.confirmDeleteDownload(*tr) }}},
	}
}
