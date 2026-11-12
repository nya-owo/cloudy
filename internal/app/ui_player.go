package app

import (
	"math"
	"time"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/player"
)

// scrubState is a progress bar's own: the value dragged, while it is, and
// the levels of the music its smoke follows.
type scrubState struct {
	v float64
	// held is set while the pointer pressed on the bar is down, wherever
	// it has gone since: Pressed is false off the bar, which is thin.
	held   bool
	active bool // dragged since the last frame, to seek once let go
	// lens makes the knob a lens of glass, as the sliders of iOS 26 have,
	// for a bar on glass.
	lens bool
	// lamp gives the bar its glow in Now Playing (ui_light.go).
	lamp  bool
	level barState
}

func secs(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }

// scrubber shows how far the track is, and seeks where it is dragged or
// clicked: the bar thickens under the pointer, and its knob shows. With a
// smoke color, the bar is a comet while music plays: what has played is a
// streak brightening to the playhead, which glows and trails smoke that
// rises and fades, thicker as the music is louder.
func (a *app) scrubber(c *ui.Context, st *scrubState, track, fill ui.Color, height float32, smoke ui.Color) ui.Element {
	pl := a.pl
	dur := pl.Duration().Seconds()
	if !st.active && !st.held {
		st.v = pl.Position().Seconds()
	}
	s := ui.SliderBase(c, &st.v, 0, max(dur, 1)).Height(height).FocusRing(false).Label("播放进度").Cursor(ui.CursorPointer)
	// The press and its release come to the bar wherever the pointer is,
	// and go on to the slider, which follows the pointer.
	s.HandleInput(func(ev ui.InputEvent) bool {
		switch ev.Kind {
		case ui.InputPointerDown:
			st.held = ev.Button == 0
		case ui.InputPointerUp:
			st.held = false
		}
		return false
	})
	pressed := st.held
	switch {
	case pressed:
		st.active = true
	case st.active:
		// Let go: there, but not right at the end, which would end the
		// song as it begins to play.
		st.active = false
		pl.Seek(secs(min(st.v, max(0, dur-1))))
	case s.Changed():
		pl.Seek(secs(min(st.v, max(0, dur-1))))
	}
	hot := s.Hovered() || pressed
	thick := float32(4)
	if hot {
		thick = 6
	}
	thick = s.AnimateTo("thick", thick, ui.Snappy)
	knob := float32(0)
	if hot {
		knob = 1
	}
	if st.lens {
		// The lens swells as it is held, and settles with a little spring.
		if pressed {
			knob = 1.25
		}
		knob = s.AnimateTo("knob", knob, ui.Animation{Spring: ui.SpringOptions{Duration: 340 * time.Millisecond, Bounce: 0.3}})
	} else {
		knob = s.AnimateTo("knob", knob, ui.Snappy)
	}
	// The smoke settles away as the music pauses, and while dragging.
	smoking := float32(0)
	if smoke.A > 0 && a.settings.Smoke && pl.Playing() && !pl.Loading() && !st.active {
		smoking = 1
	}
	smoking = s.AnimateTo("smoke", smoking, ui.Animation{Duration: 600 * time.Millisecond})
	s.Draw(func(p *ui.Painter, r ui.Rect) {
		at := st.v
		if !st.active {
			at = pl.Position().Seconds()
		}
		frac := float32(0)
		if dur > 0 {
			frac = float32(min(1, max(0, at/dur)))
		}
		y := r.Y + (r.H-thick)/2
		head, cy := r.X+r.W*frac, r.Y+r.H/2
		p.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: thick}, track, thick/2)
		// What has downloaded, a shade lighter.
		if got, ok := pl.Downloaded(); ok && float32(got) < 1 {
			p.Fill(ui.Rect{X: r.X, Y: y, W: r.W * float32(got), H: thick}, track, thick/2)
		}
		played := ui.Rect{X: r.X, Y: y, W: max(thick, head-r.X), H: thick}
		if st.lamp {
			drawBarGlow(p, played, cy, a.glowColor, a.glowGain())
		}
		if smoking > 0.01 {
			lv, live := pl.Levels()
			st.level.step(p.Now(), lv, live)
			drawStreak(p, played, head, cy, thick, st.level.group(0, 6), fill, smoke, smoking)
		} else {
			p.Fill(played, fill, thick/2)
		}
		if st.lens && knob > 0.01 {
			w, h := 30*knob, 18*knob
			k := ui.Rect{X: head - w/2, Y: cy - h/2, W: w, H: h}
			p.Fill(k, white, h/2)
		} else if knob > 0.01 {
			k := 12 * knob
			p.Shadow(ui.Rect{X: head - k/2, Y: r.Y + (r.H-k)/2, W: k, H: k}, k/2, 0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.3))
			p.Fill(ui.Rect{X: head - k/2, Y: r.Y + (r.H-k)/2, W: k, H: k}, fill, k/2)
		}
		switch {
		case smoking > 0.01 && (pl.Playing() || smoking < 1):
			// The smoke drifts slowly, under a point a frame at 30 frames
			// a second, which show it smooth for a quarter of what a 120 Hz
			// display would draw.
			p.After(time.Second / 30)
		case pl.Playing() && !st.active && dur > 0:
			// Again when the fill has moved a pixel of the screen, which
			// takes a good part of a second, rather than every frame.
			perPixel := dur / float64(r.W*p.Scale())
			p.After(max(16*time.Millisecond, secs(perPixel)))
		}
	})
	return s
}

