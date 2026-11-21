package app

import (
	"crypto/sha256"
	"encoding/json"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

// IDs are authoritative; ID-less messages use the merge's time/from/raw identity.
// Hashing raw keeps UI keys bounded without putting message text in diagnostics.
type privateMessageIdentity struct {
	id, time, from int64
	local          uint64
	raw            [32]byte
}

func privateMessageID(msg netease.PrivateMessage) privateMessageIdentity {
	if msg.ID != 0 {
		return privateMessageIdentity{id: msg.ID}
	}
	return privateMessageIdentity{time: msg.Time, from: msg.From.UserID, raw: sha256.Sum256([]byte(msg.Raw))}
}

type privateSendReveal struct {
	before map[privateMessageIdentity]bool
	text   string
}

func newPrivateSendReveal(chat *privateChat, text string) *privateSendReveal {
	if !chat.loaded {
		return nil // an incomplete baseline cannot distinguish old messages
	}
	// A refresh in flight keeps its loaded baseline: the next Send may already
	// start while the previous Send's history confirmation is still pending.
	r := &privateSendReveal{before: map[privateMessageIdentity]bool{}, text: text}
	for _, msg := range chat.messages {
		r.before[privateMessageID(msg)] = true
	}
	for _, out := range chat.outgoing {
		if out.confirmed != nil {
			r.before[*out.confirmed] = true
		}
	}
	return r
}

func (r *privateSendReveal) confirmed(messages []netease.PrivateMessage, self int64, claimed map[privateMessageIdentity]bool) *privateMessageIdentity {
	if r == nil {
		return nil
	}
	var found *privateMessageIdentity
	for _, msg := range messages {
		key := privateMessageID(msg)
		if msg.From.UserID != self || r.before[key] || claimed[key] || msg.Content().Media != nil {
			continue
		}
		var body struct {
			Text string `json:"msg"`
		}
		if json.Unmarshal([]byte(msg.Raw), &body) != nil || strings.TrimSpace(body.Text) != strings.TrimSpace(r.text) {
			continue
		}
		if found != nil && *found != key {
			return nil // multiple matching outgoing messages are ambiguous
		}
		found = &key
	}
	return found
}

// Each strip has its own match in the window, even when both are visible.
type dmContactPillMatch struct{ stickers bool }

type privateRowEntrance struct {
	pending bool
	at      time.Time
	query   string
	order   map[privateMessageIdentity]int
}

func (in *privateRowEntrance) start(keys []privateMessageIdentity, at time.Time) {
	in.pending, in.at = false, at
	in.order = make(map[privateMessageIdentity]int, len(keys))
	for i, key := range keys {
		in.order[key] = i
	}
}

// Same ease-out cubic and rowStep/rowTime as playlist rowIn, without its route state.
func privateRowProgress(elapsed, duration time.Duration) float32 {
	t := 1 - clamp01(float32(elapsed)/float32(duration))
	return 1 - t*t*t
}

func (in *privateRowEntrance) progress(c *ui.Context, key privateMessageIdentity, mode int) float32 {
	if len(in.order) == 0 {
		return 1
	}
	intervals := time.Duration(len(in.order) - 1)
	step := min(rowStep, 360*time.Millisecond/max(1, intervals))
	if mode == motionOff || c.Now().Sub(in.at) >= rowTime+intervals*step {
		in.order = nil
		return 1
	}
	i, ok := in.order[key]
	if !ok {
		return 1
	}
	elapsed := c.Now().Sub(in.at) - time.Duration(i)*step
	if elapsed < rowTime {
		c.AnimationFrame()
	}
	return privateRowProgress(elapsed, rowTime)
}

func (a *app) preparePrivateContactsIn(c *ui.Context, query string, keys []privateMessageIdentity, laidOut bool) {
	d, in := &a.dm, &a.dm.contactsIn
	if !in.pending {
		if in.query != query {
			in.order = nil
		}
		return
	}
	if !d.loaded && len(d.contacts) == 0 {
		return
	}
	if a.motion == motionOff {
		in.pending, in.query, in.order = false, query, nil
		return
	}
	if !laidOut {
		c.AnimationFrame()
		return
	}
	in.query = query
	in.start(keys, c.Now())
}

func (a *app) preparePrivateMessagesIn(c *ui.Context, chat *privateChat, historyReady bool) {
	in := &a.dm.messagesIn
	if !in.pending || !a.dm.open {
		return
	}
	if a.motion == motionOff {
		in.pending = false
		return
	}
	if len(chat.messages) == 0 && len(chat.outgoing) == 0 {
		in.pending = !chat.loaded || chat.loading
		return
	}
	// Visible is the committed viewport. Let the explicit tail jump and
	// any new row data lay out before taking the one-time entrance batch.
	first, last := chat.history.Visible()
	if chat.bottom || !historyReady {
		in.at = c.Now()
	}
	if !c.Now().After(in.at) {
		c.After(time.Millisecond) // Same-frame rebuilds can still see the old viewport.
		return
	}
	last = min(last, len(chat.historyRows)-1)
	if last < first {
		return
	}
	if !chat.history.AtEnd() {
		in.pending = false
		return
	}
	var keys []privateMessageIdentity
	for _, entry := range chat.historyRows[first : last+1] {
		if entry.key.kind == privateHistoryMessage {
			keys = append(keys, entry.key.message)
		} else if entry.outgoing != nil && entry.outgoing.entrance.done {
			keys = append(keys, privateMessageIdentity{local: entry.outgoing.key})
		}
	}
	at := c.Now().Add(80 * time.Millisecond)
	if afterContacts := a.dm.contactsIn.at.Add(rowStep + 80*time.Millisecond); at.Before(afterContacts) {
		at = afterContacts
	}
	in.start(keys, at)
}

func (a *app) privateEntranceStyle(e ui.Element, shown, rise float32) {
	if shown >= 1 {
		return
	}
	if a.motion == motionFull {
		e.Translate(0, rise*(1-shown))
	}
	e.Opacity(shown)
}

func (a *app) privateMessageMotion(c *ui.Context, e ui.Element, chat *privateChat, msg netease.PrivateMessage) {
	key := privateMessageID(msg)
	if in := chat.arriving[key]; in != nil {
		a.privateBubbleMotion(c, e, in, &chat.history)
		if in.done {
			delete(chat.arriving, key)
		}
		return
	}
	a.privateEntranceStyle(e, a.dm.messagesIn.progress(c, key, a.motion), 8)
}
