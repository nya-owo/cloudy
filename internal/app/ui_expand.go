package app

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// How things move across the app: three speeds, for one hand. What follows
// the pointer is on a spring, which keeps going as it is turned; the rest are
// on a curve that ends at rest, for nothing to land with speed left, overshoot
// what it lands on, or creep over its last pixels one at a time.
var (
	motionEase = ui.CubicBezier(0.22, 0.8, 0.24, 1)
	motionFast = ui.Animation{Spring: ui.SpringOptions{Duration: 200 * time.Millisecond}} // what follows the pointer
	motionPane = ui.Animation{Duration: 280 * time.Millisecond, Ease: motionEase}         // what comes over the app
	motionPage = ui.Animation{Duration: 340 * time.Millisecond, Ease: motionEase}         // what changes the view
)

// How much the app moves (settings.Motion).
const (
	motionOff = iota
	motionLite
	motionFull
)

// motionLevel is how much the app moves: not at all while the system asks
// for less motion.
func (a *app) motionLevel(c *ui.Context) int {
	switch {
	case c.Preferences().ReduceMotion || a.settings.Motion == "off":
		return motionOff
	case a.settings.Motion == "lite":
		return motionLite
	}
	return motionFull
}

// A flightSpot is where what stays through Now Playing opening is, and how it
// looks there: in the bar it grows out of, and in the view it grows into.
type flightSpot struct {
	cover, title, scrub, play ui.Rect
	radius                    float32  // of the cover
	track, fill, smoke        ui.Color // of the progress
}

// lerpRect is the rectangle k of the way from a to b.
func lerpRect(a, b ui.Rect, k float32) ui.Rect {
	return ui.Rect{X: lerp(a.X, b.X, k), Y: lerp(a.Y, b.Y, k), W: lerp(a.W, b.W, k), H: lerp(a.H, b.H, k)}
}

// easeRange is 0 up to from and 1 from to, smoothly between.
func easeRange(k, from, to float32) float32 {
	t := clamp01((k - from) / (to - from))
	return t * t * (3 - 2*t)
}

// What stays goes over to what flies, and back, within the last half a percent of
// the way at either end, where the two are a few pixels apart: any sooner, and
// both show, side by side. The last thousandth is where they are one.
const (
	flightEnd   = 0.005
	flightTouch = 0.001
)

// npFlying reports whether what stays is on its way, drawn by flights.
func (a *app) npFlying() bool {
	return a.motion == motionFull && a.npK > 0 && a.npK < 1 && a.barSpot.cover.W > 0 && a.npSpot.cover.W > 0
}

// flights draws what stays through Now Playing opening, at k of the way from
// the bar, over the view: the cover, the title, the progress and the play
// button. They are the same things going somewhere else, which is what ties
// the view to the bar it grew from: as those of the bar leave it, and until
// those of the view are there, they are all there is of them. So they look
// at either end as what they take over from does.
func (a *app) flights(c *ui.Context, tr *netease.Track, k float32) {
	if !a.npFlying() {
		return
	}
	from, to := a.barSpot, a.npSpot
	ink := a.pal.text.Mix(white, k)
	shown := easeRange(k, 0, flightTouch) * (1 - easeRange(k, 1-flightTouch, 1))
	// The cover stands smaller while paused, and larger under the pointer.
	s := a.npCoverScale
	land := to.cover
	land = ui.Rect{X: land.X + land.W*(1-s)/2, Y: land.Y + land.H*(1-s)/2, W: land.W * s, H: land.H * s}
	cover := lerpRect(from.cover, land, k)
	radius := lerp(from.radius, to.radius, k)
	bmp := bitmap(a.art.get(netease.Sized(tr.Album.PicURL, 600), true))
	if bmp == nil {
		bmp = bitmap(a.art.get(netease.Sized(tr.Album.PicURL, 120), false))
	}
	box := ui.Box(c.Key("fly-cover")).Absolute().Left(cover.X).Top(cover.Y).Size(cover.W, cover.H).Radius(radius).Opacity(shown).
		Shadow(0, lerp(2, 30, k), lerp(8, 60, k), -12*k, a.pal.cardShade.Mix(ui.RGBA(0, 0, 0, 0.55), k))
	if a.spins() {
		// Unwinding the short way, from however the record stood.
		box.Rotate(float32(math.Remainder(float64(a.spin.at(c.Now())), 360)) * (1 - k))
	}
	if bmp == nil {
		box.Background(white.Alpha(0.08))
	}
	box.Children(func() { artImage(c, bmp, radius, 1) })
	if to.title.W > 0 {
		t := lerpRect(from.title, to.title, k)
		s := lerp(13.5/22, 1, k)
		// As wide as it is where it is, for it to break off as it does there.
		title := ui.Text(c.Key("fly-title"), tr.Name).Absolute().Left(t.X).Top(t.Y).Width(t.W/s).FontSize(22).
			LetterSpacing(lerp(0, -0.3, k)).SingleLine().TextColor(ink).Opacity(shown).TransformOrigin(0, 0).Scale(s, s)
		if k < 0.5 {
			title.FontWeight(600)
		} else {
			title.FontWeight(700)
		}
	}
	if to.scrub.W > 0 {
		a.flyScrub(c, lerpRect(from.scrub, to.scrub, k), from.track.Mix(to.track, k), from.fill.Mix(to.fill, k), from.smoke.Mix(to.smoke, k), k, shown)
	}
	if to.play.W > 0 {
		r := lerpRect(from.play, to.play, k)
		icon := iconPlay
		if a.pl.Playing() {
			icon = iconPause
		}
		// A round button has its icon 8 DIPs in, whatever its size.
		ui.Icon(c.Key("fly-play"), icon).Absolute().Left(r.X+8).Top(r.Y+8).Size(r.W-16, r.H-16).TextColor(ink).Opacity(shown)
	}
}

