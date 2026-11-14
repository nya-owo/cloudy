package app

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// transportWidth returns how wide transport's row of controls of size is:
// the mode, previous, play and next buttons, and the spacer after next
// that keeps play in the middle.
func transportWidth(size float32) float32 {
	mode := size*0.62 + 16
	step := size*0.85 + 16
	play := size*1.25 + 16
	return 2*mode + 2*step + play + 4*size*0.5
}

// volumeReach is how far past the controls, in their size, the volume
// reaches on either side.
const volumeReach = 0.5

// volumeShown is how long the volume's percent stays after the slider was
// last moved or the wheel last turned it.
const volumeShown = 1200 * time.Millisecond

// npVolume is Now Playing's volume under its controls of size: the speaker,
// which mutes, and a slim slider. Like the controls it is centered on
// play, as wide as their row and a little more on either side: the
// speaker starts a little before the mode's button, and the slider ends
// as far past the spacer on the right. The wheel over them turns the
// volume, finely, and while it moves its percent shows over the middle of
// the slider, as the times do by the progress.
func (a *app) npVolume(c *ui.Context, size float32, color, track, digits ui.Color) {
	const speaker, gap = 32, 4
	width := transportWidth(size) + 2*size*volumeReach
	slider := max(60, width-speaker-gap)
	now := c.Now()
	moved := func() { a.more.volumeAt = now }
	row := ui.Row(c.Key("np-volume")).Width(width).Gap(gap).AlignItems(ui.Center).AlignSelf(ui.Center)
	row.HandleInput(func(ev ui.InputEvent) bool {
		if a.scrollVolume(ev) {
			moved()
			return true
		}
		return false
	})
	row.Children(func() {
		a.muteButton(c, color)
		box := ui.Box(c).Width(slider).Height(16)
		box.Children(func() {
			s := ui.SliderBase(c.Key("volume"), &a.volume, 0, 1).Width(slider).Height(16).FocusRing(false).Label("音量")
			if s.Changed() {
				a.pl.SetVolume(a.volume)
				moved()
			}
			pressed := s.Pressed()
			hot := s.Hovered() || pressed
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
			shown := pressed
			if since := now.Sub(a.more.volumeAt); !a.more.volumeAt.IsZero() && since < volumeShown {
				shown = true
				c.After(volumeShown - since)
			}
			if pressed {
				a.more.volumeAt = now
			}
			fade := float32(0)
			if shown {
				fade = 1
			}
			label := ui.Textf(c.Key("volume-percent"), "%d%%", int(a.volume*100+0.5)).FontSize(11).FontFeatures("tnum").
				TextColor(digits).TextAlign(ui.Center).Absolute().Left(0).Right(0).Top(-17).PassThrough()
			label.Opacity(label.AnimateTo("shown", fade, ui.Animation{Duration: 220 * time.Millisecond}))
		})
	})
}
