package app

import (
	"math"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
)

// Windows of their own, over the others: the lyrics on the desktop, and the
// player in small, for when the main window is out of the way.

const (
	deskW, deskH  = 920, 150
	deskToolsRoom = 40 // over the lyrics, for their tools
	miniW, miniH  = 420, 116
	miniDisc      = 72 // the cover in the mini window
)

// spot returns where the lyrics go: where they were left, or at the foot of
// the screen, centered.
func spot(x, y, w, h, foot int) (int, int) {
	if x != 0 || y != 0 {
		return x, y
	}
	wa := mygo.Screen.PrimaryDisplay().WorkArea
	return wa.X + (wa.Width-w)/2, wa.Y + wa.Height - h - foot
}

// miniSpot returns where the mini window goes: where it was left, or in the
// corner, clear of the lyrics.
func miniSpot(x, y int) (int, int) {
	if x != 0 || y != 0 {
		return x, y
	}
	wa := mygo.Screen.PrimaryDisplay().WorkArea
	return wa.X + wa.Width - miniW - 24, wa.Y + wa.Height - miniH - 24
}

// floatWindow opens a window of the app's that floats over the others, on
// nothing, its corners its content's. On the tray's icon if it has one, as a
// flyout: on Windows only those show what is behind what native UI leaves
// clear, and they never take the keyboard from the app in use. Closing it
// goes as off says; destruction waits until its current frame has ended.
func (a *app) floatWindow(title string, x, y, w, h int, view func(*ui.Context), off func()) *mygo.Window {
	var win *mygo.Window
	if a.tray != nil {
		win = mygo.NewFlyout(mygo.FlyoutOptions{Tray: a.tray, Hidden: true, Width: w, Height: h, Content: ui.View(view)})
	} else {
		win = mygo.NewWindow(mygo.WindowOptions{
			Title: mygo.App.Name() + title, Panel: true, Hidden: true, Frameless: true, Transparent: true,
			DisableShadow: true, DisableResize: true, X: x, Y: y, Width: w, Height: h, Content: ui.View(view),
		})
	}
	win.OnClose(func(e *mygo.CloseEvent) {
		if !a.quitting {
			e.PreventDefault()
			off()
			a.redraw()
		}
	})
	return win
}

// applyDeskLyrics shows the lyrics on the desktop or hides them, as the
// settings say. Locked, the pointer goes through them to what is under them.
func (a *app) applyDeskLyrics() {
	s := &a.settings
	if a.win == nil {
		return
	}
	if s.DeskLyrics && a.deskWin == nil {
		x, y := spot(s.DeskX, s.DeskY, deskW, deskH, 40)
		a.deskWin = a.floatWindow(" 桌面歌词", x, y, deskW, deskH, a.deskView, func() {
			s.DeskLyrics = false
			a.applyDeskLyrics()
		})
		a.deskWin.OnMove(func() {
			if a.deskWin != nil && a.deskWin.IsVisible() {
				b := a.deskWin.Bounds()
				s.DeskX, s.DeskY = b.X, b.Y
			}
		})
	}
	if a.deskWin == nil {
		return
	}
	a.deskWin.SetIgnoreMouseEvents(s.DeskLyrics && s.DeskLock)
	if s.DeskLyrics {
		if a.deskFly.win != nil && a.deskFly.opening {
			// Out of the button: the window comes in as the lyrics land.
			a.deskWin.SetOpacity(0)
		}
		a.deskWin.Show()
		// A flyout shows by its anchor: from there, where it was left, and
		// above all, which it isn't by itself once another app comes up.
		a.deskWin.SetPosition(spot(s.DeskX, s.DeskY, deskW, deskH, 40))
		a.deskWin.SetAlwaysOnTop(true)
	} else {
		a.dropFloat(&a.deskWin)
	}
}

