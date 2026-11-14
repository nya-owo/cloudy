package app

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// The albums of a list of songs sorted by album, a playlist's, the local
// music's, the downloads', those played lately…, can show as a grid of
// their covers, as the albums of the listener's own page do, rather than
// as one long list (settings.AlbumGrid). A cover opens its album's songs,
// the list kept to it, and the chip of the album index goes back to the
// grid. Switching keeps the albums in view where they were, and the new
// view comes in over a moment rather than at once.

// albumGridShown reports whether the songs of this page show as a grid of
// their albums now: sorted by album, no album chosen, and the listener
// asked for it.
func (a *app) albumGridShown() bool {
	v := &a.plView
	return a.settings.AlbumGrid && a.albumGridPage() && v.sort == sortAlbum && v.album == "" && len(v.all) > 1 && !a.rowsBatchActive()
}

// albumGridPage reports whether this page can show its albums as a grid:
// any list of songs but an album's own.
func (a *app) albumGridPage() bool {
	return a.route.page != pageAlbum
}

// switchView shows the albums as a grid, or their songs as a list, keeping
// the album at the top of the view at the top of the other.
func (a *app) switchView(grid bool) {
	v := &a.plView
	first, _ := a.trackList.Visible()
	row := 0
	cols := max(1, a.more.gridCols)
	if grid {
		// From the list: the album of the row at the top, as a line of cards.
		if first >= 1 {
			g := 0
			for k, r := range a.groupRow {
				if r <= first {
					g = k
				}
			}
			row = 1 + g/cols
		}
	} else if first >= 1 && len(v.groups) > 0 {
		// From the grid: the first album of the line at the top, by its title.
		g := min((first-1)*cols, len(v.groups)-1)
		row = 1 + v.groups[g].start + g
	}
	a.settings.AlbumGrid = grid
	a.changed()
	a.more.switchedAt = time.Now()
	if row > 0 {
		a.trackList.ScrollTo(row, ui.Start)
	}
	a.redraw()
}

// slideIn slides in a row of the list just switched to, as a page of a
// switch does: the grid from the right, where its button is, and the songs
// from the left, fading in as they come.
func (a *app) slideIn(c *ui.Context, e ui.Element, grid bool) {
	if a.motion == motionOff || c.Now().Sub(a.more.switchedAt) > 600*time.Millisecond {
		return
	}
	dx := float32(56)
	if !grid {
		dx = -56
	}
	e.Transition(ui.ElementTransition{Animation: &viewSlide, Enter: &ui.Motion{X: dx}})
}

// viewSlide is how the rows slide in as the view switches.
var viewSlide = ui.Animation{Spring: ui.SpringOptions{Duration: 420 * time.Millisecond, Bounce: 0.08}}

// albumViewSwitch switches the albums of a list sorted by album between the
// list and the grid: two icons in a track, the one chosen raised on a
// capsule that slides to the other as it changes, as the segmented
// controls of the settings do.
func (a *app) albumViewSwitch(c *ui.Context) {
	p := a.pal
	const seg, segH, inset = 32, 26, 3
	chosen := float32(b2f(a.settings.AlbumGrid))
	track := ui.Row(c.Key("album-view")).Height(segH + 2*inset).Padding(inset).Radius(999).Background(p.hover).AlignItems(ui.Center)
	at := track.AnimateTo("at", chosen, ui.Animation{Spring: ui.SpringOptions{Duration: 280 * time.Millisecond, Bounce: 0.08}})
	capsule, shade := p.bg, p.cardShade
	track.Draw(func(pt *ui.Painter, r ui.Rect) {
		pill := ui.Rect{X: r.X + inset + at*seg, Y: r.Y + inset, W: seg, H: segH}
		pt.Shadow(pill, segH/2, 0, 1, 3, 0, shade)
		pt.Fill(pill, capsule, segH/2)
	})
	track.Children(func() {
		for i, opt := range []struct {
			icon  *ui.SVG
			label string
			grid  bool
		}{{iconViewList, "列表", false}, {iconViewGrid, "网格", true}} {
			on := a.settings.AlbumGrid == opt.grid
			b := ui.ButtonBase(c.Key(i)).Size(seg, segH).Radius(999).Center().FocusRing(false).Label(opt.label + "视图").Tooltip(opt.label)
			// Lit as the capsule comes under it.
			k := 1 - min(1, abs32(at-float32(i)))
			col := p.muted.Mix(p.text, k)
			if b.Hovered() && !on {
				col = p.text
			}
			b.Children(func() { ui.Icon(c, opt.icon).Size(14, 14).TextColor(col) })
			if b.Clicked() && !on {
				a.switchView(opt.grid)
			}
		}
	})
}

// albumGridLine is a line of the grid of albums: the cards of groups from
// line×cols on.
func (a *app) albumGridLine(c *ui.Context, line, cols int, groups []albumGroup, tracks []netease.Track) {
	ui.Grid(c.Key(line)).Columns(cols).GapX(20).Padding(0, 12, 28, 12).Children(func() {
		for k := line * cols; k < min(len(groups), (line+1)*cols); k++ {
			a.albumGroupCard(c.Key(groups[k].key), groups[k], tracks)
		}
	})
}

// albumGroupCard is an album of the listener's songs as a card: its cover,
// lifting under the pointer, a button over it playing the album, and its
// name and what it holds. A click shows its songs.
func (a *app) albumGroupCard(c *ui.Context, g albumGroup, tracks []netease.Track) ui.Element {
	p := a.pal
	card := ui.ButtonBase(c).Column().Gap(10).AlignItems(ui.Stretch).FocusRing(false).Label(g.name)
	hovered := card.Hovered()
	card.Children(func() {
		art := ui.Box(c).AspectRatio(1).Radius(14)
		lift := ui.Pose{}
		if hovered {
			lift = ui.Pose{Y: -3}
			art.Shadow(0, 12, 28, -6, p.cardShade)
		} else {
			art.Shadow(0, 2, 8, -2, p.cardShade)
		}
		art.AnimatePose(lift, ui.Snappy)
		art.Children(func() {
			if g.cover != "" {
				a.coverFill(c, g.cover, 400, 14)
			} else {
				ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(14).Background(p.hover).Center().Children(func() {
					ui.Icon(c, iconAlbum).Size(40, 40).TextColor(p.muted)
				})
			}
			a.coverPlayButton(c, hovered, func() {
				if g.start+g.n <= len(tracks) {
					a.playList(tracks[g.start:g.start+g.n], false)
				}
			})
		})
		ui.Text(c, g.name).FontSize(13).FontWeight(500).MaxLines(2).LineHeight(1.35)
		ui.Text(c, fmt.Sprintf("%s · %d 首", g.artist, g.n)).FontSize(12).TextColor(p.muted).SingleLine().Margin(-6, 0, 0, 0)
	})
	if card.Clicked() {
		a.chooseAlbum(&a.plView, g.key)
	}
	return card
}

var (
	iconViewList = strokeIcon(`<path d="M8 6h13"/><path d="M8 12h13"/><path d="M8 18h13"/><path d="M3.5 6h.01"/><path d="M3.5 12h.01"/><path d="M3.5 18h.01"/>`)
	iconViewGrid = strokeIcon(`<rect x="3.5" y="3.5" width="7" height="7" rx="1.8"/><rect x="13.5" y="3.5" width="7" height="7" rx="1.8"/><rect x="3.5" y="13.5" width="7" height="7" rx="1.8"/><rect x="13.5" y="13.5" width="7" height="7" rx="1.8"/>`)
)
