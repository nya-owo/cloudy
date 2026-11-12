package app

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

var white = ui.RGB(255, 255, 255)

// npSettle is how long Now Playing takes to come or go, its spring at rest.
const npSettle = 640 * time.Millisecond

// npTextStart is when the playback controls and lyrics begin to appear.
const npTextStart = 0.45

// npPageBackground fades out what the transparent view still shows through
// before the page is unmounted, and fades it back after it is built again.
func npPageBackground(progress float32) float32 {
	return 1 - easeRange(progress, 0.52, 1)
}

// How Now Playing opens and closes. They end at rest rather than settling as
// a spring does, for what flies to land with nothing left of its speed, and
// closing starts gently, or it would cross the handover in a couple of frames.
var (
	npOpening = ui.Animation{Duration: 580 * time.Millisecond, Ease: ui.CubicBezier(0.32, 0.72, 0, 1)}
	npClosing = ui.Animation{Duration: 500 * time.Millisecond, Ease: ui.CubicBezier(0.42, 0, 0.12, 1)}
)

// npState tracks Now Playing opening and closing, and reports whether it is
// open and whether it has come to rest, open or closed. Call it once a
// build, before the window's content.
func (a *app) npState(c *ui.Context) (open, settled bool) {
	open = a.nowPlaying && a.pl.Current() != nil
	now := c.Now()
	if open != a.npOpen {
		a.npOpen, a.npChangedAt = open, now
		// A build once it is at rest: to stop painting what it covers, or
		// itself once gone.
		c.After(npSettle)
		// The window buttons light over its dark backdrop, and as the
		// theme over the pages (Windows).
		if a.win != nil {
			buttons := mygo.ThemeSystem
			if open {
				buttons = mygo.ThemeDark
			}
			a.win.SetWindowButtonsTheme(buttons)
		}
	}
	return open, now.Sub(a.npChangedAt) >= npSettle
}

// npProgress is shared by the playback view and the page beneath it, so
// their handover stays in sync even if the animation reverses midway.
func (a *app) npProgress(root ui.Element, open bool) float32 {
	to, move := float32(0), npClosing
	if open {
		to, move = 1, npOpening
	}
	if a.motion == motionOff || a.pl.Current() == nil {
		return to
	}
	return clamp01(root.AnimateTo("now-playing", to, move))
}

// nowPlayingLayer holds Now Playing over the whole window, growing out of
// the player bar as it opens and folding back into it as it closes: the
// bar's shape opens up to the window, showing the view through it, the view
// staying put where it is at full size, so that nothing in it stretches. It
// moves its box, not a transform, as a clip moved by one is drawn on the CPU.
func (a *app) nowPlayingLayer(c *ui.Context, open, settled bool) {
	w, h := c.Size()
	k := a.npK
	dim := 0.4 * k
	if a.settings.GlobalGlass {
		dim *= npPageBackground(k)
	}
	if dim > 0.001 {
		ui.Box(c.Key("now-playing-dim")).Absolute().Top(0).Left(0).Right(0).Bottom(0).PassThrough().
			Background(ui.RGB(0, 0, 0)).Opacity(dim)
	}
	layer := ui.Box(c.Key("now-playing-layer")).Absolute()
	if !open && settled || a.pl.Current() == nil {
		// Closed: nothing to paint or click.
		a.backdropCache = backdropCache{}
		layer.Top(0).Left(0).Right(0).Bottom(0).Invisible()
		a.queueAt, a.wallState.track = -1, 0
		a.npK = 0
		return
	}
	if !open {
		// On its way out, the window under it takes the clicks.
		layer.PassThrough()
	}
	from := a.barBox
	if from.W <= 0 {
		from = ui.Rect{Y: h - playerBarHeight, W: w, H: playerBarHeight}
	}
	r := lerpRect(from, ui.Rect{W: w, H: h}, k)
	layer.Left(r.X).Top(r.Y).Width(r.W).Height(r.H)
	if k < 1 {
		layer.Radius(from.H / 2 * (1 - k)).Clip()
	}
	layer.Children(func() {
		ui.Box(c.Key("now-playing-full")).Absolute().Left(-r.X).Top(-r.Y).Size(w, h).Children(func() {
			a.nowPlayingView(c)
			// Over the view, not in it: they don't fade with it, for what they
			// take over from in the bar has left.
			a.flights(c, a.pl.Current(), k)
		})
	})
}

