package app

import (
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"
)

// The home page leads with a desk calendar: the daily card and the style
// songs' page are its pages, bound at the top through a row of holes, the
// front one whole and the one under it showing a strip at its foot, named,
// over a board under them all. A page flips up and out, toward the
// listener, around its holes, till it is a line, and is gone; it comes
// back at the foot of the stack, fading in, and the next one rises into
// its place. A click on the strip flips, as does pulling the front page
// up, which follows the pointer, or the down key while it has the focus.
// Hovering the strip lifts the front page's foot, as a finger under it
// would; the first few times the calendar shows, it lifts by itself once,
// with a word on how to flip.

// deckPage is one of the calendar's pages.
type deckPage int8

const (
	deckDaily deckPage = iota
	deckStyle
	deckFM
	deckRadar
	deckWeek
)

// deckState is which page of the calendar is in front, the page flipping
// away and the one coming back at the foot, and the front page pulled up.
type deckState struct {
	front  deckPage
	chosen bool // front was picked, as the pages came

	leaving  deckPage
	flipping bool // leaving flips away
	arriving deckPage
	arrives  bool // arriving fades in at the foot, from the frame it starts
	arrived  bool // and it has started

	pulling bool
	lift    float32 // how far the front page is pulled up, 0 to 1
	// How far the front page is lifted, and the page flipping away, as
	// last built; and whether the strip was hovered.
	raised, gone float32
	stripHover   bool
	// The strip lifts the front page only once the pointer has left it
	// since the last flip.
	stripCool bool

	// The press on the front page: where the pointer was down the calendar
	// and how far the page was lifted then; the pointer now, and when it
	// last moved the page, and how fast the page lifts.
	pressed           bool
	pressY, pressLift float32
	pointer           func() float32
	pullAt            time.Time
	pullSpeed         float32

	// What a press or a key asked for, done once the calendar is built.
	flipNow, backNow bool
	// The front page was let go of, pulled: the release is no click.
	let bool

	// The lift by itself that shows the calendar flips, and the word with
	// it: asked for this launch, lifting, and the word showing.
	nudgeAsked, nudging, hint bool

	// The page a bookmark asked for, the pages before it flipping by in
	// turn; and whether the listener edits what the calendar holds.
	target             deckPage
	targeting, editing bool
}

// The calendar's measures.
const (
	deckSmallMin   = 400  // pages this wide, too narrow for what they show at the right, show a cover there
	deckSmallCover = 110  // this large
	deckPeek       = 16   // how much of the page under shows at the foot
	deckBoard      = 8    // and of the board under all the pages
	deckHoleY      = 12   // how far down the pages their holes are
	deckHoleR      = 3    // how big they are
	deckHoleGap    = 18   // and how far apart
	deckHover      = 0.16 // how far the front page lifts, the strip hovered
	deckNudge      = 0.26 // and by itself, to show it flips
	deckDepth      = 3200
	deckHints      = 3  // how many times it shows it flips
	deckTabW       = 26 // how far the bookmarks stand out at the right
	deckTabTop     = 14 // how far down the first is
	deckTabH       = 34 // how tall each is
	deckTabGap     = 3  // and how far apart
)

// deckSpring moves a page: it takes over from a pull at the pull's speed.
var deckSpring = ui.Animation{Spring: ui.SpringOptions{Duration: 680 * time.Millisecond}}

// deckRiffle moves a page flipping by on the way to a bookmark's.
var deckRiffle = ui.Animation{Spring: ui.SpringOptions{Duration: 300 * time.Millisecond}}

// deckLift is a page lifted p of the way to edge on: the angle its foot
// turns up and out by, in degrees. The foot rises evenly with p, as the
// eye sees it, where the angle would rise slow, then all at once.
func deckLift(p float32) float32 {
	p = clamp01(p)
	return float32(math.Acos(float64(1-p)) * 180 / math.Pi)
}

// deckPages is the calendar's pages, those the listener keeps in it that
// have what they show, in their order; the first time there are some,
// the one in front is what plays, the style songs or the FM, the first
// page otherwise.
func (a *app) deckPages() []deckPage {
	var pages []deckPage
	for _, pg := range a.settings.deckOrder() {
		if a.settings.deckOn(pg) && a.deckHas(pg) {
			pages = append(pages, pg)
		}
	}
	d := &a.deck
	if len(pages) > 0 && !d.chosen {
		d.chosen = true
		d.front = pages[0]
		switch {
		case slices.Contains(pages, deckStyle) && a.queueIs(a.style.songs.Songs):
			d.front = deckStyle
		case slices.Contains(pages, deckFM) && a.fmPlaying() != nil:
			d.front = deckFM
		}
	}
	return pages
}

