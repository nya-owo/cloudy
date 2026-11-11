package app

import (
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// The albums the listener saved, which the sidebar lists under their
// playlists, and which the album's page saves and unsaves. Tests replace
// the two calls.

var (
	subscribedAlbums = netease.SubscribedAlbums
	subscribeAlbum   = netease.SubscribeAlbum
)

// loadMyAlbums loads the albums the listener saved.
func (a *app) loadMyAlbums() {
	gen := a.accountGen
	a.myAlbumsBusy, a.myAlbumsErr = true, ""
	go func() {
		albums, err := subscribedAlbums()
		a.post(func() {
			if gen != a.accountGen {
				return
			}
			a.myAlbumsBusy = false
			if err != nil {
				a.myAlbumsErr = "专辑加载失败"
				return
			}
			a.myAlbums, a.myAlbumsErr = albums, ""
		})
	}()
}

// albumSorts are the orders the saved albums may be listed in.
var albumSorts = []struct{ id, label string }{
	{"time", "按收藏时间"},
	{"name", "按专辑名"},
	{"artist", "按艺人"},
}

// albumSort returns the order chosen, the time for the settings kept before
// there was one.
func (a *app) albumSort() string {
	switch a.settings.AlbumSort {
	case "name", "artist":
		return a.settings.AlbumSort
	}
	return "time"
}

// sortedAlbums returns the saved albums in the order chosen.
func (a *app) sortedAlbums() []netease.AlbumProfile {
	albums := slices.Clone(a.myAlbums)
	switch a.albumSort() {
	case "name":
		slices.SortStableFunc(albums, func(x, y netease.AlbumProfile) int {
			return strings.Compare(strings.ToLower(x.Name), strings.ToLower(y.Name))
		})
	case "artist":
		slices.SortStableFunc(albums, func(x, y netease.AlbumProfile) int {
			return strings.Compare(strings.ToLower(x.ArtistNames()), strings.ToLower(y.ArtistNames()))
		})
	default:
		slices.SortStableFunc(albums, func(x, y netease.AlbumProfile) int {
			switch {
			case x.SubTime == y.SubTime:
				return 0
			case x.SubTime > y.SubTime:
				return -1
			}
			return 1
		})
	}
	return albums
}

// albumSaved reports whether the listener saved the album id.
func (a *app) albumSaved(id int64) bool {
	return slices.ContainsFunc(a.myAlbums, func(al netease.AlbumProfile) bool { return al.ID == id })
}

// toggleAlbum saves the album for the listener, or unsaves it.
func (a *app) toggleAlbum(al netease.AlbumProfile) {
	if a.profile == nil {
		a.showMain()
		a.openLogin()
		return
	}
	saved := a.albumSaved(al.ID)
	gen := a.accountGen
	go func() {
		err := subscribeAlbum(al.ID, !saved)
		a.post(func() { a.albumToggled(gen, al, saved, err) })
	}()
}

// albumToggled applies what NetEase said of saving al, or of unsaving it.
func (a *app) albumToggled(gen int, al netease.AlbumProfile, saved bool, err error) {
	if gen != a.accountGen {
		return
	}
	if err != nil {
		a.toast = "操作失败，请稍后再试"
		a.redraw()
		return
	}
	a.setAlbumSaved(al, !saved)
}

// setAlbumSaved adds the album to those saved, at their head, or takes it
// out of them.
func (a *app) setAlbumSaved(al netease.AlbumProfile, saved bool) {
	if saved {
		if a.albumSaved(al.ID) {
			return
		}
		al.SubTime = time.Now().UnixMilli()
		a.myAlbums = slices.Insert(a.myAlbums, 0, al)
	} else {
		a.myAlbums = slices.DeleteFunc(a.myAlbums, func(x netease.AlbumProfile) bool { return x.ID == al.ID })
	}
	a.redraw()
}

// myAlbumsSection lists the albums the listener saved, a row each as their
// playlists, with a menu of how to order them.
func (a *app) myAlbumsSection(c *ui.Context) {
	if a.profile == nil {
		return
	}
	if len(a.myAlbums) == 0 && !a.myAlbumsBusy {
		return
	}
	p := a.pal
	const title = "我的专辑"
	ui.Row(c.Key(title)).AlignItems(ui.Center).Margin(18, 0, 6, 0).Children(func() {
		b := ui.ButtonBase(c.Key("toggle")).Grow(1).Height(28).PaddingX(10).Gap(6).Radius(8).
			AlignItems(ui.Center).Justify(ui.Start).Role(ui.RoleDisclosure).Label(title)
		if b.Clicked() {
			a.albumsCollapsed = !a.albumsCollapsed
		}
		b.Expanded(!a.albumsCollapsed)
		if b.Hovered() {
			b.Background(p.hover)
		}
		b.Children(func() {
			ui.Text(c, title).FontSize(a.side().title).FontWeight(600).TextColor(p.faint).Grow(1)
			turn := float32(0)
			if a.albumsCollapsed {
				turn = -90
			}
			ui.Icon(c, iconDown).Size(12, 12).TextColor(p.faint).Rotate(b.AnimateTo("turn", turn, motionFast))
		})
		more := iconButton(c.Key("album-sort"), iconMore, 13, p.faint, "专辑排序").Size(26, 26).Radius(8)
		if more.Clicked() {
			a.menuBelow(more, false, a.albumSortMenu(), nil)
		}
	})
	if a.albumsCollapsed {
		return
	}
	rows := ui.Column(c.Key(title + "-rows")).Gap(4)
	if a.motion > motionOff {
		rows.Transition(ui.ElementTransition{Animation: &motionPane, Enter: &ui.Motion{Collapse: true}, Exit: &ui.Motion{Collapse: true}})
	}
	rows.Children(func() {
		albums := a.sortedAlbums()
		shown := a.sideShown(len(albums))
		a.albumRows(c, albums[:shown])
		if shown < len(albums) {
			a.sideMore(c, len(albums), mineAlbums)
		}
	})
}

// albumSortMenu is the menu of the orders, and of loading the albums again.
func (a *app) albumSortMenu() [][]menuAction {
	var orders []menuAction
	for _, s := range albumSorts {
		id := s.id
		orders = append(orders, menuAction{
			label:   s.label,
			checked: a.albumSort() == id,
			do: func() {
				a.settings.AlbumSort = id
				a.changed()
			},
		})
	}
	return [][]menuAction{orders, {{label: "重新同步", do: a.loadMyAlbums}}}
}

// albumRows lists saved albums in the sidebar.
func (a *app) albumRows(c *ui.Context, albums []netease.AlbumProfile) {
	p := a.pal
	if len(albums) == 0 {
		label := "还没有收藏专辑"
		if a.myAlbumsErr != "" {
			label = a.myAlbumsErr
		}
		ui.Text(c, label).FontSize(12).TextColor(p.faint).Padding(6, 10, 6, 10)
		return
	}
	for i := range albums {
		al := &albums[i]
		selected := a.route.page == pageAlbum && a.route.id == al.ID && !a.nowPlaying
		d := a.side()
		b := ui.ButtonBase(c.Key(al.ID)).Height(d.row).Radius(8).FocusRing(false).Label(al.Name)
		if b.Hovered() && !selected {
			b.Background(p.hover)
		}
		a.navRowContent(c, b, selected, 8, func() {
			a.cover(c, al.PicURL, 96, d.cover, d.coverRadius)
			label := ui.Text(c, al.Name).FontSize(d.text).TextColor(p.text).SingleLine().Shrink(1).MinWidth(0)
			if selected {
				label.FontWeight(600)
			}
		})
		a.contextMenu(b, false, func() [][]menuAction {
			return [][]menuAction{{
				{label: "取消收藏", do: func() { a.toggleAlbum(*al) }},
			}}
		}, nil)
		if b.Clicked() {
			a.open(route{page: pageAlbum, id: al.ID})
		}
	}
}