// nowPlayingView fills the window with what plays: its cover over a slow
// drift of its own colors, the controls, and lyrics that follow the song.
func (a *app) nowPlayingView(c *ui.Context) {
	pl := a.pl
	tr := pl.Current()
	w, h := c.Size()
	art := a.art.get(netease.Sized(tr.Album.PicURL, 600), true)
	shown := a.npArt
	tint := ui.RGB(120, 120, 130)
	if shown != nil && !shown.failed {
		tint = shown.tint
	}
	accent := tint.Mix(white, 0.45)
	a.glowColor = tint.Mix(white, 0.75)
	a.npScrub.lamp = a.settings.NpLight

	root := ui.Box(c.Key("now-playing")).Absolute().Top(0).Left(0).Right(0).Bottom(0).
		Background(ui.Hex("#0b0b0d")).TextColor(white)
	artOpacity := float32(1)
	if a.settings.GlobalGlass && c.Vibrancy() {
		root.Background(ui.Transparent)
		artOpacity = 0.53
	}
	// The pointer is over the view whenever it is in the window.
	pointerIn := root.Hovered()
	a.npPointer = pointerIn
	k := a.npK
	if k < 1 {
		// The view comes in through the bar's glass, and what is on it later,
		// once there is room for it.
		root.Opacity(easeRange(k, 0, 0.3))
	}
	root.Children(func() {
		a.backdrop(c, shown, tint, artOpacity)

		// Over the backdrop, which is positioned too: positioned elements
		// paint over those in the flow.
		body := ui.Column(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Children(func() { a.nowPlayingBody(c, tr, art, accent, w, h, pointerIn) })
		if k < 1 {
			body.Opacity(easeRange(k, npTextStart, 0.95))
		}
	})
}

func (a *app) nowPlayingBody(c *ui.Context, tr *netease.Track, art *artwork, accent ui.Color, w, h float32, pointerIn bool) {
	pl := a.pl
	a.commentsFor(tr)
	a.npSpot = flightSpot{radius: 14, track: white.Alpha(0.18), fill: white.Alpha(0.9), smoke: accent.Mix(white, 0.35)}
	// An episode has what it is about and its captions in place of lyrics;
	// a song, lyrics in place of those.
	switch program := tr.ProgramID != 0; {
	case program && a.npPane == paneWall:
		a.npPane = paneQueue
	case program && (a.npPane == paneLyrics || a.npPane == paneStage):
		a.npPane = paneProgram
		if a.captionsOn() {
			a.npPane = paneCaptions
		}
	case program && a.npPane == paneCaptions && !a.captionsShown():
		a.npPane = paneProgram
	case !program && (a.npPane == paneProgram || a.npPane == paneCaptions):
		a.npPane = a.lyricsPaneChosen()
	}
	if a.npPane != paneQueue {
		a.queueAt = -1
	}
	if a.npPane != paneWall {
		a.wallState.track = 0
	}
	{
		// The top strip drags the window; the chevron closes the view,
		// showing only while the pointer is in the window, as the pane tabs do.
		// In full screen there is no window to drag.
		strip := ui.Row(c).Height(topBar(c)).Justify(ui.Center).AlignItems(ui.Center)
		if !a.npFull {
			strip.DragWindow()
		}
		strip.Children(func() {
			reveal(ui.Box(c.Key("close-reveal")), pointerIn).Children(func() {
				if iconButton(c.Key("close"), iconDown, 22, white.Alpha(0.7), "关闭").Clicked() {
					a.nowPlaying = false
				}
			})
		})
		a.fullScreenButton(c, pointerIn)
		if a.npPane == paneStage {
			// The stage takes the whole view: the lyrics are the show, the
			// controls a slim bar at the foot.
			tabs := npTabsRow(c, w)
			reveal(tabs, pointerIn).Children(func() {
				a.paneTabs(c, tabs)
				a.stageStyleButton(c)
			})
			a.stage(c, tr, accent)
			a.stageBar(c, tr, art, accent)
			return
		}
		if a.npPane == paneWall {
			// The wall takes the whole view too, under the song playing.
			a.wallPane(c, tr, art, accent, w, h, pointerIn)
			return
		}

		side := float32(math.Min(float64(w)*0.36, 440))
		// The column is in the middle of the room under the strip, and the
		// cover leaves room above it for the tabs, and as much under the
		// column: as small as it takes for what goes under it (a radio's
		// chip too) to fit, never up under the tabs.
		rest := a.npRest
		if rest <= 0 {
			rest = npRestGuess
		}
		room := h - topBar(c) - npRowFoot
		coverSize := max(min(side, room-rest-2*(npTabsHeight+npTabsGap)), npCoverLeast)
		// The column is as wide as the cover, for the cover, what plays
		// and the tabs over it to start at one edge, but wide enough for
		// the controls.
		side = max(coverSize, 280)
		padX := npSideX(w)
		tabs := npTabsRow(c, w)
		reveal(tabs, pointerIn).Children(func() { a.paneTabs(c, tabs) })
		row := ui.Row(c.Key("np-row")).Grow(1).Padding(0, padX, npRowFoot, padX).Gap(72).AlignItems(ui.Stretch)
		row.Children(func() {
			var coverTop float32
			var volume ui.Element
			col := ui.Column(c.Key("np-side")).Width(side).Justify(ui.Center).Gap(0)
			col.Children(func() {
				coverTop = ui.Box(c.Key("cover-top")).Height(0).LayoutBounds().Y
				a.npCover(c, tr, art, coverSize)

				ui.Row(c).Gap(8).AlignItems(ui.Center).Margin(36, 0, 18, 0).Children(func() {
					ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
						if r := pl.Radio(); r != "" {
							a.radioChip(c, r)
						}
						title := ui.Text(c, tr.Name).FontSize(22).FontWeight(700).LetterSpacing(-0.3).SingleLine()
						a.npSpot.title = title.LayoutBounds()
						a.flown(title)
						a.artistLinks(c, tr, white.Alpha(0.6), white, true).FontSize(15).SingleLine()
					})
					a.likeCountButton(c.Key("like"), tr, 22, white.Alpha(0.7))
					a.songMenu(c.Key("more"), tr, 22, white.Alpha(0.7)).Margin(0, -8, 0, 0)
				})
				scrub := a.scrubber(c.Key("np-scrub"), &a.npScrub, white.Alpha(0.18), white.Alpha(0.9), 16, accent.Mix(white, 0.35))
				a.npSpot.scrub = scrub.LayoutBounds()
				a.flown(scrub)
				// As tall with the quality's badge as without it: the badge goes
				// while the next song loads, and the column would move with it.
				ui.Row(c).Height(16).AlignItems(ui.Center).Children(func() {
					ui.Text(c, clock(pl.Position())).FontSize(11).FontFeatures("tnum").TextColor(white.Alpha(0.5))
					ui.Spacer(c)
					if tr.ProgramID != 0 {
						// An episode's speed, where a song shows its quality.
						a.speedButton(c)
					} else {
						qualityBadge(c, pl.Stream())
					}
					ui.Spacer(c)
					status := "-" + clock(pl.Duration()-pl.Position())
					if pl.Loading() {
						status = "正在缓冲…"
					}
					ui.Text(c, status).FontSize(11).FontFeatures("tnum").TextColor(white.Alpha(0.5))
				})
				ui.Box(c).Margin(14, 0, 18, 0).Children(func() {
					a.playFlies = a.flown
					a.transport(c, white, white.Alpha(0.45), accent, 28)
					a.playFlies = nil
					a.npSpot.play = a.playSpot
				})
				volume = ui.Row(c.Key("np-volume")).Justify(ui.Center).Children(func() {
					a.volumeSlider(c, white.Alpha(0.7), white.Alpha(0.18), 180)
				})
				// How tall what is under the cover came out, for the cover's
				// size in the next frame.
				end := ui.Box(c.Key("np-end")).Height(0).LayoutBounds().Y
				if cv := a.npSpot.cover; coverTop > 0 && end > coverTop && cv.H > 0 {
					if r := end - coverTop - cv.H; math.Abs(float64(r-a.npRest)) > 0.5 {
						a.npRest = r
						c.AnimationFrame()
					}
				}
			})
			tools := false

			pane := ui.Column(c.Key("pane")).Grow(1).Basis(0).MinWidth(0)
			pane.Children(func() {
				// The pane starts level with the cover beside it, but below the
				// tabs, which start level with the pane's top: they come and go
				// with the pointer, so they sit out of the flow, never over it.
				b := pane.LayoutBounds()
				if b.H > 0 && coverTop > 0 {
					if top := max(coverTop-b.Y, npTabsHeight+8); top != a.npPaneTop {
						a.npPaneTop = top
						c.AnimationFrame()
					}
				} else {
					c.AnimationFrame() // not laid out yet: the next frame places it
				}
				ui.Box(c).Height(a.npPaneTop)
				switch a.npPane {
				case paneLyrics:
					tools = a.lyricsPane(c, tr, h)
				case paneComments:
					a.commentsPane(c, tr)
				case paneProgram:
					a.programPane(c, tr)
				case paneCaptions:
					a.captionsPane(c, tr, h)
				default:
					a.queuePane(c)
				}
			})
			// The lyrics' tools go under the volume, in the room the column
			// leaves at its foot: out of its flow, which moves nothing in it.
			if tools {
				r, s, v := row.LayoutBounds(), col.LayoutBounds(), volume.LayoutBounds()
				if v.H == 0 {
					c.AnimationFrame() // not laid out yet: the next frame places them
					return
				}
				a.lyricTools(c, tr.ID, ui.Rect{X: s.X - r.X, Y: v.Y + v.H - r.Y + 12, W: s.W})
			}
		})
	}
}

