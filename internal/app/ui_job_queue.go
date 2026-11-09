package app

import (
	"fmt"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// The windows over the download queue and the upload list are one window,
// with each queue's own words: what runs, what waits, what failed, and which
// of those the listener chose.

// queueWindow is the state of one queue's window.
type queueWindow struct {
	open     bool
	selected map[string]bool // by the queue's key
	list     ui.ListState
}

// queueEntry is one row of a queue's window.
type queueEntry struct {
	key    string
	title  string
	sub    string // the artist, or what stands for one
	cover  string // the cover's address, none for a note
	active bool
	done   int64
	size   int64
	pos    int    // 1 for the first waiting song
	failed string // why it failed, for a song that did
}

// queueSpec is what tells the two windows apart.
type queueSpec struct {
	id           string // the dialog's key
	keys         string // the prefix of its buttons' keys
	title        string // 下载队列, 上传列表
	verb         string // 下载, 上传
	noun         string // 队列, 列表
	icon         *ui.SVG
	cancelChosen string
	preparing    bool // a job that sent nothing yet is shown as being prepared

	add      func() // the button to queue more, if the window has one
	retry    func(key string)
	retryAll func()
	remove   func(key string)
	cancel   func() // the chosen ones
	clear    func()
}

func (w *queueWindow) reset() {
	*w = queueWindow{open: true, selected: map[string]bool{}}
}

func (w *queueWindow) chosen(entries []queueEntry) int {
	n := 0
	for _, e := range entries {
		if w.selected[e.key] {
			n++
		}
	}
	return n
}

func (w *queueWindow) all(entries []queueEntry) bool {
	return len(entries) > 0 && w.chosen(entries) == len(entries)
}

func (w *queueWindow) toggle(key string) {
	if w.selected[key] {
		delete(w.selected, key)
	} else {
		w.selected[key] = true
	}
}

func (w *queueWindow) selectAll(entries []queueEntry) {
	all := w.all(entries)
	w.selected = make(map[string]bool, len(entries))
	if !all {
		for _, e := range entries {
			w.selected[e.key] = true
		}
	}
}

// queueEntries lists q as its window shows it: the job running, those
// waiting, then those that failed.
func queueEntries[T any, K jobKey[T]](q *jobQueue[T, K], show func(T) queueEntry) []queueEntry {
	var out []queueEntry
	entry := func(it T) queueEntry {
		e := show(it)
		e.key = q.key(it)
		return e
	}
	if it, ok := q.current(); ok {
		e := entry(it)
		e.active, e.done, e.size = true, q.done, q.size
		out = append(out, e)
	}
	for i, it := range q.waiting {
		e := entry(it)
		e.pos = i + 1
		out = append(out, e)
	}
	for _, f := range q.failed {
		e := entry(f.item)
		e.failed = f.reason
		out = append(out, e)
	}
	return out
}

// dropChosen takes the songs chosen in w out of q, stops the running one if
// it was chosen, and says how many went.
func dropChosen[T any, K jobKey[T]](q *jobQueue[T, K], w *queueWindow, stop func()) int {
	n, running := q.drop(w.selected)
	if running {
		n++
		stop()
	}
	w.selected = map[string]bool{}
	return n
}

// The download queue.

func (a *app) downloadQueueCount() int { return a.dl.queue.count() }

func (a *app) openDownloadQueue() {
	a.dl.view.reset()
	a.redraw()
}

func (a *app) downloadQueueEntries() []queueEntry {
	return queueEntries(&a.dl.queue, func(tr netease.Track) queueEntry {
		return queueEntry{title: tr.Name, sub: tr.ArtistNames(), cover: tr.Album.PicURL}
	})
}

// cancelSelectedDownloads takes the chosen songs out of the queue, cancels
// the one downloading if it was chosen, and forgets the chosen ones that
// failed.
func (a *app) cancelSelectedDownloads() {
	if len(a.dl.view.selected) == 0 {
		return
	}
	if n := dropChosen(&a.dl.queue, &a.dl.view, a.cancelDownload); n > 0 {
		a.toast = fmt.Sprintf("已取消 %d 项下载", n)
	}
	a.saveDownloadQueue()
	a.redraw()
}

func (a *app) clearDownloadQueue() {
	a.dl.view.selected = map[string]bool{}
	if a.dl.queue.clear() {
		a.cancelDownload()
	}
	a.toast = "已清空下载队列"
	a.saveDownloadQueue()
	a.redraw()
}

// removeDownload takes one song out of the queue, cancels it while it is
// the one downloading, or forgets it once it failed.
func (a *app) removeDownload(id int64) {
	key := strconv.FormatInt(id, 10)
	delete(a.dl.view.selected, key)
	tr, how := a.dl.queue.remove(key)
	switch how {
	case wasRunning:
		a.cancelDownload()
		return
	case wasWaiting:
		a.toast = "已从下载队列移除：" + tr.Name
	}
	a.saveDownloadQueue()
	a.redraw()
}

func songID(key string) int64 {
	id, _ := strconv.ParseInt(key, 10, 64)
	return id
}

// downloadQueueDialog is the queue's own window.
func (a *app) downloadQueueDialog(c *ui.Context) {
	a.queueDialog(c, &a.dl.view, a.downloadQueueEntries(), len(a.dl.queue.waiting), len(a.dl.queue.failed), queueSpec{
		id: "download-queue", keys: "dq", title: "下载队列", verb: "下载", noun: "队列",
		icon: iconDownload, cancelChosen: "取消所选下载",
		retry:    func(key string) { a.retryDownloads(songID(key)) },
		retryAll: func() { a.retryDownloads() },
		remove:   func(key string) { a.removeDownload(songID(key)) },
		cancel:   a.cancelSelectedDownloads,
		clear:    a.clearDownloadQueue,
	})
}

// The upload list.

// uploadQueueCount is the songs going up, waiting, and failed.
func (a *app) uploadQueueCount() int {
	return a.up.queue.count() + len(a.up.queue.failed)
}

func (a *app) openUploadQueue() {
	a.up.view.reset()
	a.redraw()
}

func (a *app) uploadQueueEntries() []queueEntry {
	return queueEntries(&a.up.queue, func(u cloudUpload) queueEntry {
		sub := u.Artist
		if sub == "" {
			sub = sizeText(u.Size)
		}
		return queueEntry{title: u.name(), sub: sub}
	})
}

// cancelSelectedUploads takes the chosen songs out of the queue, cancels the
// one going up if it was chosen, and forgets the chosen ones that failed.
func (a *app) cancelSelectedUploads() {
	if len(a.up.view.selected) == 0 {
		return
	}
	if n := dropChosen(&a.up.queue, &a.up.view, a.cancelUpload); n > 0 {
		a.toast = fmt.Sprintf("已取消 %d 项上传", n)
	}
	a.saveUploadQueue()
	a.redraw()
}

func (a *app) clearUploadQueue() {
	a.up.view.selected = map[string]bool{}
	if a.up.queue.clear() {
		a.cancelUpload()
	}
	a.toast = "已清空上传列表"
	a.saveUploadQueue()
	a.redraw()
}

// removeUpload takes the song of key, its uploadKey, out of the queue,
// cancels it while it goes up, or forgets it once it failed.
func (a *app) removeUpload(key string) {
	delete(a.up.view.selected, key)
	u, how := a.up.queue.remove(key)
	switch how {
	case wasRunning:
		a.cancelUpload()
		return
	case wasWaiting:
		a.toast = "已从上传列表移除：" + u.name()
	}
	a.saveUploadQueue()
	a.redraw()
}

// uploadQueueDialog is the upload list's own window.
func (a *app) uploadQueueDialog(c *ui.Context) {
	a.queueDialog(c, &a.up.view, a.uploadQueueEntries(), len(a.up.queue.waiting), len(a.up.queue.failed), queueSpec{
		id: "upload-queue", keys: "uq", title: "上传列表", verb: "上传", noun: "列表",
		icon: iconUpload, cancelChosen: "取消所选", preparing: true,
		add:      a.pickUploads,
		retry:    func(key string) { a.retryUploads(key) },
		retryAll: func() { a.retryUploads() },
		remove:   a.removeUpload,
		cancel:   a.cancelSelectedUploads,
		clear:    a.clearUploadQueue,
	})
}

// queueDialog is a queue's window.
func (a *app) queueDialog(c *ui.Context, q *queueWindow, entries []queueEntry, waiting, failed int, spec queueSpec) {
	if !q.open {
		return
	}
	ink := a.inkOn(false)
	w, h := c.Size()
	active := 0
	if len(entries) > 0 && entries[0].active {
		active = 1
	}
	sub := fmt.Sprintf("正在%s %d 首 · 等待 %d 首", spec.verb, active, waiting)
	if active == 0 {
		sub = fmt.Sprintf("等待%s %d 首", spec.verb, waiting)
	}
	if failed > 0 {
		sub += fmt.Sprintf(" · 失败 %d 首", failed)
	}
	if len(entries) == 0 {
		sub = "当前没有" + spec.verb + "任务"
	}
	a.dialog(c, spec.id, 0.35, &q.open, func(panel ui.Element) {
		// As tall as what it lists, up to what the window has room for:
		// three songs are not lost in a tall empty pane.
		tall := min(560, h-80)
		panel.Width(min(680, w-80)).Height(min(tall, max(300, 170+float32(len(entries))*66))).TextColor(ink.text)
		a.pane(panel, 26, false)
		panel.Children(func() {
			dialogHeader(c, ink, spec.title, sub, func() { q.open = false })
			fill, edge := a.cardColors()
			list := ui.Column(c).Grow(1).Shrink(1).MinHeight(0).Margin(0, 28).Radius(20).Background(fill).Border(0.5, edge).Clip()
			list.Children(func() {
				if len(entries) == 0 {
					ui.Column(c).Grow(1).Center().Gap(8).Children(func() {
						ui.Icon(c, spec.icon).Size(26, 26).TextColor(ink.faint)
						ui.Text(c, spec.title+"是空的").FontSize(13).TextColor(ink.muted)
					})
					return
				}
				ui.List(c.Key(spec.id+"-list"), &q.list, len(entries), func(i int) {
					a.queueRow(c, q, &spec, entries[i], i)
				}).Grow(1).Padding(6)
			})
			chosen := q.chosen(entries)
			dialogFooter(c, func() {
				ui.Textf(c, "共 %d 项，已选 %d 项", len(entries), chosen).FontSize(12).TextColor(ink.muted).Grow(1)
				if spec.add != nil && settingButton(c.Key(spec.keys+"-add"), ink, "添加音乐", spec.icon, false, false).Clicked() {
					spec.add()
				}
				if failed > 0 && settingButton(c.Key(spec.keys+"-retry"), ink, "全部重试", iconReload, false, false).Clicked() {
					spec.retryAll()
				}
				label := "全选"
				if q.all(entries) {
					label = "取消全选"
				}
				if settingButton(c.Key(spec.keys+"-all"), ink, label, nil, len(entries) == 0, false).Clicked() {
					q.selectAll(entries)
					a.redraw()
				}
				if settingButton(c.Key(spec.keys+"-cancel"), ink, spec.cancelChosen, nil, chosen == 0, true).Clicked() {
					spec.cancel()
				}
				if settingButton(c.Key(spec.keys+"-clear"), ink, "全部取消", nil, len(entries) == 0, true).Clicked() {
					spec.clear()
				}
			})
		})
	})
}

func (a *app) queueRow(c *ui.Context, q *queueWindow, spec *queueSpec, e queueEntry, i int) {
	pal, ink := a.pal, a.inkOn(false)
	selected := q.selected[e.key]
	row := ui.CheckboxBase(c.Key(i), &selected).Height(62).PaddingX(12).Gap(12).Radius(14).
		AlignItems(ui.Center).FocusRing(false).Label(e.title)
	bg := ui.Transparent
	if selected {
		bg = pal.accent.Alpha(0.12)
	} else if row.Hovered() {
		bg = pal.hover
	}
	row.Background(row.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond}))
	row.Children(func() {
		a.batchCheck(c, selected)
		if e.cover != "" {
			a.cover(c, e.cover, 96, 36, 8).Shrink(0)
		} else {
			ui.Row(c).Size(36, 36).Shrink(0).Radius(8).Background(pal.press).Center().Children(func() {
				ui.Icon(c, iconNote).Size(16, 16).TextColor(ink.faint)
			})
		}
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
			ui.Text(c, e.title).FontSize(13).FontWeight(600).SingleLine()
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, e.sub).FontSize(11.5).TextColor(ink.muted).SingleLine().Grow(1).Shrink(1).MinWidth(0)
				status, color := fmt.Sprintf("等待中 · 第 %d 位", e.pos), ink.faint
				switch {
				case e.failed != "":
					status, color = spec.verb+"失败 · "+e.failed, pal.accent
				case e.active && spec.preparing && e.done == 0:
					status = "正在准备…"
				case e.active:
					status = "正在" + spec.verb + " · " + sizeText(e.done)
					if e.size > 0 {
						status += " / " + sizeText(e.size)
					}
				}
				ui.Text(c, status).FontSize(10.5).TextColor(color).SingleLine().Shrink(1).MinWidth(0).MaxWidth(260)
			})
			if e.active {
				share := float32(0)
				if e.size > 0 {
					share = min(float32(e.done)/float32(e.size), 1)
				}
				bar := ui.Box(c).Grow(1).MinWidth(40).Height(4).Radius(4).Background(pal.press)
				bar.Draw(func(p *ui.Painter, r ui.Rect) {
					if share > 0 {
						p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * share, H: r.H}, pal.accent, 4)
					}
				})
			}
		})
		if e.failed != "" && iconButton(c.Key("retry"), iconReload, 13, ink.muted, "重新"+spec.verb).Clicked() {
			spec.retry(e.key)
		}
		label := "移出" + spec.noun
		switch {
		case e.active:
			label = "取消" + spec.verb
		case e.failed != "":
			label = "不再" + spec.verb
		}
		if iconButton(c.Key("remove"), iconX, 13, ink.muted, label).Clicked() {
			spec.remove(e.key)
		}
	})
	if row.Changed() {
		q.toggle(e.key)
		a.redraw()
	}
}