// dropFloat releases a closed player's renderer after any frame building
// its close button. A later opening gets a fresh window at the saved spot.
func (a *app) dropFloat(slot **mygo.Window) {
	win := *slot
	if win == nil {
		return
	}
	*slot = nil
	win.Hide()
	a.post(win.Destroy)
}

// toggleDeskLyrics turns them on or off, from wherever that is offered.
func (a *app) toggleDeskLyrics() {
	s := &a.settings
	s.DeskLyrics = !s.DeskLyrics
	switch {
	case s.DeskLyrics:
		// Out of the button, to where the window goes.
		x, y := spot(s.DeskX, s.DeskY, deskW, deskH, 40)
		a.startDeskFly(true, mygo.Rectangle{X: x, Y: y, Width: deskW, Height: deskH})
		a.applyDeskLyrics()
	case a.deskWin != nil && a.startDeskFly(false, a.deskWin.Bounds()):
		// It goes back into the button first, and its window after.
		a.deskWin.SetIgnoreMouseEvents(true)
	default:
		a.applyDeskLyrics()
	}
	a.changed()
}

// toggleDeskLock locks them where they are, or lets them be moved again.
func (a *app) toggleDeskLock() {
	a.settings.DeskLock = !a.settings.DeskLock
	a.applyDeskLyrics()
	a.changed()
}

// floatTheme gives a floating window the app's font, on nothing.
func (a *app) floatTheme(c *ui.Context) {
	t := *c.Theme()
	t.Font = a.fontFamily()
	c.SetTheme(&t)
	c.Root().Background(ui.Transparent)
}

// How lyrics can look on the desktop. Whatever is behind them, they have to
// read: the first two on a surface of their own, and the two outlined by an
// edge as sharp as their letters, for a soft shadow is a smear on white.
var deskStyles = []struct{ id, label string }{
	{"glass", "深色胶囊"},
	{"light", "浅色胶囊"},
	{"outline", "白字描边"},
	{"color", "彩色描边"},
	{"plain", "无背景"},
}

// A deskLook is the colors of a style: what they lie on, if anything, the
// words sung and to come, the line under them, and their edge, if any.
type deskLook struct {
	pill, sung, unsung, second, edge ui.Color
}

// deskLookOf returns the colors of a style, in the color of what plays.
func (a *app) deskLookOf(id string) deskLook {
	lamp := a.pal.accent
	if accent, ok := a.coverAccent(); ok {
		lamp = accent
	}
	switch id {
	case "light":
		return deskLook{pill: ui.RGBA(250, 250, 252, 0.94), sung: lamp.Mix(ui.RGB(0, 0, 0), 0.25),
			unsung: ui.RGBA(20, 20, 24, 0.48), second: ui.RGBA(20, 20, 24, 0.6)}
	case "outline":
		return deskLook{sung: lamp.Mix(white, 0.45), unsung: white, second: white, edge: ui.RGBA(0, 0, 0, 0.82)}
	case "color":
		return deskLook{sung: lamp.Mix(ui.RGB(0, 0, 0), 0.1), unsung: lamp.Mix(white, 0.55), second: lamp.Mix(ui.RGB(0, 0, 0), 0.2), edge: white}
	case "plain":
		return deskLook{sung: lamp.Mix(white, 0.45), unsung: white.Alpha(0.62), second: white.Alpha(0.7)}
	}
	return deskLook{pill: ui.RGBA(16, 16, 20, 0.8), sung: lamp.Mix(white, 0.55), unsung: white.Alpha(0.5), second: white.Alpha(0.62)}
}