// npMaxWidth is how wide the cover and the pane beside it go, at most.
const npMaxWidth = 1280

// npSideX is how far in from the sides of a view w wide the cover and the
// pane beside it are: in a wide window they keep together in the middle,
// rather than the pane taking all the width the cover leaves.
func npSideX(w float32) float32 {
	return max(72, (w-npMaxWidth)/2)
}

// npTabsHeight is how tall the row of Now Playing's tabs is, for what is
// under it to start below it.
const npTabsHeight = 28

const (
	npTabsGap    = 16  // between the tabs and the cover under them, at least
	npRowFoot    = 44  // under the cover's column, for the lyrics' tools
	npRestGuess  = 270 // what goes under the cover, before it is laid out
	npCoverLeast = 96  // the cover's least size, in the shortest window
)

// npTabsRow is the row of Now Playing's tabs, in the same place for every
// pane, so that they stay put going from one to another: at the top, level
// with the cover's left. They start from the left, so that only the last,
// the comments, moves as its count changes.
func npTabsRow(c *ui.Context, w float32) ui.Element {
	return ui.Row(c.Key("tabs")).Absolute().Top(topBar(c)).Left(npSideX(w)).Height(npTabsHeight).Gap(6).AlignItems(ui.Center)
}

// reveal fades e in while the pointer is in the window, and out when it
// leaves.
func reveal(e ui.Element, shown bool) ui.Element {
	pose := ui.Pose{Fade: 1}
	if shown {
		pose = ui.Pose{}
	}
	return e.AnimatePose(pose, ui.Animation{Duration: 200 * time.Millisecond})
}