// drawStreak draws what has played, as music plays: a streak of light,
// brightest at its head, with the smoke of the head and its glow, swelling
// with the bass.
func drawStreak(p *ui.Painter, played ui.Rect, head, cy, thick, bass float32, fill, smoke ui.Color, smoking float32) {
	p.FillGradient(played, ui.LinearGradient{Angle: 90, Oklab: true,
		From: fill.Mix(smoke, smoking).Alpha(0.3), To: fill.Mix(smoke, 0.8*smoking)}, thick/2)
	drawSmoke(p, head, cy, p.Now(), bass, smoke, smoking)
	glow := 9 + 8*bass
	p.Shadow(ui.Rect{X: head, Y: cy}, 0, 0, 0, glow, 2.5+2.5*bass, smoke.Alpha(0.95*smoking))
	core := thick * (1 + 0.25*bass)
	p.Fill(ui.Rect{X: head - core/2, Y: cy - core/2, W: core, H: core}, white.Mix(smoke, 0.15).Alpha(smoking), core/2)
}

// Smoke from the playhead: wisps shed one after another, each drifting
// back along the bar and rising, growing and fading over its life. Where a
// wisp is follows from the time alone, so nothing is kept between frames.
const (
	smokeLife  = 1.4 // seconds
	smokeWisps = 36  // alive at once, at most
)

// drawSmoke draws the smoke of a playhead at (x, y) at now, thicker as
// loud (0 to 1) is higher, at opacity shown.
func drawSmoke(p *ui.Painter, x, y float32, now time.Time, loud float32, color ui.Color, shown float32) {
	t := float64(now.UnixMilli()) / 1000
	interval := smokeLife / smokeWisps
	first := math.Floor(t / interval)
	for j := range smokeWisps {
		k := first - float64(j)
		age := float32(t - k*interval)
		if age < 0 || age > smokeLife {
			continue
		}
		// A wisp of its own: how fast it drifts and rises, how it curls,
		// how large it grows. Quiet music sheds fewer of them.
		r := func(o float64) float32 { return lineRandom(k, o) }
		if r(5) > 0.6+0.4*loud {
			continue
		}
		f := age / smokeLife
		wx := x - age*(12+22*r(1))
		wy := y - age*(12+18*r(2)) + float32(math.Sin(float64(age)*(2.5+2*float64(r(3)))+float64(r(4))*2*math.Pi))*6*f
		size := 3 + f*(12+10*r(4))
		alpha := 0.5 * float32(math.Pow(float64(1-f), 1.4)) * shown * (0.75 + 0.5*loud)
		p.Shadow(ui.Rect{X: wx, Y: wy}, 0, 0, 0, size, size*0.45, color.Alpha(alpha))
	}
}

