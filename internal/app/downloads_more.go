package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/netease"
)

// downloadLevels are the levels the listener may download at, in the
// settings: the first follows the level songs play at.
var downloadLevels = []struct {
	level string
	label string
}{
	{"", "跟随播放音质"},
	{"standard", "标准"},
	{"higher", "较高"},
	{"exhigh", "极高"},
	{"lossless", "无损"},
	{"hires", "Hi-Res"},
}

// downloadLevel is the level songs download at.
func (a *app) downloadLevel() string {
	level := a.settings.DownloadQuality
	if level == "" {
		level = a.settings.Quality
	}
	if level == "" {
		level = "exhigh"
	}
	return level
}

// tagDownload writes into a song just downloaded its title, artists,
// album, cover and lyrics, and its lyrics beside it as a .lrc, for other
// players, and for this one to know it again if its index is lost. It runs
// off the main thread; what it cannot fetch it leaves out. Tests replace it.
var tagDownload = func(item download.Item) bool {
	tr := item.Track
	tags := download.Tags{Title: tr.Name, Artist: tr.ArtistNames(), Album: tr.Album.Name, ID: tr.ID}
	if pic := tr.Album.PicURL; pic != "" && strings.HasPrefix(pic, "http") {
		if b, err := netease.Fetch(netease.Sized(pic, 800)); err == nil {
			tags.Cover = b
		}
	}
	if tr.ID > 0 {
		if lrc, _, _, err := netease.Lyrics(tr.ID); err == nil && lrc != "" {
			tags.Lyrics = lrc
			os.WriteFile(download.LyricsPath(item.Path), []byte(lrc), 0o644)
		}
	}
	return download.WriteTags(item.Path, tags) == nil
}

// searchSongs finds songs on NetEase; tests replace it.
var searchSongs = netease.Search

// matchDownload finds the NetEase song a file put in the folder, or
// downloaded before its tags were written, is: one of the same title, an
// artist in common, and as long, within a few seconds.
func matchDownload(tr netease.Track) (netease.Track, bool) {
	artist := strings.TrimSpace(tr.ArtistNames())
	if tr.Name == "" || artist == "" || artist == "未知歌手" {
		return netease.Track{}, false
	}
	found, err := searchSongs(artist+" "+tr.Name, 10)
	if err != nil {
		return netease.Track{}, false
	}
	norm := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
	for _, f := range found {
		if norm(f.Name) != norm(tr.Name) {
			continue
		}
		if tr.Duration > 0 && f.Duration > 0 && max(tr.Duration-f.Duration, f.Duration-tr.Duration) > 3000 {
			continue
		}
		for _, ar := range f.Artists {
			if ar.Name != "" && strings.Contains(norm(artist), norm(ar.Name)) {
				return f, true
			}
		}
	}
	return netease.Track{}, false
}

// fixDownloads, once a launch, writes the tags of the songs downloaded
// before Cloudy wrote them, and finds again on NetEase those the folder
// holds without saying which song they are: they get their cover, their
// lyrics and their menu back. It works one song at a time, off the main
// thread, and keeps the index as it goes.
func (a *app) fixDownloads() {
	if a.downloadsFixed {
		return
	}
	a.downloadsFixed = true
	var todo []download.Item
	for _, it := range a.downloads {
		if !it.Tagged {
			todo = append(todo, it)
		}
	}
	if len(todo) == 0 {
		return
	}
	go func() {
		for _, it := range todo {
			known := it.Track.ID > 0
			if !known {
				m, ok := matchDownload(it.Track)
				if !ok {
					continue
				}
				m.LocalPath = ""
				it.Track, known = m, true
			}
			tagged := tagDownload(it)
			a.post(func() {
				for i := range a.downloads {
					if downloadPathKey(a.downloads[i].Path) == downloadPathKey(it.Path) {
						a.downloads[i].Track, a.downloads[i].Tagged = it.Track, tagged
					}
				}
				a.saveDownloads()
			})
		}
	}()
}

// failedDownload is a song whose download failed, and why, as the queue's
// file keeps it.
type failedDownload struct {
	Track  netease.Track `json:"track"`
	Reason string        `json:"reason"`
}

// downloadQueuePath is where the queue is kept, beside the settings: none
// for settings that were never loaded, as in tests.
func (a *app) downloadQueuePath() string {
	if a.settings.path == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(a.settings.path), "download-queue.json")
}

// saveDownloadQueue keeps the queue, so that a song waiting, or the one
// downloading as the app quit, is downloaded once it opens again.
func (a *app) saveDownloadQueue() {
	var failed []failedDownload
	for _, f := range a.downloadQ.failed {
		failed = append(failed, failedDownload{f.item, f.reason})
	}
	saveQueueFile(a.downloadQueuePath(), a.downloadQ.kept(), failed)
}

// restoreDownloadQueue picks the queue up where the app left it, once the
// listener's session is back: the songs they may download depend on it.
func (a *app) restoreDownloadQueue() {
	q := &a.downloadQ
	if q.restored {
		return
	}
	q.restored = true
	kept, ok := readQueueFile[netease.Track, failedDownload](a.downloadQueuePath())
	if !ok {
		return
	}
	var failed []failedJob[netease.Track]
	for _, f := range kept.Failed {
		failed = append(failed, failedJob[netease.Track]{f.Track, f.Reason})
	}
	q.restore(kept.Queue, failed)
	if len(kept.Queue) > 0 {
		a.toast = "继续下载上次没完成的歌曲"
	}
	a.pumpDownload()
	a.redraw()
}

// retryDownloads queues again the failed songs of ids, or all of them for
// none.
func (a *app) retryDownloads(ids ...int64) {
	if n := a.downloadQ.retry(songKeys(ids)...); n > 0 {
		a.toast = fmt.Sprintf("重新下载 %d 首", n)
	}
	a.pumpDownload()
	a.saveDownloadQueue()
	a.redraw()
}

func songKeys(ids []int64) []string {
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = songKey{}.key(netease.Track{ID: id})
	}
	return keys
}

// removeDownloadFiles takes away what was written beside a downloaded
// song: its lyrics.
func removeDownloadFiles(path string) {
	os.Remove(download.LyricsPath(path))
}
