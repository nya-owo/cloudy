package app

import (
	"runtime"
	"time"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// sidebarWidth is the sidebar's width until the listener drags its edge.
const sidebarWidth = 232

func (a *app) view(c *ui.Context) {
	t := *c.Theme()
	a.pal = paletteFor(&t)
	if accent := a.chosenAccent(); accent != "" {
		// The color the listener chose: the buttons take it, and the window
		// and the sidebar a breath of it.
		a.pal = themeAccent(a.pal, accent)
	}
	a.motion = a.motionLevel(c)
	a.followCover()
	if accent, ok := a.coverAccent(); ok && a.settings.AccentCover {
		a.pal.accent = accent
	}
	p := a.pal
	t.Accent, t.AccentText = p.accent, p.onAccent
	if accent, ok := a.coverAccent(); ok && a.settingsView.open {
		// The settings' switches take the color of what plays, a little
		// quieter, not to outshine the rest of the pane.
		t.Accent = accent.Mix(ui.RGB(128, 128, 128), 0.22)
	}
	t.Selection = p.accent.Alpha(0.3)
	t.Font = a.fontFamily()
	c.SetTheme(&t)

	a.shortcuts(c)
	a.syncThumbs()
	// The bar says where its button is, if it shows it this frame.
	a.deskBtn = ui.Rect{}

	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}
	root := ui.Box(c).Fill()
	if !c.Focused(a.searchBox) && !c.Focused(a.filterBox) && !c.Focused(a.dl.box) && !c.Focused(a.mineView.box) && !c.Focused(a.settingsView.searchBox) {
		// Space plays and pauses wherever the focus is, as in Music. On
		// the container around everything, it comes before a button the
		// pointer focused, which would take Space as a click; on the
		// window, it would come after.
		root.OnShortcut(0, ui.KeySpace, a.pl.TogglePlay)
		// And where nothing has the focus, as once Now Playing covers the
		// button that had it.
		c.OnShortcut(0, ui.KeySpace, a.pl.TogglePlay)
	}
	npOpen, npSettled := a.npState(c)
	root.Children(func() {
		page := ui.Row(c).Fill().AlignItems(ui.Stretch)
		page.Children(func() {
			if npOpen && npSettled {
				// Covered by Now Playing: not built, which would cost each
				// of its frames as much again as the stage itself. Where
				// it is scrolled is the app's (pageScroll, navScroll,
				// trackList), so it shows again as it was.
				return
			}
			winW, _ := c.Size()
			side := ui.Column(c).Width(a.sideWidth(winW))
			switch {
			case !c.Vibrancy():
				a.sideColor = side.AnimateColor("tint", a.sidebarColor(), ui.Animation{Duration: 700 * time.Millisecond})
				side.Background(a.sideColor)
				a.sideEdge = a.sideColor
			case a.chosenAccent() != "":
				// The window's material shows through the sidebar, which has
				// no color of its own: lay the color over it, thin enough
				// that the material still shows, so the color covers the
				// sidebar too.
				a.sideColor = side.AnimateColor("tint", a.sidebarColor(), ui.Animation{Duration: 700 * time.Millisecond})
				side.Background(a.sideColor.Alpha(0.55))
			}
			side.Children(func() {
				a.sidebar(c)
				a.sidebarResizer(c, winW)
			})
			// Keyed, so that a popover of the sidebar opening or closing
			// keeps what the page and its bars hold, their edges' fades.
			ui.Box(c.Key("main")).Grow(1).Background(p.bg).Children(func() {
				a.content(c)
				// The bars float over the page, which scrolls under them:
				// positioned, they paint over it.
				a.pageTop(c)
				a.playerBar(c)
				a.batchBar(c)
			})
		})
		a.coverFlightView(c)
		a.nowPlayingLayer(c, npOpen, npSettled)
		a.splash(c)
	})
	a.loginDialog(c)
	a.playlistNameDialog(c)
	a.stylePickerDialog(c)
	a.deletePlaylistDialog(c)
	a.downloadDeleteDialog(c)
	a.settingsPanel(c)
	a.accentDialog(c)
	a.updateDialog(c)
	a.shortcutEditorDialog(c)
	a.fontPickerDialog(c)
	a.effectsDialog(c)
	a.menuOverlay(c)
	a.downloadQueueDialog(c)
	a.uploadQueueDialog(c)
	a.paneToasts(c)
	if a.toast != "" {
		c.Toast(a.toast)
		a.toast = ""
	}
	a.syncWindows()
}

