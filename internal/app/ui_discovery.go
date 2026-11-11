package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

var playlistFilters = [...]struct {
	label, category string
	quality         bool
}{
	{"推荐", "", false},
	{"精品", "", true},
	{"华语", "华语", false},
	{"R&B", "R&B/Soul", false},
	{"轻音乐", "轻音乐", false},
	{"电子", "电子", false},
	{"学习", "学习", false},
	{"运动", "运动", false},
}

var (
	categoryPlaylists = netease.CategoryPlaylists
	qualityPlaylists  = netease.HighQualityPlaylists
)

type playlistBrowseData struct {
	lists   []netease.Playlist
	loading bool
	err     string
}

// playlistFilterMenu keeps all eight destinations on their own line.
func (a *app) playlistFilterMenu(c *ui.Context) {
	p := a.pal
	chosen := a.playlistFilter
	ui.ScrollHorizontal(c.Key("playlist-filters")).Height(38).Shrink(0).Margin(0, 0, 18, 0).Children(func() {
		row := ui.Row(c).Height(34).Gap(8).AlignItems(ui.Center)
		x, width, height := a.playlistPill.X, a.playlistPill.W, a.playlistPill.H
		if a.motion == motionFull {
			move := ui.Animation{Spring: ui.SpringOptions{Duration: 320 * time.Millisecond}}
			x = row.AnimateTo("pill-x", x, move)
			width = row.AnimateTo("pill-width", width, move)
		}
		tint := ui.RGBA(255, 255, 255, 0.82)
		if p.dark {
			tint = ui.RGBA(255, 255, 255, 0.16)
		}
		// Paint the flat selection tint under the labels.
		row.Draw(func(painter *ui.Painter, r ui.Rect) {
			if width <= 0 || height <= 0 {
				return
			}
			pill := ui.Rect{X: r.X + x, Y: r.Y + (r.H-height)/2, W: width, H: height}
			painter.Shadow(pill, height/2, 0, 2, 6, -2, p.cardShade)
			painter.Fill(pill, tint, height/2)
		})
		row.Children(func() {
			for i, filter := range playlistFilters {
				selected := chosen == i
				button := ui.ButtonBase(c.Key(i)).Height(34).PaddingX(15).Radius(999).
					Center().Label("歌单分类：" + filter.label).Selected(selected)
				bg, fg := ui.Transparent, p.muted
				if selected {
					fg = p.text
				} else if button.Hovered() {
					bg, fg = p.hover, p.text
				}
				button.Background(button.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond}))
				button.Children(func() {
					ui.Text(c, filter.label).FontSize(13.5).FontWeight(600).TextColor(fg).SingleLine()
				})
				if selected {
					b, r := button.LayoutBounds(), row.LayoutBounds()
					if at := (ui.Rect{X: b.X - r.X, W: b.W, H: b.H}); at != a.playlistPill && b.W > 0 {
						a.playlistPill = at
						c.AnimationFrame()
					}
				}
				if button.Clicked() {
					a.choosePlaylistFilter(i)
				}
			}
		})
	})
}

func (a *app) choosePlaylistFilter(filter int) {
	a.playlistFilter = filter
	a.loadPlaylistFilter(filter)
}

// loadPlaylistFilter caches each result separately, including an empty result or error.
func (a *app) loadPlaylistFilter(filter int) {
	if filter == 0 || a.filteredPlaylists[filter] != nil {
		return
	}
	data := &playlistBrowseData{loading: true}
	a.filteredPlaylists[filter] = data
	gen := a.accountGen
	pick := playlistFilters[filter]
	go func() {
		var lists []netease.Playlist
		var err error
		if pick.quality {
			lists, err = qualityPlaylists(30)
		} else {
			lists, err = categoryPlaylists(pick.category, 30)
		}
		a.post(func() { a.playlistFilterAnswered(gen, filter, data, lists, err) })
	}()
}

func (a *app) playlistFilterAnswered(gen, filter int, data *playlistBrowseData, lists []netease.Playlist, err error) {
	if gen != a.accountGen || a.filteredPlaylists[filter] != data {
		return
	}
	data.loading, data.lists = false, lists
	if err != nil {
		data.err = "歌单加载失败，请重试"
	}
}

// recommendedPlaylists changes only the grid below the category menu.
func (a *app) recommendedPlaylists(c *ui.Context) {
	a.section(c, "推荐歌单")
	a.playlistFilterMenu(c)
	filter := a.playlistFilter
	lists := a.featured
	if filter == 0 && len(lists) == 0 && a.homeLoading {
		a.playlistCardsSkeleton(c.Key("category-loading"), a.pageShimmer(c), a.columns(c, 168)*2, 168, false)
		return
	}
	if filter != 0 {
		a.loadPlaylistFilter(filter)
		data := a.filteredPlaylists[filter]
		if data.loading {
			a.playlistCardsSkeleton(c.Key("category-loading"), a.pageShimmer(c), a.columns(c, 168)*2, 168, false)
			return
		}
		if data.err != "" || len(data.lists) == 0 {
			text := data.err
			if text == "" {
				text = "这个分类暂时没有歌单"
			}
			ui.Row(c.Key("category-status")).Gap(14).Padding(18, 0).AlignItems(ui.Center).Children(func() {
				ui.Text(c, text).FontSize(13).TextColor(a.pal.muted)
				if ui.Button(c.Key("retry"), "重新加载").Clicked() {
					a.filteredPlaylists[filter] = nil
					a.loadPlaylistFilter(filter)
				}
			})
			return
		}
		lists = data.lists
	}
	ui.Grid(c.Key(filter)).Columns(a.columns(c, 168)).GapX(20).GapY(28).Children(func() {
		for i := range lists {
			a.playlistCard(c.Key(lists[i].ID), &lists[i])
		}
	})
}
