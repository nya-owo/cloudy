package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const (
	trayPopupWidth  = 288 // 240 DIP menu, with room for its transparent shadow
	trayPopupHeight = 432
	trayPopupMargin = 24
)

var iconTrayUpdate = strokeIcon(`<path d="M20 7v5h-5"/><path d="M4 17v-5h5"/><path d="M6 7a7 7 0 0 1 11.5-2L20 8M4 16l2.5 3A7 7 0 0 0 18 17"/>`)

// Keep the window boundary small so interaction tests can drive dismissal and
// deferred actions without opening native windows or depending on the main one.
type trayPopupWindow interface {
	Show()
	Hide()
	Destroy()
	Invalidate()
	Update(func())
}

var newTrayPopupWindow = func(opts mygo.FlyoutOptions, onClose func(*mygo.CloseEvent)) trayPopupWindow {
	w := mygo.NewFlyout(opts)
	w.OnClose(onClose)
	return w
}

// toggleTrayPopup is only registered for Windows' distinct right-click event.
func (a *app) toggleTrayPopup() {
	if a.trayPopup != nil {
		a.closeTrayPopup()
		return
	}
	if a.tray == nil || a.quitting {
		return
	}
	var w trayPopupWindow
	w = newTrayPopupWindow(mygo.FlyoutOptions{
		Tray: a.tray, Placement: mygo.PlacementBottom, Gap: 4,
		Width: trayPopupWidth, Height: trayPopupHeight, Hidden: true, Focusable: true,
		// Draw the rounded card and its shadow ourselves, including on Win10.
		// No window material or text Effect: the solid fallback stays readable.
		Content: ui.View(a.trayPopupView),
	}, func(e *mygo.CloseEvent) {
		if !a.quitting {
			e.PreventDefault()
			if a.trayPopup == w {
				a.closeTrayPopup()
			}
		}
	})
	a.trayPopup = w
	w.Show()
}

func (a *app) closeTrayPopup() { a.finishTrayPopup(nil) }

// Hide before dispatch; destroy after the frame that may have clicked the row.
// Post through the popup itself, so a hidden main window is never a dependency.
func (a *app) finishTrayPopup(action func()) {
	w := a.trayPopup
	if w == nil {
		return
	}
	a.trayPopup = nil
	w.Hide()
	w.Update(func() {
		w.Destroy()
		if action != nil {
			action()
			a.redraw()
		}
	})
}

type trayCommand struct {
	icon     *ui.SVG
	label    string
	status   string
	disabled bool
	run      func()
}

func (a *app) trayCommands() [][]trayCommand {
	tr := a.pl.Current()
	playIcon, playLabel := iconPlay, "播放"
	if a.pl.Playing() {
		playIcon, playLabel = iconPause, "暂停"
	}
	heart, likeLabel := iconHeart, "喜欢"
	if tr != nil && a.isLiked(tr) {
		heart, likeLabel = iconHeartFill, "取消喜欢"
	}
	lockLabel, lockState := "锁定桌面歌词", ""
	if a.settings.DeskLock {
		lockLabel, lockState = "解锁桌面歌词", "已锁定"
	}
	lyricsState := "已关闭"
	if a.settings.DeskLyrics {
		lyricsState = "已开启"
	}
	miniState := ""
	if a.miniWin != nil && a.miniWin.IsVisible() {
		miniState = "已打开"
	}
	front := func(fn func()) func() {
		return func() { mygo.App.Focus(); fn() }
	}
	return [][]trayCommand{
		{{icon: iconHome, label: "打开Cloudy", run: a.showMain}},
		{
			{icon: playIcon, label: playLabel, disabled: tr == nil, run: a.pl.TogglePlay},
			{icon: iconPrev, label: "上一首", disabled: tr == nil, run: a.pl.Prev},
			{icon: iconNext, label: "下一首", disabled: tr == nil, run: a.pl.Next},
			{icon: heart, label: likeLabel, disabled: tr == nil || tr.LocalPath != "", run: a.likeCurrent},
		},
		{
			{icon: iconLyrics, label: "桌面歌词", status: lyricsState, run: a.toggleDeskLyrics},
			{icon: iconLock, label: lockLabel, status: lockState, run: a.toggleDeskLock},
			{icon: iconMiniWin, label: "迷你窗口", status: miniState, run: a.showMini},
		},
		{
			{icon: iconSettings, label: "设置…", run: front(a.openSettings)},
			{icon: iconTrayUpdate, label: "检查更新…", run: front(a.checkForUpdates)},
			{icon: iconSignOut, label: "退出Cloudy", run: mygo.App.Quit},
		},
	}
}

// trayPopupView shares the account menu's row metrics and colors. Its standalone
// card uses pane's solid fallback, since a desktop flyout has no app glass behind it.
func (a *app) trayPopupView(c *ui.Context) {
	a.floatTheme(c)
	p := a.pal
	bg, edge := ui.Hex("#f6f6f8"), ui.RGBA(0, 0, 0, .12)
	if p.dark {
		bg, edge = ui.Hex("#232327"), white.Alpha(.12)
	}
	c.OnShortcut(0, ui.KeyEscape, a.closeTrayPopup)
	ui.Box(c).Fill().Children(func() {
		ui.ButtonBase(c.Key("dismiss")).Absolute().Top(0).Left(0).Right(0).Bottom(0).
			Label("关闭托盘菜单").FocusRing(false).OnClick(a.closeTrayPopup)
		ui.Column(c.Key("tray-card")).Absolute().Top(trayPopupMargin).Left(trayPopupMargin).
			Right(trayPopupMargin).Bottom(trayPopupMargin).Radius(20).Background(bg).Border(.5, edge).
			Shadow(0, 18, 44, -12, ui.RGBA(0, 0, 0, .45)).TextColor(p.text).Children(func() {
			ui.Scroll(c.Key("tray-commands")).Fill().Padding(6).Gap(1).
				Role(ui.RoleMenu).Label("托盘菜单").FocusGroup(ui.Vertical).Children(func() {
				for i, group := range a.trayCommands() {
					if i > 0 {
						ui.Box(c).Height(.5).Margin(4, 8).Background(popoverColors(p.dark).edge)
					}
					for j, command := range group {
						b := a.menuItem(c, command.icon, command.label, command.status).Disabled(command.disabled)
						if i == 0 && j == 0 {
							b.AutoFocus()
						}
						if b.Clicked() {
							a.finishTrayPopup(command.run)
						}
					}
				}
			})
		})
	})
}
