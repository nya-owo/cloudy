package app

import (
	"math"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
)

// How the desktop lyrics fly out of their button, character by character,
// and back into it.
const (
	deskGlyphFly  = 380 * time.Millisecond // a character's way
	deskGlyphLag  = 22 * time.Millisecond  // between one setting off and the next
	deskGlyphLags = 240 * time.Millisecond // between the first and the last, at most
	deskNextFade  = 260 * time.Millisecond // the line under them, once they are down
	deskFlyStep   = 34 * time.Millisecond  // the most a frame moves them on
)

// A deskFlight is the desktop lyrics on their way out of the player bar's
// button, or back into it: each character of the line playing leaves the
// button in turn, arcs over to where it shows in the lyrics' window and
// lands there, the line made up again as that window shows it; or, back,
// each leaves its place in turn for the button. Down, they are the line
// the lyrics' window shows, to the pixel, which takes over from them; the
// line under it fades in after. They are drawn on a clear window as large
// as their way.
type deskFlight struct {
	win     *mygo.Window
	coming  bool // its window is being made, to show once they fly
	opening bool
	from    ui.Rect  // the button, in that window
	desk    ui.Point // the lyrics' window's corner, in that window
	line    deskMain // the line that flies, as it was when it set off
	at      ui.Rect  // where the line lies in the lyrics' window
	gone    time.Duration
	last    time.Time // the frame before
	frames  int       // shown so far
	// landed is when they came down, and the lyrics' window showed the
	// line again; zero before.
	landed time.Time
	ending int // frames since

	// Cached per-flight shaping, GPU sprite strip, and last deskWin opacity so
	// flying frames don't reshape text or rasterize per-character CPU clip masks.
	glyphs      []ui.Rect
	lineH       float32
	edge        *ui.TextLayout
	edgeStyle   string
	strip       deskFlyStrip
	deskOpacity float64
}

// deskFlyStrip holds the flying lyric line rendered once into a GPU canvas
// texture at device-pixel resolution, so each flying character draws as one
// transformed ImageRegion quad with zero clipped transforms and zero CPU
// raster.ClipMask work.
type deskFlyStrip struct {
	canvas *ui.Canvas
	scale  float32
	look   deskLook
	pos    time.Duration
	ready  bool
	src    []ui.Rect
}

// deskMain is the line the desktop lyrics show large: its text, its words
// if it is sung word by word, and its size.
type deskMain struct {
	text  string
	words []lyrics.Word
	size  float32
}

// deskLines returns the line the desktop lyrics show large, and the one
// under it, if any: what follows it, or says it in Chinese.
func (a *app) deskLines() (main deskMain, next string) {
	scale := float32(1)
	if a.settings.DeskScale > 0 {
		scale = float32(a.settings.DeskScale)
	}
	tr := a.pl.Current()
	if tr == nil {
		return deskMain{text: mygo.App.Name(), size: 30 * scale}, ""
	}
	lines, _ := a.shownLyrics()
	cur := lyrics.Index(lines, a.lyricPos())
	if len(lines) == 0 || cur < 0 {
		return deskMain{text: tr.Name, size: 30 * scale}, ""
	}
	l := &lines[cur]
	main = deskMain{text: l.Text, words: l.Words, size: 32 * scale}
	switch {
	case l.Translation != "" && a.settings.LyricTrans:
		next = l.Translation
	case cur+1 < len(lines):
		next = lines[cur+1].Text
	}
	return main, next
}

// deskFlyShown says how much of the lyrics' window's text shows while they
// fly: none of the line while it is on its way out or back, which the
// flight draws, and the line under it fading in once they are down.
func (a *app) deskFlyShown(now time.Time) (line, next float32) {
	f := &a.deskFly
	switch {
	case f.win != nil && f.opening && f.landed.IsZero():
		return 0, 0
	case f.win != nil && !f.opening && f.frames > 1:
		return 0, 0
	case f.opening && !f.landed.IsZero():
		return 1, easeRange(float32(now.Sub(f.landed))/float32(deskNextFade), 0, 1)
	}
	return 1, 1
}

