package app

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

// Manual and background refreshes used to leave deleted server rows and their previews on screen.
func TestPrivateHistoryRemovesDeletedMessages(t *testing.T) {
	for _, background := range []bool{false, true} {
		name := "manual"
		if background {
			name = "background"
		}
		t.Run(name, func(t *testing.T) {
			a, _ := privateTestApp(t)
			user := a.dm.contacts[0].User
			a.openPrivateMessages(&user)
			chat := a.dm.chats[user.UserID]
			chat.loaded = false
			first := netease.PrivateMessage{ID: 1, Time: 100, From: user, Raw: `{"msg":"离线消息 A"}`}
			middle := netease.PrivateMessage{ID: 2, Time: 200, From: user, Raw: `{"msg":"离线消息 B"}`}
			last := netease.PrivateMessage{ID: 3, Time: 300, From: user, Raw: `{"msg":"离线消息 C"}`}
			// Use a different snapshot summary format to verify tail deletion still rolls back preview.
			a.dm.contacts[0].Time, a.dm.contacts[0].Summary = last.Time, "服务端格式化摘要 C"
			chat.serverUnreadAt, chat.unread = last.Time, 1
			manual := func(page netease.PrivateHistoryPage, err error) {
				chat.err = ""
				a.privateHistoryAnswered(a.accountGen, a.dm.epoch, user.UserID, chat.gen, 0, page, err)
			}
			manual(netease.PrivateHistoryPage{Items: []netease.PrivateMessage{first, middle, last}, Before: first.Time, Authoritative: true}, nil)
			defer a.stopPrivateInbox()
			in := a.dm.inbox
			reply := func(page netease.PrivateHistoryPage, err error) bool {
				if !background {
					manual(page, err)
					return true
				}
				request := privateInboxChange{id: user.UserID, after: -1}
				return a.privateInboxAnswered(in, a.accountGen, in.gen, request, netease.PrivateConversationPage{}, page, err, time.Unix(100, 0))
			}
			tt := ui.NewTester(a.view, 1180, 780)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true})
			privateShowContents(tt)
			tt.Advance(time.Second)
			if _, visible := tt.Find(middle.Text()); !visible {
				t.Fatal("message B was not displayed before refresh")
			}
			page := netease.PrivateHistoryPage{Items: []netease.PrivateMessage{first, last}, Before: first.Time, Authoritative: true}
			reply(page, errors.New("offline"))
			tt.Frame()
			if _, visible := tt.Find(middle.Text()); !visible || len(chat.messages) != 3 {
				t.Fatal("a failed refresh removed a message")
			}
			chat.arriving = map[privateMessageIdentity]*privateBubbleEntrance{privateMessageID(middle): {}}
			if !reply(page, nil) || chat.historyRows != nil || len(chat.arriving) != 0 || len(chat.messages) != 2 {
				t.Fatal("deleting B did not invalidate rows and discard only its entrance")
			}
			tt.Frame()
			if _, visible := tt.Find(middle.Text()); visible {
				t.Fatal("B is still visible after server deletion")
			}
			if background && reply(page, nil) {
				t.Fatal("identical history requested another redraw")
			}
			page.Items = page.Items[:1]
			// Simulate unread remaining on the deleted tail (user had not read it locally).
			chat.unread, chat.readAt, chat.serverUnreadAt = 1, 0, last.Time
			reply(page, nil)
			preview := slices.IndexFunc(a.dm.contacts, func(item netease.PrivateConversation) bool { return item.User.UserID == user.UserID })
			if a.dm.contacts[preview].Summary != first.Text() || a.dm.contacts[preview].Time != first.Time {
				t.Fatalf("deleting the newest message did not reveal the previous preview: %+v", a.dm.contacts[preview])
			}
			// Rolled-back serverUnreadAt allows a subsequent server zero at the older tail.
			if !a.applyPrivateUnreadSnapshot([]netease.PrivateConversation{{User: user, Time: first.Time, Unread: 0, UnreadKnown: true}}) || chat.unread != 0 {
				t.Fatalf("tail deletion left stale serverUnreadAt=%d blocking server zero (unread=%d)", chat.serverUnreadAt, chat.unread)
			}
			reply(netease.PrivateHistoryPage{Items: []netease.PrivateMessage{}, Authoritative: true}, nil)
			tt.Frame()
			if len(chat.messages) != 0 || len(chat.arriving) != 0 || !slices.ContainsFunc(chat.historyRows, func(row privateHistoryEntry) bool {
				return row.key.kind == privateHistoryEmpty
			}) {
				t.Fatal("complete empty history did not show the empty row")
			}
		})
	}
}