// lyricsPaneChosen is the pane of lyrics the listener chose last: what
// the app starts on, before the player is made. Now Playing shows an
// episode's in place of either.
func (a *app) lyricsPaneChosen() int {
	if a.settings.LyricsStage {
		return paneStage
	}
	return paneLyrics
}

// The panes of Now Playing.
const (
	paneLyrics = iota
	paneQueue
	paneStage
	paneComments
	paneProgram
	paneCaptions
	paneWall
)

// npTab is a tab of Now Playing: its pane, and what it says.
type npTab struct {
	pane  int
	label string
}

// npTabs returns the tabs of Now Playing, in order, for what plays: ⌘1
// chooses the first, ⌘2 the second, and on.
func (a *app) npTabs() []npTab {
	if tr := a.pl.Current(); tr != nil && tr.ProgramID != 0 {
		tabs := []npTab{{paneProgram, "简介"}}
		if a.captionsShown() {
			tabs = append(tabs, npTab{paneCaptions, "字幕"})
		}
		return append(tabs, npTab{paneQueue, "待播清单"}, npTab{paneComments, "评论"})
	}
	return []npTab{{paneLyrics, "歌词"}, {paneStage, "律动"}, {paneQueue, "待播清单"}, {paneWall, "选歌"}, {paneComments, "评论"}}
}

// paneTabs fills row with the tabs of Now Playing, a pill sliding under them
// to the one chosen, which it finds laid out in the frame before.
func (a *app) paneTabs(c *ui.Context, row ui.Element) {
	move := ui.Animation{Spring: ui.SpringOptions{Duration: 340 * time.Millisecond, Bounce: 0.12}}
	x, w, h := row.AnimateTo("pill-x", a.npPill.X, move), row.AnimateTo("pill-w", a.npPill.W, move), a.npPill.H
	// Drawn on the row, under the tabs: over them, it would bend their words.
	row.Draw(func(p *ui.Painter, r ui.Rect) {
		if w <= 0 {
			return
		}
		pill := ui.Rect{X: r.X + x, Y: r.Y + (r.H-h)/2, W: w, H: h}
		p.Fill(pill, white.Alpha(0.14), h/2)
	})
	for _, t := range a.npTabs() {
		label := t.label
		if t.pane == paneComments {
			label = a.commentsTab()
		}
		tab := a.paneTab(c, t.pane, label)
		if t.pane == a.npPane {
			b, r := tab.LayoutBounds(), row.LayoutBounds()
			if at := (ui.Rect{X: b.X - r.X, W: b.W, H: b.H}); at != a.npPill && b.W > 0 {
				a.npPill = at
				c.AnimationFrame()
			}
		}
	}
}

// choosePane shows pane i in Now Playing, remembering lyrics or the stage,
// whichever was chosen, for the next song.
func (a *app) choosePane(i int) {
	a.npPane = i
	if (i == paneLyrics || i == paneStage) && a.settings.LyricsStage != (i == paneStage) {
		a.settings.LyricsStage = i == paneStage
		a.settings.save()
	}
}

// radioChip says which radio plays, over the title: a click on it, or on
// its cross, leaves the radio, playing on the song.
func (a *app) radioChip(c *ui.Context, name string) {
	icon, exit := iconRadio, "退出私人 FM"
	switch {
	case name == heartRadio:
		icon, exit = iconHeartFill, "退出心动模式"
	case isRoam(name):
		exit = "退出风格漫游"
	}
	chip := ui.ButtonBase(c.Key("radio-chip")).Gap(5).Padding(3, 6, 3, 8).Margin(0, 0, 4, -8).Radius(999).
		AlignItems(ui.Center).AlignSelf(ui.Start).FocusRing(false).Label(exit).Tooltip(exit)
	bg, fg, cross := ui.Transparent, white.Alpha(0.6), float32(0.5)
	if chip.Hovered() {
		bg, fg, cross = white.Alpha(0.12), white.Alpha(0.85), 1
	}
	chip.Background(chip.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond})).TextColor(fg)
	chip.Children(func() {
		ui.Icon(c, icon).Size(12, 12)
		ui.Text(c, radioLabel(name)).FontSize(11.5).FontWeight(700).LetterSpacing(0.3)
		ui.Icon(c, iconX).Size(11, 11).Opacity(cross)
	})
	if chip.Clicked() {
		a.exitRadio()
	}
}

// qualityBadge marks a lossless stream, as it plays.
func qualityBadge(c *ui.Context, st *netease.Stream) {
	if st == nil || !netease.Lossless(st.Level) {
		return
	}
	label := "无损"
	if st.Level == "hires" {
		label = "Hi-Res"
	}
	ui.Text(c, label).FontSize(9.5).FontWeight(800).LetterSpacing(0.6).TextColor(ui.Hex("#f5d58a")).
		Padding(1, 6).Radius(4).Border(1, ui.Hex("#f5d58a").Alpha(0.55))
}

