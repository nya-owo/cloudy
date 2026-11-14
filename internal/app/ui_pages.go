package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

const pagePad = 40

// columns returns how many cards of at least min DIPs fit across the page.
func (a *app) columns(c *ui.Context, min float32) int {
	w, _ := c.Size()
	inner := w - a.sideWidth(w) - 2*pagePad
	return max(2, int((inner+20)/(min+20)))
}

func (a *app) heading(c *ui.Context, title, sub string) {
	ui.Column(c).Gap(4).Margin(0, 0, 18, 0).Children(func() {
		if sub != "" {
			ui.Text(c, sub).FontSize(12).FontWeight(600).TextColor(a.pal.muted).LetterSpacing(0.4)
		}
		ui.Text(c, title).FontSize(30).FontWeight(700).LetterSpacing(-0.6)
	})
}

func (a *app) section(c *ui.Context, title string) ui.Element {
	return ui.Text(c, title).FontSize(19).FontWeight(700).LetterSpacing(-0.3).Margin(34, 0, 14, 0)
}

// sectionLarge is a section's title where the page turns on it, as the FM
// page does on heart mode and on roaming a style: set larger than the
// sections that only list things.
func (a *app) sectionLarge(c *ui.Context, title string) ui.Element {
	return ui.Text(c, title).FontSize(24).FontWeight(700).LetterSpacing(-0.4).Margin(38, 0, 16, 0)
}

func (a *app) homePage(c *ui.Context) {
	loading := len(a.featured) == 0 && len(a.charts) == 0
	if loading && !a.homeLoading {
		a.centered(c, a.homeErr, a.loadHome)
		return
	}
	now := c.Now()
	top, bottom := a.insets(c)
	ui.Scroll(c.Key("home")).TrackScroll(&a.pageScroll).Grow(1).Padding(top+pageGap, pagePad, bottom+48, pagePad).ScrollbarInsets(top, 0, bottom, 0).Children(func() {
		a.startMark(c)
		defer a.endMark(c)
		hello := greeting(now)
		if a.profile != nil {
			hello += "，" + a.profile.Nickname
		}
		a.heading(c, hello, fmt.Sprintf("%d月%d日 %s", now.Month(), now.Day(), weekday(now)))
		if loading {
			a.homeSkeleton(c, a.pageShimmer(c))
			return
		}
		a.dailyCard(c, now)

		if len(a.recPlaylists) > 0 {
			a.section(c, "今日为你推荐")
			a.recommendedGrid(c)
		}
		a.radarSection(c)

		a.section(c, "官方榜单")
		official := a.charts[:min(4, len(a.charts))]
		cols := min(4, max(2, a.columns(c, 260)))
		ui.Grid(c).Columns(cols).Gap(20).Children(func() {
			for i := range official {
				a.chartCard(c.Key(official[i].ID), &official[i])
			}
		})

		a.recommendedPlaylists(c)
	})
}

func weekday(t time.Time) string {
	return [...]string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}[t.Weekday()]
}

func (a *app) chartsPage(c *ui.Context) {
	loading := len(a.charts) == 0
	if loading && !a.homeLoading {
		a.centered(c, a.homeErr, a.loadHome)
		return
	}
	top, bottom := a.insets(c)
	ui.Scroll(c.Key("charts")).TrackScroll(&a.pageScroll).Grow(1).Padding(top+pageGap, pagePad, bottom+48, pagePad).ScrollbarInsets(top, 0, bottom, 0).Children(func() {
		a.startMark(c)
		defer a.endMark(c)
		a.heading(c, "排行榜", "每日更新")
		if loading {
			s := a.pageShimmer(c)
			a.chartCardsSkeleton(c, s)
			a.section(c, "全部榜单")
			a.playlistCardsSkeleton(c, s, a.columns(c, 150)*2, 150, true)
			return
		}
		official := a.charts[:min(4, len(a.charts))]
		cols := min(4, max(2, a.columns(c, 260)))
		ui.Grid(c).Columns(cols).Gap(20).Children(func() {
			for i := range official {
				a.chartCard(c.Key(official[i].ID), &official[i])
			}
		})
		if len(a.charts) > 4 {
			a.section(c, "全部榜单")
			rest := a.charts[4:]
			ui.Grid(c).Columns(a.columns(c, 150)).GapX(20).GapY(28).Children(func() {
				for i := range rest {
					a.playlistCard(c.Key(rest[i].ID), &rest[i])
				}
			})
		}
	})
}

