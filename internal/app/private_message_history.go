package app

import (
	"strings"
	"time"

	"netease-music-mygo/internal/netease"
)

type privateHistoryKey struct {
	kind    uint8
	message privateMessageIdentity
	local   uint64
}

type privateHistoryEntry struct {
	key      privateHistoryKey
	message  int
	outgoing *privateOutgoing
	time     int64
}

const (
	privateHistoryMessage uint8 = iota
	privateHistoryTime
	privateHistoryOlder
	privateHistoryError
	privateHistoryEmpty
	privateHistoryEnd
	privateHistoryOutgoing
)

// Rows are data only. The framework measures and builds only visible UI rows.
func (chat *privateChat) prepareHistoryRows() {
	if chat.historyRows != nil && chat.historyCount == len(chat.messages) {
		return
	}
	chat.historyCount = len(chat.messages)
	rows := make([]privateHistoryEntry, 0, len(chat.messages)+4)
	add := func(kind uint8, index int, id privateMessageIdentity) {
		rows = append(rows, privateHistoryEntry{key: privateHistoryKey{kind: kind, message: id}, message: index})
	}
	if chat.more {
		add(privateHistoryOlder, 0, privateMessageIdentity{})
	}
	if len(chat.messages) == 0 && len(chat.outgoing) == 0 {
		if chat.err != "" {
			add(privateHistoryError, 0, privateMessageIdentity{})
		} else if chat.loaded && !chat.loading {
			add(privateHistoryEmpty, 0, privateMessageIdentity{})
		}
	}
	last := int64(0)
	aliases := chat.outgoingAliases()
	emitted := map[uint64]bool{}
	addOutgoing := func(out *privateOutgoing) {
		emitted[out.key] = true
		if out.divider {
			rows = append(rows, privateHistoryEntry{key: privateHistoryKey{kind: privateHistoryTime, local: out.key}, time: out.message.Time})
		}
		rows = append(rows, privateHistoryEntry{key: privateHistoryKey{kind: privateHistoryOutgoing, local: out.key}, outgoing: out})
		last = out.message.Time
	}
	for i, msg := range chat.messages {
		id := privateMessageID(msg)
		if out := aliases[id]; out != nil {
			// An earlier local Send may still be awaiting history. A later
			// confirmation must not reverse their submission order on screen.
			for _, prior := range chat.outgoing {
				if prior.key >= out.key {
					break
				}
				if prior.confirmed == nil && !emitted[prior.key] {
					addOutgoing(prior)
				}
			}
			if !emitted[out.key] {
				addOutgoing(out)
			}
			continue
		}
		if msg.Time > 0 && (last == 0 || msg.Time-last > 5*60*1000) {
			add(privateHistoryTime, i, id)
			rows[len(rows)-1].time = msg.Time
		}
		last = msg.Time
		add(privateHistoryMessage, i, id)
	}
	for _, out := range chat.outgoing {
		if out.confirmed == nil && !emitted[out.key] {
			addOutgoing(out)
		}
	}
	add(privateHistoryEnd, 0, privateMessageIdentity{})
	chat.historyRows = rows
	chat.history.FollowEnd = true
}

func (chat *privateChat) outgoingAliases() map[privateMessageIdentity]*privateOutgoing {
	aliases := map[privateMessageIdentity]*privateOutgoing{}
	for _, out := range chat.outgoing {
		if out.confirmed != nil {
			aliases[*out.confirmed] = out
		}
	}
	chat.associateOutgoingRows(aliases)
	return aliases
}

// Presentation-only pairs conserve visible send counts without asserting an ACK.
// Keep established pairs as history grows; each server identity owns at most
// one local row, whose pending/failure state and stable key stay intact.
func (chat *privateChat) associateOutgoingRows(aliases map[privateMessageIdentity]*privateOutgoing) {
	eligible := func(out *privateOutgoing, msg netease.PrivateMessage) bool {
		id := privateMessageID(msg)
		delta := msg.Time - out.message.Time
		return out.message.From.UserID != 0 && msg.From.UserID == out.message.From.UserID &&
			delta >= 0 && delta <= (30*time.Second).Milliseconds() &&
			(out.snapshot == nil || !out.snapshot.before[id]) && msg.Content().Media == nil &&
			strings.TrimSpace(msg.Content().Text) == strings.TrimSpace(out.message.Content().Text)
	}
	paired := map[*privateOutgoing]bool{}
	for _, out := range aliases {
		paired[out] = true
	}
	for _, out := range chat.outgoing {
		if out.confirmed != nil || out.visual == nil {
			continue
		}
		for _, msg := range chat.messages {
			id := privateMessageID(msg)
			if id == *out.visual && aliases[id] == nil && eligible(out, msg) {
				aliases[id], paired[out] = out, true
				break
			}
		}
	}
	for _, msg := range chat.messages {
		id := privateMessageID(msg)
		if aliases[id] != nil {
			continue
		}
		for _, out := range chat.outgoing {
			if !paired[out] && out.confirmed == nil && eligible(out, msg) {
				out.visual = &id
				aliases[id], paired[out] = out, true
				break
			}
		}
	}
}