// stageBar is the slim bar along the foot of the stage: what plays, where
// it is, and the controls.
func (a *app) stageBar(c *ui.Context, tr *netease.Track, art *artwork, accent ui.Color) {
	pl := a.pl
	ui.Column(c).Padding(0, 48, 28, 48).Gap(10).Children(func() {
		a.scrubber(c.Key("stage-scrub"), &a.npScrub, white.Alpha(0.18), white.Alpha(0.9), 14, accent.Mix(white, 0.35))
		ui.Row(c).Gap(14).AlignItems(ui.Center).Children(func() {
			ui.Row(c).Grow(1).Basis(0).MinWidth(0).Gap(12).AlignItems(ui.Center).Children(func() {
				if bmp := bitmap(art); bmp != nil {
					cov := ui.Box(c.Key(tr.ID)).Size(44, 44)
					in := artIn(cov, bmp)
					cov.Children(func() { artImage(c, bmp, 8, in) })
				}
				ui.Column(c).Shrink(1).MinWidth(0).Gap(2).Children(func() {
					ui.Text(c, tr.Name).FontSize(14).FontWeight(700).SingleLine()
					a.artistLinks(c, tr, white.Alpha(0.55), white).FontSize(12).SingleLine()
				})
				a.likeCountButton(c.Key("like"), tr, 17, white.Alpha(0.6))
				a.songMenu(c.Key("more"), tr, 17, white.Alpha(0.6)).Margin(0, 0, 0, -10)
			})
			a.transport(c, white, white.Alpha(0.45), white, 22)
			ui.Row(c).Grow(1).Basis(0).MinWidth(0).Justify(ui.End).Children(func() {
				ui.Text(c, clock(pl.Position())+" / "+clock(pl.Duration())).FontSize(11.5).FontFeatures("tnum").TextColor(white.Alpha(0.5))
			})
		})
	})
}

func (a *app) paneTab(c *ui.Context, i int, label string) ui.Element {
	b := ui.ButtonBase(c.Key(i)).Padding(6, 14).Radius(999).FocusRing(false).Label(label)
	bg, fg := ui.Transparent, white.Alpha(0.55)
	if a.npPane == i {
		fg = white
	} else if b.Hovered() {
		bg = white.Alpha(0.06)
	}
	b.Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond}))
	b.Children(func() {
		if i != paneComments {
			ui.Text(c, label).FontSize(13).FontWeight(600).TextColor(fg)
			return
		}
		// The count rolls as it changes, from one song's to the next.
		n, text := a.commentCount(), ""
		if n > 0 {
			text = " " + count(n)
		}
		ui.Text(c, "评论").FontSize(13).FontWeight(600).TextColor(fg)
		rollCount(c, &a.tabCount, n, text, fg)
	})
	if b.Clicked() {
		a.choosePane(i)
	}
	return b
}

// lyricsPane shows the lyrics of tr, and reports whether it has any, for
// their tools to show with them.
func (a *app) lyricsPane(c *ui.Context, tr *netease.Track, h float32) bool {
	pl := a.pl
	lines, loaded := a.shownLyrics()
	if len(lines) == 0 {
		col := ui.Column(c).Grow(1)
		if loaded {
			col.Center()
		}
		dim, _ := a.quietShown(c, col, tr, lines, loaded)
		quietUnder(col, dim)
		col.Children(func() {
			if !loaded {
				lyricsSkeleton(c, npShimmer(c))
				return
			}
			ui.Text(c, "纯音乐，请欣赏").FontSize(24).FontWeight(700).TextColor(white.Alpha(0.4))
		})
		a.quietCard(c, dim, h*0.18)
		return false
	}
	shift := a.lyricShift(tr.ID)
	pos := pl.Position() + shift
	cur := lyrics.Index(lines, pos)
	// Redraw as the next line begins.
	if pl.Playing() && cur+1 < len(lines) {
		c.After(lines[cur+1].At - pos + 20*time.Millisecond)
	}
	if a.lyricTrack != tr.ID {
		a.lyricTrack, a.lyricAt = tr.ID, -2
		a.lyricScroll, a.lyricGlide = ui.ScrollState{}, lyricGlide{}
	}
	beforeY := a.lyricScroll.Y
	lag := a.lyricGlide.step(c, &a.lyricScroll)

	scroll := ui.Scroll(c.Key("lyrics")).Grow(1).TrackScroll(&a.lyricScroll).Padding(0, 8, h*0.5, 0)
	if a.settings.LyricPerspective {
		scroll.Effect(scrollEdgeFade{depth: 72, top: easeRange(a.lyricScroll.Y, 0, 16), bottom: 1})
	}
	st := linesStyle{size: 30 * a.lyricScale(), weight: 800, lineHeight: 1.25, shift: shift}
	if a.settings.LyricPerspective {
		st.tilt = 2 * clamp01(float32(a.settings.LyricTilt))
		if a.motion != motionOff {
			st.bounce = clamp01(float32(a.settings.LyricBounce))
		}
	}
	if box := scroll.LayoutBounds(); st.tilt > 0 && box.H > 0 {
		// The sung line passes through the rightmost point of the arc.
		st.focusY = max(1, min(box.H, h*0.53-box.Y))
		st.arcY = box.Y + st.focusY + lag + a.lyricScroll.Y - beforeY
		st.arcInset, st.arcRadius = min(125, box.W*0.17), box.H*0.82
		scroll.Padding(st.focusY, 8+st.arcInset*st.tilt, max(h*0.5, box.H-st.focusY), 0).
			TransformOrigin(0, st.focusY/box.H).RotateY(-19 * st.tilt).Perspective(max(700, box.W*2.7))
	}
	dim, quiet := a.quietShown(c, scroll, tr, lines, loaded)
	if dim > 0.98 {
		// Under the hot comment, gone, not drawn faint.
		scroll.Invisible()
	}
	defer a.quietCard(c, dim, h*0.18)
	st.quiet = quiet
	a.timedLines(c, scroll, lines, pos, cur, lag, st, nil)
	return true
}

