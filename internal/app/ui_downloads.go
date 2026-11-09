package app

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
)

// downloadsPage lists the songs saved to disk, and what is on its way.
func (a *app) downloadsPage(c *ui.Context) {
	if !a.dl.loaded {
		a.loadDownloads(false)
	}
	tracks := a.downloadTracks()
	if a.dl.err != "" && len(tracks) == 0 && a.dl.queue.running == nil {
		a.centered(c, a.dl.err, func() { a.loadDownloads(true) })
		return
	}
	if len(tracks) == 0 && a.dl.queue.running == nil && len(a.dl.queue.waiting) == 0 {
		a.downloadsEmpty(c)
		return
	}
	a.toolRows(c, tracks, true, a.downloadsHeader)
}

// downloadArtURL is the cover of the newest song saved, or of the one
// arriving while there is no other.
func (a *app) downloadArtURL() string {
	if len(a.dl.items) > 0 {
		return a.dl.items[0].Track.Album.PicURL
	}
	if tr, ok := a.dl.queue.current(); ok {
		return tr.Album.PicURL
	}
	return ""
}

// downloadCovers are the covers the page can stack beside its title.
func (a *app) downloadCovers() []string {
	urls := make([]string, 0, 3)
	seen := map[string]bool{}
	for _, it := range a.dl.items {
		url := it.Track.Album.PicURL
		if url != "" && !seen[url] {
			seen[url] = true
			urls = append(urls, url)
			if len(urls) == 3 {
				break
			}
		}
	}
	return urls
}

// downloadsHeader is the page as a playlist is: its newest cover, what it
// says, where it is kept, and what plays or reads it.
func (a *app) downloadsHeader(c *ui.Context) {
	p := a.pal
	tracks := a.downloadTracks()
	var total time.Duration
	for i := range tracks {
		total += tracks[i].Length()
	}
	meta := "保存在本机"
	if len(a.dl.items) > 0 {
		meta = fmt.Sprintf("保存在本机 · %d 首 · %s · %s", len(a.dl.items), longTime(total), sizeText(a.downloadBytes()))
	}
	head := ui.Row(c).Gap(28).AlignItems(ui.End).Padding(0, 0, 26, 0)
	head.Children(func() {
		a.headCover(c, a.downloadArtURL())
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Children(func() {
			// What the page is, as 歌单 and 专辑 head theirs.
			ui.Text(c, "本地").FontSize(12).FontWeight(600).TextColor(p.accent)
			ui.Text(c, "下载歌曲").FontSize(30).FontWeight(800).LetterSpacing(-0.6).MaxLines(1)
			ui.Text(c, meta).FontSize(13).TextColor(p.muted)
			if a.downloadBatchActive() {
				ui.Text(c, "选择歌曲，完成后在下方删除").FontSize(12.5).TextColor(p.muted).Margin(10, 0, 0, 0)
			} else {
				ui.Row(c).Wrap().GapX(10).GapY(8).Margin(10, 0, 0, 0).Children(func() {
					if len(a.dl.items) > 0 {
						if pillButton(c.Key("play"), iconPlay, "播放", p.accent, p.onAccent).Clicked() {
							a.playList(a.downloadTracks(), false)
						}
						if pillButton(c.Key("shuffle"), iconShuffle, "随机播放", p.hover.Over(p.bg), p.text).Clicked() {
							a.playList(a.downloadTracks(), true)
						}
					}
					if pillButton(c.Key("open"), iconFolder, "打开文件夹", p.hover.Over(p.bg), p.text).Clicked() {
						a.openDownloadFolder()
					}
					count := a.downloadQueueCount() + len(a.dl.queue.failed)
					queueLabel := "下载队列"
					if count > 0 {
						queueLabel = fmt.Sprintf("下载队列 %d", count)
					}
					if pillButton(c.Key("queue"), iconQueue, queueLabel, p.hover.Over(p.bg), p.text).Clicked() {
						a.openDownloadQueue()
					}
					// What is done seldom, as a playlist keeps its own: the
					// row stays one line in a window of the least width.
					more := iconButton(c.Key("more"), iconMore, 18, p.muted, "更多").Size(36, 36)
					if more.Clicked() {
						var groups [][]menuAction
						if len(a.dl.items) > 0 {
							groups = append(groups, []menuAction{{label: "批量删除", icon: iconTrash, do: a.startDownloadBatch}})
						}
						groups = append(groups, []menuAction{
							{label: "更换文件夹…", icon: iconFolderSwap, do: a.pickDownloadFolder},
							{label: "重新扫描", icon: iconReload, do: func() {
								a.loadDownloads(true)
								a.toast = "已重新读取下载歌曲"
							}},
						})
						a.menuBelow(more, false, groups, nil)
					}
				})
			}
			a.downloadProgress(c)
		})
		a.coverStackURLs(c, a.downloadCovers())
	})
}