func (a *app) shortcuts(c *ui.Context) {
	// The side buttons of a mouse, the keys of keyboards that have them
	// and swipes between pages, and the keys of Music.
	c.OnShortcut(0, ui.KeyBack, a.back)
	c.OnShortcut(0, ui.KeyForward, a.goForward)
	c.OnShortcut(ui.Cmd, ui.KeyBracketLeft, a.back)
	c.OnShortcut(ui.Cmd, ui.KeyBracketRight, a.goForward)
	// As the global key, for when those are off.
	c.OnShortcut(ui.Cmd|ui.Alt, ui.KeyV, a.likeCurrent)
	c.OnShortcut(ui.Cmd, ui.KeyF, func() {
		a.nowPlaying = false
		a.searchBox.Focus(c)
	})
	if !a.nowPlaying {
		a.setNpFull(false)
	}
	if a.nowPlaying {
		c.OnShortcut(0, ui.KeyF11, func() { a.setNpFull(!a.npFull) })
		c.OnShortcut(0, ui.KeyEscape, func() {
			if a.npFull {
				a.setNpFull(false)
			} else {
				a.nowPlaying = false
			}
		})
		// ⌘ and a number choose Now Playing's tab of that place.
		if a.pl.Current() != nil {
			for i, t := range a.npTabs() {
				c.OnShortcut(ui.Cmd, ui.Key1+ui.Key(i), func() { a.choosePane(t.pane) })
			}
		}
	}
}

// go opens a page, remembering the one before for the back button, and
// closes Now Playing over it, though it is the page open.
func (a *app) open(r route) {
	a.nowPlaying = false
	if r == a.route {
		return
	}
	a.history, a.forward = append(a.history, a.route), nil
	a.show(r)
}

// show shows the page of r.
func (a *app) show(r route) {
	if a.batch.on {
		keep := false
		switch {
		case a.batch.songs:
			keep = r == a.batch.at
		case a.batch.downloads:
			keep = r.page == pageDownloads
		default:
			keep = r.page == pagePlaylist && r.id == a.batch.playlist
		}
		if !keep {
			a.batch = batchSelection{}
		}
	}
	a.pageDir = pageLevel(r) - pageLevel(a.route)
	a.route = r
	if r.page == pageSearch {
		a.query, a.searched = r.query, r
	}
	a.load(r)
}

// load loads what the page of r shows, unless it has.
func (a *app) load(r route) {
	switch r.page {
	case pageHome:
		a.freshenHome()
	case pagePlaylist:
		a.loadPlaylist(r.id, false)
	case pageSearch:
		a.loadSearch(r, false)
	case pageArtist:
		a.loadArtist(r.id)
	case pageAlbum:
		a.loadAlbum(r.id)
	case pageUser:
		a.loadUser(r.id)
	case pagePodcast:
		a.loadPodcast(r.id)
	case pagePodcasts:
		a.loadPodcastTab()
	case pageDownloads:
		a.loadDownloads(false)
	case pageRecent:
		a.recent.sure = false
	case pageCloud:
		a.loadCloud(false)
	case pageStyle:
		if !a.style.loaded {
			a.loadStyle()
		}
	}
}

// back goes to the page before, as the back button of a mouse does, or
// closes Now Playing, which covers the pages.
func (a *app) back() {
	if a.nowPlaying {
		a.nowPlaying = false
		return
	}
	if len(a.history) == 0 {
		return
	}
	a.forward = append(a.forward, a.route)
	r := a.history[len(a.history)-1]
	a.history = a.history[:len(a.history)-1]
	a.flyBack(r)
	a.show(r)
	a.pageDir = -1
}

// goForward goes to the page back left, as the forward button of a mouse
// does.
func (a *app) goForward() {
	if len(a.forward) == 0 {
		return
	}
	a.history = append(a.history, a.route)
	r := a.forward[len(a.forward)-1]
	a.forward = a.forward[:len(a.forward)-1]
	a.nowPlaying = false
	a.show(r)
	a.pageDir = 1
}

