package app

import (
	"time"

	"github.com/egoist/mygo/ui"
)

type privatePresentation struct {
	completeAt, contactsAt time.Time
	contents, chat         bool
}

// Cancel requests immediately and keep the visible tree for the shared dialog
// exit. Forced closes use closePrivateMessages directly.
func (a *app) requestPrivateClose() {
	d := &a.dm
	if !d.open {
		return
	}
	p, contacts, messages := d.presentation, d.contactsIn, d.messagesIn
	a.closePrivateMessages()
	if a.motion != motionOff && p.contents {
		d.presentation, d.contactsIn, d.messagesIn = p, contacts, messages
	}
	a.redraw()
}

func (p *privatePresentation) admit(c *ui.Context, k float32, open bool, mode int) {
	if !open {
		return
	}
	if mode == motionOff {
		p.contents, p.chat = true, true
		return
	}
	if !p.contents && k == 1 {
		if p.completeAt.IsZero() {
			p.completeAt = c.Now()
		}
		if c.Now().After(p.completeAt) {
			p.contents, p.contactsAt = true, c.Now()
		}
		if !p.contents {
			c.After(time.Millisecond)
		}
	}
	if p.contents && !p.chat {
		if c.Now().Sub(p.contactsAt) >= rowStep {
			p.chat = true
		} else {
			c.After(rowStep - c.Now().Sub(p.contactsAt))
		}
	}
}