// tickTimes redraws the view as the next second of the track begins, for
// the times shown.
func (a *app) tickTimes(c *ui.Context) {
	if a.pl.Playing() && a.pl.Ready() {
		pos := a.pl.Position()
		c.After(time.Second - pos%time.Second + 10*time.Millisecond)
	}
}

// volumeButton is the speaker, which opens the volume in a popover above
// it, as an AppKit popover: a native slider standing up, the volume in
// percent over it. It opens as the pointer comes over the speaker, which
// shows that the wheel there turns it, and closes a moment after the
// pointer has left both; a click keeps it open until a click elsewhere.
func (a *app) volumeButton(c *ui.Context, color ui.Color) {
	b := iconButton(c.Key("volume-button"), volumeIcon(a.volume), 16, color, "音量")
	h := &a.volumeHover
	now := c.Now()
	if b.Clicked() {
		// A click mutes, and another brings the volume back; the slider
		// opens under the pointer.
		a.toggleMute()
	}
	over := b.Hovered()
	if over && !a.volumeOpen {
		a.volumeOpen, h.byHover = true, true
	}
	b.HandleInput(func(ev ui.InputEvent) bool { return a.scrollVolume(ev) })
	pop := popoverColors(a.pal.dark)
	ui.PopoverBase(c.Key("volume-popover"), b, &a.volumeOpen, func(panel ui.Element) {
		if panel.Hovered() {
			h.at = c.Now()
		}
		const arrow = 7
		a.pane(panel, 26, false)
		panel.AttachTo(b, ui.AnchorTop, ui.AnchorBottom).Margin(0, 0, arrow+3, 0).Width(52).Padding(11, 0, 12, 0).
			AlignItems(ui.Center).Gap(8).Label("音量").TransformOrigin(0.5, 1)
		a.paneMotion(panel)
		// Of glass, there is no arrow: what shows through would stop at it.
		if a.settings.panelGlass() {
			ui.Textf(c, "%d%%", int(a.volume*100+0.5)).FontSize(11).FontWeight(500).FontFeatures("tnum").TextColor(a.pal.muted)
			a.nativeVolumeSlider(c, pop)
			return
		}
		panel.Draw(func(p *ui.Painter, r ui.Rect) {
			// The arrow pointing at the speaker, its sides edged as the
			// panel is, over the panel's border.
			cx, foot := r.X+r.W/2, r.Y+r.H
			tip := (&ui.Path{}).MoveTo(cx-arrow-1, foot-0.75).LineTo(cx-1.2, foot+arrow-1.2).
				QuadTo(cx, foot+arrow, cx+1.2, foot+arrow-1.2).LineTo(cx+arrow+1, foot-0.75).Close()
			p.FillPath(tip, pop.fill)
			edge := (&ui.Path{}).MoveTo(cx-arrow-1, foot).LineTo(cx-1.2, foot+arrow-1.2).
				QuadTo(cx, foot+arrow, cx+1.2, foot+arrow-1.2).LineTo(cx+arrow+1, foot)
			p.StrokePath(edge, 0.5, pop.edge)
		})
		ui.Textf(c, "%d%%", int(a.volume*100+0.5)).FontSize(11).FontWeight(500).FontFeatures("tnum").TextColor(a.pal.muted)
		a.nativeVolumeSlider(c, pop)
	})
	// Opened by the pointer, it closes a moment after the pointer left the
	// speaker and the popover both, which the panel says as it is built.
	if over {
		h.at = now
	}
	if a.volumeOpen && h.byHover {
		if left := now.Sub(h.at); left >= volumeLinger {
			a.volumeOpen, h.byHover = false, false
		} else {
			c.After(volumeLinger - left)
		}
	}
}

// hoverOpen is how a popover the pointer opens stays open: whether the
// pointer opened it rather than a click, and when the pointer was last
// over it or what opened it.
type hoverOpen struct {
	byHover bool
	at      time.Time
}

