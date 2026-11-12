package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// rowOfTrack is the row of trackList the song shown at i is at.
func (a *app) rowOfTrack(i int) int {
	if i >= 0 && i < len(a.trackRow) {
		return a.trackRow[i]
	}
	return i + 1 // the list's first row is the header
}

// albumTitle heads an album's songs in a list sorted by album: its cover,
// its name, which opens it when NetEase knows it, and what it holds.
func (a *app) albumTitle(c *ui.Context, g albumGroup, first bool) {
	p := a.pal
	top := float32(22)
	if first {
		top = 8
	}
	ui.Row(c.Key(fmt.Sprint("album-title-", g.start))).Padding(top, 12, 8, 12).Gap(12).AlignItems(ui.Center).Children(func() {
		a.cover(c, g.cover, 96, 40, 8)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
			title := ui.RichText(c).FontSize(15).FontWeight(700).TextColor(p.text).SingleLine()
			title.Children(func() {
				switch {
				case g.id > 0:
					a.link(c.Key("album"), g.name, p.text, p.accent, route{page: pageAlbum, id: g.id})
				case g.artistID > 0:
					a.link(c.Key("artist"), g.name, p.text, p.accent, route{page: pageArtist, id: g.artistID})
				default:
					ui.Text(c, g.name)
				}
			})
			ui.Text(c, fmt.Sprintf("%s · %d 首 · %s", g.artist, g.n, albumLength(g.length))).
				FontSize(12).TextColor(p.muted).SingleLine()
		})
	})
}

// albumLength says how long an album's songs in a list are, as its title
// does: minutes, or hours and minutes.
func albumLength(d time.Duration) string {
	m := int(d.Round(time.Minute) / time.Minute)
	if m < 60 {
		return fmt.Sprintf("%d 分钟", max(m, 1))
	}
	return fmt.Sprintf("%d 小时 %d 分钟", m/60, m%60)
}

// albumIndexButton opens the albums of a list sorted by album, which a
// search narrows: the one chosen is all the list shows, until 全部专辑.
func (a *app) albumIndexButton(c *ui.Context, v *plView) {
	p := a.pal
	chosen := ""
	if len(v.groups) == 1 && v.album != "" && v.groups[0].key == v.album {
		chosen = v.groups[0].name
	}
	b := ui.ButtonBase(c.Key("album-index")).Height(32).PaddingX(12).Gap(6).Radius(999).AlignItems(ui.Center).FocusRing(false).
		Label(indexName(v)).MaxWidth(240)
	bg := p.hover
	switch {
	case chosen != "":
		bg = p.accent.Alpha(0.14)
	case b.Hovered():
		bg = p.press
	}
	b.Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 100 * time.Millisecond}))
	b.Children(func() {
		ui.Icon(c, iconAlbum).Size(13, 13).TextColor(p.muted).Shrink(0)
		if chosen != "" {
			ui.Text(c, chosen).FontSize(12.5).SingleLine().Shrink(1).MinWidth(0)
		} else {
			ui.Text(c, fmt.Sprintf("%s %d", indexName(v), len(v.all))).FontSize(12.5).SingleLine()
		}
	})
	if chosen != "" && iconButton(c.Key("album-all"), iconX, 11, p.muted, "显示全部").Size(24, 24).Clicked() {
		a.chooseAlbum(v, "")
	}
	if b.Clicked() {
		v.indexOpen = !v.indexOpen
		v.indexQuery = ""
	}
	ui.PopoverBase(c.Key("album-index-pop"), b, &v.indexOpen, func(panel ui.Element) {
		ink := a.inkOn(false)
		a.pane(panel, 20, false)
		panel.AttachTo(b, ui.AnchorBottomEnd, ui.AnchorTopEnd).Margin(6, 0, 0, 0).Width(320).Padding(6).Gap(4).
			TextColor(ink.text).Label(indexName(v)).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.96, Y: -4}, Exit: &ui.Motion{Scale: 0.98}})
		field := ui.Row(c).Height(32).PaddingX(10).Gap(7).Radius(14).AlignItems(ui.Center).Background(ink.hover)
		field.Children(func() {
			ui.Icon(c, iconSearch).Size(13, 13).TextColor(ink.muted)
			ui.TextInputBase(c.Key("album-search"), &v.indexQuery).AutoFocus().Grow(1).MinWidth(0).
				Placeholder(indexSearch(v)).FontSize(12.5).Label(indexSearch(v))
			if v.indexQuery != "" && iconButton(c.Key("album-search-clear"), iconX, 11, ink.muted, "清除搜索").Size(20, 20).Clicked() {
				v.indexQuery = ""
			}
		})
		q := strings.ToLower(strings.TrimSpace(v.indexQuery))
		ui.Scroll(c.Key("album-index-list")).MaxHeight(max(min(menuTallest, a.winHeight(c)-200), 150)).Gap(1).Children(func() {
			if q == "" {
				all, unit := "全部专辑", "张"
				if v.sort == sortArtist {
					all, unit = "全部歌手", "位"
				}
				if a.albumRow(c, ink, all, fmt.Sprintf("%d %s", len(v.all), unit), "", v.album == "", true).Clicked() {
					a.chooseAlbum(v, "")
				}
			}
			found := 0
			for _, g := range v.all {
				if q != "" && !strings.Contains(strings.ToLower(g.name+" "+g.artist), q) {
					continue
				}
				found++
				if a.albumRow(c.Key(g.key), ink, g.name, fmt.Sprintf("%s · %d 首", g.artist, g.n), g.cover, g.key == v.album, false).Clicked() {
					a.chooseAlbum(v, g.key)
				}
			}
			if found == 0 {
				ui.Text(c, "没有找到").FontSize(12.5).TextColor(ink.muted).Padding(14, 10)
			}
		})
	})
}