// deckOrder is the pages from the one in front back.
func (a *app) deckOrder(pages []deckPage) []deckPage {
	at := 0
	for i, pg := range pages {
		if pg == a.deck.front {
			at = i
		}
	}
	order := make([]deckPage, 0, len(pages))
	for i := range pages {
		order = append(order, pages[(at+i)%len(pages)])
	}
	a.deck.front = order[0]
	return order
}

// deckNext flips the front page away, on from as far as it was pulled.
func (a *app) deckNext(c *ui.Context, order []deckPage) {
	d := &a.deck
	if len(order) < 2 || d.flipping {
		return
	}
	d.leaving, d.flipping = order[0], true
	d.stripCool = true
	d.front, d.lift, d.pulling = order[1], 0, false
	a.deckLearned()
	c.AnimationFrame()
}

// deckBack brings the page under all the others to the front, as the up
// key does: it rises into place without flipping.
func (a *app) deckBack(c *ui.Context, order []deckPage) {
	d := &a.deck
	if len(order) < 2 || d.flipping {
		return
	}
	d.front, d.lift, d.pulling = order[len(order)-1], 0, false
	a.deckLearned()
	c.AnimationFrame()
}

// deckLearned notes that the listener flipped a page, who needs showing
// no more.
func (a *app) deckLearned() {
	a.deck.hint = false
	if a.settings.DeckHints < deckHints {
		a.settings.DeckHints = deckHints
		a.settings.save()
	}
}

// grip is what pulls a page of the calendar: the page's card.
type grip interface {
	Dragged() (dx, dy float32, ok bool)
	DragEnded() (vx, vy float32, ok bool)
	Shortcut(mods ui.Modifiers, key ui.Key) bool
}

// deckGrip lets the front page, pg, be pulled up by its card g, its foot
// under the pointer: let go past two fifths of the way, or flung up, it
// flips away; short of that it falls back. The down key flips it and the
// up key brings the last page back.
func (a *app) deckGrip(g grip, pg deckPage) {
	d := &a.deck
	if pg != d.front || (d.flipping && d.leaving == pg) {
		return
	}
	// The foot follows the pointer as the window has it, read off the
	// calendar, which never turns: in the card's own axes, which turn
	// with it, the pointer runs off to nowhere once the foot passes it.
	reach := float32(dailyHeight - deckHoleY)
	if _, _, ok := g.Dragged(); ok && d.pointer != nil {
		y, now := d.pointer(), time.Now()
		if !d.pressed {
			d.pressed, d.pressY, d.pressLift = true, y, d.raised
		}
		moved := d.pressY - y
		if d.pulling || moved > 4 {
			l := clamp01(d.pressLift + moved/reach)
			if dt := float32(now.Sub(d.pullAt).Seconds()); d.pulling && dt > 0 && dt < 0.2 {
				d.pullSpeed = d.pullSpeed*0.4 + (l-d.lift)/dt*0.6
			}
			d.pulling, d.lift, d.pullAt = true, l, now
		}
	} else if d.pressed {
		// Let go without a drag that ended: a press that never moved.
		if _, _, ended := g.DragEnded(); !ended {
			d.pressed, d.pulling, d.pullSpeed = false, false, 0
		}
	}
	if _, _, ok := g.DragEnded(); ok {
		if d.pulling {
			d.let = true
			if d.lift > 0.4 || (d.pullSpeed > 1.5 && d.lift > 0.04) {
				d.flipNow = true
			}
		}
		d.pressed, d.pulling, d.pullSpeed = false, false, 0
	}
	if g.Shortcut(0, ui.KeyDown) {
		d.flipNow = true
	}
	if g.Shortcut(0, ui.KeyUp) {
		d.backNow = true
	}
}

// deckFlat reports whether the page pg lies flat, as its card's shadow
// does only then.
func (a *app) deckFlat(pg deckPage) bool {
	d := &a.deck
	if d.flipping && d.leaving == pg {
		return false
	}
	return pg != d.front || d.raised < 0.01
}