// deskView is the line playing, and what follows it or says it in Chinese.
// Over the pointer it shows it can be moved, and its tools.
func (a *app) deskView(c *ui.Context) {
	a.floatTheme(c)
	a.lyricFace(c.Root())
	a.followCover()
	s, pl := &a.settings, a.pl
	lines, _ := a.shownLyrics()
	pos := a.lyricPos()
	cur := lyrics.Index(lines, pos)
	scale := float32(1)
	if s.DeskScale > 0 {
		scale = float32(s.DeskScale)
	}
	look := a.deskLookOf(s.DeskStyle)
	// The lyrics hang from a line under the room for their tools, rather
	// than in the middle: the tools stay just over them however many lines
	// or how large, and the line sung stays put as the one under it comes
	// and goes.
	root := ui.Column(c.Key("desk")).Fill().AlignItems(ui.Center).Padding(deskToolsRoom, 24, 0, 24)
	over := root.Hovered()
	if over && !s.DeskLock {
		root.Cursor(ui.CursorGrab)
	}
	root.DragWindow().Children(func() {
		body := ui.Column(c.Key("body")).Gap(3).AlignItems(ui.Center).DragWindow()
		if look.pill.A > 0 {
			body.Padding(10, 28).Radius(26).Background(look.pill)
		}
		body.Children(func() {
			tr := pl.Current()
			switch {
			case tr == nil:
				a.deskLine(c, "main", mygo.App.Name(), nil, pos, 30*scale, look, true)
			case len(lines) == 0 || cur < 0:
				a.deskLine(c, "main", tr.Name, nil, pos, 30*scale, look, true)
			default:
				l := &lines[cur]
				a.deskLine(c, "main", l.Text, l.Words, pos, 32*scale, look, true)
				switch {
				case l.Translation != "" && a.settings.LyricTrans:
					a.deskLine(c, "next", l.Translation, nil, pos, 19*scale, look, false)
				case cur+1 < len(lines):
					a.deskLine(c, "next", lines[cur+1].Text, nil, pos, 19*scale, look, false)
				}
			}
		})
		if !s.DeskLock {
			a.deskTools(c, look, over)
		}
	})
	if pl.Playing() && len(lines) > 0 {
		c.After(40 * time.Millisecond)
	}
}

// deskLine is a line of the lyrics, lit word by word as it is sung if it is
// the main one, with an edge of eight copies of it around it if the style has
// one: there is no stroke to give text. The copies are laid out as the line
// is, word by word too: a line of runs and the same text in one run are not
// always as wide, kana and hanzi falling back to fonts of their own, and an
// edge laid out the other way drifted off its words along the line.
func (a *app) deskLine(c *ui.Context, key, text string, words []lyrics.Word, pos time.Duration, size float32, look deskLook, main bool) {
	style := func(e ui.Element) ui.Element {
		weight := 700
		if main {
			weight = 800
		}
		return e.FontSize(size).FontWeight(weight).SingleLine().MaxWidth(deskW - 90)
	}
	byWord := main && len(words) > 0
	// lay is the line in color, or each word in a color of its own.
	lay := func(key any, color func(wd lyrics.Word) ui.Color) ui.Element {
		if !byWord {
			return style(ui.Text(c.Key(key), text)).TextColor(color(lyrics.Word{}))
		}
		return style(ui.RichText(c.Key(key))).Children(func() {
			for i, wd := range words {
				ui.Text(c.Key(i), wd.Text).TextColor(color(wd))
			}
		})
	}
	flat := func(color ui.Color) func(lyrics.Word) ui.Color {
		return func(lyrics.Word) ui.Color { return color }
	}
	ui.Box(c.Key(key)).Shrink(0).PassThrough().Children(func() {
		// Positioned elements paint over those in the flow, in order: the
		// line itself is positioned too, last, and this holds its place.
		lay("room", flat(ui.Transparent)).Opacity(0)
		if look.edge.A > 0 {
			width := max(1.1, size*0.055)
			for i := range 8 {
				turn := float64(i) * math.Pi / 4
				lay(i, flat(look.edge)).Absolute().
					Left(width * float32(math.Cos(turn))).Top(width * float32(math.Sin(turn)))
			}
		}
		var line ui.Element
		switch {
		case byWord && a.settings.DeskWords:
			line = lay("words", func(wd lyrics.Word) ui.Color { return look.unsung.Mix(look.sung, wordLit(wd, pos)) })
		case main:
			line = lay("words", flat(look.sung))
		default:
			line = lay("line", flat(look.second))
		}
		line.Absolute().Left(0).Top(0)
	})
}