// playlistCard is a square cover with the playlist's name below: it lifts
// under the pointer, where a play button shows on it.
func (a *app) playlistCard(c *ui.Context, pl *netease.Playlist) {
	if a.coverCard(c, pl.Name, pl.Cover(), pl.PlayCount, pl.UpdateFreq, func() { a.loadPlaylist(pl.ID, true) }) {
		a.open(route{page: pagePlaylist, id: pl.ID})
	}
}

// coverCard is a playlist's card, or a podcast's: its cover, with how many
// times it was played on it, over its name and a line under that. The
// button on the cover calls play; it reports whether the rest was clicked.
func (a *app) coverCard(c *ui.Context, name, cover string, playCount float64, sub string, play func()) bool {
	_, clicked := a.coverCardOf(c, name, cover, playCount, sub, play)
	return clicked
}

// coverCardOf is coverCard, with the card itself, for a menu of its own.
func (a *app) coverCardOf(c *ui.Context, name, cover string, playCount float64, sub string, play func()) (ui.Element, bool) {
	p := a.pal
	// From the top: a grid stretches the cards of a row as tall as the
	// tallest, whose name or line under it takes two lines.
	card := ui.ButtonBase(c).Column().Gap(10).AlignItems(ui.Stretch).Justify(ui.Start).FocusRing(false).Label(name)
	hovered := card.Hovered()
	var artAt ui.Rect
	card.Children(func() {
		art := ui.Box(c).AspectRatio(1).Radius(14)
		artAt = art.Bounds()
		a.landBack(c, art, cover)
		lift := ui.Pose{}
		if hovered {
			lift = ui.Pose{Y: -5, Scale: 1.015}
			art.Shadow(0, 12, 28, -6, p.cardShade)
		} else {
			art.Shadow(0, 2, 8, -2, p.cardShade)
		}
		art.AnimatePose(lift, ui.Snappy)
		art.Children(func() {
			a.coverFill(c, cover, 400, 14)
			if playCount > 0 {
				ui.Row(c).Absolute().Top(8).Right(8).Padding(2, 7).Radius(999).Gap(3).AlignItems(ui.Center).
					Background(ui.RGBA(0, 0, 0, 0.35)).TextColor(ui.RGB(255, 255, 255)).Children(func() {
					ui.Icon(c, iconHeadset).Size(10, 10)
					ui.Text(c, plays(playCount)).FontSize(10.5).FontWeight(600)
				})
			}
			a.coverPlayButton(c, hovered, play)
		})
		ui.Text(c, name).FontSize(13).FontWeight(500).MaxLines(2).LineHeight(1.35)
		if sub != "" {
			ui.Text(c, sub).FontSize(12).TextColor(p.muted).SingleLine().Margin(-6, 0, 0, 0)
		}
	})
	clicked := card.Clicked()
	if clicked {
		a.flyFrom(c, cover, artAt, 14)
	}
	return card, clicked
}

// coverFill fills its parent with a cover, its corners rounded by radius.
// The cover rounds itself rather than its parent clipping it: a clip moved
// by a transform, as a card lifting under the pointer, is drawn on the CPU.
func (a *app) coverFill(c *ui.Context, url string, px int, radius float32) {
	bmp := bitmap(a.art.get(netease.Sized(url, px), false))
	box := ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(radius)
	in := artIn(box, bmp)
	if in < 1 {
		box.Background(a.pal.hover)
	}
	box.Children(func() { artImage(c, bmp, radius, in) })
}