// downloadProgress shows the song arriving, how far it has come, and what
// waits behind it.
func (a *app) downloadProgress(c *ui.Context) {
	p := a.pal
	q := &a.dl.queue
	tr, ok := q.current()
	if !ok {
		return
	}
	share := float32(0)
	if q.size > 0 {
		share = min(float32(q.done)/float32(q.size), 1)
	}
	ui.Column(c).Gap(5).Margin(6, 0, 0, 0).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconDownload).Size(13, 13).TextColor(p.accent)
			ui.Text(c, "正在下载 · "+tr.Name).FontSize(12).FontWeight(600).TextColor(p.text).SingleLine().Grow(1).Shrink(1).MinWidth(0)
			if pillButton(c.Key("download-cancel"), nil, "取消", p.hover.Over(p.bg), p.text).Clicked() {
				a.cancelDownload()
			}
		})
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			bar := ui.Box(c).Grow(1).MinWidth(40).Height(5).Radius(3).Background(p.press)
			bar.Draw(func(pt *ui.Painter, r ui.Rect) {
				if share > 0 {
					pt.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * share, H: r.H}, p.accent, 3)
				}
			})
			if q.size > 0 {
				ui.Text(c, fmt.Sprintf("%s / %s", sizeText(q.done), sizeText(q.size))).FontSize(10.5).FontFeatures("tnum").TextColor(p.muted)
			} else {
				ui.Text(c, sizeText(q.done)).FontSize(10.5).FontFeatures("tnum").TextColor(p.muted)
			}
			if len(a.dl.queue.waiting) > 0 {
				ui.Text(c, fmt.Sprintf("还有 %d 首", len(a.dl.queue.waiting))).FontSize(10.5).TextColor(p.faint)
			}
		})
	})
}

// downloadsEmpty is the page with nothing saved: where downloads go, and
// how to start one.
func (a *app) downloadsEmpty(c *ui.Context) {
	p := a.pal
	top, bottom := a.insets(c)
	ui.Column(c).Grow(1).Padding(top, 0, bottom, 0).Center().Gap(14).Children(func() {
		ui.Icon(c, iconDownload).Size(56, 56).TextColor(p.faint)
		ui.Text(c, "还没有下载歌曲").FontSize(15).FontWeight(600)
		ui.Text(c, "在任意歌曲的右键菜单里选择「下载到本机」，也可以点歌曲旁的「更多」。下载会按设置里的音质保存到本机。").FontSize(12.5).TextColor(p.muted).MaxWidth(560).TextAlign(ui.Center)
		ui.Row(c).Wrap().GapX(10).GapY(8).Children(func() {
			if pillButton(c, iconFolder, "打开文件夹", p.hover.Over(p.bg), p.text).Clicked() {
				a.openDownloadFolder()
			}
			if pillButton(c, iconFolder, "更换文件夹", p.hover.Over(p.bg), p.text).Clicked() {
				a.pickDownloadFolder()
			}
			if pillButton(c, iconReload, "重新扫描", p.hover.Over(p.bg), p.text).Clicked() {
				a.loadDownloads(true)
			}
			if pillButton(c, iconHome, "去为你推荐", p.hover.Over(p.bg), p.text).Clicked() {
				a.open(route{page: pageHome})
			}
		})
	})
}

// sizeText says bytes in the unit that keeps them short.
func sizeText(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