// flyScrub draws the progress on its way at r, as a scrubber draws it.
func (a *app) flyScrub(c *ui.Context, r ui.Rect, track, fill, smoke ui.Color, k, shown float32) {
	pl, st := a.pl, &a.npScrub
	dur := pl.Duration().Seconds()
	at := float32(0)
	if dur > 0 {
		at = float32(pl.Position().Seconds() / dur)
	}
	smoking := float32(0)
	if a.settings.Smoke && pl.Playing() && !pl.Loading() {
		smoking = 1
	}
	ui.Box(c.Key("fly-scrub")).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).Opacity(shown).Draw(func(p *ui.Painter, b ui.Rect) {
		const thick = 4
		y := b.Y + (b.H-thick)/2
		head, cy := b.X+b.W*at, b.Y+b.H/2
		p.Fill(ui.Rect{X: b.X, Y: y, W: b.W, H: thick}, track, thick/2)
		if got, ok := pl.Downloaded(); ok && float32(got) < 1 {
			p.Fill(ui.Rect{X: b.X, Y: y, W: b.W * float32(got), H: thick}, track, thick/2)
		}
		played := ui.Rect{X: b.X, Y: y, W: max(thick, head-b.X), H: thick}
		if st.lamp {
			drawBarGlow(p, played, cy, a.glowColor, a.glowGain()*k)
		}
		if smoking > 0 {
			lv, live := pl.Levels()
			st.level.step(p.Now(), lv, live)
			drawStreak(p, played, head, cy, thick, st.level.group(0, 6), fill, smoke, smoking)
		} else {
			p.Fill(played, fill, thick/2)
		}
	})
}

// flown hides e, what stays in the view, until the flight that draws it is
// about to land on it.
func (a *app) flown(e ui.Element) {
	if a.npFlying() {
		e.Opacity(easeRange(a.npK, 1-flightEnd, 1-flightTouch))
	}
}

// lifted hides e, what stays in the bar, once the flight that draws it has
// lifted off it, and until it is about to land on it again.
func (a *app) lifted(e ui.Element) {
	if a.npFlying() {
		e.Opacity(1 - easeRange(a.npK, flightTouch, flightEnd))
	}
}

// pageLevel is how deep a page is: those of the sidebar, and what opens from
// them.
func pageLevel(r route) int {
	switch r.page {
	case pagePlaylist, pageArtist, pageAlbum, pageUser, pagePodcast, pageStyle:
		return 1
	}
	return 0
}