// coverPlayButton plays a playlist or a podcast from its card, without
// opening it.
func (a *app) coverPlayButton(c *ui.Context, shown bool, play func()) {
	b := ui.ButtonBase(c.Key("play")).Absolute().Bottom(10).Right(10).Size(38, 38).Radius(999).Center().
		TextColor(ui.RGB(255, 255, 255)).Label("播放").FocusRing(false).
		Shadow(0, 4, 12, 0, ui.RGBA(0, 0, 0, 0.3))
	b.Background(a.pal.accent)
	pose := ui.Pose{Fade: 1, Scale: 0.8, Y: 6}
	if shown || b.Hovered() {
		pose = ui.Pose{Scale: 1}
		if b.Hovered() {
			pose.Scale = 1.08
		}
	}
	b.AnimatePose(pose, ui.Snappy)
	b.Children(func() { ui.Icon(c, iconPlay).Size(15, 15).Margin(0, 0, 0, 2) })
	if b.Clicked() {
		play()
	}
}

// chartCard shows a chart with its top three songs beside its cover.
func (a *app) chartCard(c *ui.Context, pl *netease.Playlist) {
	p := a.pal
	card := ui.ButtonBase(c).Gap(14).Padding(12).Radius(14).AlignItems(ui.Center).FocusRing(false).Label(pl.Name)
	hovered := card.Hovered()
	bg := p.hover
	if hovered {
		bg = p.press
	}
	card.Background(card.AnimateColor("bg", bg, ui.Animation{Duration: 140 * time.Millisecond}))
	var artAt ui.Rect
	card.Children(func() {
		art := ui.Box(c).Size(84, 84).Radius(8).Shadow(0, 3, 10, -2, p.cardShade)
		artAt = art.Bounds()
		a.landBack(c, art, pl.Cover())
		art.Children(func() { a.coverFill(c, pl.Cover(), 200, 8) })
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(5).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Baseline).Children(func() {
				ui.Text(c, pl.Name).FontSize(14).FontWeight(700).SingleLine()
				ui.Text(c, pl.UpdateFreq).FontSize(11).TextColor(p.faint).SingleLine()
			})
			for i, line := range pl.Preview {
				ui.Row(c).Gap(7).Children(func() {
					ui.Textf(c, "%d", i+1).FontSize(12).FontWeight(700).Width(10).FontFeatures("tnum").TextColor(p.muted)
					ui.RichText(c,
						ui.Span{Text: line.First, Weight: 500},
						ui.Span{Text: "  " + line.Second, Color: p.muted},
					).FontSize(12).SingleLine().Shrink(1).MinWidth(0)
				})
			}
		})
	})
	if card.Clicked() {
		a.flyFrom(c, pl.Cover(), artAt, 8)
		a.open(route{page: pagePlaylist, id: pl.ID})
	}
}

func (a *app) playlistPage(c *ui.Context) {
	id := a.route.id
	pl := a.playlists[id]
	if pl == nil {
		if !a.loadingPl[id] {
			a.centered(c, a.plErr[id], func() { a.loadPlaylist(id, false) })
			return
		}
		s := a.pageShimmer(c)
		a.trackSkeleton(c, s, true, func(c *ui.Context) { a.playlistHeaderSkeleton(c, s) })
		return
	}
	a.toolRows(c, pl.Tracks, true, func(c *ui.Context) { a.playlistHeader(c, pl) })
}