// homeDeck is the calendar at the head of the home page.
func (a *app) homeDeck(c *ui.Context, now time.Time) {
	pages := a.deckPages()
	if len(pages) == 0 {
		return
	}
	d := &a.deck
	order := a.deckOrder(pages)
	if d.flipping && !slices.Contains(pages, d.leaving) {
		d.flipping = false
	}
	if d.arrives && !slices.Contains(pages, d.arriving) {
		d.arrives = false
	}
	n := len(order)
	// Two pages show under the front one at most; the bookmarks show them all.
	peeks := min(n-1, 2)
	tall := float32(dailyHeight)
	if n > 1 {
		tall += float32(peeks)*deckPeek + deckBoard
	}
	still := c.Preferences().ReduceMotion
	settle := ui.Animation{Spring: ui.SpringOptions{Duration: 460 * time.Millisecond}}
	page := func(pg deckPage, back bool) {
		switch pg {
		case deckDaily:
			a.dailyCard(c, now, back)
		case deckStyle:
			a.styleCard(c, back)
		case deckFM:
			a.fmSheet(c, back)
		case deckRadar:
			a.radarSheet(c, back)
		case deckWeek:
			a.weekSheet(c, back)
		}
	}
	origin := float32(deckHoleY) / dailyHeight
	reach := float32(dailyHeight - deckHoleY)
	// The first few times, once a launch, the front page lifts by itself
	// a moment after the calendar shows, with a word on how to flip.
	if n > 1 && !d.nudgeAsked && a.settings.DeckHints < deckHints && !still && a.win != nil {
		d.nudgeAsked = true
		go func() {
			time.Sleep(1400 * time.Millisecond)
			a.post(func() {
				if d.flipping || d.pulling || a.route.page != pageHome {
					return
				}
				d.nudging, d.hint = true, true
				a.settings.DeckHints++
				a.settings.save()
			})
			time.Sleep(700 * time.Millisecond)
			a.post(func() { d.nudging = false })
			time.Sleep(4 * time.Second)
			a.post(func() { d.hint = false })
		}()
	}

	// How far the front page and the page flipping away are lifted, read
	// as they are built, for the shadow they cast.
	var frontLift, leaveLift float32

	deck := ui.Box(c.Key("deck")).Height(tall)
	d.pointer = func() float32 {
		_, y, _ := deck.PointerPosition()
		return y
	}
	deck.Children(func() {
		// The board under the pages.
		if n > 1 {
			ui.Box(c.Key("board")).Absolute().Top(float32(peeks)*deckPeek+deckBoard).Left(0).Right(deckTabW).Height(dailyHeight).
				Radius(dailyRadius).Background(a.pal.faint.Alpha(0.5)).Shadow(0, 8, 20, -10, a.pal.cardShade)
		}
		a.deckTabs(c, pages)

		// The pages under the front one, the farthest first, each lower
		// and darker; the page flipping away is not among them till it
		// has gone, and comes back fading in at the foot.
		for depth := n - 1; depth >= 1; depth-- {
			pg := order[depth]
			if d.flipping && pg == d.leaving || depth > peeks {
				continue
			}
			sheet := ui.Box(c.Key(pg)).Absolute().Top(0).Left(0).Right(deckTabW).Height(dailyHeight)
			sheet.Jump("lift", 0)
			// The page right under the front one comes up to meet it as it is
			// pulled up, lit as it comes.
			up := float32(0)
			if depth == 1 && d.pulling {
				up = d.raised
			}
			up = sheet.AnimateTo("up", up, ui.Animation{Duration: 120 * time.Millisecond})
			y := float32(depth) * deckPeek
			y -= deckPeek * up
			show := float32(1)
			if d.arrives && pg == d.arriving {
				if !d.arrived {
					sheet.Jump("show", 0)
					d.arrived = true
				}
				show = sheet.AnimateTo("show", 1, ui.Animation{Duration: 240 * time.Millisecond, Ease: ui.EaseOut})
				y += 10 * (1 - show)
				if show > 0.999 {
					d.arrives = false
				}
			} else {
				sheet.Jump("show", 1)
			}
			sheet.Translate(0, y).Opacity(show)
			sheet.Children(func() {
				// Static back pages render once into a cached GPU layer so
				// their binder holes and rounded clips don't multiply draw
				// batches under the front card.
				ui.Box(c.Key("face")).Fill().Cache().Children(func() {
					page(pg, true)
					deckHoles(c)
					deckEdge(c)
				})
				deckShade(c, 0.32*(float32(depth)-up))
			})
		}

		// The front page: rising into place as the page over it flips
		// away, else lifted as far as it is pulled, the strip hovered, or
		// lifting by itself.
		front := order[0]
		sheet := ui.Box(c.Key(front)).Absolute().Top(0).Left(0).Right(deckTabW).Height(dailyHeight)
		sheet.Jump("show", 1)

		// Lifted while the strip at the foot is hovered, as it was last.
		lifted := d.stripHover && !d.stripCool && n > 1 && !d.flipping

		var lift float32
		switch {
		case d.pulling:
			lift = sheet.Jump("lift", d.lift)
		default:
			to := float32(0)
			switch {
			case lifted:
				to = deckHover
			case d.nudging:
				to = deckNudge
			}
			lift = sheet.AnimateTo("lift", to, settle)
		}
		frontLift = lift
		d.raised = lift
		// Rising as the page over it goes.
		rise := float32(0)
		if d.flipping {
			rise = deckPeek * (1 - clamp01(d.gone))
		} else {
			sheet.Jump("rise", 0)
		}
		dim := 0.32 * rise / deckPeek
		sheet.Translate(0, rise)
		lifted3D := lift > 0.0005
		if lifted3D {
			sheet.TransformOrigin(0.5, origin).RotateX(deckLift(lift)).Perspective(deckDepth)
		}
		sheet.Children(func() {
			// While tilted in 3D or rising during a flip, cache the card face
			// on the GPU so its rounded backdrop clip, binder holes and bottom
			// edge don't rasterize clip masks on the CPU every frame.
			face := ui.Box(c.Key("face")).Fill()
			if lifted3D || d.flipping {
				face.Cache()
			}
			face.Children(func() {
				page(front, false)
				deckHoles(c)
				deckEdge(c)
			})
			// Its face turns from the light as it lifts.
			deckShade(c, dim+0.3*lift)
		})

		// The shadow the lifted page casts on the one under it.
		ui.Box(c.Key("cast")).Absolute().Top(0).Left(0).Right(deckTabW).Height(tall).PassThrough().Draw(func(p *ui.Painter, r ui.Rect) {
			for _, l := range []float32{frontLift, leaveLift} {
				if l < 0.005 || l > 0.995 {
					continue
				}
				foot := r.Y + deckHoleY + reach*(1-l)
				deep := 0.34 * float32(math.Sin(float64(l)*math.Pi))
				band := 10 + 50*l
				p.Clip(ui.Rect{X: r.X, Y: foot, W: r.W, H: r.H - (foot - r.Y)}, 0, func() {
					p.FillGradient(ui.Rect{X: r.X, Y: foot, W: r.W, H: band}, ui.LinearGradient{Angle: 180, Stops: []ui.GradientStop{
						{Color: ui.RGBA(0, 0, 0, deep), At: 0}, {Color: ui.RGBA(0, 0, 0, 0), At: 1},
					}}, 0)
				})
			}
		})

		// The strip at the foot, which flips.
		d.stripHover = false
		if n > 1 && !d.flipping {
			strip := ui.ButtonBase(c.Key("deck-next")).Absolute().Left(0).Right(deckTabW).Top(dailyHeight - 6).Height(deckPeek + deckBoard + 6).
				FocusRing(false).Label("翻到" + deckName(order[1]))
			d.stripHover = strip.Hovered()
			if !d.stripHover {
				d.stripCool = false
			}
			if strip.Clicked() {
				d.flipNow = true
			}
			ink := strip.AnimateTo("ink", 0.62+0.38*b2f(d.stripHover), ui.Animation{Duration: 140 * time.Millisecond})
			at := slices.Index(pages, order[0]) + 1
			strip.Children(func() { deckStrip(c, order, at, ink) })
		}

		// The page flipping away, over the rest: up and out around its
		// holes, its foot rising evenly, till it is a line, and gone.
		if d.flipping {
			pg := d.leaving
			leaf := ui.Box(c.Key(pg)).Absolute().Top(0).Left(0).Right(deckTabW).Height(dailyHeight).PassThrough()
			// Aimed past edge on, so that it goes as fast to the end as on
			// the way, not slowing to a line that lingers.
			spring := deckSpring
			if d.targeting {
				spring = deckRiffle
			}
			l := min(1, leaf.AnimateTo("lift", 1.25, spring))
			leaveLift = l
			d.gone = l
			leaf.TransformOrigin(0.5, origin).RotateX(deckLift(l)).Perspective(deckDepth)
			leaf.Opacity(clamp01((0.97 - l) / 0.12))
			leaf.Children(func() {
				ui.Box(c.Key("face")).Fill().Cache().Children(func() {
					page(pg, true)
					deckHoles(c)
					deckEdge(c)
				})
				deckShade(c, 0.3*l)
			})
			if l >= 0.97 {
				d.flipping = false
				d.arriving, d.arrives, d.arrived = pg, true, false
				leaf.Jump("lift", 0)
				c.AnimationFrame()
			}
		}

		// The word on how to flip, under the calendar.
		hint := ui.Box(c.Key("deck-hint")).Absolute().Top(tall + 10).Left(0).Right(deckTabW).PassThrough().AlignItems(ui.Center)
		shown := hint.AnimateTo("shown", b2f(d.hint && n > 1), ui.Animation{Duration: 260 * time.Millisecond, Ease: ui.EaseOut})
		if shown > 0.005 {
			hint.Opacity(shown).Translate(0, -6*(1-shown))
			hint.Children(func() {
				ui.Row(c).Height(30).PaddingX(14).Gap(6).Radius(999).AlignItems(ui.Center).Background(ui.RGBA(24, 24, 28, 0.9)).
					TextColor(white).Shadow(0, 8, 20, -8, ui.RGBA(0, 0, 0, 0.4)).Children(func() {
					ui.Icon(c, iconUp).Size(13, 13)
					ui.Text(c, "点下面这条、右边的书签，或把卡片往上拉，就能翻页").FontSize(12).FontWeight(600).SingleLine()
				})
			})
		}
	})

	d.pointer = nil
	d.let = false
	switch {
	case d.flipNow:
		d.flipNow = false
		a.deckNext(c, order)
	case d.backNow:
		d.backNow = false
		a.deckBack(c, order)
	}

	// On to a bookmark's page, a page at a time.
	if d.targeting {
		switch {
		case !slices.Contains(pages, d.target), d.front == d.target && !d.flipping:
			d.targeting = false
		case !d.flipping && d.front != d.target:
			a.deckNext(c, order)
		}
	}

	a.deckEditor(c)
}