// deskTools change the lyrics' look and size, lock them where they are, and
// close them: a capsule of the mini window's stuff, in the color of what
// plays, to read over any style, centered just over the lyrics. It rises in
// as the pointer comes over them, shown, and sinks away as it leaves.
func (a *app) deskTools(c *ui.Context, look deskLook, shown bool) {
	s := &a.settings
	size := func(by float64) {
		if s.DeskScale == 0 {
			s.DeskScale = 1
		}
		s.DeskScale = math.Max(0.7, math.Min(s.DeskScale+by, 1.3))
		a.changed()
	}
	style := 0
	for i, d := range deskStyles {
		if d.id == s.DeskStyle {
			style = i
		}
	}
	pose := ui.Pose{Y: 6, Scale: 0.96, Fade: 1}
	if shown {
		pose = ui.Pose{}
	}
	fade := ui.Animation{Duration: 120 * time.Millisecond}
	// As wide as the window, as the lyrics are centered in it: the capsule
	// centers over them, and the pointer goes through the rest to drag them.
	ui.Row(c.Key("tools-row")).Absolute().Top(deskToolsRoom - 35).Left(0).Right(0).Justify(ui.Center).PassThrough().Children(func() {
		bar := ui.Row(c.Key("tools")).Height(30).Padding(2).Gap(1).Radius(999).AlignItems(ui.Center).
			Background(a.miniSurface().Alpha(0.94)).Border(1, white.Alpha(0.14)).Shadow(0, 6, 16, -6, ui.RGBA(0, 0, 0, 0.5))
		bar.AnimatePose(pose, ui.Animation{Spring: ui.SpringOptions{Duration: 280 * time.Millisecond, Bounce: 0.18}})
		bar.Children(func() {
			// Every button keeps its width, whatever it says: one that changed
			// would move those beside it from under the pointer, between two
			// clicks.
			chip := func(key, says string, wide float32, hot ui.Color, do func(), inside func()) {
				b := ui.ButtonBase(c.Key(key)).Size(wide, 26).Radius(13).Gap(6).Center().FocusRing(false).Label(says)
				bg, fg := ui.Transparent, white.Alpha(0.8)
				if b.Hovered() {
					bg, fg = hot, white
				}
				b.Background(b.AnimateColor("bg", bg, fade)).TextColor(b.AnimateColor("fg", fg, fade))
				press := ui.Pose{}
				if b.Pressed() {
					press.Scale = 0.9
				}
				b.AnimatePose(press, ui.Snappy).Children(inside)
				if b.Clicked() {
					do()
				}
			}
			rule := func() { ui.Box(c).Size(1, 14).Margin(0, 4).Background(white.Alpha(0.14)) }
			hover := white.Alpha(0.12)

			// The style: a dot of its colors, the words' on what they lie on
			// or are edged with, and its name. A click goes on to the next.
			chip("look", "换一种样式", 88, hover, func() {
				next := (style + 1) % len(deskStyles)
				s.DeskStyle = deskStyles[next].id
				a.changed()
				a.say("desk-style", "桌面歌词："+deskStyles[next].label)
			}, func() {
				ring := white.Alpha(0.35)
				switch {
				case look.pill.A > 0:
					ring = look.pill.Alpha(1)
				case look.edge.A > 0:
					ring = look.edge
				}
				ui.Box(c).Size(12, 12).Radius(999).Background(look.sung).Border(2.5, ring).Shadow(0, 0, 0, 1, white.Alpha(0.25))
				ui.Text(c, deskStyles[style].label).FontSize(12).FontWeight(600).SingleLine()
			})
			rule()
			// The size, between a small A and a large one.
			chip("smaller", "歌词字号减小", 28, hover, func() { size(-0.1) }, func() {
				ui.Text(c, "A").FontSize(11).FontWeight(700)
			})
			chip("larger", "歌词字号增大", 28, hover, func() { size(0.1) }, func() {
				ui.Text(c, "A").FontSize(16).FontWeight(700)
			})
			rule()
			chip("lock", "锁定桌面歌词", 28, hover, func() {
				s.DeskLock = true
				a.applyDeskLyrics()
				a.changed()
				a.toast = "桌面歌词已锁定，可在设置或托盘菜单解锁"
			}, func() { ui.Icon(c, iconLock).Size(13, 13) })
			chip("close", "关闭桌面歌词", 28, ui.RGBA(232, 66, 63, 0.9), a.toggleDeskLyrics, func() {
				ui.Icon(c, iconX).Size(13, 13)
			})
		})
	})
}