// A partial page may remove a visible message but must preserve older rows, the cursor and pending sends,
// and neither automatic nor manual lagging pages may prune newer cached rows while a snapshot leads History.
func TestPrivateHistoryDeletionRespectsPageAndPendingSend(t *testing.T) {
	a, _ := privateTestApp(t)
	user := a.dm.contacts[0].User
	chat := a.dm.chats[user.UserID]
	chat.more, chat.before = true, 75
	for id := int64(1); id <= 32; id++ {
		chat.messages = append(chat.messages, netease.PrivateMessage{ID: id, Time: 100 + id, From: user, Raw: `{"msg":"离线记录"}`})
	}
	chat.messages[0].Time, chat.messages[1].Time = 75, chat.messages[2].Time
	pending := chat.addOutgoing("尚待确认", time.UnixMilli(133), a.profile.UserID)
	chat.prepareHistoryRows()
	a.dm.selected = user.UserID
	startTestPrivateInbox(a)
	defer a.stopPrivateInbox()
	in := a.dm.inbox
	now := time.Unix(100, 0)
	chat.contactAt = 133
	in.queuePending(user.UserID, 131)
	request := *in.pending
	chat.loading = true
	a.tickPrivateInbox(now)
	if in.busy && in.requestID == user.UserID {
		t.Fatal("an automatic history fetch overlapped the same chat's manual refresh")
	}
	// Manual lagging empty/old page while snapshot leads History must also degrade Authoritative.
	a.privateHistoryAnswered(a.accountGen, a.dm.epoch, user.UserID, chat.gen, 0,
		netease.PrivateHistoryPage{Items: []netease.PrivateMessage{}, Authoritative: true}, nil)
	if len(chat.messages) != 32 || in.pending == nil {
		t.Fatal("manual lagging empty page pruned cached rows or consumed the pending request")
	}
	reply := func(page netease.PrivateHistoryPage) bool {
		return a.privateInboxAnswered(in, a.accountGen, in.gen, request, netease.PrivateConversationPage{}, page, nil, now)
	}
	if reply(netease.PrivateHistoryPage{Items: []netease.PrivateMessage{}, Authoritative: true}) ||
		len(chat.messages) != 32 {
		t.Fatal("automatic lagging empty page removed rows before the snapshot time became visible")
	}
	items := append([]netease.PrivateMessage(nil), chat.messages[2:19]...)
	items = append(items, chat.messages[20:]...)
	fresh := netease.PrivateMessage{ID: 33, Time: 133, From: user, Raw: `{"msg":"离线新消息"}`}
	items = append(items, fresh)
	page := netease.PrivateHistoryPage{Items: items, More: true, Before: items[0].Time, Authoritative: true}
	if !reply(page) || chat.historyRows != nil || len(chat.messages) != 32 ||
		chat.messages[0].ID != 1 || chat.messages[1].ID != 2 || chat.before != 75 || !chat.more || chat.unread != 1 || in.pending != nil ||
		slices.ContainsFunc(chat.messages, func(msg netease.PrivateMessage) bool { return msg.ID == 20 }) {
		t.Fatal("latest page did not replace only covered messages")
	}
	chat.prepareHistoryRows()
	if pending.confirmed != nil || !slices.ContainsFunc(chat.historyRows, func(row privateHistoryEntry) bool { return row.outgoing == pending }) {
		t.Fatal("server pruning removed or confirmed the local pending bubble")
	}
	if reply(page) || chat.unread != 1 {
		t.Fatal("identical page duplicated a redraw or unread message")
	}
	// A stale Conversations rollback cannot auto-queue pruning of newer cached history.
	a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
		netease.PrivateConversationPage{Items: []netease.PrivateConversation{{User: user, Time: 110, Summary: "旧副本"}}},
		netease.PrivateHistoryPage{}, nil, now)
	if in.pending != nil || len(chat.messages) != 32 {
		t.Fatal("stale conversation rollback queued automatic pruning or dropped cached messages")
	}
	// An automatic lagging History behind contactAt must not prune cached rows either.
	stalePage := netease.PrivateHistoryPage{Items: []netease.PrivateMessage{chat.messages[0]}, Before: chat.messages[0].Time, Authoritative: true}
	if a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{id: user.UserID, after: -1}, netease.PrivateConversationPage{}, stalePage, nil, now) ||
		len(chat.messages) != 32 {
		t.Fatal("automatic History behind contactAt pruned newer cached messages")
	}
	older := netease.PrivateMessage{ID: 100, Time: 50, From: user, Raw: `{"msg":"离线更早记录"}`}
	a.updatePrivateHistory(user.UserID, chat.before, netease.PrivateHistoryPage{Items: []netease.PrivateMessage{older}, Before: older.Time}, false, -1)
	if len(chat.messages) != 33 || chat.messages[0].ID != older.ID || chat.messages[32].ID != fresh.ID {
		t.Fatal("older paging disturbed the latest server rows")
	}
}
