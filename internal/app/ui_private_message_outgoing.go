package app

import (
	"time"

	"github.com/egoist/mygo/ui"
)

type privateFailureVisual struct {
	icon, text, scale, rise, textRise, space float32
}

func privateFailureEntering(elapsed time.Duration, mode int) privateFailureVisual {
	icon := privateRowProgress(elapsed, 200*time.Millisecond)
	text := privateRowProgress(elapsed-40*time.Millisecond, 180*time.Millisecond)
	v := privateFailureVisual{icon: icon, text: text, scale: 1, space: max(icon, text)}
	if mode == motionFull {
		v.rise, v.textRise = 4*(1-icon), 3*(1-text)
		if elapsed < 140*time.Millisecond {
			v.scale = 0.75 + 0.33*privateRowProgress(elapsed, 140*time.Millisecond)
		} else {
			v.scale = 1.08 - 0.08*privateRowProgress(elapsed-140*time.Millisecond, 60*time.Millisecond)
		}
	}
	return v
}

func (out *privateOutgoing) failureVisual(c *ui.Context, mode int) privateFailureVisual {
	if out.failedAt.IsZero() {
		if !out.failed {
			return privateFailureVisual{scale: 1}
		}
		out.failedAt = c.Now() // once per failure transition, never per reconstruction
	}
	elapsed := c.Now().Sub(out.failedAt)
	if mode == motionOff {
		if out.failed {
			return privateFailureVisual{icon: 1, text: 1, scale: 1, space: 1}
		}
		out.clearedAt, out.clearIcon, out.clearText = c.Now(), 0, 0
		return privateFailureVisual{scale: 1}
	}
	if out.failed {
		if elapsed < 220*time.Millisecond {
			c.AnimationFrame()
		}
		return privateFailureEntering(elapsed, mode)
	}
	if out.clearedAt.IsZero() {
		out.clearedAt = c.Now()
		v := privateFailureEntering(elapsed, mode)
		out.clearIcon, out.clearText = v.icon, v.text
	}
	elapsed = c.Now().Sub(out.clearedAt)
	if elapsed < 140*time.Millisecond {
		c.AnimationFrame()
	}
	fade := 1 - privateRowProgress(elapsed, 140*time.Millisecond)
	return privateFailureVisual{icon: out.clearIcon * fade, text: out.clearText * fade, scale: 1, space: max(out.clearIcon, out.clearText) * fade}
}

func (a *app) privateOutgoingRow(c *ui.Context, out *privateOutgoing, history *ui.ListState) {
	v := out.failureVisual(c, a.motion)
	row := ui.Row(c.Key("local-outgoing")).AlignItems(ui.Start)
	if out.entrance.done {
		a.privateEntranceStyle(row, a.dm.messagesIn.progress(c, privateMessageIdentity{local: out.key}, a.motion), 8)
	} else {
		a.privateBubbleMotion(c, row, &out.entrance, history)
	}
	row.Children(func() {
		ui.Box(c).Grow(1)
		ui.Column(c.Key("outgoing-bubble")).Shrink(1).MinWidth(0).MaxWidth(448).AlignItems(ui.End).Children(func() {
			ui.Row(c).MinWidth(0).AlignItems(ui.Center).Children(func() {
				a.privateMessageBubble(c, out.message, true)
				if v.space > 0 {
					ui.Box(c.Key("failure-gutter")).Width(28*v.space).Shrink(0).Height(24).Padding(0, 0, 0, 6*v.space).Children(func() {
						icon := ui.ButtonBase(c.Key("failure")).Absolute().Right(2).Top(1).Size(22, 22).Radius(999).Border(1.5, dangerColor).
							Label("发送失败说明").Tooltip(out.detail).Opacity(v.icon).Scale(v.scale, v.scale).Translate(0, v.rise)
						icon.Children(func() { ui.Text(c, "!").FontSize(15).FontWeight(700).TextColor(dangerColor) })
						icon.Center().OnClick(func() { a.toast = out.detail })
					})
				}
			})
			if v.space > 0 {
				ui.Box(c.Key("failure-caption")).Height(20*v.space).Padding(3, 28*v.space, 0, 0).AlignSelf(ui.Stretch).Children(func() {
					ui.Text(c, "尚未送达").FontSize(11).TextColor(dangerColor).TextAlign(ui.End).Opacity(v.text).Translate(0, v.textRise)
				})
			}
		})
	})
}
