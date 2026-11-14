package app

import (
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
)

// How the desktop lyrics fly out of their button, or back into it.
const deskFlyTime = 340 * time.Millisecond

// A deskFlight is the desktop lyrics on their way out of the player bar's
// button, or back into it: a capsule that grows out of the button with the
// line playing in it, arcs over to where the lyrics show, and hands over to
// their window; or leaves it there and shrinks back into the button. It is
// drawn on a clear window as large as its way.
type deskFlight struct {
	win     *mygo.Window
	opening bool
	from    ui.Rect // the button, in that window
	to      ui.Rect // the lyrics' capsule, in that window
	text    string
	size    float32 // of the line where it lands
	at      time.Time
	handed  bool // the lyrics' window has come in, or gone
	ending  int  // frames since it arrived
}

// deskLineNow is the line the desktop lyrics show now.
func (a *app) deskLineNow() string {
	tr := a.pl.Current()
	if tr == nil {
		return mygo.App.Name()
	}
	lines, _ := a.pl.Lyrics()
	if cur := lyrics.Index(lines, a.lyricPos()); cur >= 0 && cur < len(lines) && lines[cur].Text != "" {
		return lines[cur].Text
	}
	return tr.Name
}

// startDeskFly sends the lyrics out of their button to their window at its
// bounds, or back, and reports whether it could: otherwise they come or go
// at once.
func (a *app) startDeskFly(opening bool, desk mygo.Rectangle) bool {
	if a.motion != motionFull || a.tray == nil || a.win == nil || !a.win.IsVisible() || a.win.IsMinimized() ||
		a.deskBtn.W == 0 || a.deskFly.win != nil || a.morph.win != nil {
		return false
	}
	cb := a.win.ContentBounds()
	btn := ui.Rect{X: float32(cb.X) + a.deskBtn.X, Y: float32(cb.Y) + a.deskBtn.Y, W: a.deskBtn.W, H: a.deskBtn.H}
	scale := float32(1)
	if a.settings.DeskScale > 0 {
		scale = float32(a.settings.DeskScale)
	}
	text := a.deskLineNow()
	size := 32 * scale
	// About as wide as the line, which is all the window shows of itself.
	w := min(max(float32(utf8.RuneCountInString(text))*size*0.92+64, 220), deskW-48)
	h := size*1.35 + 22
	lyric := ui.Rect{X: float32(desk.X) + (deskW-w)/2, Y: float32(desk.Y) + deskToolsRoom, W: w, H: h}

	left := int(min(btn.X, lyric.X)) - 16
	top := int(min(btn.Y, lyric.Y)) - 16
	right := int(max(btn.X+btn.W, lyric.X+lyric.W)) + 16
	bottom := int(max(btn.Y+btn.H, lyric.Y+lyric.H)) + 16
	in := func(r ui.Rect) ui.Rect {
		return ui.Rect{X: r.X - float32(left), Y: r.Y - float32(top), W: r.W, H: r.H}
	}
	f := &a.deskFly
	*f = deskFlight{opening: opening, from: in(btn), to: in(lyric), text: text, size: size}
	f.win = mygo.NewFlyout(mygo.FlyoutOptions{Tray: a.tray, Hidden: true, Width: right - left, Height: bottom - top, Content: ui.View(a.deskFlyView)})
	f.win.Show()
	f.win.SetPosition(left, top)
	f.win.SetAlwaysOnTop(true)
	f.win.SetIgnoreMouseEvents(true)
	return true
}

// deskFlyView draws the lyrics on their way, handing over to their window as
// they arrive, or taking over from it as they leave.
func (a *app) deskFlyView(c *ui.Context) {
	a.floatTheme(c)
	a.lyricFace(c.Root())
	f := &a.deskFly
	if f.win == nil {
		return
	}
	now := c.Now()
	if f.at.IsZero() {
		f.at = now
	}
	t := clamp01(float32(now.Sub(f.at)) / float32(deskFlyTime))
	// How far out of the button it is: 0 in it, 1 where the lyrics show.
	var out, shown float32
	if f.opening {
		out = t
		shown = 1 - easeRange(t, 0.78, 1)
		if t >= 0.62 && a.deskWin != nil {
			f.handed = true
			a.deskWin.SetOpacity(float64(easeRange(t, 0.62, 0.95)))
		}
	} else {
		out = 1 - clamp01((t-0.12)/0.88)
		shown = 1 - easeRange(t, 0.86, 1)
		if !f.handed && a.deskWin != nil && !a.settings.DeskLyrics {
			a.deskWin.SetOpacity(float64(1 - easeRange(t, 0, 0.18)))
		}
	}
	a.paintDeskFly(c, f, out, shown)
	c.AnimationFrame()
	if t < 1 {
		return
	}
	if f.ending++; f.ending < 3 {
		return
	}
	win, opening := f.win, f.opening
	f.win = nil
	// Not from within its own frame.
	a.post(func() {
		win.Hide()
		win.Destroy()
		switch {
		case a.deskWin == nil:
		case a.settings.DeskLyrics:
			a.deskWin.SetOpacity(1)
		case !opening:
			a.dropFloat(&a.deskWin)
		}
		a.redraw()
	})
}

// paintDeskFly draws the capsule out of the button by out, from 0 in it to
// 1 where the lyrics show, at opacity shown.
func (a *app) paintDeskFly(c *ui.Context, f *deskFlight, out, shown float32) {
	// First it swells on the button, which says where it comes from; then
	// it leaves, gently, and grows into the lyrics on its way, landing
	// softly. Back, it does the same the other way.
	pop := easeRange(out, 0, 0.12)
	travel := easeRange(out, 0.06, 1)
	grow := easeRange(out, 0.22, 1)
	// An arc rather than a line, up and over: it rises sooner than it
	// crosses, and comes down into the button last.
	fx, fy := f.from.X+f.from.W/2, f.from.Y+f.from.H/2
	tx, ty := f.to.X+f.to.W/2, f.to.Y+f.to.H/2
	cx, cy := lerp(fx, tx, travel), lerp(fy, ty, 1-(1-travel)*(1-travel))
	bubble := f.from.H * (1 + 0.35*pop)
	w, h := lerp(bubble, f.to.W, grow), lerp(bubble, f.to.H, grow)
	r := ui.Rect{X: cx - w/2, Y: cy - h/2, W: w, H: h}
	accent := a.pal.accent
	if ca, ok := a.coverAccent(); ok && a.settings.AccentCover {
		accent = ca
	}
	// On the button it is the button lit; out, a dark capsule as the
	// lyrics' own, with the line in it.
	fill := accent.Mix(ui.RGBA(18, 18, 22, 0.72), easeRange(out, 0.15, 0.55))
	icon := 1 - easeRange(out, 0.2, 0.42)
	words := easeRange(out, 0.45, 0.85)
	ui.Box(c.Key("fly")).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).Radius(r.H/2).Opacity(shown).
		Background(fill).Border(1, white.Alpha(0.16)).Shadow(0, 10, 28, -8, ui.RGBA(0, 0, 0, 0.15+0.25*grow)).Clip().
		Center().Children(func() {
		if icon > 0 {
			ui.Icon(c, iconSubtitles).Size(16, 16).TextColor(white).Opacity(icon).Absolute().Left((r.W - 16) / 2).Top((r.H - 16) / 2)
		}
		if words > 0 {
			ui.Text(c, f.text).FontSize(lerp(12, f.size, grow)).FontWeight(700).TextColor(white).NoWrap().Opacity(words)
		}
	})
}