// pageMotion moves a page as it comes and goes: in further or back out the
// way it went, a little, and across with a small rise, which is all a change
// made many times over should take.
func (a *app) pageMotion(page ui.Element) {
	if a.motion == motionOff {
		return
	}
	// With no exit: two pages showing through each other read as neither.
	in := &ui.Motion{}
	// Under a cover flying to it, the page stays where the cover lands.
	if a.motion == motionFull && a.flight.url == "" {
		switch {
		case a.pageDir > 0:
			in.X = 34
		case a.pageDir < 0:
			in.X = -24
		default:
			in.Y = 10
		}
	}
	page.Transition(ui.ElementTransition{Animation: &motionPage, Enter: in})
}

// paneMotion grows a pane from its transform origin, what opened it.
func (a *app) paneMotion(panel ui.Element) {
	if a.motion == motionOff {
		return
	}
	scale := float32(0.86)
	if a.motion == motionLite {
		scale = 1
	}
	panel.Transition(ui.ElementTransition{Animation: &motionPane, Enter: &ui.Motion{Scale: scale}, Exit: &ui.Motion{Scale: 0.97}})
}

// dialog is DialogBase coming and going. A panel is as new as the backdrop it
// is on, and a Transition moves only what appears in a parent that was there,
// so a dialog's Enter and Exit never show: it moves by hand. The backdrop fades
// by its color, dim at most, not its opacity: that would take the panel with
// it, whose glass would see nothing through it until the last frame.
func (a *app) dialog(c *ui.Context, key string, dim float32, open *bool, fn func(panel ui.Element)) {
	stage := ui.Box(c.Key(key+"-stage")).Absolute().Size(0, 0).PassThrough()
	k := b2f(*open)
	if a.motion != motionOff {
		k = clamp01(stage.AnimateTo("in", k, motionPane))
	}
	if k == 0 && !*open {
		return
	}
	shown := true
	ui.DialogBase(c.Key(key), &shown, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, dim*k))
		fn(panel)
		if k < 1 {
			panel.Opacity(k)
			if a.motion == motionFull {
				s := lerp(0.92, 1, k)
				panel.Scale(s, s)
			}
		}
	})
	if !shown {
		*open = false
	}
}

// A coverFlight is a cover on its way from a card to the page it opens, or
// back. It is drawn over the window, for a Match doesn't fly to an element of a
// new page.
type coverFlight struct {
	url        string
	from, to   ui.Rect
	rFrom, rTo float32   // its corners there, 14 if unset
	back       bool      // it is going back to its card
	asked      time.Time // when it was clicked
	at         time.Time // when its way began, the page laid out
	here       bool      // this frame's page has a place for it
}

// A cardOrigin is the card a page was opened from, for going back to it.
type cardOrigin struct {
	route  route
	url    string
	y      float32 // how far its page was scrolled
	radius float32
}

// flyFrom sends a cover from its card, at from, to the page about to open.
func (a *app) flyFrom(c *ui.Context, url string, from ui.Rect, radius float32) {
	a.cameFrom = cardOrigin{route: a.route, url: url, y: a.pageScroll.Y, radius: radius}
	if a.motion == motionFull {
		a.flight = coverFlight{url: url, from: from, rFrom: radius, asked: c.Now()}
	}
}

// flyBack sends the cover of the page left to the card that opened it, on r,
// if the header it is in shows, and has r come back as it was scrolled.
func (a *app) flyBack(r route) {
	o := a.cameFrom
	if o.route != r || o.url == "" {
		return
	}
	a.cameFrom, a.restoreY = cardOrigin{}, o.y
	if a.motion == motionFull && a.headAt.W > 0 && a.headGone < 160 {
		a.flight = coverFlight{url: o.url, from: a.headAt, rTo: o.radius, back: true}
	}
}

// landBack gives a cover going back its card, e, the card of url.
func (a *app) landBack(c *ui.Context, e ui.Element, url string) {
	if a.flight.back && a.flight.url == url {
		a.landCover(c, e)
	}
}

// How long a cover takes, and how long it waits for a place to land: a page
// without one, as a podcast's, leaves its cover where it is.
const (
	coverFlightTime = 460 * time.Millisecond
	coverFlightWait = 300 * time.Millisecond
)