func (a *app) sidebar(c *ui.Context) {
	// Under the window buttons where they sit over the sidebar, as the
	// traffic lights do; elsewhere, as close to the top as to the sides.
	top := float32(14)
	if c.TitleBar().Left > 0 {
		top = topBar(c) + 4
	}
	// Where nothing in it takes the press, the sidebar moves the window, as
	// the title bar it reaches up into would.
	ui.Column(c).Fill().Padding(top, 14, 14, 14).Gap(4).DragWindow().Children(func() {
		a.searchField(c)

		nav := ui.Box(c.Key("nav-box")).Grow(1).MinHeight(0).Margin(0, -6)
		nav.Children(func() {
			ui.Scroll(c.Key("nav")).TrackScroll(&a.navScroll).Fill().Padding(0, 6).Gap(4).Children(func() {
				a.navItem(c, iconHome, "为你推荐", route{page: pageHome})
				a.navItem(c, iconRadio, "私人 FM", route{page: pageFM})
				a.navItem(c, iconCharts, "排行榜", route{page: pageCharts})
				a.navItem(c, iconPodcast, "播客", route{page: pagePodcasts})
				a.navItem(c, iconDownload, "下载歌曲", route{page: pageDownloads})
				a.navItem(c, iconHistory, "最近播放", route{page: pageRecent})
				if a.profile != nil {
					a.navItem(c, iconCloud, "我的音乐云盘", route{page: pageCloud})
				}
				if a.searched.query != "" {
					a.navItem(c, iconSearch, "搜索结果", a.searched)
				}
				a.myPlaylists(c, "我的歌单", a.mine, true, &a.mineCollapsed)
				a.myPlaylists(c, "收藏的歌单", a.saved, false, &a.savedCollapsed)
				a.myAlbumsSection(c)
			})
			a.scrollFades(c, &a.navScroll)
		})

		// What plays, as a card at the foot of the sidebar, over who listens.
		// Not with the bar of glass, which says as much, and more, beside it.
		if tr := a.pl.Current(); tr != nil && !a.settings.LiquidGlass {
			a.sidebarNowPlaying(c, tr)
		}
		a.accountRow(c)
	})
}

func (a *app) navItem(c *ui.Context, icon *ui.SVG, label string, r route) {
	p := a.pal
	selected := a.route.page == r.page && !a.nowPlaying
	d := a.side()
	b := ui.ButtonBase(c.Key(label)).Height(d.row).PaddingX(10).Gap(10).Radius(8).AlignItems(ui.Center).Justify(ui.Start).FocusRing(false)
	bg := ui.Transparent
	if b.Hovered() && !selected {
		bg = p.hover
	}
	b.Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond}))
	color := p.text
	if selected {
		color = p.accent
	}
	b.Children(func() {
		if selected {
			a.navPill(c)
		}
		ui.Icon(c, icon).Size(d.icon, d.icon).TextColor(color)
		ui.Text(c, label).FontSize(d.navText).FontWeight(500).TextColor(p.text)
	})
	if b.Clicked() {
		a.open(r)
	}
}

func (a *app) sidebarNowPlaying(c *ui.Context, tr *netease.Track) {
	p := a.pal
	card := ui.ButtonBase(c.Key("np-card")).Gap(10).Padding(8).Radius(14).AlignItems(ui.Center).FocusRing(false).Label("正在播放")
	bg := p.hover
	if card.Hovered() {
		bg = p.press
	}
	card.Background(card.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond}))
	card.Children(func() {
		a.cover(c, tr.Album.PicURL, 96, 40, 8).Shadow(0, 2, 6, 0, p.cardShade)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, tr.Name).FontSize(12.5).FontWeight(600).SingleLine()
			a.artistLinks(c, tr, p.muted, p.text).FontSize(11.5).SingleLine()
		})
	})
	if card.Clicked() {
		a.nowPlaying = true
	}
}

// playerBarHeight is the height of the player along the foot of the pages.
const playerBarHeight = 84

// pageGap is the room between the bar over a page and its content.
const pageGap = 12

// insets returns the room the bars over a page take at its top and its
// bottom: pages pad their content by it, and scroll under the bars.
func (a *app) insets(c *ui.Context) (top, bottom float32) {
	top = topBar(c)
	if runtime.GOOS == "darwin" {
		// The edge reaches a little below the traffic lights' strip.
		top += topEdgePad
	}
	return top, playerBarHeight
}

// topEdgePad is the room the edge over a page leaves below the strip, on
// macOS.
const topEdgePad = 6

// buttonsHeight is the height of the window buttons off macOS, which fill
// the edge over a page.
const buttonsHeight = topBarHeight + topEdgePad