// deckStrip is what shows on the strip of the page under the front one,
// in white as strong as ink: its name, and which page of how many is in
// front.
func deckStrip(c *ui.Context, order []deckPage, at int, ink float32) {
	ui.Row(c).Absolute().Left(dailyPadX).Right(dailyPadX).Top(6).Height(deckPeek).AlignItems(ui.Center).TextColor(white.Alpha(ink)).Children(func() {
		ui.Box(c).Grow(1).Basis(0)
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconUp).Size(11, 11)
			ui.Text(c, deckName(order[1])).FontSize(11).FontWeight(650).SingleLine()
		})
		ui.Row(c).Grow(1).Basis(0).Justify(ui.End).Children(func() {
			ui.Text(c, fmt.Sprintf("%d / %d", at, len(order))).FontSize(11).FontFeatures("tnum").SingleLine()
		})
	})
}

// deckShade darkens the page it is put on by alpha, from black.
func deckShade(c *ui.Context, alpha float32) {
	if alpha <= 0.003 {
		return
	}
	ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(dailyRadius).PassThrough().Background(ui.RGBA(0, 0, 0, min(alpha, 0.9)))
}

// deckEdge is a page's cut edge at its foot, catching the light, which
// tells it from the page under it of the same colors.
func deckEdge(c *ui.Context) {
	ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).PassThrough().Draw(func(p *ui.Painter, r ui.Rect) {
		p.Clip(ui.Rect{X: r.X, Y: r.Y + r.H - dailyRadius, W: r.W, H: dailyRadius}, 0, func() {
			p.Stroke(ui.Rect{X: r.X + 0.5, Y: r.Y + 0.5, W: r.W - 1, H: r.H - 1}, white.Alpha(0.22), dailyRadius, 1)
		})
	})
}

