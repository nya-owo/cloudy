package app

import (
	"encoding/json"
	"errors"
	"time"

	"netease-music-mygo/internal/netease"
)

// Local presentation identity is separate from every server ID, including ID=0.
// Confirmed records retain their alias for pagination and never enter messages.
type privateOutgoing struct {
	key                  uint64
	message              netease.PrivateMessage
	at                   time.Time
	entrance             privateBubbleEntrance
	divider              bool
	snapshot             *privateSendReveal
	confirmed            *privateMessageIdentity
	visual               *privateMessageIdentity // display association only, never confirmation
	failed               bool
	detail               string
	failedAt, clearedAt  time.Time
	clearIcon, clearText float32
}

func (chat *privateChat) addOutgoing(text string, at time.Time, self int64) *privateOutgoing {
	raw, _ := json.Marshal(struct {
		Text string `json:"msg"`
	}{text})
	last := int64(0)
	if n := len(chat.messages); n > 0 {
		last = chat.messages[n-1].Time
	}
	for _, prior := range chat.outgoing {
		if prior.confirmed == nil {
			last = prior.message.Time
		}
	}
	chat.nextOutgoing++
	out := &privateOutgoing{
		key: chat.nextOutgoing, at: at, snapshot: newPrivateSendReveal(chat, text),
		message: netease.PrivateMessage{Time: at.UnixMilli(), From: netease.MessageUser{UserID: self}, Raw: string(raw)}.WithContentCache(),
		divider: last == 0 || at.UnixMilli()-last > 5*60*1000,
	}
	chat.outgoing = append(chat.outgoing, out)
	chat.historyRows = nil
	return out
}

func (out *privateOutgoing) fail(err error) {
	if out == nil || out.confirmed != nil || out.failed {
		return
	}
	out.failed = true
	out.detail = "发送结果未能确认，请先刷新消息确认，勿盲目重发。"
	var status *netease.PrivateMessageStatusError
	if errors.As(err, &status) {
		out.detail = status.Error() + "。请先刷新消息确认，再决定是否重试。"
	} else if err != nil {
		out.detail += " " + err.Error()
	}
}

// All outstanding snapshots take part, even an RPC still in flight: two same-
// text sends must never greedily claim a single server row. History, not ACK,
// is the authority; a late unique match can also clear an uncertain failure.
func (chat *privateChat) reconcileOutgoing(self int64) bool {
	changed := false
	claimed := map[privateMessageIdentity]bool{}
	for _, out := range chat.outgoing {
		if out.confirmed != nil {
			claimed[*out.confirmed] = true
		}
	}
	for {
		counts := map[privateMessageIdentity]int{}
		candidates := map[*privateOutgoing]*privateMessageIdentity{}
		for _, out := range chat.outgoing {
			if out.confirmed == nil {
				if id := out.snapshot.confirmed(chat.messages, self, claimed); id != nil {
					candidates[out] = id
					counts[*id]++
				}
			}
		}
		progress := false
		for out, id := range candidates {
			if counts[*id] == 1 {
				out.confirmed, out.failed = id, false
				out.snapshot = nil
				claimed[*id] = true
				changed, progress = true, true
			}
		}
		if !progress {
			break
		}
	}
	return changed
}
