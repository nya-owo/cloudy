package app

import (
	"github.com/egoist/mygo/ui"
	"time"
)

// A new row can be constructed in overscan, outside the viewport.
// Start only after its committed geometry actually reaches the viewport.
type privateBubbleEntrance struct {
	handle ui.Handle
	at     time.Time
	done   bool
}

var privateHistoryFollow = ui.Animation{Duration: 240 * time.Millisecond, Ease: motionEase}

const privateBubbleDuration = 380 * time.Millisecond

func (a *app) privateBubbleMotion(c *ui.Context, row ui.Element, in *privateBubbleEntrance, history *ui.ListState) {
	row.Bind(&in.handle)
	if in.done {
		return
	}
	if a.motion == motionOff {
		in.done = true
		return
	}
	if in.at.IsZero() {
		r, v := in.handle.Bounds(c), history.Handle.Bounds(c)
		visible := r.H > 0 && v.H > 0 && r.Y < v.Y+v.H && r.Y+r.H > v.Y
		if !visible {
			row.Opacity(0)
			history.Visible() // Rebuild when scrolling changes the visible rows.
			if r.H == 0 || v.H == 0 {
				c.AnimationFrame()
			}
			return
		}
		in.at = c.Now()
	}
	elapsed := c.Now().Sub(in.at)
	if elapsed >= privateBubbleDuration {
		in.done = true
		return
	}
	c.AnimationFrame()
	shown := privateRowProgress(elapsed, privateBubbleDuration)
	a.privateEntranceStyle(row, shown, 16)
	if a.motion == motionFull {
		scale := float32(0.985) + 0.015*shown
		row.TransformOrigin(1, 1).Scale(scale, scale)
	}
}