// miniSurface is what the mini window is made of: dark, in the cover's color.
func (a *app) miniSurface() ui.Color {
	tint := ui.RGB(70, 70, 78)
	if art := a.npArt; art != nil && !art.failed {
		tint = art.tint
	}
	return ui.Hex("#141418").Mix(tint, 0.3)
}

// How the player goes between the bar of the main window and the mini window.
const morphTime = 460 * time.Millisecond

// A floatMorph is the player on its way: the bar's capsule and its cover, going
// over the screen, which is what shows the small window is the bar of the large
// one. It is drawn on a clear window as large as its way.
type floatMorph struct {
	win       *mygo.Window
	toMini    bool
	box, disc [2]ui.Rect // from and to, in that window
	corner    [2]float32 // the capsule's, from and to
	at        time.Time
	handed    bool // the mini window has come in, or gone
	ending    int  // frames since it arrived, to leave the mini window showing
}

// barOnScreen returns where the bar of the main window and its cover are, on
// the screen.
func (a *app) barOnScreen() (bar, cover ui.Rect) {
	cb := a.win.ContentBounds()
	at := func(r ui.Rect) ui.Rect {
		return ui.Rect{X: float32(cb.X) + r.X, Y: float32(cb.Y) + r.Y, W: r.W, H: r.H}
	}
	return at(a.barBox), at(a.barSpot.cover)
}

// startMorph sends the player between the bar and the mini window at x and y,
// and reports whether it could: otherwise it is there at once.
func (a *app) startMorph(x, y int, toMini bool) bool {
	bar, cover := a.barOnScreen()
	if a.motion != motionFull || a.tray == nil || a.morph.win != nil || bar.W == 0 {
		return false
	}
	mini := ui.Rect{X: float32(x), Y: float32(y), W: miniW, H: miniH}
	disc := ui.Rect{X: mini.X + 14, Y: mini.Y + 12, W: miniDisc, H: miniDisc}
	left := int(min(bar.X, mini.X)) - 8
	top := int(min(bar.Y, mini.Y)) - 8
	w := int(max(bar.X+bar.W, mini.X+mini.W)) + 8 - left
	h := int(max(bar.Y+bar.H, mini.Y+mini.H)) + 8 - top
	in := func(r ui.Rect) ui.Rect {
		return ui.Rect{X: r.X - float32(left), Y: r.Y - float32(top), W: r.W, H: r.H}
	}
	m := &a.morph
	*m = floatMorph{toMini: toMini}
	m.box, m.disc, m.corner = [2]ui.Rect{in(bar), in(mini)}, [2]ui.Rect{in(cover), in(disc)}, [2]float32{bar.H / 2, 26}
	if !toMini {
		m.box[0], m.box[1] = m.box[1], m.box[0]
		m.disc[0], m.disc[1] = m.disc[1], m.disc[0]
		m.corner[0], m.corner[1] = m.corner[1], m.corner[0]
	}
	m.win = mygo.NewFlyout(mygo.FlyoutOptions{Tray: a.tray, Hidden: true, Width: w, Height: h, Content: ui.View(a.morphView)})
	m.win.Show()
	m.win.SetPosition(left, top)
	m.win.SetAlwaysOnTop(true)
	m.win.SetIgnoreMouseEvents(true)
	return true
}

