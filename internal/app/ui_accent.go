package app

import (
	"fmt"
	"math"

	"github.com/egoist/mygo/ui"
)

// The listener may give the app a color of its own: the buttons and the
// highlights take it, and the window and the sidebar take a breath of it, so
// that the color covers the interface rather than marking a few controls in
// it. Each appearance gets the accent that reads on its background, from the
// color chosen once.

// accentColors are the colors the listener may choose: the app's red, the
// clay Claude is known by, and common ones. The last color is their own,
// which the picker takes.
var accentColors = []struct{ id, label, hex string }{
	{"", "网易云红", ""},
	{"clay", "陶土", "#D97757"},
	{"amber", "琥珀", "#E0A526"},
	{"green", "青草", "#4E9E60"},
	{"teal", "湖水", "#2E9191"},
	{"blue", "海蓝", "#3D7BD9"},
	{"violet", "紫罗兰", "#7C5CD6"},
	{"rose", "玫瑰", "#D6608E"},
}

// themeAccent gives p the accent chosen and lets the window and the sidebar
// take a breath of it, a little more of it on the sidebar.
func themeAccent(p palette, chosen string) palette {
	c := ui.Hex(chosen)
	p.accent = accentFor(c, p.dark)
	p.onAccent = inkOnAccent(p.accent)
	bg, side := float32(0.2), float32(0.26)
	if p.dark {
		bg, side = 0.14, 0.18
	}
	p.bg = p.bg.Mix(c, bg)
	p.sidebar = p.sidebar.Mix(c, side)
	return p
}

// accentFor returns c as an accent for the light or the dark: bright colors
// deepen on white, dark ones lift on black, and none takes more chroma than
// an accent reads with.
func accentFor(c ui.Color, dark bool) ui.Color {
	l, ch, h := oklchOf(c)
	if dark {
		l = min(max(l, 0.64), 0.82)
	} else {
		l = min(max(l, 0.45), 0.6)
	}
	return ui.Oklch(float32(l), float32(min(ch, 0.2)), float32(h))
}

// inkOnAccent returns the words to write on an accent: dark ones where the
// accent is light enough to need them, and white otherwise.
func inkOnAccent(accent ui.Color) ui.Color {
	if l, _, _ := oklchOf(accent); l > 0.72 {
		return ui.Hex("#1F1E1D")
	}
	return white
}

// hexOf returns c as #RRGGBB.
func hexOf(c ui.Color) string { return fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B) }

// colorWheel paints a turn of hues in r, the way a color well shows that any
// color is there: the dot the listener opens to pick one of their own.
func colorWheel(p *ui.Painter, r ui.Rect) {
	var stops []ui.GradientStop
	for i := range 7 {
		stops = append(stops, ui.GradientStop{
			At:    float32(i) / 6,
			Color: ui.Oklch(0.72, 0.16, float32(i*60)),
		})
	}
	p.FillConicGradient(r, ui.ConicGradient{Stops: stops, Angle: 90}, 999)
}

// accentPick is the color well's dialog open: the color it shows while it is
// open, and whether it changed one not yet kept.
type accentPick struct {
	open  bool
	dirty bool
	color ui.Color
}

// accentDialog picks a color of the listener's own: a color well as AppKit's,
// whose color the app takes at once, and keeps when the well closes.
func (a *app) accentDialog(c *ui.Context) {
	d := &a.settingsView.accentDraft
	if !d.open && !d.dirty {
		return
	}
	p := a.pal
	a.dialog(c, "accent", 0.35, &d.open, func(panel ui.Element) {
		a.pane(panel, 20, false)
		panel.Width(312).Padding(20).Gap(14)
		ui.Text(c, "自定义主题色").FontSize(17).FontWeight(700)
		picker := ui.ColorPicker(c, &d.color)
		if picker.Changed() {
			d.color.A = 255
			a.settings.Accent, a.settings.AccentCover = hexOf(d.color), false
			d.dirty = true
			a.redraw()
		}
		ui.Text(c, "浅色和深色各自取合适的深浅，不会照搬这个颜色。").
			FontSize(11.5).TextColor(p.muted).LineHeight(1.4)
		ui.Row(c).Justify(ui.End).Children(func() {
			if pillButton(c.Key("done"), nil, "完成", p.accent, p.onAccent).Clicked() {
				d.open = false
			}
		})
	})
	if !d.open && d.dirty {
		d.dirty = false
		a.settings.save()
	}
}

// oklchOf returns the lightness, from 0 to 1, chroma and hue of c, in
// degrees, as Oklch takes them.
func oklchOf(c ui.Color) (l, ch, h float64) {
	lin := func(v uint8) float64 {
		x := float64(v) / 255
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	r, g, b := lin(c.R), lin(c.G), lin(c.B)
	lr := cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	lg := cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	lb := cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	l = 0.2104542553*lr + 0.7936177850*lg - 0.0040720468*lb
	a := 1.9779984951*lr - 2.4285922050*lg + 0.4505937099*lb
	bb := 0.0259040371*lr + 0.7827717662*lg - 0.8086757660*lb
	ch = math.Hypot(a, bb)
	h = math.Atan2(bb, a) * 180 / math.Pi
	if h < 0 {
		h += 360
	}
	return l, ch, h
}

func cbrt(x float64) float64 {
	if x < 0 {
		return -math.Cbrt(-x)
	}
	return math.Cbrt(x)
}