// linesStyle is how timedLines sets its lines: the size, weight and
// leading of their type; quiet nearly hides them, under a hot comment.
type linesStyle struct {
	size       float32
	weight     int
	lineHeight float32
	focusY     float32 // active line's anchor in the pane, or 0 in the regular view
	arcY       float32 // arc center in the previous layout's coordinates
	arcInset   float32 // horizontal room reserved for the arc's rightmost point
	arcRadius  float32
	tilt       float32 // 1 preserves the original arc, 0 uses the flat layout
	bounce     float32 // the strength of the automatic scroll's overshoot
	quiet      bool
	shift      time.Duration // how early they run, as a line clicked seeks
}

// timedLines fills scroll with lines that follow the track playing at
// pos: the line at cur lit, and brought to a third of the way down once
// it changes, but none with cur -1 before the first; a click on one seeks
// to it. The lines are held back by lag, how far the glide to the line
// lit has still to go (lyricGlide). foot, if set, follows the lines.
func (a *app) timedLines(c *ui.Context, scroll ui.Element, lines []lyrics.Line, pos time.Duration, cur int, lag float32, st linesStyle, foot func()) {
	pl := a.pl
	var active ui.Element
	scroll.Children(func() {
		ui.Column(c).Gap(6).Translate(0, -lag).Children(func() {
			for i, l := range lines {
				l = a.npLine(l)
				line := ui.ButtonBase(c.Key(i)).Column().AlignItems(ui.Start).Padding(10, 12).Radius(14).Gap(6).FocusRing(false).Label(l.Text)
				on := i == cur
				hovered := line.Hovered()
				alpha := float32(0.28)
				if on {
					alpha = 1
				} else if hovered {
					alpha = 0.55
				}
				blur := float32(0)
				if st.focusY > 0 && !on && !hovered && !line.FocusVisible() && !a.settings.PowerSaver {
					distance := abs32(float32(i - cur))
					alpha *= 1 - 0.5*clamp01((distance-1)/4)
					blur = min(4, 0.9*distance)
				}
				if st.quiet {
					// Nearly gone under a hot comment.
					alpha *= quietFaded
				}
				if hovered {
					line.Background(white.Alpha(0.06))
				}
				scale := float32(0.94)
				if on {
					scale = 1
				}
				line.TransformOrigin(0, 0.5)
				if bounds := line.LayoutBounds(); st.arcRadius > 0 && bounds.H > 0 {
					// The left end follows a circle; the line turns gently
					// outward with its radius as the lyrics roll past.
					r := st.arcRadius
					dy := bounds.Y + bounds.H/2 - st.arcY
					dy = max(-0.8*r, min(0.8*r, dy))
					dx := float32(math.Sqrt(float64(r*r - dy*dy)))
					angle := float32(math.Atan2(float64(dy), float64(dx))) * (0.45 * 180 / math.Pi)
					line.Rotate(angle*st.tilt).Translate(max(0, st.arcInset-(r-dx))*st.tilt, 0)
				}
				line.AnimatePose(ui.Pose{Scale: scale}, ui.Animation{Spring: ui.SpringOptions{Duration: 600 * time.Millisecond, Bounce: 0.1}})
				glows := on && a.settings.NpLight && !st.quiet
				line.Children(func() {
					// The line fades as a pose, in frames that only paint, rather
					// than building the view at the display's rate as it does.
					text := ui.Column(c).AlignItems(ui.Start).Gap(6)
					text.AnimatePose(ui.Pose{Fade: 1 - alpha}, ui.Animation{Duration: 380 * time.Millisecond})
					if st.focusY > 0 && !a.settings.PowerSaver {
						text.Blur(text.AnimateTo("depth", blur, ui.Animation{Duration: 300 * time.Millisecond}))
					}
					text.Children(func() {
						ui.Box(c.Key("sung")).Children(func() {
							if on && len(l.Words) > 0 {
								// The line sung lights up word by word as it is sung.
								ui.RichText(c.Key("words")).FontSize(st.size).FontWeight(st.weight).LineHeight(st.lineHeight).LetterSpacing(-0.4).Children(func() {
									for wi, wd := range l.Words {
										ui.Text(c.Key(wi), wd.Text).TextColor(white.Alpha(0.38 + 0.62*wordLit(wd, pos)))
									}
								})
								if pl.Playing() {
									// Words brighten smoothly enough at 25 frames a second.
									c.After(40 * time.Millisecond)
								}
							} else {
								ui.Text(c, l.Text).FontSize(st.size).FontWeight(st.weight).LineHeight(st.lineHeight).LetterSpacing(-0.4).TextColor(white)
							}
							if glows {
								a.lineGlow(c, l, pos, st)
							}
						})
						if l.Translation != "" && a.settings.LyricTrans {
							ui.Text(c, l.Translation).FontSize(17).FontWeight(600).TextColor(white.Alpha(0.7))
						}
					})
				})
				if line.Clicked() {
					pl.Seek(l.At - st.shift)
				}
				if on {
					active = line
				}
			}
		})
		if foot != nil {
			foot()
		}
	})
	box := scroll.LayoutBounds()
	focus := lyricFocus{}
	if st.focusY > 0 {
		focus = lyricFocus{st.focusY, box.W, box.H, st.size, st.tilt, c.Theme().Font, a.settings.LyricTrans}
	}
	// Wait for layout to take the new padding or type size before measuring the target.
	if focus != a.lyricGlide.focus {
		a.lyricGlide.focus = focus
		// The new layout may move the offset to keep it in range: not the
		// listener scrolling.
		a.lyricGlide.reflowed = true
		a.lyricAt = -2
		c.AnimationFrame()
		return
	}
	// Bring the line sung to the center of the arc, or a third of the way down.
	// Not while the listener looks through the lines.
	if cur != a.lyricAt && !a.lyricGlide.holding(c, scroll) {
		if st.focusY == 0 {
			box = scroll.Bounds()
		}
		target := float32(0)
		if active.Valid() {
			// Where the glide is going, without where it still is.
			if lb := active.LayoutBounds(); lb.H > 0 && box.H > 0 {
				target = lb.Y - box.Y + a.lyricScroll.Y - box.H*0.3
				if st.focusY > 0 {
					target = lb.Y + lb.H/2 - box.Y + a.lyricScroll.Y - st.focusY
				}
			} else {
				return // not laid out yet: the next frame scrolls
			}
		}
		a.lyricAt = cur
		a.lyricGlide.to(c, &a.lyricScroll, min(max(0, target), a.lyricScroll.MaxY), st.bounce)
		if a.npK < 0.5 {
			// Opening: they are there, rather than scrolling as the view grows.
			a.lyricGlide.on = false
		}
	}
}