func (a *app) playlistHeader(c *ui.Context, pl *netease.Playlist) {
	p := a.pal
	head := ui.Row(c).Gap(28).AlignItems(ui.End).Padding(0, 0, 26, 0)
	head.Children(func() {
		a.headCover(c, pl.Cover())
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Children(func() {
			kind := "歌单"
			if pl.UpdateFreq != "" {
				kind = "榜单 · " + pl.UpdateFreq
			}
			ui.Text(c, kind).FontSize(12).FontWeight(600).TextColor(p.accent)
			ui.Text(c, pl.Name).FontSize(30).FontWeight(800).LetterSpacing(-0.6).MaxLines(2).LineHeight(1.2)
			var meta []string
			if pl.Creator.Nickname != "" {
				meta = append(meta, pl.Creator.Nickname)
			}
			if pl.PlayCount > 0 {
				meta = append(meta, plays(pl.PlayCount)+" 次播放")
			}
			ui.Text(c, strings.Join(meta, " · ")).FontSize(13).TextColor(p.muted)
			if d := strings.TrimSpace(pl.Description); d != "" {
				a.playlistSays(c, strings.ReplaceAll(d, "\n", " "))
			}
			if a.batchActive(pl.ID) {
				hint := "选择歌曲，完成后在下方选择下载、喜欢、下一首播放或加入队列"
				if pl.SpecialType == 5 {
					hint = "选择歌曲，完成后在下方选择下载、取消喜欢或加入队列"
				}
				ui.Text(c, hint).FontSize(12.5).TextColor(p.muted).Margin(10, 0, 0, 0)
			} else {
				ui.Row(c).Gap(10).Margin(10, 0, 0, 0).Children(func() {
					if pillButton(c.Key("play"), iconPlay, "播放", p.accent, p.onAccent).Clicked() {
						a.playList(pl.Tracks, false)
					}
					if pillButton(c.Key("shuffle"), iconShuffle, "随机播放", p.hover.Over(p.bg), p.text).Clicked() {
						a.playList(pl.Tracks, true)
					}
					if a.savable(pl) {
						icon, label := iconPlus, "收藏"
						if a.isSaved(pl) {
							icon, label = iconCheck, "已收藏"
						}
						if pillButton(c.Key("save"), icon, label, p.hover.Over(p.bg), p.text).Clicked() {
							a.toggleSaved(*pl)
						}
					}
					switch {
					case pl.SpecialType != 5:
					case a.pl.Radio() == heartRadio:
						if pillButton(c.Key("heart"), iconX, "退出心动模式", p.hover.Over(p.bg), p.text).Clicked() {
							a.exitRadio()
						}
					default:
						label := "心动模式"
						if a.radioStarting == heartRadio {
							label = "正在开启…"
						}
						if pillButton(c.Key("heart"), iconHeartFill, label, p.hover.Over(p.bg), p.text).Clicked() {
							a.startHeartMode()
						}
					}
					// One ⋯, with what the listener can do to the playlist and
					// what there is to know of it, rather than two side by side.
					// The listener's own playlists have them too: they were
					// left with renaming and deleting alone.
					mine := a.editable(pl.ID)
					details := a.savable(pl) || pl.SpecialType == 5 || mine != nil
					if details || mine != nil {
						label := "歌单详情"
						if mine != nil {
							label = "更多"
						}
						more := iconButton(c.Key("more"), iconMore, 18, p.muted, label)
						if more.Clicked() {
							var groups [][]menuAction
							var head func(*ui.Context, paneInk)
							if mine != nil {
								groups = append(groups, a.playlistMenu(mine)...)
							}
							if details {
								groups = append(groups, a.playlistDetailsMenu(pl)...)
								head = a.playlistDetailsHead(pl)
							}
							a.menuBelow(more, false, groups, head)
						}
					}
				})
			}
		})
		a.coverStack(c, pl)
	})
}

// trackRows lists tracks under a header, which scrolls with them; a click
// plays the list from the track clicked.
func (a *app) trackRows(c *ui.Context, tracks []netease.Track, covers bool, header func(c *ui.Context), footer ...func(c *ui.Context)) {
	a.trackRowsGrouped(c, tracks, nil, covers, header, footer...)
}

