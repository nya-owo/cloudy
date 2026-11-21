package app

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// Now Playing can take the whole screen, the taskbar too: F11 or the
// button at its top right go in and out, as Escape goes out. Full screen
// belongs to Now Playing: closing it, or hiding the window, leaves it.

var (
	iconFullScreen = strokeIcon(`<path d="M15 3h6v6"/><path d="M9 21H3v-6"/><path d="M21 3l-7 7"/><path d="M3 21l7-7"/>`)
	iconLeaveFull  = strokeIcon(`<path d="M4 14h6v6"/><path d="M20 10h-6V4"/><path d="M14 10l7-7"/><path d="M3 21l7-7"/>`)
)

// setNpFull puts the main window in full screen, or takes it out.
func (a *app) setNpFull(v bool) {
	if a.npFull == v {
		return
	}
	a.npFull = v
	if a.win != nil {
		a.win.SetFullScreen(v)
	}
}

// hideMain hides the main window, out of full screen first, so that it
// does not come back covering the screen.
func (a *app) hideMain() {
	a.closePrivateMessages()
	a.setNpFull(false)
	if a.win != nil {
		a.win.Hide()
	}
}

// fullHintFor is how long the bubble under the button says F11 is there,
// the first time Now Playing opens after the app starts.
const fullHintFor = 5 * time.Second

// fullScreenButton is the button at Now Playing's top right that goes in
// and out of full screen, left of the window's own buttons. The first time
// Now Playing opens in a run, a bubble under it tells of F11 for a while.
func (a *app) fullScreenButton(c *ui.Context, shown bool) {
	now := c.Now()
	if a.fullHintAt.IsZero() {
		a.fullHintAt = now
	}
	hint := false
	if left := fullHintFor - now.Sub(a.fullHintAt); left > 0 && !a.npFull {
		hint = true
		c.After(left)
	}
	icon, label := iconFullScreen, "全屏 (F11)"
	if a.npFull {
		icon, label = iconLeaveFull, "退出全屏 (Esc)"
	}
	right := c.TitleBar().Right + 14
	reveal(ui.Row(c.Key("full-reveal")).Absolute().Top(0).Right(right).Height(topBar(c)).AlignItems(ui.Center), shown || hint).Children(func() {
		if iconButton(c.Key("full"), icon, 18, white.Alpha(0.7), label).Clicked() {
			a.setNpFull(!a.npFull)
		}
	})
	if hint {
		ui.Row(c.Key("full-hint")).Absolute().Top(topBar(c)-2).Right(right-8).Height(30).PaddingX(12).Radius(999).AlignItems(ui.Center).
			Background(ui.RGBA(0, 0, 0, 0.55)).Border(0.5, white.Alpha(0.16)).PassThrough().
			Transition(ui.ElementTransition{Animation: &motionPane, Enter: &ui.Motion{Y: -6}, Exit: &ui.Motion{Y: -6}}).
			Children(func() {
				ui.Text(c, "按 F11 全屏，Esc 退出").FontSize(12.5).TextColor(white.Alpha(0.92)).SingleLine()
			})
	}
}