// lyricFocus records the layout inputs that can move the line relative to the arc.
type lyricFocus struct {
	y, width, height, size, tilt float32
	font                         string
	translation                  bool
}

// lyricGlide scrolls the lyrics to the line sung with a damped spring.
// ScrollTo moves the offset in every frame,
// and MyGo builds the view again in the frame after each that moved it,
// and its spring creeps over the last pixel for half a second: with a line
// following another every second or two, most of a song. So the offset
// goes to the line at once, in one frame, and the lines are held back by
// what the glide has still to go, its lag, which settles to nothing within
// half a pixel.
type lyricGlide struct {
	on                     bool
	a, b, decay, frequency float64
	start                  time.Time
	// The offset it goes to, set in the next frame (pending), with the lag
	// that holds the lines back from it: the layout of the frame that
	// chose it takes the offset too, the lines built without the lag.
	target  float32
	pending bool
	set     float32 // the offset it set
	// When the listener last scrolled the lines: they stay where they
	// were taken for a while (lyricHold), to pick a line from.
	took  time.Time
	focus lyricFocus
	// The layout changed last frame: an offset moved with it is no
	// scroll of the listener.
	reflowed bool
}

// lyricHold is how long the lines stay where the listener scrolled them
// before following the song again; lyricHoldOver, while the pointer
// stays over them.
const (
	lyricHold     = 4 * time.Second
	lyricHoldOver = 10 * time.Second
)

// holding reports whether the lines stay where the listener scrolled
// them, rather than following the song.
func (g *lyricGlide) holding(c *ui.Context, scroll ui.Element) bool {
	if g.took.IsZero() {
		return false
	}
	hold := lyricHold
	if scroll.Hovered() {
		hold = lyricHoldOver
	}
	if left := hold - c.Now().Sub(g.took); left > 0 {
		c.After(left + 20*time.Millisecond)
		return true
	}
	g.took = time.Time{}
	return false
}

// lyricOmega is the spring's ω: a line or two away, it comes within half a
// pixel in about 800 ms.
const lyricOmega = 10

// at returns the lag at now, and how fast it changes, in DIPs a second.
func (g *lyricGlide) at(now time.Time) (lag, v float64) {
	if !g.on {
		return 0, 0
	}
	t := now.Sub(g.start).Seconds()
	e := math.Exp(-g.decay * t)
	if g.frequency == 0 {
		lag := g.a + g.b*t
		return lag * e, (g.b - g.decay*lag) * e
	}
	sin, cos := math.Sincos(g.frequency * t)
	lag = g.a*cos + g.b*sin
	return lag * e, (g.frequency*(g.b*cos-g.a*sin) - g.decay*lag) * e
}