// heldCover is the cover of the page opening, known from its card before the
// rest of it has loaded.
func (a *app) heldCover() string {
	if url := a.flight.url; url != "" {
		a.heldRoute, a.heldURL = a.route, url
		return url
	}
	if a.heldRoute == a.route {
		return a.heldURL
	}
	return ""
}

// landCover gives the flight of a cover, if there is one, where it lands: e,
// wherever e is by now, which it hides until it has.
func (a *app) landCover(c *ui.Context, e ui.Element) {
	f := &a.flight
	if f.url == "" {
		return
	}
	if to := e.LayoutBounds(); to.W > 0 {
		if f.to.W == 0 {
			f.at = c.Now()
		}
		f.to = to
	}
	f.here = true
	e.Opacity(0)
}

// coverFlightView draws the cover on its way, if one is. It draws the frame
// it lands on too, which still hides what it lands on.
func (a *app) coverFlightView(c *ui.Context) {
	f := &a.flight
	if f.url == "" {
		return
	}
	c.AnimationFrame()
	now := c.Now()
	if f.asked.IsZero() {
		f.asked = now
	}
	k, shown, done := float32(0), float32(1), false
	switch {
	case f.to.W > 0 && !f.here:
		// Its page has gone.
		*f = coverFlight{}
		return
	case f.to.W > 0:
		t := min(float32(now.Sub(f.at))/float32(coverFlightTime), 1)
		k, done = 1-(1-t)*(1-t)*(1-t), t == 1
	case !f.here:
		// Nowhere to land yet: it fades where it was, if that stays so.
		t := min(float32(now.Sub(f.asked))/float32(coverFlightWait), 1)
		shown, done = 1-t, t == 1
	}
	f.here = false
	r := lerpRect(f.from, f.to, k)
	if f.to.W == 0 {
		r = f.from
	}
	corner := lerp(or14(f.rFrom), or14(f.rTo), k)
	ui.Box(c.Key("cover-flight")).Absolute().Left(r.X).Top(r.Y).Size(r.W, r.H).Radius(corner).PassThrough().Opacity(shown).
		Shadow(0, lerp(12, 18, k), lerp(28, 40, k), lerp(-6, -10, k), a.pal.cardShade).Children(func() {
		bmp := bitmap(a.art.get(netease.Sized(f.url, 600), false))
		if bmp == nil {
			bmp = bitmap(a.art.peek(netease.Sized(f.url, 400)))
		}
		artImage(c, bmp, corner, 1)
	})
	if done {
		*f = coverFlight{}
	}
}

func or14(r float32) float32 {
	if r == 0 {
		return 14
	}
	return r
}

// A discState is how far the bar's cover has turned. The cover turns as it is
// painted, at the rate of discFrame, and the view isn't built for it: this
// says how far, for that and for the flight out of the bar to start from it.
type discState struct {
	on    bool
	angle float32
	since time.Time
}

// How the cover turns: slowly, in degrees a second, far below a record's 200,
// for what shows on it to stay readable, which a few frames a second keep
// smooth; and how long it takes to stop, in seconds, as music pauses.
const (
	discRate   = 30
	discCoast  = 0.35
	discFrame  = time.Second / 15
	discSmooth = time.Second / 40 // where it is large enough to be looked at
)

// at is how far it has turned by now.
func (s *discState) at(now time.Time) float32 {
	t := now.Sub(s.since).Seconds()
	if s.on {
		return s.angle + discRate*float32(t)
	}
	return s.angle + discRate*discCoast*float32(1-math.Exp(-t/discCoast))
}

// spins reports whether the bar's cover turns.
func (a *app) spins() bool { return a.settings.BarSpin && a.motion == motionFull }

// barDisc is the cover in the bar as a record, which turns as music plays and
// coasts to a stop as it pauses. It is drawn alone, a few times a second, for
// nothing else to be painted for it.
func (a *app) barDisc(c *ui.Context, tr *netease.Track, size float32, frame time.Duration) ui.Element {
	s, now, playing := &a.spin, c.Now(), a.pl.Playing()
	if playing != s.on {
		s.angle, s.since, s.on = s.at(now), now, playing
	}
	bmp := bitmap(a.art.get(netease.Sized(tr.Album.PicURL, 120), false))
	box := ui.Box(c.Key(tr.ID)).Size(size, size).Radius(999)
	in := artIn(box, bmp)
	if in < 1 {
		box.Background(a.pal.hover)
	}
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		now := p.Now()
		if s.on || now.Sub(s.since) < time.Duration(4*discCoast*float64(time.Second)) {
			p.After(frame)
		}
		if bmp == nil {
			return
		}
		p.Clip(r, r.W/2, func() {
			p.Transform(ui.Rotation(s.at(now)), r.X+r.W/2, r.Y+r.H/2, func() {
				p.ImageRegion(bmp, ui.Rect{}, r, ui.ImageOptions{Tint: white.Alpha(in)})
			})
		})
	})
	return box
}

