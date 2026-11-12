package app

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/local"
	"netease-music-mygo/internal/netease"
)

// mostDropped is the most songs a drop plays: a whole disk dropped by
// mistake is not read to the end.
const mostDropped = 2000

// listenForDrops plays the songs dropped on the window: files, and the songs
// in folders, MP3 and FLAC.
func (a *app) listenForDrops() {
	a.win.OnFileDrop(func(e *mygo.FileDropEvent) {
		paths := slices.Clone(e.Paths)
		art := a.dl.artDir
		if art == "" {
			if cache, err := mygo.App.Path(mygo.PathCache); err == nil {
				art = filepath.Join(cache, "download-covers")
			}
		}
		go func() {
			tracks, other := droppedTracks(paths, art)
			a.post(func() { a.playDropped(tracks, other) })
		}()
	})
}

// droppedTracks reads the songs at paths, files or folders, in the order
// they were dropped, and counts the files that are no songs it plays.
func droppedTracks(paths []string, artDir string) (tracks []netease.Track, other int) {
	add := func(path string) {
		if len(tracks) >= mostDropped {
			return
		}
		s, err := local.Read(path)
		if err != nil {
			other++
			return
		}
		tracks = append(tracks, downloadFileTrack(*s, artDir))
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			other++
			continue
		}
		if !info.IsDir() {
			switch strings.ToLower(filepath.Ext(path)) {
			case ".mp3", ".flac":
				add(path)
			default:
				other++
			}
			continue
		}
		inside, err := local.Paths(path)
		if err != nil {
			other++
			continue
		}
		for _, p := range inside {
			add(p)
		}
	}
	return tracks, other
}

// playDropped plays the songs dropped at once, after the song playing,
// and keeps what was queued after them: in place of the queue, a song
// dropped alone was all there was to skip to, and played again.
func (a *app) playDropped(tracks []netease.Track, other int) {
	if len(tracks) == 0 {
		a.toast = "只能播放 MP3 和 FLAC 文件"
		a.redraw()
		return
	}
	if a.pl.Current() != nil && a.pl.Radio() == "" && len(a.pl.Queue()) > 0 {
		if a.pl.PlayNext(tracks) > 0 {
			a.pl.Next()
		}
	} else {
		a.playList(tracks, false)
	}
	msg := fmt.Sprintf("正在播放拖入的 %d 首歌", len(tracks))
	if len(tracks) == 1 {
		msg = "正在播放：" + tracks[0].Name
	}
	if other > 0 {
		msg += fmt.Sprintf("（%d 个文件不是 MP3 或 FLAC，已跳过）", other)
	}
	a.toast = msg
	a.redraw()
}