// volumeLinger is how long the volume stays after the pointer left it, for
// a pointer passing from the speaker up to the slider, or out a moment.
const volumeLinger = 350 * time.Millisecond

// popover holds the colors of a popover as AppKit draws them.
type popover struct {
	fill, edge, track, knob ui.Color
}

func popoverColors(dark bool) popover {
	if dark {
		return popover{fill: ui.Hex("#2b2b2e"), edge: ui.RGBA(255, 255, 255, 0.14), track: ui.RGBA(255, 255, 255, 0.16), knob: ui.Hex("#ececec")}
	}
	return popover{fill: ui.Hex("#f6f6f6"), edge: ui.RGBA(0, 0, 0, 0.12), track: ui.RGBA(0, 0, 0, 0.12), knob: ui.Hex("#ffffff")}
}

// nativeVolumeSlider is a vertical slider as AppKit draws one: a thin
// track, filled with the accent from the foot to a round knob.
func (a *app) nativeVolumeSlider(c *ui.Context, pop popover) {
	const knob, track = 20, 4
	s := ui.SliderBase(c.Key("volume"), &a.volume, 0, 1).Vertical().Size(knob+8, 124).PaddingY(knob / 2).
		Step(0.01).FocusRing(false).Label("音量")
	// Opened by a click, the keys move it; opened by the pointer passing,
	// it leaves the keys where they were, as in the search field.
	if !a.volumeHover.byHover {
		s.AutoFocus()
	}
	if s.Changed() {
		a.pl.SetVolume(a.volume)
	}
	s.HandleInput(func(ev ui.InputEvent) bool { return a.scrollVolume(ev) })
	pressed := s.Pressed()
	v := float32(a.volume)
	accent := a.pal.accent
	s.Draw(func(p *ui.Painter, r ui.Rect) {
		top, foot := r.Y+knob/2, r.Y+r.H-knob/2
		x := r.X + (r.W-track)/2
		y := foot - (foot-top)*v
		p.Fill(ui.Rect{X: x, Y: top - track/2, W: track, H: foot - top + track}, pop.track, track/2)
		p.Fill(ui.Rect{X: x, Y: y, W: track, H: foot - y + track/2}, accent, track/2)
		k := ui.Rect{X: r.X + (r.W-knob)/2, Y: y - knob/2, W: knob, H: knob}
		p.Shadow(k, knob/2, 0, 0.5, 1.5, 0, ui.RGBA(0, 0, 0, 0.3))
		p.Shadow(k, knob/2, 0, 1, 4, 0, ui.RGBA(0, 0, 0, 0.12))
		fill := pop.knob
		if pressed {
			fill = fill.Mix(ui.RGB(0, 0, 0), 0.06)
		}
		p.Fill(k, fill, knob/2)
		p.Stroke(k, ui.RGBA(0, 0, 0, 0.08), knob/2, 0.5)
	})
}

// scrollVolume turns the volume up as the wheel or the fingers scroll up.
func (a *app) scrollVolume(ev ui.InputEvent) bool {
	if ev.Kind != ui.InputScroll {
		return false
	}
	// One wheel notch is about 100 DIPs, and should be one percent; a
	// touchpad's finer deltas stay fine.
	a.volume = min(1, max(0, a.volume-float64(ev.DY)/10000))
	a.pl.SetVolume(a.volume)
	return true
}

// volumeIcon is the speaker for volume v: silent, or with one or two waves.
func volumeIcon(v float64) *ui.SVG {
	switch {
	case v == 0:
		return iconMute
	case v < 0.4:
		return iconVolumeLow
	default:
		return iconVolume
	}
}

// muteButton is the speaker, which mutes and unmutes.
func (a *app) muteButton(c *ui.Context, color ui.Color) {
	icon := iconVolume
	if a.volume == 0 {
		icon = iconMute
	}
	if iconButton(c.Key("mute"), icon, 16, color, "静音").Clicked() {
		a.toggleMute()
	}
}