// indexName names the index of a list's groups: of its albums or artists.
func indexName(v *plView) string {
	if v.sort == sortArtist {
		return "歌手目录"
	}
	return "专辑目录"
}

// indexSearch is what the search of the index finds.
func indexSearch(v *plView) string {
	if v.sort == sortArtist {
		return "搜索歌手"
	}
	return "搜索专辑或歌手"
}

// albumRow is an album in the album index: its cover, name and what it
// holds, checked when it is the one shown.
func (a *app) albumRow(c *ui.Context, ink paneInk, name, note, cover string, checked, all bool) ui.Element {
	b := ui.ButtonBase(c.Key(name)).Label(name).Height(46).PaddingX(8).Gap(10).Radius(14).AlignItems(ui.Center).FocusRing(false)
	bg := ui.Transparent
	switch {
	case b.Pressed():
		bg = ink.press
	case b.Hovered() || b.FocusVisible():
		bg = ink.hover
	}
	b.Background(bg)
	b.Children(func() {
		if all {
			ui.Box(c).Size(32, 32).Radius(8).Background(ink.hover).Center().Children(func() {
				ui.Icon(c, iconAlbum).Size(15, 15).TextColor(ink.muted)
			})
		} else {
			a.cover(c, cover, 64, 32, 8)
		}
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, name).FontSize(13.5).FontWeight(500).SingleLine()
			ui.Text(c, note).FontSize(11.5).TextColor(ink.muted).SingleLine()
		})
		if checked {
			ui.Icon(c, iconCheck).Size(15, 15).Shrink(0).TextColor(a.pal.accent)
		}
	})
	return b
}

// chooseAlbum keeps the list to the album of key, or "" to all of them,
// from its top.
func (a *app) chooseAlbum(v *plView, key string) {
	v.album, v.indexOpen = key, false
	a.trackList.ScrollTo(0, ui.Start)
	a.redraw()
}

// winHeight is how tall the window is.
func (a *app) winHeight(c *ui.Context) float32 {
	_, h := c.Size()
	return h
}