// pageCover fills a page's header with its cover: the card's, which a cover
// flying here shows, holds its place until the larger one comes in over it.
func (a *app) pageCover(c *ui.Context, url string) {
	big, small := bitmap(a.art.get(netease.Sized(url, 600), false)), bitmap(a.art.peek(netease.Sized(url, 400)))
	box := ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(14)
	in := artIn(box, big)
	if in < 1 && small == nil {
		box.Background(a.pal.hover)
	}
	box.Children(func() {
		if in < 1 {
			artImage(c, small, 14, 1)
		}
		artImage(c, big, 14, in)
	})
}

// navPillMatch is the pill behind the page chosen in the sidebar.
type navPillMatch struct{}

// navRowContent keeps row content above the flat selection tint.
func (a *app) navRowContent(c *ui.Context, button ui.Element, selected bool, padding float32, content func()) {
	button.AlignSelf(ui.Stretch).Children(func() {
		if selected {
			a.navPill(c)
		}
		ui.Row(c.Key("nav-content")).Absolute().Top(0).Left(0).Right(0).Bottom(0).
			PaddingX(padding).Gap(10).AlignItems(ui.Center).PassThrough().Children(content)
	})
}

// navPill is that pill, which slides from the page chosen before.
func (a *app) navPill(c *ui.Context) {
	alpha := float32(0.12)
	if a.pal.dark {
		alpha = 0.16
	}
	pill := ui.Box(c.Key("nav-pill")).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(8).PassThrough().Background(a.pal.accent.Alpha(alpha))
	if a.motion == motionFull {
		pill.Transition(ui.ElementTransition{Animation: &motionPane, Match: navPillMatch{}})
	}
}

// coverAccent is the accent the cover playing gives, if it gives one: a
// color of its hue, as rich as an accent needs to be.
func (a *app) coverAccent() (ui.Color, bool) {
	art := a.npArt
	if art == nil || art.failed {
		return ui.Color{}, false
	}
	h, s, _ := hsv(float64(art.tint.R)/255, float64(art.tint.G)/255, float64(art.tint.B)/255)
	if s < 0.12 {
		return ui.Color{}, false
	}
	return hsvColor(h, min(max(s, 0.5), 0.8), 0.8), true
}

// followCover takes the cover playing, which the colors that follow it read
// whether Now Playing shows or not. They stay the last song's while its cover
// loads, rather than going gray between the two.
func (a *app) followCover() {
	tr := a.pl.Current()
	if tr == nil {
		a.npArt = nil
		return
	}
	if art := a.art.get(netease.Sized(tr.Album.PicURL, 600), true); art != nil || tr.Album.PicURL == "" {
		a.npArt = art
	}
}

// sidebarColor is the color of the sidebar: the theme's own. It used to take
// a little of the cover playing, which tinted the sidebar with whatever was
// on, so that it changed color with every song.
func (a *app) sidebarColor() ui.Color {
	return a.pal.sidebar
}

// rowGlow lights the row of the song playing from the bars at its start, a
// little more on the beat, as the progress is lit.
func (a *app) rowGlow(row ui.Element) {
	if !a.settings.NpLight {
		return
	}
	accent, gain := a.pal.accent, a.glowGain()
	row.Draw(func(p *ui.Painter, r ui.Rect) {
		bass := a.rowBars.group(0, 6)
		p.FillGradient(ui.Rect{X: r.X, Y: r.Y, W: min(r.W, 280), H: r.H}, ui.LinearGradient{Angle: 90, Oklab: true,
			From: accent.Alpha(min((0.1+0.14*bass)*gain, 1)), To: accent.Alpha(0)}, 14)
	})
}