func (a *app) toggleMute() {
	if a.volume > 0 {
		a.mutedFrom, a.volume = a.volume, 0
	} else if a.mutedFrom > 0 {
		a.volume = a.mutedFrom
	} else {
		a.volume = 0.5
	}
	a.pl.SetVolume(a.volume)
}

// volumeSlider is a slim slider with the speaker before it, which mutes.
func (a *app) volumeSlider(c *ui.Context, color, track ui.Color, width float32) {
	ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
		a.muteButton(c, color)
		s := ui.SliderBase(c.Key("volume"), &a.volume, 0, 1).Width(width).Height(16).FocusRing(false).Label("音量")
		if s.Changed() {
			a.pl.SetVolume(a.volume)
		}
		hot := s.Hovered() || s.Pressed()
		thick := float32(4)
		if hot {
			thick = 5
		}
		thick = s.AnimateTo("thick", thick, ui.Snappy)
		v := float32(a.volume)
		lamp := a.settings.NpLight
		s.Draw(func(p *ui.Painter, r ui.Rect) {
			y := r.Y + (r.H-thick)/2
			p.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: thick}, track, thick/2)
			filled := ui.Rect{X: r.X, Y: y, W: max(thick, r.W*v), H: thick}
			if lamp {
				drawBarGlow(p, filled, y+thick/2, a.glowColor, a.glowGain())
			}
			p.Fill(filled, color, thick/2)
		})
	})
}

// transport is the row of controls: the play mode, previous, play, next.
func (a *app) transport(c *ui.Context, color, dim, on ui.Color, size float32) {
	pl := a.pl
	ui.Row(c).Gap(size * 0.5).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
		// One button goes through the modes, lit but in order.
		mode := pl.Mode()
		tint := on
		if mode == player.ModeOrder {
			tint = dim
		}
		if iconButton(c.Key("mode"), modeIcon(mode), size*0.62, tint, mode.Name()).Clicked() {
			a.say("mode", pl.CycleMode().Name())
		}
		if iconButton(c.Key("prev"), iconPrev, size*0.85, color, "上一首").Clicked() {
			pl.Prev()
		}
		play := playButton(c.Key("play"), pl.Playing(), size*1.25, color)
		a.playSpot = play.LayoutBounds()
		if a.playFlies != nil {
			a.playFlies(play)
		}
		if play.Clicked() {
			pl.TogglePlay()
		}
		if iconButton(c.Key("next"), iconNext, size*0.85, color, "下一首").Clicked() {
			pl.Next()
		}
		// As wide as the mode's button, for play to stay in the middle.
		ui.Box(c).Width(size*0.62 + 16).Height(1)
	})
}

// modeIcon is the icon of play mode m.
func modeIcon(m player.PlayMode) *ui.SVG {
	switch m {
	case player.ModeLoop:
		return iconRepeat
	case player.ModeOne:
		return iconRepeat1
	case player.ModeShuffle:
		return iconShuffle
	}
	return iconOrder
}

// The bar of glass floats as a capsule, its corners concentric with those of
// the cover it holds, in the room the bar along the foot takes: pages don't
// change for it.
const (
	glassBarSide  = 14
	glassBarFoot  = 12
	glassBarInset = 12 // around the cover
)

