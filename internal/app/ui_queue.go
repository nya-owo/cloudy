package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"
)

// queueLook is how the rows of the queue are colored: white over Now
// Playing, the pane's ink in the bar's popover.
type queueLook struct {
	text, head, artist, time ui.Color
	on, hover, pill          ui.Color
	dark                     bool // its menus go over dark
}

// queueRows lists the songs to play, in the order they will, the one
// playing lit: a click plays one, a drag moves it, and the ✕ takes it out.
// list scrolls them; at is the row it last brought into view, -1 while
// they were not shown, for them to open at the song playing.
func (a *app) queueRows(c *ui.Context, list *ui.ListState, at *int, look queueLook, rowH, coverSize float32) {
	pl := a.pl
	order, cur := pl.PlayOrder()
	// Opened, it shows the track playing at its top; after, it follows it
	// as little as keeps it in view.
	if cur >= 0 && cur != *at {
		if *at < 0 {
			list.ScrollTo(cur, ui.Start)
		} else {
			list.ScrollIntoView(cur)
		}
		*at = cur
	}
	queue := pl.Queue()
	// Keyed by the song: removing one shifts the indices of those after it.
	list.Key = func(row int) any { return queue[order[row]].ID }
	// Dragged, the rows move in the play order; the song playing plays on.
	list.Reorder = func(rows []int, to int) { pl.Reorder(rows, to) }
	if len(order) > 1 {
		ui.Row(c.Key("queue-head")).Height(36).PaddingX(10).Gap(10).AlignItems(ui.Center).Children(func() {
			left := len(order) - max(cur, 0) - 1
			ui.Text(c, fmt.Sprintf("共 %d 首 · 接下来 %d 首 · 拖动可调整顺序", len(order), left)).FontSize(12).TextColor(look.head).Grow(1).MinWidth(0).SingleLine()
			if left > 0 && pillButton(c.Key("clear-queue"), iconTrash, "清空待播", look.pill, look.text).Clicked() {
				pl.ClearUpcoming()
				a.toast = "已清空待播清单"
			}
		})
	}
	ui.List(c.Key("queue"), list, len(order), func(r int) {
		i := order[r]
		tr := &queue[i]
		playing := r == cur
		row := ui.ButtonBase(c.Key(tr.ID)).Height(rowH).PaddingX(10).Gap(14).Radius(14).AlignItems(ui.Center).FocusRing(false).Label(tr.Name)
		a.contextMenu(row, look.dark, func() [][]menuAction {
			acts := a.songActions(tr, 0)
			if !playing {
				acts = append([][]menuAction{{{label: "从待播清单移除", icon: iconX, do: func() { pl.RemoveAt(r) }}}}, acts...)
			}
			return acts
		}, a.songHead(tr))
		switch {
		case playing:
			row.Background(look.on)
		case row.Hovered():
			row.Background(look.hover)
		}
		if !tr.Playable() {
			row.Opacity(0.4)
		}
		row.Children(func() {
			a.cover(c, tr.Album.PicURL, 96, coverSize, 8)
			ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
				name := ui.Text(c, tr.Name).FontSize(14).FontWeight(600).SingleLine()
				if playing && !look.dark {
					name.TextColor(a.pal.accent)
				}
				a.artistLinks(c, tr, look.artist, look.text).FontSize(12.5).SingleLine()
			})
			ui.Text(c, clock(tr.Length())).FontSize(12).FontFeatures("tnum").TextColor(look.time)
			// The song playing stays: there would be nothing in its place.
			if !playing {
				reveal(ui.Box(c.Key("remove-reveal")), row.Hovered()).Children(func() {
					if iconButton(c.Key("remove"), iconX, 12, look.artist, "从待播清单移除").Size(26, 26).Clicked() {
						pl.RemoveAt(r)
					}
				})
			}
		})
		if row.Clicked() && tr.Playable() && !playing {
			pl.Jump(i)
		}
	}).Grow(1).Padding(8, 0, 40, 0).Gap(2)
}

// queueButton opens the queue from the player bar, in a pane over the
// page: the songs to play, the one playing in view.
func (a *app) queueButton(c *ui.Context, color ui.Color) {
	q := &a.more.queue
	tint := color
	if q.open {
		tint = a.pal.accent
	}
	b := iconButton(c.Key("queue-button"), iconQueue, 16, tint, "播放列表")
	if b.Clicked() {
		q.open = !q.open
	}
	if !q.open {
		q.at = -1
	}
	ui.PopoverBase(c.Key("queue-popover"), b, &q.open, func(panel ui.Element) {
		ink := a.inkOn(false)
		a.pane(panel, 20, false)
		h := a.winHeight(c)
		panel.AttachTo(b, ui.AnchorTopEnd, ui.AnchorBottomEnd).Margin(0, 0, 12, 0).Width(400).Height(max(240, min(600, h-160))).
			Padding(10, 8, 0, 8).TextColor(ink.text).Label("播放列表").TransformOrigin(1, 1)
		a.paneMotion(panel)
		panel.Children(func() {
			ui.Row(c).PaddingX(10).Height(30).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "播放列表").FontSize(15).FontWeight(700).Grow(1)
				if iconButton(c.Key("queue-close"), iconX, 12, ink.muted, "关闭").Size(26, 26).Clicked() {
					q.open = false
				}
			})
			if len(a.pl.Queue()) == 0 {
				ui.Column(c).Grow(1).Center().Children(func() {
					ui.Text(c, "还没有要播放的歌曲").FontSize(13).TextColor(ink.muted)
				})
				return
			}
			a.queueRows(c, &q.list, &q.at, queueLook{
				text: ink.text, head: ink.muted, artist: ink.muted, time: ink.faint,
				on: a.pal.accent.Alpha(0.12), hover: ink.hover, pill: ink.hover,
			}, 52, 38)
		})
	})
}
