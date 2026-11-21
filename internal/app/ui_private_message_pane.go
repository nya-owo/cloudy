package app

import "github.com/egoist/mygo/ui"

// Keep text and controls live. Only the plain pane's stable decoration is a
// bitmap; row entrance, hover and typing cannot invalidate its cached scene.
func (a *app) privateMessagePane(c *ui.Context, panel ui.Element, content func()) {
	if a.settings.GlobalGlass || a.settings.panelGlass() {
		a.pane(panel, 26, false)
		panel.Children(content)
		return
	}
	bg, line := ui.Hex("#f6f6f8"), ui.RGBA(0, 0, 0, 0.12)
	if a.pal.dark {
		bg = ui.Hex("#232327")
		line = white.Alpha(0.12)
	}
	// Retain the original border's layout and an opaque backing for the
	// framework's text coverage decision. No text is flattened into the cache.
	panel.Radius(26).Border(0.5, line).Background(bg)
	panel.Children(func() {
		surface := ui.Box(c.Key("dm-pane-surface")).Absolute().
			Top(-0.5).Left(-0.5).Right(-0.5).Bottom(-0.5).PassThrough().Cache()
		surface.Radius(26).Shadow(0, 18, 44, -12, ui.RGBA(0, 0, 0, 0.45))
		// Absolute decorations paint after normal-flow children in MyGo;
		// use a second absolute plane so the live content stays above it.
		ui.Column(c.Key("dm-pane-content")).Absolute().Top(0).Left(0).Right(0).Bottom(0).Children(content)
	})
}