// playerBar runs along the foot of the content: what plays, its controls
// and its progress, and the volume. With LiquidGlass, it floats over the
// page as a capsule of glass, which what scrolls under it bends through.
func (a *app) playerBar(c *ui.Context, foreground float32) {
	p := a.pal
	pl := a.pl
	tr := pl.Current()
	a.tickTimes(c)
	if a.picked.id != 0 && (tr == nil || tr.ID != a.picked.id || pl.Playing()) {
		// Played, or gone: it says what it is again.
		a.picked = pickedUp{}
	}
	liquid := a.settings.playerGlass()
	a.barScrub.lens = liquid
	// Glass shows anything through: what is on it keeps its contrast by
	// being stronger than over the plain page.
	muted, faint, track, cover := p.muted, p.faint, p.press, float32(8)
	glassH := float32(playerBarHeight - glassBarFoot)
	if liquid {
		muted, faint, track = p.text.Alpha(0.84), p.text.Alpha(0.74), p.text.Alpha(0.22)
		cover = glassH/2 - glassBarInset - 8
	} else {
		// Over the foot of the page: while content is under it, its edge
		// frosts it, with a hairline along the top.
		edge := ui.Box(c.Key("bottom-edge")).Absolute().Left(0).Right(0).Bottom(0).Height(playerBarHeight).
			Material(glass.ScrollEdge{Hard: true, Bottom: true, Background: p.bg})
		edge.Opacity(edgeShown(edge, a.underBottom))
	}
	// A record, as it turns.
	spinning := tr != nil && a.spins()
	if spinning {
		cover = 24
	}
	// The controls keep the middle, between sides that share what is left:
	// narrower, the middle gives up to 140 DIPs, and what plays keeps room
	// for its cover, some of its title and its heart.
	// A window of the least width has room for four of the six buttons on the
	// right: the floating windows give way, which the tray and the settings
	// open too.
	ww, _ := c.Size()
	roomy := ww >= 1100
	controls := float32(4*32 + 3*4) // translation, adding to a playlist, the speaker and the details
	if roomy {
		controls = 6*32 + 5*4 // and the floating windows
	}
	bar := ui.Row(c).Absolute().Gap(20).AlignItems(ui.Center)
	a.barBox = bar.LayoutBounds()
	if liquid {
		bar.Left(glassBarSide).Right(glassBarSide).Bottom(glassBarFoot).Height(glassH).Padding(0, 20, 0, glassBarInset).
			Radius(glassH/2).Shadow(0, 6, 24, -6, ui.RGBA(0, 0, 0, 0.3)).
			Material(a.floatGlass(p.dark))
	} else {
		bar.Left(0).Right(0).Bottom(0).Height(playerBarHeight).PaddingX(20)
	}
	bar.Children(func() {
		// What plays.
		ui.Row(c).Grow(1).Basis(0).MinWidth(150).Opacity(foreground).Children(func() {
			if tr == nil {
				ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(48, 48).Radius(cover).Background(p.hover).Center().Children(func() {
						ui.Icon(c, iconNote).Size(18, 18).TextColor(faint)
					})
					ui.Text(c, "未在播放").FontSize(13).TextColor(muted)
				})
				return
			}
			info := ui.ButtonBase(c.Key("info")).Gap(12).AlignItems(ui.Center).FocusRing(false).Shrink(1).MinWidth(0).Label("打开正在播放")
			hovered := info.Hovered()
			info.Children(func() {
				art := ui.Box(c).Size(48, 48).Shrink(0)
				a.barSpot.cover = art.LayoutBounds()
				a.lifted(art)
				art.Children(func() {
					// Another song's comes in over the last one's.
					var shown ui.Element
					if spinning {
						shown = a.barDisc(c, tr, 48, discFrame)
					} else {
						shown = a.cover(c.Key(tr.ID), tr.Album.PicURL, 120, 48, cover)
					}
					shown.Shadow(0, 2, 8, 0, p.cardShade).
						Transition(ui.ElementTransition{Animation: &motionPane, Enter: &ui.Motion{Scale: 0.9}, Exit: &ui.Motion{Scale: 0.9}})
					if hovered {
						ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(cover).Center().
							Background(ui.RGBA(0, 0, 0, 0.35)).TextColor(ui.RGB(255, 255, 255)).Children(func() {
							ui.Icon(c, iconDown).Size(18, 18).Rotate(180)
						})
					}
				})
				ui.Column(c).Shrink(1).MinWidth(0).Gap(3).Children(func() {
					title := ui.Text(c.Key(tr.ID), tr.Name).FontSize(13.5).FontWeight(600).SingleLine().
						Transition(ui.ElementTransition{Animation: &motionPane, Enter: &ui.Motion{Y: 10}, Exit: &ui.Motion{Y: -10}})
					a.barSpot.title = title.LayoutBounds()
					a.lifted(title)
					// What it is waiting on shows in place of the artists.
					var sub string
					color := muted
					switch {
					case a.picked.id == tr.ID:
						sub = a.picked.heard
					case pl.Loading():
						sub = "正在缓冲…"
					case pl.Err() != "":
						sub, color = pl.Err(), p.accent
					}
					if sub == "" {
						a.artistLinks(c, tr, muted, p.text).FontSize(12).SingleLine()
					} else {
						ui.Text(c, sub).FontSize(12).TextColor(color).SingleLine()
					}
				})
			})
			if info.Clicked() {
				a.nowPlaying = true
			}
			a.likeCountButton(c.Key("like"), tr, 16, muted).Margin(0, 0, 0, 4).Shrink(0)
		})

		// Controls over the progress.
		middle := float32(300)
		if !roomy {
			middle = 240
		}
		ui.Column(c).Width(440).Shrink(1).MinWidth(middle).Gap(0).AlignItems(ui.Stretch).Opacity(foreground).Children(func() {
			a.playFlies = a.lifted
			a.transport(c, p.text, muted, p.accent, 18)
			a.playFlies = nil
			a.barSpot.play = a.playSpot
			if tr == nil {
				// Nothing to show the progress of: what to do, where the
				// times and an empty bar said 0:00 of nothing.
				ui.Row(c).Height(14).Margin(-4, 0, 0, 0).Justify(ui.Center).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "从左侧挑一张歌单，或搜索一首歌开始播放").FontSize(10.5).TextColor(faint).SingleLine()
				})
				return
			}
			ui.Row(c).Gap(10).AlignItems(ui.Center).Margin(-4, 0, 0, 0).Children(func() {
				ui.Text(c, clock(pl.Position())).FontSize(10.5).FontFeatures("tnum").TextColor(faint).Width(34).TextAlign(ui.End)
				scrub := a.scrubber(c.Key("bar-scrub"), &a.barScrub, track, p.text.Alpha(0.75), 14, p.accent).Grow(1)
				a.barSpot.scrub = scrub.LayoutBounds()
				a.barSpot.radius, a.barSpot.track, a.barSpot.fill, a.barSpot.smoke = cover, track, p.text.Alpha(0.75), p.accent
				a.lifted(scrub)
				ui.Text(c, clock(pl.Duration())).FontSize(10.5).FontFeatures("tnum").TextColor(faint).Width(34)
			})
		})

		// The floating windows, the volume, and the song's details.
		ui.Row(c).Grow(1).Basis(0).MinWidth(controls).Justify(ui.End).AlignItems(ui.Center).Gap(4).Opacity(foreground).Children(func() {
			desk := muted
			if a.settings.DeskLyrics {
				desk = p.accent
			}
			if roomy {
				deskBtn := iconButton(c.Key("desk-lyrics"), iconSubtitles, 16, desk, "桌面歌词")
				a.deskBtn = deskBtn.LayoutBounds()
				if deskBtn.Clicked() {
					a.toggleDeskLyrics()
				}
				if iconButton(c.Key("mini"), iconMiniWin, 16, muted, "迷你窗口").Clicked() {
					a.showMini()
				}
			}
			// The song's translation under each line of its lyrics, lit
			// while it shows: Now Playing's and the desktop lyrics'.
			trans := muted
			if a.settings.LyricTrans {
				trans = p.accent
			}
			if iconButton(c.Key("lyric-trans"), iconTranslate, 16, trans, "歌词翻译").Clicked() {
				a.toggleLyricTrans()
			}
			// Adding the song playing to one of the listener's playlists.
			if tr != nil && tr.ProgramID == 0 && tr.ID > 0 {
				add := iconButton(c.Key("add-to-playlist"), iconListPlus, 16, muted, "添加到歌单")
				if add.Clicked() {
					a.addToPlaylistMenu(add, tr)
				}
			}
			a.volumeButton(c, p.text.Alpha(0.75))
			if tr != nil {
				a.songMenuLabeled(c.Key("details"), tr, 16, muted, "歌曲详情")
			}
		})
	})
}