// deskFlyLags returns how long after the one before each of n characters
// sets off, and how long they all take.
func deskFlyLags(n int) (lag, total time.Duration) {
	if n > 1 {
		lag = min(deskGlyphLag, deskGlyphLags/time.Duration(n-1))
	}
	return lag, lag*time.Duration(max(n-1, 0)) + deskGlyphFly
}

// startDeskFly sends the lyrics out of their button to their window at its
// bounds, or back, and reports whether it could: otherwise they come or go
// at once.
func (a *app) startDeskFly(opening bool, desk mygo.Rectangle) bool {
	if a.motion != motionFull || a.tray == nil || a.win == nil || !a.win.IsVisible() || a.win.IsMinimized() ||
		a.deskBtn.W == 0 || a.deskFly.win != nil || a.morph.win != nil {
		return false
	}
	main, _ := a.deskLines()
	if strings.TrimSpace(main.text) == "" {
		return false
	}
	cb := a.win.ContentBounds()
	btn := ui.Rect{X: float32(cb.X) + a.deskBtn.X, Y: float32(cb.Y) + a.deskBtn.Y, W: a.deskBtn.W, H: a.deskBtn.H}
	// Where the line lies: as the lyrics' window laid it out last, going
	// back; coming out, reckoned as it will, until it does.
	look := a.deskLookOf(a.settings.DeskStyle)
	baseInk := a.deskShape(main, look, 0, false)
	w, h := baseInk.Size()
	glyphs := deskGlyphs(baseInk, main)
	at := a.deskLineAt
	if opening || at.W == 0 {
		w = min(w, deskW-90)
		pad := float32(0)
		if look.pill.A > 0 {
			pad = 10
		}
		at = ui.Rect{X: (deskW - w) / 2, Y: deskToolsRoom + pad, W: w, H: h}
	}
	line := ui.Rect{X: float32(desk.X) + at.X, Y: float32(desk.Y) + at.Y, W: at.W, H: at.H}
	// Room for a character turned or swelling where it lands.
	room := main.size
	left := int(min(btn.X, line.X) - room)
	top := int(min(btn.Y, line.Y) - room)
	right := int(max(btn.X+btn.W, line.X+line.W) + room)
	bottom := int(max(btn.Y+btn.H, line.Y+line.H) + room)
	f := &a.deskFly
	*f = deskFlight{opening: opening, line: main, at: at,
		from:        ui.Rect{X: btn.X - float32(left), Y: btn.Y - float32(top), W: btn.W, H: btn.H},
		desk:        ui.Point{X: float32(desk.X - left), Y: float32(desk.Y - top)},
		glyphs:      glyphs,
		lineH:       h,
		deskOpacity: -1}
	if look.edge.A > 0 {
		f.edge, f.edgeStyle = baseInk, a.settings.DeskStyle
	}
	f.win = mygo.NewFlyout(mygo.FlyoutOptions{Tray: a.tray, Hidden: true, Width: right - left, Height: bottom - top, Content: ui.View(a.deskFlyView)})
	showAtOnce(f.win.NativeHandle())
	f.win.SetPosition(left, top)
	f.win.SetAlwaysOnTop(true)
	f.win.Show()
	f.win.SetIgnoreMouseEvents(true)
	return true
}