// morphView draws the player on its way, and fades the main window as it
// goes from it or comes back to it.
func (a *app) morphView(c *ui.Context) {
	a.floatTheme(c)
	m := &a.morph
	if m.win == nil {
		return
	}
	now := c.Now()
	if m.at.IsZero() {
		m.at = now
	}
	t := clamp01(float32(now.Sub(m.at)) / float32(morphTime))
	// The window comes in over the capsule as that arrives, or goes off it
	// before that leaves: what is on it doesn't pop in, or out.
	const handOver = 0.62
	var move float32
	if m.toMini {
		move = t
		a.win.SetOpacity(float64(1 - easeRange(t, 0, 0.75)))
		if t >= handOver {
			if !m.handed {
				m.handed = true
				a.placeMini()
			}
			a.miniWin.SetOpacity(float64(easeRange(t, handOver, 1)))
		}
	} else {
		move = clamp01((t - 0.14) / 0.86)
		// The window is all there before the capsule lands, for the bar's
		// glass to be there to melt into.
		a.win.SetOpacity(float64(easeRange(t, 0.2, 0.72)))
		if !m.handed {
			a.miniWin.SetOpacity(float64(1 - easeRange(t, 0, 0.2)))
			if t >= 0.2 {
				m.handed = true
				a.miniWin.Hide()
				a.miniWin.SetOpacity(1)
			}
		}
	}
	k := 1 - (1-move)*(1-move)*(1-move)
	box, disc := lerpRect(m.box[0], m.box[1], k), lerpRect(m.disc[0], m.disc[1], k)
	// Back on the bar, the solid capsule clears into the bar's glass under
	// it: its fill thins first, its edge and shadow after, and its cover
	// last, onto the bar's own.
	fill, edge, cover := float32(1), float32(1), float32(1)
	if !m.toMini {
		fill = 1 - easeRange(move, 0.5, 0.92)
		edge = 1 - easeRange(move, 0.7, 1)
		cover = 1 - easeRange(move, 0.8, 1)
	}
	ui.Box(c.Key("capsule")).Absolute().Left(box.X).Top(box.Y).Size(box.W, box.H).Radius(lerp(m.corner[0], m.corner[1], k)).
		Background(a.miniSurface().Alpha(fill)).Border(1, white.Alpha((0.14+0.12*(1-fill))*edge)).
		Shadow(0, 12, 30, -10, ui.RGBA(0, 0, 0, 0.45*fill))
	if tr := a.pl.Current(); tr != nil && cover > 0 {
		a.barDisc(c, tr, disc.W, discSmooth).Absolute().Left(disc.X).Top(disc.Y).Opacity(cover)
	}
	c.AnimationFrame()
	if t < 1 {
		return
	}
	if m.ending++; m.ending < 3 {
		return
	}
	win, toMini := m.win, m.toMini
	m.win = nil
	// Not from within its own frame.
	a.post(func() {
		win.Hide()
		win.Destroy()
		if toMini {
			a.hideMain()
		} else {
			a.dropFloat(&a.miniWin)
		}
		a.win.SetOpacity(1)
	})
}

// placeMini shows the mini window where it goes.
func (a *app) placeMini() {
	s := &a.settings
	if a.miniWin == nil {
		x, y := miniSpot(s.MiniX, s.MiniY)
		a.miniWin = a.floatWindow(" 迷你窗口", x, y, miniW, miniH, a.miniView, a.closeMini)
		a.miniWin.OnMove(func() {
			if a.miniWin != nil && a.miniWin.IsVisible() {
				b := a.miniWin.Bounds()
				s.MiniX, s.MiniY = b.X, b.Y
			}
		})
	}
	x, y := miniSpot(s.MiniX, s.MiniY)
	a.miniWin.SetOpacity(0)
	a.miniWin.Show()
	a.miniWin.SetPosition(x, y)
	a.miniWin.SetAlwaysOnTop(true)
	if a.morph.win == nil {
		a.miniWin.SetOpacity(1)
	}
}

