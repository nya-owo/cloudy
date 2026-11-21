package app

import (
	"context"
	"time"
)

type privateReadKey struct {
	message    privateMessageIdentity
	time       int64
	gen, epoch int
}

type privateReadState struct {
	confirmed      *privateMessageIdentity
	confirmedAt    int64
	confirmedAtIDs map[privateMessageIdentity]bool
	attempt        *privateReadKey
	attemptIDs     []privateMessageIdentity
	busy           bool
}

func (chat *privateChat) privateUnreadHistoryReady() bool {
	if chat.gapUntil > 0 {
		return false
	}
	return chat.unread == 0 || chat.serverUnreadAt == 0 ||
		len(chat.messages) > 0 && chat.messages[len(chat.messages)-1].Time >= chat.serverUnreadAt
}

func (a *app) privateChatReadReady(chat *privateChat) bool {
	return chat.privateUnreadHistoryReady() && !a.privateHasPendingTarget(chat.user.UserID)
}

func (chat *privateChat) clearPrivateUnread() {
	chat.unread = 0
	chat.hasNewTail = false
	chat.unreadEstimated = false
	if len(chat.messages) > 0 {
		chat.readAt = max(chat.readAt, chat.messages[len(chat.messages)-1].Time)
	}
}

// Local badges clear on reading. A successful single-peer business ACK records
// the seen incoming identity separately, without relying on diagnostic counts.
func (a *app) markPrivateRead(chat *privateChat) {
	d := &a.dm
	if a.profile == nil || d.client == nil || d.ctx == nil || !chat.loaded || chat.loading || chat.err != "" ||
		!a.privateReading(chat.user.UserID) || !chat.history.AtEnd() || !a.privateChatReadReady(chat) || chat.read.busy {
		return
	}
	if chat.historyRows == nil || chat.historyCount != len(chat.messages) {
		return // Wait until the current server history has reached the list.
	}
	var message privateMessageIdentity
	var msgTime int64
	var atTime []privateMessageIdentity
	found := false
	for i := len(chat.messages) - 1; i >= 0; i-- {
		msg := chat.messages[i]
		if msg.From.UserID != chat.user.UserID {
			continue
		}
		if !found {
			msgTime, found = msg.Time, true
		}
		if msg.Time < msgTime {
			break
		}
		atTime = append(atTime, privateMessageID(msg))
	}
	if !found {
		return
	}
	if msgTime > chat.read.confirmedAt {
		message = atTime[0]
	} else if msgTime == chat.read.confirmedAt {
		unconfirmed := false
		for _, id := range atTime {
			if !chat.read.confirmedAtIDs[id] {
				message, unconfirmed = id, true
				break
			}
		}
		if !unconfirmed {
			return
		}
	} else {
		return
	}
	if chat.arriving[message] != nil {
		first, last := chat.history.Visible()
		for i := max(0, first); i <= last && i < len(chat.historyRows); i++ {
			row := chat.historyRows[i]
			if row.key.kind == privateHistoryMessage && row.key.message == message {
				return // Wait for the visible incoming row to finish entering.
			}
		}
	}
	key := privateReadKey{message: message, time: msgTime, gen: chat.gen, epoch: d.epoch}
	if chat.read.attempt != nil && *chat.read.attempt == key {
		return // Failure retries on reopening/loading, not on every UI frame.
	}
	chat.read.attempt, chat.read.attemptIDs, chat.read.busy = &key, atTime, true
	account, id, client := a.accountGen, chat.user.UserID, d.client
	ctx, cancel := context.WithTimeout(d.ctx, 12*time.Second)
	go func() {
		defer cancel()
		err := client.Read(ctx, id)
		a.post(func() { a.privateReadAnswered(account, id, key, err) })
	}()
}

func (a *app) privateReadAnswered(account int, id int64, key privateReadKey, err error) {
	if !a.privateRequestCurrent(account, key.epoch) {
		return
	}
	chat := a.dm.chats[id]
	if chat == nil || !chat.read.busy || chat.read.attempt == nil || *chat.read.attempt != key {
		return
	}
	chat.read.busy = false
	attemptIDs := chat.read.attemptIDs
	chat.read.attemptIDs = nil
	if err == nil {
		message := key.message
		chat.read.confirmed = &message
		if key.time > chat.read.confirmedAt || chat.read.confirmedAtIDs == nil {
			chat.read.confirmedAt = key.time
			chat.read.confirmedAtIDs = make(map[privateMessageIdentity]bool, max(1, len(attemptIDs)))
		}
		if key.time == chat.read.confirmedAt {
			chat.read.confirmedAtIDs[message] = true
			for _, mid := range attemptIDs {
				chat.read.confirmedAtIDs[mid] = true
			}
		}
		if key.time > 0 {
			chat.readAt = max(chat.readAt, key.time)
		}
	}
	// An old reply must never clear unread from a later incoming message.
}