// deskFlyView draws the characters on their way, handing over to the
// lyrics' window as they land, or taking over from it as they leave.
func (a *app) deskFlyView(c *ui.Context) {
	a.floatTheme(c)
	a.lyricFace(c.Root())
	f := &a.deskFly
	if f.win == nil {
		return
	}
	// Time goes on as frames come, a slow one moving them on no more than
	// a frame's worth: the first frames of a new window come late, and
	// counting all of that would skip the start of the way.
	now := c.Now()
	if !f.last.IsZero() {
		f.gone += min(now.Sub(f.last), deskFlyStep)
	}
	f.last = now
	f.frames++
	if f.frames == 2 && !f.opening && a.deskWin != nil {
		// The flight shows the line now: its window can leave it to it.
		a.deskWin.Invalidate()
	}
	if f.glyphs == nil {
		ink := a.deskShape(f.line, a.deskLookOf(a.settings.DeskStyle), 0, false)
		_, f.lineH = ink.Size()
		f.glyphs = deskGlyphs(ink, f.line)
	}
	_, total := deskFlyLags(len(f.glyphs))
	k := float32(f.gone) / float32(total)
	// The lyrics' window, its capsule if it has one, comes in as the
	// characters near it, or goes as they leave. Skip redundant OS opacity
	// syscalls while the value hasn't changed.
	if a.deskWin != nil {
		op := f.deskOpacity
		if f.opening {
			op = float64(easeRange(k, 0.5, 0.92))
		} else if !a.settings.DeskLyrics {
			op = float64(1 - easeRange(k, 0.05, 0.4))
		}
		if op >= 0 && math.Abs(op-f.deskOpacity) > 1e-4 {
			f.deskOpacity = op
			a.deskWin.SetOpacity(op)
		}
	}
	gone := f.gone
	ui.Box(c.Key("fly")).Absolute().Top(0).Left(0).Right(0).Bottom(0).PassThrough().Draw(func(p *ui.Painter, _ ui.Rect) {
		a.paintDeskFly(p, f, gone)
	})
	c.AnimationFrame()
	if f.gone < total {
		return
	}
	if f.opening && f.landed.IsZero() {
		// Down: the lyrics' window shows the line where they are.
		f.landed = now
		if a.deskWin != nil {
			a.deskWin.SetOpacity(1)
			a.deskWin.Invalidate()
		}
	}
	// A few frames more, for the lyrics' window to show its own line
	// before these leave it.
	if f.ending++; f.ending < 4 {
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

// paintDeskFly paints the characters gone into their way, from the button
// to where they land, and the button lit as they leave it or come back.
func (a *app) paintDeskFly(p *ui.Painter, f *deskFlight, gone time.Duration) {
	look := a.deskLookOf(a.settings.DeskStyle)
	pos := a.lyricPos()
	if f.glyphs == nil {
		ink := a.deskShape(f.line, look, pos, false)
		_, f.lineH = ink.Size()
		f.glyphs = deskGlyphs(ink, f.line)
	}
	glyphs := f.glyphs
	lag, total := deskFlyLags(len(glyphs))
	ox, oy := f.desk.X+f.at.X, f.desk.Y+f.at.Y
	bx, by := f.from.X+f.from.W/2, f.from.Y+f.from.H/2
	a.paintDeskGlyphsCached(p, f.line, look, pos, ox, oy, func(i int, g ui.Rect) (k float32, turn float32) {
		// Back, the way out run backwards: the last one out goes first,
		// from the end of the line, none crossing those still in it.
		order := i
		if !f.opening {
			order = len(glyphs) - 1 - i
		}
		u := clamp01(float32(gone-time.Duration(order)*lag) / float32(deskGlyphFly))
		if !f.opening {
			u = 1 - u
		}
		// Each its own way to turn, the same each time.
		turn = float32(6 + (i*7)%9)
		if i%2 == 1 {
			turn = -turn
		}
		return u, turn
	}, bx, by, f)
	// The button, lit, swelling as they leave it, and gone after the last
	// one; or lit as they come back into it.
	accent := a.pal.accent
	if ca, ok := a.coverAccent(); ok && a.settings.AccentCover {
		accent = ca
	}
	lags := total - deskGlyphFly
	var lit, swell float32
	if f.opening {
		swell = easeRange(float32(gone)/float32(90*time.Millisecond), 0, 1)
		lit = 1 - easeRange(float32(gone-lags)/float32(deskGlyphFly/3), 0, 1)
	} else {
		swell = 1
		lit = easeRange(float32(gone)/float32(deskGlyphFly), 0.55, 0.8) * (1 - easeRange(float32(total-gone)/float32(deskGlyphFly/4), 1, 0))
	}
	if lit > 0 {
		r := f.from.H / 2 * (1 + 0.35*swell)
		p.Fill(ui.Rect{X: bx - r, Y: by - r, W: 2 * r, H: 2 * r}, accent.Alpha(0.9*lit), r)
	}
}

// deskShape lays out the line as the lyrics' window does (deskLine): word
// by word if it is sung so, in the colors of where it is at pos if colored,
// else to be painted in one color.
func (a *app) deskShape(m deskMain, look deskLook, pos time.Duration, colored bool) *ui.TextLayout {
	family := a.fontFamily()
	if a.settings.LyricFont != "" {
		family = a.lyricFamily()
	}
	font := ui.Font{Family: family, Size: m.size, Weight: 800}
	if len(m.words) == 0 {
		return ui.ShapeText(m.text, font, 0)
	}
	spans := make([]ui.Span, len(m.words))
	for i, wd := range m.words {
		spans[i] = ui.Span{Text: wd.Text}
		if colored {
			spans[i].Color = a.deskInk(look, &wd, pos)
		}
	}
	return ui.ShapeRichText(spans, font, 0)
}

// deskInk is the color of a word of the line large on the desktop, or of
// all of it with no word.
func (a *app) deskInk(look deskLook, wd *lyrics.Word, pos time.Duration) ui.Color {
	if wd != nil && a.settings.DeskWords {
		return look.unsung.Mix(look.sung, wordLit(*wd, pos))
	}
	return look.sung
}

// deskGlyphs returns where each character of the line m, as laid out in
// ink, lies in it: from its left to the next one's, over and under the line
// for the edge. Spaces go with the character before them, and what the
// lyrics' window cuts off is left out.
func deskGlyphs(ink *ui.TextLayout, m deskMain) []ui.Rect {
	return deskGlyphsLimit(ink, m, deskW-90)
}

func deskGlyphsLimit(ink *ui.TextLayout, m deskMain, maxW float32) []ui.Rect {
	w, h := ink.Size()
	limit := min(w, maxW)
	units := utf16.Encode([]rune(m.shaped()))
	var out []ui.Rect
	for at := 0; at < len(units); {
		next := ink.NextBoundary(at)
		if next <= at {
			break
		}
		x0, x1 := ink.Caret(at).X, ink.Caret(next).X
		if x0 > x1 {
			x0, x1 = x1, x0
		}
		if x0 < limit && strings.TrimSpace(string(utf16.Decode(units[at:next]))) != "" {
			out = append(out, ui.Rect{X: x0, Y: -h * 0.6, W: min(x1, limit) - x0, H: h * 2.2})
		}
		at = next
	}
	// Each reaches the next, over the spaces between, which the edge and
	// some letters reach into: together they are the whole line.
	for i := 0; i+1 < len(out); i++ {
		out[i].W = out[i+1].X - out[i].X
	}
	return out
}

// shaped is the text of the line as deskShape lays it out: its words
// together, if it has them.
func (m deskMain) shaped() string {
	if len(m.words) == 0 {
		return m.text
	}
	var b strings.Builder
	for _, wd := range m.words {
		b.WriteString(wd.Text)
	}
	return b.String()
}

// deskGlyphClip returns the rectangle of character i in window DIPs, with
// overhang padding on the first and last characters for outline strokes.
func deskGlyphClip(glyphs []ui.Rect, i int, ox, oy, size float32) ui.Rect {
	g := glyphs[i]
	clip := ui.Rect{X: ox + g.X, Y: oy + g.Y, W: g.W, H: g.H}
	if i == 0 {
		clip.X -= size * 0.4
		clip.W += size * 0.4
	}
	if i == len(glyphs)-1 {
		clip.W += size * 0.4
	}
	return clip
}

// prepare renders the lyric line once into a GPU canvas at device-pixel
// resolution and records each character's source slice in canvas pixels.
func (s *deskFlyStrip) prepare(a *app, p *ui.Painter, m deskMain, look deskLook, pos time.Duration, glyphs []ui.Rect, lineH float32) {
	if len(glyphs) == 0 {
		return
	}
	scale := p.Scale()
	if scale <= 0 {
		scale = 1
	}
	wordTimed := len(m.words) > 0 && a.settings.DeskWords
	if s.ready && s.canvas != nil && s.scale == scale && s.look == look && len(s.src) == len(glyphs) && (!wordTimed || s.pos == pos) {
		return
	}
	mHi := m
	mHi.size = m.size * scale
	inkHi := a.deskShape(mHi, look, pos, true)
	var edgeHi *ui.TextLayout
	if look.edge.A > 0 {
		edgeHi = a.deskShape(mHi, look, pos, false)
	}
	_, hHi := inkHi.Size()
	gHi := deskGlyphsLimit(inkHi, mHi, (deskW-90)*scale)
	useHi := len(gHi) == len(glyphs)

	var minX, maxX, minY, maxY float32
	if useHi {
		first := deskGlyphClip(gHi, 0, 0, 0, mHi.size)
		last := deskGlyphClip(gHi, len(gHi)-1, 0, 0, mHi.size)
		minX, maxX = float32(math.Floor(float64(first.X))), float32(math.Ceil(float64(last.X+last.W)))
		minY, maxY = float32(math.Floor(float64(-hHi*0.6))), float32(math.Ceil(float64(hHi*1.6)))
	} else {
		first := deskGlyphClip(glyphs, 0, 0, 0, m.size)
		last := deskGlyphClip(glyphs, len(glyphs)-1, 0, 0, m.size)
		minX, maxX = float32(math.Floor(float64(first.X*scale))), float32(math.Ceil(float64((last.X+last.W)*scale)))
		minY, maxY = float32(math.Floor(float64(-lineH*0.6*scale))), float32(math.Ceil(float64(lineH*1.6*scale)))
	}
	cw, ch := max(1, int(maxX-minX)), max(1, int(maxY-minY))
	if s.canvas == nil {
		s.canvas = ui.NewCanvas(cw, ch)
	} else if w, h := s.canvas.Size(); w != cw || h != ch {
		s.canvas = ui.NewCanvas(cw, ch)
	}
	widthHi := max(1.1, m.size*0.055) * scale
	color := a.deskInk(look, nil, pos)
	dx, dy := -minX, -minY
	s.canvas.Draw(p, func(cp *ui.Painter) {
		if edgeHi != nil {
			for i := range 8 {
				turn := float64(i) * math.Pi / 4
				cp.TextLayout(edgeHi, dx+widthHi*float32(math.Cos(turn)), dy+widthHi*float32(math.Sin(turn)), look.edge)
			}
		}
		cp.TextLayout(inkHi, dx, dy, color)
	})
	s.src = s.src[:0]
	for i := range glyphs {
		var r ui.Rect
		if useHi {
			r = deskGlyphClip(gHi, i, dx, dy, mHi.size)
		} else {
			r = deskGlyphClip(glyphs, i, 0, 0, m.size)
			r = ui.Rect{X: r.X*scale + dx, Y: r.Y*scale + dy, W: r.W * scale, H: r.H * scale}
		}
		x0, y0 := max(float32(0), r.X), max(float32(0), r.Y)
		x1, y1 := min(float32(cw), r.X+r.W), min(float32(ch), r.Y+r.H)
		s.src = append(s.src, ui.Rect{X: x0, Y: y0, W: max(0, x1-x0), H: max(0, y1-y0)})
	}
	s.scale, s.look, s.pos, s.ready = scale, look, pos, true
}

// paintDeskGlyphs paints the line, its edge first if it has one, as the
// lyrics' window does, at (ox, oy), each character where way says it is:
// k of the way from the button at (bx, by), where it is a third of its size,
// to its place, turned by turn degrees out of the way. All at their place,
// it is the line as the lyrics' window shows it.
func (a *app) paintDeskGlyphs(p *ui.Painter, m deskMain, look deskLook, pos time.Duration, ox, oy float32, way func(i int, g ui.Rect) (k, turn float32), bx, by float32) {
	a.paintDeskGlyphsCached(p, m, look, pos, ox, oy, way, bx, by, nil)
}

func (a *app) paintDeskGlyphsCached(p *ui.Painter, m deskMain, look deskLook, pos time.Duration, ox, oy float32, way func(i int, g ui.Rect) (k, turn float32), bx, by float32, f *deskFlight) {
	var glyphs []ui.Rect
	var h float32
	var ink, edge *ui.TextLayout
	if f != nil && f.glyphs != nil {
		glyphs, h = f.glyphs, f.lineH
	} else {
		ink = a.deskShape(m, look, pos, true)
		_, h = ink.Size()
		glyphs = deskGlyphs(ink, m)
	}
	if len(glyphs) == 0 {
		return
	}
	type step struct {
		k, turn float32
	}
	steps := make([]step, len(glyphs))
	anyLanded, anyFlying := false, false
	for i, g := range glyphs {
		k, turn := way(i, g)
		steps[i] = step{k, turn}
		if k >= 1 {
			anyLanded = true
		} else {
			anyFlying = true
		}
	}

	// Landed characters draw directly under axis-aligned GPU scissor clips
	// (merged across contiguous runs) so the handover to deskWin is pixel-exact.
	if anyLanded {
		if ink == nil {
			ink = a.deskShape(m, look, pos, true)
		}
		if look.edge.A > 0 {
			if f != nil && f.edge != nil && f.edgeStyle == a.settings.DeskStyle {
				edge = f.edge
			} else {
				edge = a.deskShape(m, look, pos, false)
				if f != nil {
					f.edge, f.edgeStyle = edge, a.settings.DeskStyle
				}
			}
		}
		width := max(1.1, m.size*0.055)
		color := a.deskInk(look, nil, pos)
		paint := func() {
			if edge != nil {
				for i := range 8 {
					turn := float64(i) * math.Pi / 4
					p.TextLayout(edge, ox+width*float32(math.Cos(turn)), oy+width*float32(math.Sin(turn)), look.edge)
				}
			}
			p.TextLayout(ink, ox, oy, color)
		}
		for i := 0; i < len(glyphs); {
			if steps[i].k < 1 {
				i++
				continue
			}
			span := deskGlyphClip(glyphs, i, ox, oy, m.size)
			j := i + 1
			for j < len(glyphs) && steps[j].k >= 1 {
				next := deskGlyphClip(glyphs, j, ox, oy, m.size)
				x0 := min(span.X, next.X)
				x1 := max(span.X+span.W, next.X+next.W)
				span.X, span.W = x0, x1-x0
				j++
			}
			p.Clip(span, 0, paint)
			i = j
		}
	}

	if !anyFlying {
		return
	}

	// Flying characters sample from a single GPU canvas strip via ImageRegion
	// without p.Clip inside p.Transform, avoiding per-character CPU ClipMask
	// rasterization and batching all glyphs into one GPU draw call.
	var local deskFlyStrip
	strip := &local
	if f != nil {
		strip = &f.strip
	}
	strip.prepare(a, p, m, look, pos, glyphs, h)
	if strip.canvas == nil {
		return
	}
	bmp := strip.canvas.Bitmap()
	for i, g := range glyphs {
		st := steps[i]
		if st.k >= 1 {
			continue
		}
		cx, cy := ox+g.X+g.W/2, oy+h/2
		clip := deskGlyphClip(glyphs, i, ox, oy, m.size)
		// Its center where it lands, and where it is now: on the way out of
		// the button it rises sooner than it crosses, and comes down last.
		travel := easeRange(st.k, 0, 1)
		nx, ny := lerp(bx, cx, travel), lerp(by, cy, 1-(1-travel)*(1-travel))
		// A third of its size out of the button, growing on its way, a
		// little more just before it lands, then itself.
		s := lerp(0.32, 1, easeRange(st.k, 0, 0.78)) + 0.08*float32(math.Sin(math.Pi*float64(clamp01((st.k-0.78)/0.22))))
		r := st.turn * (1 - easeRange(st.k, 0.3, 1))
		t := ui.Scaling(s, s).Then(ui.Rotation(r)).Then(ui.Translation(nx-cx, ny-cy))
		src := strip.src[i]
		p.Transform(t, cx, cy, func() {
			p.ImageRegion(bmp, src, clip, ui.ImageOptions{})
		})
	}
}
