package app

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/netease"
)

// These boundaries keep the native dialog and network out of tests.
var fetchCover = netease.Fetch
var chooseCoverFile = mygo.Dialog.Save
var exportCover = saveTrackCover

// saveTrackCover keeps the original image bytes, not the player's thumbnail.
// The format comes from the image, since CDN URLs need not name an extension.
func saveTrackCover(song netease.Track, parent *mygo.Window) (bool, error) {
	if song.Album.PicURL == "" {
		return false, fmt.Errorf("这首歌没有封面")
	}
	b, err := fetchCover(song.Album.PicURL)
	if err != nil {
		return false, fmt.Errorf("无法获取封面：%w", err)
	}
	_, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		return false, fmt.Errorf("无法识别封面图片：%w", err)
	}
	ext := format
	if format == "jpeg" {
		ext = "jpg"
	}
	dir, err := mygo.App.Path(mygo.PathPictures)
	if err != nil {
		return false, err
	}
	name := "封面 - " + download.FileName(song, ext)
	path, err := chooseCoverFile(mygo.SaveDialogOptions{
		Parent: parent, Title: "下载封面", ButtonLabel: "保存",
		DefaultPath: filepath.Join(dir, name),
		Filters:     []mygo.FileFilter{{Name: strings.ToUpper(format) + " 图片", Extensions: []string{ext}}},
	})
	if err != nil || path == "" {
		return false, err
	}
	// Do not silently change the chosen destination after the dialog has
	// confirmed whether to overwrite it, nor write PNG bytes as a JPEG.
	chosen := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	if chosen != ext && !(format == "jpeg" && chosen == "jpeg") {
		return false, fmt.Errorf("请使用 .%s 扩展名保存封面", ext)
	}
	if err := writeFileAtomic(path, b, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// startCoverDownload captures the selected song before fetching or asking for a
// path, so changing tracks while the dialog is open cannot change the image.
func (a *app) startCoverDownload(song netease.Track) {
	if a.savingCover {
		a.toast = "正在保存封面…"
		return
	}
	a.savingCover = true
	a.toast = "正在获取封面…"
	parent, save := a.win, exportCover
	go func() {
		saved, err := save(song, parent)
		a.post(func() {
			a.savingCover = false
			switch {
			case err != nil:
				a.toast = "封面保存失败：" + err.Error()
			case saved:
				a.toast = "封面已保存"
			default:
				a.toast = ""
			}
		})
	}()
}

func (a *app) coverAction(tr *netease.Track) menuAction {
	song := *tr
	return menuAction{label: "下载封面", icon: iconDownload, do: func() { a.startCoverDownload(song) }}
}

// coverContext uses the same glass pane and row styling as the song menu.
func (a *app) coverContext(el ui.Element, tr *netease.Track) {
	if tr.Album.PicURL == "" {
		return
	}
	a.contextMenu(el, a.nowPlaying, func() [][]menuAction {
		return [][]menuAction{{a.coverAction(tr)}}
	}, a.songHead(tr))
}