// topBar returns the height of the strip along the top of the window, as
// tall as the window buttons and the room above and below them, which
// what sits in it centers on.
func topBar(c *ui.Context) float32 {
	if runtime.GOOS != "darwin" {
		// The whole edge, which the buttons fill: what sits in the strip
		// centers on their glyphs.
		return buttonsHeight
	}
	return max(c.TitleBar().Height, topBarHeight)
}

// topBarHeight is the least height of the strip along the top.
const topBarHeight = 40

// startMark marks where a page's content starts, first in what scrolls:
// once it has moved up, content is under the bar at the top.
func (a *app) startMark(c *ui.Context) {
	b := ui.Box(c.Key("start-mark")).Height(0).Bounds()
	a.headGone = 0
	if b.W > 0 {
		top, _ := a.insets(c)
		a.underTop = b.Y < top+pageGap-0.5
		a.headGone = top + pageGap - b.Y
	}
}

// endMark marks where a page's content ends, last in what scrolls: while it
// is below the top of the player, content is under the player.
func (a *app) endMark(c *ui.Context) {
	b := ui.Box(c.Key("end-mark")).Height(0).Bounds()
	if b.W > 0 {
		_, h := c.Size()
		_, bottom := a.insets(c)
		a.underBottom = b.Y > h-bottom+0.5
	}
}

// edgeShown returns how much of a scroll edge shows over el: fully while
// content is under its bar, and none at rest, as AppKit shows its edges.
func edgeShown(el ui.Element, under bool) float32 {
	target := float32(0)
	if under {
		target = 1
	}
	return el.AnimateTo("shown", target, ui.Animation{Duration: 180 * time.Millisecond})
}

// pageTop is the strip over the top of a page, what scrolls under it frosted
// as macOS 26's hard scroll edge does, or, beside a bar of glass that floats,
// fading away from it, as the soft one does, with no line across the window:
// it drags the window, and holds the back button once there is a page to go
// back to.
func (a *app) pageTop(c *ui.Context) {
	top, _ := a.insets(c)
	if a.settings.LiquidGlass {
		edge := ui.Box(c.Key("top-edge")).Absolute().Top(0).Left(0).Right(0).Height(top + 24).PassThrough().
			Material(glass.ScrollEdge{Background: a.pal.bg})
		edge.Opacity(edgeShown(edge, a.underTop))
		ui.Box(c.Key("top-drag")).Absolute().Top(0).Left(0).Right(0).Height(top).DragWindow()
	} else {
		edge := ui.Box(c.Key("top-edge")).Absolute().Top(0).Left(0).Right(0).Height(top).DragWindow().
			Material(glass.ScrollEdge{Hard: true, Background: a.pal.bg})
		edge.Opacity(edgeShown(edge, a.underTop))
	}
	if len(a.history) > 0 || len(a.forward) > 0 {
		ui.Row(c).Absolute().Top(0).Left(20).Height(topBar(c)).AlignItems(ui.Center).Children(func() {
			back := iconButton(c.Key("back"), iconBack, 18, a.pal.muted, "返回").Disabled(len(a.history) == 0)
			if len(a.history) == 0 {
				back.Opacity(0.35)
			}
			if back.Clicked() {
				a.back()
			}
			if len(a.forward) > 0 && iconButton(c.Key("forward"), iconForward, 18, a.pal.muted, "前进").Clicked() {
				a.goForward()
			}
			a.playlistTop(c)
		})
	}
}

func (a *app) content(c *ui.Context) {
	// A new page starts at its top. Not as it is opened: a control in a
	// list, as a page's tabs in its header, opens it as the list builds.
	if a.scrolled != a.route {
		a.trackList, a.pageScroll, a.scrolled = ui.ListState{}, ui.ScrollState{}, a.route
		a.pageScroll.Y, a.restoreY = a.restoreY, 0
	}
	a.headAt = ui.Rect{}
	// Pages mark where their content is, as they scroll.
	a.underTop, a.underBottom = false, false
	// Far away, unless its start is built, as it isn't scrolled out of view.
	a.headGone = 1 << 20
	// Keyed by its route, the page before leaves as this one comes.
	page := ui.Box(c.Key(a.route)).Absolute().Top(0).Left(0).Right(0).Bottom(0)
	a.pageMotion(page)
	if url := a.pageArt(); url != "" {
		a.pageAura(page, url)
	}
	page.Children(func() {
		switch a.route.page {
		case pageHome:
			a.homePage(c)
		case pageDaily:
			a.dailyPage(c)
		case pageCharts:
			a.chartsPage(c)
		case pagePlaylist:
			a.playlistPage(c)
		case pageSearch:
			a.searchPage(c)
		case pageFM:
			a.fmPage(c)
		case pageArtist:
			a.artistPage(c)
		case pageAlbum:
			a.albumPage(c)
		case pageUser:
			a.userPage(c)
		case pagePodcast:
			a.podcastPage(c)
		case pagePodcasts:
			a.podcastsPage(c)
		case pageDownloads:
			a.downloadsPage(c)
		case pageRecent:
			a.recentPage(c)
		case pageCloud:
			a.cloudPage(c)
		case pageStyle:
			a.stylePage(c)
		}
	})
}