// showMini puts the player in its small window, in place of the main one: the
// bar goes there, and the window around it fades.
func (a *app) showMini() {
	if a.win == nil || a.morph.win != nil {
		return
	}
	s := &a.settings
	x, y := miniSpot(s.MiniX, s.MiniY)
	if !a.startMorph(x, y, true) {
		a.placeMini()
		a.hideMain()
	}
}

// closeMini goes back to the main window: the small one goes back into its bar.
func (a *app) closeMini() {
	if a.morph.win != nil {
		return
	}
	var x, y int
	if a.miniWin != nil {
		b := a.miniWin.Bounds()
		x, y = b.X, b.Y
	}
	if a.miniWin != nil && a.startMorph(x, y, false) {
		a.win.SetOpacity(0)
	} else if a.miniWin != nil {
		a.dropFloat(&a.miniWin)
	}
	a.showMain()
}

// miniView is what plays, in small: over its cover's colors, as Now Playing,
// with how far it has played and how loud along with what controls it.
func (a *app) miniView(c *ui.Context) {
	a.floatTheme(c)
	a.followCover()
	pl := a.pl
	tr := pl.Current()
	root := ui.Box(c.Key("mini")).Fill().Radius(26).TextColor(white).
		Background(a.miniSurface()).Border(1, white.Alpha(0.14)).DragWindow()
	over := root.Hovered()
	root.Children(func() {
		ui.Row(c.Key("top")).Absolute().Top(12).Left(14).Right(12).Gap(13).AlignItems(ui.Start).Children(func() {
			if tr == nil {
				ui.Box(c).Size(miniDisc, miniDisc).Radius(999).Center().Background(white.Alpha(0.08)).Children(func() { mascotView(c, 52) })
			} else {
				a.barDisc(c, tr, miniDisc, discSmooth)
			}
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Start).Children(func() {
					ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(1).Children(func() {
						name, sub := "未在播放", ""
						if tr != nil {
							name, sub = tr.Name, firstArtist(tr)
						}
						ui.Text(c, name).FontSize(14).FontWeight(700).SingleLine()
						ui.Text(c, sub).FontSize(12).TextColor(white.Alpha(0.62)).SingleLine()
					})
					ink := white.Alpha(0.35)
					if over {
						ink = white.Alpha(0.85)
					}
					if iconButton(c.Key("back"), iconExpand, 14, ink, "回到主窗口").Size(26, 26).Clicked() {
						a.closeMini()
					}
				})
				ui.Row(c).Gap(2).AlignItems(ui.Center).Margin(3, 0, 0, -6).Children(func() {
					if iconButton(c.Key("prev"), iconPrev, 14, white, "上一首").Size(30, 28).Clicked() {
						pl.Prev()
					}
					if playButton(c.Key("play"), pl.Playing(), 18, white).Size(30, 28).Clicked() {
						pl.TogglePlay()
					}
					if iconButton(c.Key("next"), iconNext, 14, white, "下一首").Size(30, 28).Clicked() {
						pl.Next()
					}
					ui.Spacer(c)
					a.volumeSlider(c, white.Alpha(0.75), white.Alpha(0.18), 92)
				})
			})
		})
		a.scrubber(c.Key("mini-scrub"), &a.miniScrub, white.Alpha(0.18), white.Alpha(0.85), 14, ui.Color{}).
			Absolute().Left(18).Right(18).Bottom(5)
	})
	if pl.Playing() {
		c.After(time.Second)
	}
}