// deckHoles is the row of holes along the top of a page, through which
// the pages are bound: dark, the paper's cut edge catching the light under
// each.
func deckHoles(c *ui.Context) {
	ui.Box(c).Absolute().Top(0).Left(0).Right(0).Height(2 * deckHoleY).PassThrough().Draw(func(p *ui.Painter, r ui.Rect) {
		from := r.X + dailyRadius + 12
		span := r.W - 2*(dailyRadius+12)
		count := int(span/deckHoleGap) + 1
		gap := span / float32(max(1, count-1))
		y := r.Y + deckHoleY
		for i := range count {
			x := from + float32(i)*gap
			hole := ui.Rect{X: x - deckHoleR, Y: y - deckHoleR, W: 2 * deckHoleR, H: 2 * deckHoleR}
			p.Fill(hole, ui.RGBA(0, 0, 0, 0.55), deckHoleR)
			p.Clip(ui.Rect{X: hole.X - 1, Y: y, W: hole.W + 2, H: deckHoleR + 2}, 0, func() {
				p.Stroke(ui.Rect{X: hole.X - 0.5, Y: hole.Y - 0.5, W: hole.W + 1, H: hole.H + 1}, white.Alpha(0.28), deckHoleR+0.5, 1)
			})
		}
	})
}

// deckClicked reports whether the card of a page of the calendar was
// clicked, and not let go of after a pull.
func (a *app) deckClicked(clicked bool) bool {
	return clicked && !a.deck.let
}
