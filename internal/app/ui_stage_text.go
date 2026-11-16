package app

import (
	"math"

	"github.com/egoist/mygo/ui"
)

var stageTextRenderModes = []struct{ id, label string }{
	{"", "标准"},
	{"msdf", "高清动态文字"},
}

func stageTextRenderMode(value string) string {
	if value == "msdf" {
		return value
	}
	return ""
}

// stageFontFamily preserves the current app theme and v1.2.17's separate lyric font.
func (a *app) stageFontFamily(c *ui.Context) string {
	if a.settings.LyricFont != "" {
		return a.lyricFamily()
	}
	return c.Theme().Font
}

// stageRasterRatio requests final physical pixels, including every enclosing
// stage scale. Cached mode rounds up by half a physical pixel: its residual
// transform only downsamples, by less than half a pixel at the font's em size.
func stageRasterRatio(size, totalScale, dpi float32) float32 {
	if size <= 0 || totalScale <= 0 || dpi <= 0 {
		return 1
	}
	px := size * totalScale * dpi
	px = float32(math.Ceil(float64(px*2))) / 2
	return px / (size * dpi)
}

// stageTextRaster keeps the original Text as the layout/accessibility carrier:
// its size, wrapping, center, sparks and all ancestor poses remain unchanged.
// Only its ink is replaced, at paint time when actual device scale is known.
func (a *app) stageTextRaster(e ui.Element, text string, font ui.Font, totalScale float32, color ui.Color) {
	mode := stageTextRenderMode(a.settings.StageTextRender)
	if mode == "" {
		return
	}
	e.TextColor(ui.Transparent).Draw(func(p *ui.Painter, r ui.Rect) {
		if p.DynamicMSDFTextLayout(a.stageDynamic, ui.ShapeText(text, font, 0), r.X, r.Y, color) {
			return
		}
		q := stageRasterRatio(font.Size, totalScale, p.Scale())
		native := font
		native.Size *= q
		// ShapeText resolves the same system fonts/fallback as Text. Retain no
		// native font pointers across frames: MyGo may evict them at EndFrame.
		// Its font/glyph atlas still reuses quantized sizes in cached mode.
		layout := ui.ShapeText(text, native, 0)
		p.Transform(ui.Scaling(1/q, 1/q), r.X, r.Y, func() {
			p.TextLayout(layout, r.X, r.Y, color)
		})
	})
}