// to scrolls s to target from the next frame, gliding from where it
// shows, at the speed it went if it was on its way.
func (g *lyricGlide) to(c *ui.Context, s *ui.ScrollState, target, bounce float32) {
	now := c.Now()
	lag, v := g.at(now)
	shown := float64(s.Y) + lag
	g.target, g.pending = target, true
	g.on = !c.Preferences().ReduceMotion
	g.start, g.a = now, shown-float64(target)
	// Long seeks should land gently as well. Limit the bounce of a jump
	// across many lines without changing the speed of ordinary line changes.
	strength := float64(bounce) * 220 / max(220, math.Abs(g.a))
	ratio := 1 - 0.45*strength
	g.decay = lyricOmega * ratio
	g.frequency = lyricOmega * math.Sqrt(1-ratio*ratio)
	g.b = v + g.decay*g.a
	if g.frequency > 0 {
		g.b /= g.frequency
	}
	c.AnimationFrame()
}

// step returns the lag of the lines in this frame, and asks for the next
// while there is one. The listener scrolling stops the glide where it
// shows, and holds the lines there a while.
func (g *lyricGlide) step(c *ui.Context, s *ui.ScrollState) float32 {
	if g.pending {
		s.Y, g.set, g.pending = g.target, g.target, false
	}
	reflowed := g.reflowed
	g.reflowed = false
	lag, v := g.at(c.Now())
	if s.Y != g.set && reflowed {
		g.set = s.Y
	} else if s.Y != g.set {
		// The listener took over: the offset takes what showed.
		if g.on {
			s.Y, g.on = max(0, min(s.Y+float32(lag), s.MaxY)), false
		}
		g.set, g.took = s.Y, c.Now()
		return 0
	}
	if !g.on {
		return 0
	}
	if math.Abs(lag) < 0.5 && math.Abs(v) < 20 {
		g.on = false
		return 0
	}
	c.AnimationFrame()
	return float32(lag)
}

// queuePane lists the whole queue in play order, the track playing marked
// among the others: a click plays one there, and the list stays as it is.
func (a *app) queuePane(c *ui.Context) {
	pl := a.pl
	order, at := pl.PlayOrder()
	// Opened, it shows the track playing at its top; after, it follows it
	// as little as keeps it in view.
	if at >= 0 && at != a.queueAt {
		if a.queueAt < 0 {
			a.queueList.ScrollTo(at, ui.Start)
		} else {
			a.queueList.ScrollIntoView(at)
		}
		a.queueAt = at
	}
	queue := pl.Queue()
	// Keyed by the song: removing one shifts the indices of those after it.
	a.queueList.Key = func(row int) any { return queue[order[row]].ID }
	// Dragged, the rows move in the play order; the song playing plays on.
	a.queueList.Reorder = func(rows []int, to int) { pl.Reorder(rows, to) }
	if len(order) > 1 {
		ui.Row(c.Key("queue-head")).Height(36).PaddingX(10).Gap(10).AlignItems(ui.Center).Children(func() {
			left := len(order) - max(at, 0) - 1
			ui.Text(c, fmt.Sprintf("共 %d 首 · 接下来 %d 首 · 拖动可调整顺序", len(order), left)).FontSize(12).TextColor(white.Alpha(0.5)).Grow(1).MinWidth(0).SingleLine()
			if left > 0 && pillButton(c.Key("clear-queue"), iconTrash, "清空待播", white.Alpha(0.1), white).Clicked() {
				pl.ClearUpcoming()
				a.toast = "已清空待播清单"
			}
		})
	}
	ui.List(c.Key("queue"), &a.queueList, len(order), func(r int) {
		i := order[r]
		tr := &queue[i]
		playing := r == at
		row := ui.ButtonBase(c.Key(tr.ID)).Height(58).PaddingX(10).Gap(14).Radius(14).AlignItems(ui.Center).FocusRing(false).Label(tr.Name)
		a.contextMenu(row, a.nowPlaying, func() [][]menuAction {
			acts := a.songActions(tr, 0)
			if !playing {
				acts = append([][]menuAction{{{label: "从待播清单移除", icon: iconX, do: func() { pl.RemoveAt(r) }}}}, acts...)
			}
			return acts
		}, a.songHead(tr))
		switch {
		case playing:
			row.Background(white.Alpha(0.12))
		case row.Hovered():
			row.Background(white.Alpha(0.08))
		}
		if !tr.Playable() {
			row.Opacity(0.4)
		}
		row.Children(func() {
			a.cover(c, tr.Album.PicURL, 96, 42, 8)
			ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
				ui.Text(c, tr.Name).FontSize(14).FontWeight(600).SingleLine()
				a.artistLinks(c, tr, white.Alpha(0.55), white).FontSize(12.5).SingleLine()
			})
			ui.Text(c, clock(tr.Length())).FontSize(12).FontFeatures("tnum").TextColor(white.Alpha(0.4))
			// The song playing stays: there would be nothing in its place.
			if !playing {
				reveal(ui.Box(c.Key("remove-reveal")), row.Hovered()).Children(func() {
					if iconButton(c.Key("remove"), iconX, 12, white.Alpha(0.6), "从待播清单移除").Size(26, 26).Clicked() {
						pl.RemoveAt(r)
					}
				})
			}
		})
		if row.Clicked() && tr.Playable() && !playing {
			pl.Jump(i)
		}
	}).Grow(1).Padding(8, 0, 40, 0).Gap(2)
}