// trackRowsGrouped lists tracks as trackRows does, each of groups, the
// albums of a list sorted by album, under a title of its own.
func (a *app) trackRowsGrouped(c *ui.Context, tracks []netease.Track, groups []albumGroup, covers bool, header func(c *ui.Context), footer ...func(c *ui.Context)) {
	p := a.pal
	// What each row of the list is, after the header: a song, by where it
	// is in tracks, or the title of an album, as -1 - which.
	items := make([]int, 0, len(tracks)+len(groups))
	a.trackRow = a.trackRow[:0]
	a.groupRow = a.groupRow[:0]
	for g, i := 0, 0; i < len(tracks); i++ {
		if g < len(groups) && groups[g].start == i {
			a.groupRow = append(a.groupRow, len(items)+1)
			items = append(items, -1-g)
			g++
		}
		a.trackRow = append(a.trackRow, len(items)+1)
		items = append(items, i)
	}
	a.shownTracks = tracks
	batch := a.rowsBatchActive()
	cur := a.pl.Current()
	// A song's menu can take it out of the playlist it is listed in.
	var pid int64
	if a.route.page == pagePlaylist {
		pid = a.route.id
	}
	w, _ := c.Size()
	// An album's page has no column of the album its songs are on.
	wide := w-a.sideWidth(w) > 820 && a.route.page != pageAlbum
	top, bottom := a.insets(c)
	listBottom := bottom
	if batch {
		listBottom += 64
	}
	// Rows out of view are not built, their marks with them: a list
	// scrolled away from its start or end has content under the bars.
	a.underTop, a.underBottom = true, true
	a.listOpened(c)
	rows := len(items) + 1 + len(footer)
	// Sorted by album, the albums may show as a grid of covers in place of
	// the songs, under the same header (ui_album_grid.go).
	grid := a.albumGridShown() && len(groups) > 1
	cols, lines := 1, 0
	if grid {
		cols = a.columns(c, 156)
		a.more.gridCols = cols
		lines = (len(groups) + cols - 1) / cols
		rows = 1 + lines + len(footer)
	}
	// Stable song identities let a row carry its animation to a new slot.
	// Keep the viewport offset while neighboring rows close a removed gap.
	moveRows := !grid && a.route.page == pagePlaylist && a.isLikedPlaylist(a.route.id)
	var positions []float32
	if moveRows {
		positions = a.movingTrackRows(tracks, items, groups, rows)
	} else {
		a.trackList.Key = nil
		a.trackList.KeepOffset = false
		a.trackLayout = trackRowLayout{}
	}
	rowAt := func(i int) {
		if i == 0 {
			ui.Column(c).Children(func() {
				a.startMark(c)
				header(c)
			})
			return
		}
		if i == rows-1 {
			defer a.endMark(c)
		}
		if i > len(items) {
			footer[i-len(items)-1](c)
			return
		}
		slot := i - 1
		i = items[slot]
		if i < 0 {
			a.albumTitle(c, groups[-1-i], -1-i == 0)
			return
		}
		tr := &tracks[i]
		playing := cur != nil && cur.ID == tr.ID
		selected := batch && a.batch.selected[tr.ID]
		row := ui.ButtonBase(c.Key(tr.ID)).Height(54).PaddingX(12).Gap(14).Radius(14).AlignItems(ui.Center).FocusRing(false).Label(tr.Name)
		shift := float32(0)
		if moveRows {
			shift = a.trackRowShift(row, positions[slot])
		}
		if !batch {
			a.songContext(row, tr, pid)
		}
		hovered := row.Hovered()
		bg := ui.Transparent
		switch {
		case selected:
			bg = p.accent.Alpha(0.14)
		case playing:
			bg = p.accent.Alpha(0.09)
		case hovered:
			bg = p.hover
			if tint, ok := a.rowTint(tr.Album.PicURL); ok {
				bg = tint.Alpha(0.16)
			}
		}
		row.Background(row.AnimateColor("bg", bg, ui.Animation{Duration: 140 * time.Millisecond}))
		if playing && !batch {
			a.rowGlow(row)
		}
		shown := a.rowIn(c, i)
		if shown < 1 {
			shift += 10 * (1 - shown)
		}
		row.Translate(0, shift)
		if !tr.Playable() {
			shown *= 0.42
		}
		if shown < 1 {
			row.Opacity(shown)
		}
		row.Children(func() {
			ui.Box(c).Width(26).Center().Children(func() {
				if batch {
					a.batchCheck(c, selected)
					return
				}
				switch {
				case playing:
					a.nowPlayingBars(c)
				case hovered && tr.Playable():
					ui.Icon(c, iconPlay).Size(13, 13).TextColor(p.text)
				default:
					ui.Textf(c, "%d", i+1).FontSize(13).FontFeatures("tnum").TextColor(p.faint)
				}
			})
			if covers {
				grown := float32(1)
				if hovered {
					grown = 1.08
				}
				a.cover(c, tr.Album.PicURL, 96, 38, 8).AnimatePose(ui.Pose{Scale: grown}, ui.Snappy)
			}
			ui.Column(c).Grow(3).Basis(0).MinWidth(0).Gap(3).Children(func() {
				name := p.text
				if playing {
					name = p.accent
				}
				spans := []ui.Span{{Text: tr.Name, Weight: 600, Color: name}}
				if len(tr.Alias) > 0 {
					spans = append(spans, ui.Span{Text: "（" + tr.Alias[0] + "）", Color: p.faint})
				}
				ui.RichText(c, spans...).FontSize(14).SingleLine()
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					if !tr.Playable() {
						ui.Text(c, "VIP").FontSize(9).FontWeight(800).TextColor(p.accent).Padding(0, 4).Radius(3).Border(1, p.accent.Alpha(0.6))
					}
					if batch {
						ui.Text(c, tr.ArtistNames()).FontSize(12.5).TextColor(p.muted).SingleLine().Shrink(1).MinWidth(0)
					} else {
						a.artistLinks(c, tr, p.muted, p.text).FontSize(12.5).SingleLine().Shrink(1).MinWidth(0)
					}
					if tr.Reason != "" {
						ui.Text(c, tr.Reason).FontSize(10.5).FontWeight(600).TextColor(p.accent).Padding(1, 6).Radius(999).Background(p.accent.Alpha(0.1)).SingleLine()
					}
				})
			})
			if wide {
				// As quiet as the artists under the title, a column of the
				// list rather than a word floating in it; brighter as the row
				// is looked at.
				shade := p.muted.Alpha(0.85)
				if hovered {
					shade = p.text.Alpha(0.85)
				}
				album := ui.RichText(c).Grow(2).Basis(0).MinWidth(0).FontSize(12.5).TextColor(shade).SingleLine()
				album.Children(func() {
					if batch || tr.Album.ID == 0 {
						ui.Text(c, tr.Album.Name)
					} else {
						a.link(c.Key("album"), tr.Album.Name, shade, p.text, route{page: pageAlbum, id: tr.Album.ID})
					}
				})
			}
			// A liked song's heart shows; the others' as the pointer is on them.
			if tr.LocalPath == "" && !batch {
				ui.Box(c).Size(32, 32).Center().Children(func() {
					if a.liked[tr.ID] || hovered {
						a.likeButton(c.Key("like"), tr, 15, p.muted)
					}
				})
			}
			ui.Text(c, clock(tr.Length())).FontSize(12.5).FontFeatures("tnum").TextColor(p.faint).Width(44).TextAlign(ui.End)
		})
		if row.Clicked() {
			switch {
			case batch:
				a.toggleBatch(tr.ID)
			case tr.Playable():
				a.pl.PlayQueue(tracks, i)
			}
		}
	}
	// The header stays as the rows under it switch between the songs and
	// the grid, those coming sliding in from the side of their button.
	ui.List(c.Key(a.route), &a.trackList, rows, func(i int) {
		if i == 0 {
			rowAt(0)
			return
		}
		view := ui.Column(c.Key(listView{grid}))
		a.slideIn(c, view, grid)
		view.Children(func() {
			if !grid {
				rowAt(i)
				return
			}
			if i == rows-1 {
				defer a.endMark(c)
			}
			if i > lines {
				footer[i-lines-1](c)
				return
			}
			a.albumGridLine(c, i-1, cols, groups, tracks)
		})
	}).Grow(1).Padding(top+pageGap, pagePad-12, listBottom+32, pagePad-12).ScrollbarInsets(top, 0, listBottom, 0)
}

// listView keys the rows of a list apart as songs or as a grid of albums.
type listView struct{ grid bool }

// nowPlayingBars dance in the row of the track playing.
func (a *app) nowPlayingBars(c *ui.Context) {
	color := a.pal.accent
	ui.Box(c).Size(16, 14).Draw(func(p *ui.Painter, r ui.Rect) {
		lv, live := a.pl.Levels()
		a.rowBars.step(p.Now(), lv, live)
		drawBars(p, r, &a.rowBars, 4, color, 2.5)
		if live || a.rowBars.group(0, player.Bands) > 0.01 {
			p.AnimationFrame()
		}
	})
}