// centered shows a message, with a retry button if retry is set: what a
// page shows in place of what failed to load.
func (a *app) centered(c *ui.Context, err string, retry func()) {
	top, bottom := a.insets(c)
	ui.Column(c).Grow(1).Padding(top, 0, bottom, 0).Center().Gap(12).Children(func() {
		mascotView(c, 96)
		ui.Text(c, err).TextColor(a.pal.muted).FontSize(13)
		if retry != nil && ui.Button(c, "重试").Clicked() {
			retry()
		}
	})
}

// Loading what the pages show.

func (a *app) loadHome() {
	a.homeLoading, a.homeErr = true, ""
	a.style.loaded = false
	a.loadStyle()
	a.filteredPlaylists = [len(playlistFilters)]*playlistBrowseData{}
	signedIn := a.profile != nil
	at := time.Now()
	go func() {
		featured, err1 := netease.FeaturedPlaylists(30)
		charts, err2 := netease.Charts()
		daily, _ := netease.DailySongs()
		var rec []netease.Playlist
		if signedIn {
			rec, _ = netease.RecommendedPlaylists()
		}
		a.post(func() {
			a.homeLoading = false
			if err1 != nil && err2 != nil {
				// Loading the new day's again, what shows stays, to try
				// once more.
				if len(a.featured) == 0 && len(a.charts) == 0 {
					a.homeErr = "无法连接网易云音乐，请检查网络"
				}
				return
			}
			a.featured, a.charts, a.daily, a.recPlaylists = featured, charts, daily, rec
			a.homeAt = at
		})
	}()
}

// freshenHome loads the home page again once NetEase has picked the day's
// songs anew since it last did: the app may stay open overnight.
func (a *app) freshenHome() {
	if !a.homeLoading && !a.homeAt.IsZero() && dailyRenewed(a.homeAt, time.Now()) {
		a.loadHome()
	}
}

// beijing is NetEase's time, without daylight saving.
var beijing = time.FixedZone("CST", 8*60*60)

// dailyRenewed reports whether NetEase has picked new daily songs between
// loaded and now. It does at 6:00, Beijing time.
func dailyRenewed(loaded, now time.Time) bool {
	b := now.In(beijing)
	six := time.Date(b.Year(), b.Month(), b.Day(), 6, 0, 0, 0, beijing)
	if b.Before(six) {
		six = six.AddDate(0, 0, -1)
	}
	return loaded.Before(six)
}

func (a *app) loadPlaylist(id int64, thenPlay bool) {
	if pl := a.playlists[id]; pl != nil {
		if thenPlay {
			a.pl.PlayQueue(pl.Tracks, firstPlayable(pl.Tracks))
		}
		return
	}
	if a.loadingPl[id] {
		return
	}
	a.loadingPl[id] = true
	delete(a.plErr, id)
	gen := a.accountGen
	go func() {
		pl, err := netease.PlaylistDetail(id)
		a.post(func() {
			a.playlistAnswered(gen, id, pl, err, thenPlay)
		})
	}()
}

func firstPlayable(tracks []netease.Track) int {
	for i := range tracks {
		if tracks[i].Playable() {
			return i
		}
	}
	return 0
}

// playlistAnswered applies a reply only to the login that requested it.
func (a *app) playlistAnswered(gen int, id int64, pl *netease.Playlist, err error, thenPlay bool) {
	if gen != a.accountGen {
		return
	}
	delete(a.loadingPl, id)
	if err != nil {
		a.plErr[id] = "歌单加载失败"
		return
	}
	a.playlists[id] = pl
	if thenPlay {
		a.pl.PlayQueue(pl.Tracks, firstPlayable(pl.Tracks))
	}
}
