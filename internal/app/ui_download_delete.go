package app

import (
	"fmt"
	"os"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/netease"
)

type downloadDelete struct {
	open  bool
	track netease.Track
	path  string
	items []download.Item
}

// confirmDeleteDownload asks before a file the listener downloaded leaves
// the disk. A file playing is left until another song is chosen.
func (a *app) confirmDeleteDownload(tr netease.Track) {
	path := tr.LocalPath
	if path == "" {
		a.loadDownloads(false)
		for _, it := range a.dl.items {
			if it.Track.ID == tr.ID {
				path = it.Path
				break
			}
		}
	}
	if path == "" {
		a.toast = "没有找到这首歌的本地文件"
		a.redraw()
		return
	}
	if cur := a.pl.Current(); cur != nil && cur.LocalPath != "" && downloadPathKey(cur.LocalPath) == downloadPathKey(path) {
		a.toast = "这首歌正在播放，请先切歌再删除"
		a.redraw()
		return
	}
	a.dl.deletion = downloadDelete{open: true, track: tr, path: path}
	a.redraw()
}

func (a *app) confirmDeleteSelectedDownloads() {
	items := a.downloadBatchItems()
	if len(items) == 0 {
		return
	}
	a.dl.deletion = downloadDelete{open: true, items: items}
	a.redraw()
}

func (a *app) deleteDownload() {
	d := &a.dl.deletion
	if len(d.items) > 0 {
		a.deleteDownloadItems(d.items)
		return
	}
	if d.path == "" {
		d.open = false
		return
	}
	removeDownloadFiles(d.path)
	if err := os.Remove(d.path); err != nil && !os.IsNotExist(err) {
		a.toast = "无法删除本地文件，请先切歌后再试"
		a.redraw()
		return
	}
	keep := a.dl.items[:0]
	for _, it := range a.dl.items {
		if it.Track.ID != d.track.ID && downloadPathKey(it.Path) != downloadPathKey(d.path) {
			keep = append(keep, it)
		}
	}
	a.dl.items = keep
	if err := a.saveDownloads(); err != nil {
		a.toast = "已删除文件，但下载记录没有保存"
	} else {
		a.toast = "已从本地删除：" + d.track.Name
	}
	d.open = false
	a.redraw()
}

// deleteDownloadItems deletes the chosen files, keeping one that is playing
// and any one the file system refuses. Its index entries go with the files.
func (a *app) deleteDownloadItems(items []download.Item) {
	d := &a.dl.deletion
	removed := map[string]bool{}
	failed := 0
	playing := a.pl.Current()
	for _, it := range items {
		if playing != nil && playing.LocalPath != "" && downloadPathKey(playing.LocalPath) == downloadPathKey(it.Path) {
			failed++
			continue
		}
		removeDownloadFiles(it.Path)
		if err := os.Remove(it.Path); err != nil && !os.IsNotExist(err) {
			failed++
			continue
		}
		removed[downloadPathKey(it.Path)] = true
	}
	keep := a.dl.items[:0]
	for _, it := range a.dl.items {
		if !removed[downloadPathKey(it.Path)] {
			keep = append(keep, it)
		}
	}
	a.dl.items = keep
	n := len(removed)
	if err := a.saveDownloads(); err != nil {
		a.toast = fmt.Sprintf("已删除 %d 首，但下载记录没有保存", n)
	} else if failed > 0 {
		a.toast = fmt.Sprintf("已删除 %d 首，%d 首未删除", n, failed)
	} else {
		a.toast = fmt.Sprintf("已从本地删除 %d 首", n)
	}
	a.batch = batchSelection{}
	d.open = false
	a.redraw()
}

func (a *app) downloadDeleteDialog(c *ui.Context) {
	d := &a.dl.deletion
	if !d.open {
		return
	}
	a.dialog(c, "download-delete", 0.35, &d.open, func(panel ui.Element) {
		ink := a.inkOn(false)
		panel.Width(400).TextColor(ink.text)
		a.pane(panel, 26, false)
		panel.Children(func() {
			body := fmt.Sprintf("确定删除“%s”的本地文件吗？删除后需要重新下载。", d.track.Name)
			if len(d.items) > 0 {
				body = fmt.Sprintf("确定删除选中的 %d 首本地文件吗？删除后需要重新下载。", len(d.items))
			}
			dialogHeader(c, ink, "从本地删除", body, func() { d.open = false })
			dialogFooter(c, func() {
				if settingButton(c.Key("cancel"), ink, "取消", nil, false, false).Clicked() {
					d.open = false
				}
				if dangerButton(c.Key("delete"), "删除", iconTrash, false).Clicked() {
					a.deleteDownload()
				}
			})
		})
	})
}
