package app

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

// A cold server count is displayed once, subsequent snapshots update without double counting,
// cross-device server zero clears unread, and stale snapshots cannot undo a local read.
func TestPrivateServerUnread(t *testing.T) {
	a, fake := privateTestApp(t)
	a.settings.Motion = "off"
	first, other := a.dm.contacts[0], a.dm.contacts[1]
	first.Time, first.Unread, first.UnreadKnown = 120, 3, true
	other.Time, other.Unread, other.UnreadKnown = 110, 1, true
	a.dm.contacts, a.dm.chats = nil, nil
	backend := &privateInboxBackend{fakePrivateMessages: fake, requested: make(chan privateInboxRequest, 3)}
	a.dm.client = backend
	t.Cleanup(a.closePrivateMessages)

	tt := ui.NewTester(a.view, 1180, 780)
	tt.Frame()
	open := func() {
		t.Helper()
		if err := chooseItem(tt, "演示账号", "私信"); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Unix(100, 0)
	contacts := func(err error, items ...netease.PrivateConversation) {
		a.privateContactsAnswered(a.accountGen, a.dm.epoch, 0, netease.PrivateConversationPage{Items: items}, err)
	}
	open()
	contacts(nil, first, other)
	tt.Frame()
	id, otherID := first.User.UserID, other.User.UserID
	chat, peer := a.dm.chats[id], a.dm.chats[otherID]
	if chat == nil || peer == nil || chat.unread != 3 || peer.unread != 1 || a.dm.inbox.pending != nil {
		t.Fatal("cold open did not establish the server counts directly")
	}
	if _, shown := tt.Find("会话未读私信"); !shown {
		t.Fatal("server unread did not reach the live conversation badge")
	}
	in := a.dm.inbox
	// Background snapshot advances first to Time=150, Unread=5; unselected chat does not fetch History.
	updated := first
	updated.Time, updated.Unread = 150, 5
	a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
		netease.PrivateConversationPage{Items: []netease.PrivateConversation{updated, other}},
		netease.PrivateHistoryPage{}, nil, now)
	if chat.unread != 5 || peer.unread != 1 || in.pending != nil {
		t.Fatal("background snapshot did not update server count or scanned unselected History")
	}
	// An older in-flight contact reply for Time=120 must leave the newer Time=150 count intact.
	a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
		netease.PrivateConversationPage{Items: []netease.PrivateConversation{first}},
		netease.PrivateHistoryPage{}, nil, now)
	if chat.unread != 5 {
		t.Fatal("an older contact snapshot rolled back newer server unread")
	}

	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	open()
	first.Time, first.Unread = 150, 5
	contacts(nil, first, other)
	if chat.unread != 5 || a.dm.inbox.pending != nil {
		t.Fatal("reopening added the server count to existing unread")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	open()
	first.Time, first.Unread = 170, 6
	contacts(nil, first, other)
	if chat.unread != 6 || a.dm.inbox.pending != nil {
		t.Fatal("reopening did not apply a newer server count for arrivals while closed")
	}
	var history netease.PrivateHistoryPage
	history.Authoritative = true
	for i, at := range []int64{100, 110, 120, 140, 150, 170} {
		history.Items = append(history.Items, netease.PrivateMessage{ID: int64(i + 1), Time: at, From: first.User})
	}
	tt.Frame()
	if err := tt.Click("会话：" + first.User.Nickname); err != nil {
		t.Fatal(err)
	}
	call := receivePrivateRequest(t, backend.requested)
	a.privateHistoryAnswered(a.accountGen, a.dm.epoch, call.id, chat.gen, call.before, history, nil)
	tt.Frame()
	tt.Advance(time.Millisecond)
	tt.Frame()
	if !chat.history.AtEnd() || chat.unread != 0 || peer.unread != 1 {
		t.Fatal("reading the tail did not clear only its conversation")
	}
	contacts(nil, first)
	absent := other
	absent.Time++
	absent.UnreadKnown, absent.Unread = false, 0
	contacts(nil, absent)
	other.Unread = 0
	contacts(errors.New("offline"), other)
	if chat.unread != 0 || peer.unread != 1 || a.dm.inbox.pending != nil {
		t.Fatal("a lagging read reply, missing field or failed snapshot replaced unread or queued history")
	}
	contacts(nil, other)
	if peer.unread != 0 {
		t.Fatal("an explicit server zero did not clear unread")
	}

	account, epoch := a.accountGen, a.dm.epoch
	a.accountGen++
	a.resetPrivateMessages() // The account change drops the entire current DM state.
	a.dm.client = fake
	a.openPrivateMessages(nil)
	a.privateContactsAnswered(account, epoch, 0, netease.PrivateConversationPage{Items: []netease.PrivateConversation{first}}, nil)
	if len(a.dm.chats) != 0 || len(a.dm.contacts) != 0 {
		t.Fatal("a previous account snapshot restored badges after account change")
	}
}

// An overlapping explicit History reply can satisfy a pending HTTPS snapshot request without fetching or counting twice.
func TestPrivateServerUnreadOverlappingHistory(t *testing.T) {
	a, _ := privateTestApp(t)
	first := a.dm.contacts[0]
	a.openPrivateMessages(&first.User)
	t.Cleanup(a.closePrivateMessages)
	first.Time, first.Unread, first.UnreadKnown = 120, 3, true
	a.privateContactsAnswered(a.accountGen, a.dm.epoch, 0, netease.PrivateConversationPage{Items: []netease.PrivateConversation{first}}, nil)
	chat := a.dm.chats[first.User.UserID]
	if !chat.loading {
		t.Fatal("no explicit history request to overlap")
	}
	in := a.dm.inbox
	now := time.Unix(100, 0)
	// Snapshot advances while explicit History for the same chat is still in flight.
	newer := first
	newer.Time, newer.Unread, newer.Summary = 140, 4, "新来信"
	a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
		netease.PrivateConversationPage{Items: []netease.PrivateConversation{newer}},
		netease.PrivateHistoryPage{}, nil, now)
	if in.pending == nil || in.pending.id != first.User.UserID {
		t.Fatalf("advanced snapshot did not queue pending History for selected chat: %+v", in.pending)
	}
	a.tickPrivateInbox(now)
	if in.busy && in.requestID == first.User.UserID {
		t.Fatal("inbox started a duplicate History request while explicit History was loading")
	}
	page := netease.PrivateHistoryPage{Items: []netease.PrivateMessage{{ID: 3, Time: 120, From: first.User}, {ID: 4, Time: 140, From: first.User}}}
	a.privateHistoryAnswered(a.accountGen, a.dm.epoch, first.User.UserID, chat.gen, 0, page, nil)
	if chat.unread != 4 || in.pending != nil {
		t.Fatalf("explicit reply did not satisfy the waiting pending request once: unread=%d pending=%v", chat.unread, in.pending)
	}
}

// Direct profile open can read before the first snapshot without badge revival; missing UnreadKnown
// still prompts "有新消息" and yields to a later server zero; same-millisecond new IDs are not missed.
func TestPrivateServerUnreadBoundaries(t *testing.T) {
	t.Run("read-before-first-snapshot", func(t *testing.T) {
		a, fake := privateTestApp(t)
		a.settings.Motion = "off"
		first := a.dm.contacts[0]
		a.dm.contacts, a.dm.chats = nil, nil
		backend := &privateInboxBackend{fakePrivateMessages: fake, requested: make(chan privateInboxRequest, 2)}
		a.dm.client = backend
		a.openPrivateMessages(&first.User)
		t.Cleanup(a.closePrivateMessages)

		id := first.User.UserID
		chat := a.dm.chats[id]
		call := receivePrivateRequest(t, backend.requested)
		history := netease.PrivateHistoryPage{Items: []netease.PrivateMessage{{ID: 5, Time: 150, From: first.User}}}
		a.privateHistoryAnswered(a.accountGen, a.dm.epoch, call.id, chat.gen, call.before, history, nil)

		tt := ui.NewTester(a.view, 1180, 780)
		tt.Frame()
		tt.Advance(time.Millisecond)
		tt.Frame()
		if chat.unread != 0 || !chat.read.busy || chat.read.attempt == nil {
			t.Fatal("reading the tail did not start Read ACK")
		}
		a.privateReadAnswered(a.accountGen, id, *chat.read.attempt, nil)

		first.Time, first.Unread, first.UnreadKnown = 150, 5, true
		a.privateContactsAnswered(a.accountGen, a.dm.epoch, 0, netease.PrivateConversationPage{Items: []netease.PrivateConversation{first}}, nil)
		if chat.unread != 0 {
			t.Fatalf("first same-tail snapshot revived a confirmed read: unread=%d", chat.unread)
		}
	})

	t.Run("unknown-unread-scrolled-up-then-phone-zero", func(t *testing.T) {
		a, _ := privateTestApp(t)
		a.settings.Motion = "off"
		first := a.dm.contacts[0]
		a.openPrivateMessages(&first.User)
		t.Cleanup(a.closePrivateMessages)
		chat, in := a.dm.chats[first.User.UserID], a.dm.inbox
		chat.loading, a.dm.loading = false, false
		for i := range 50 {
			chat.messages = append(chat.messages, netease.PrivateMessage{
				ID: int64(i + 1), Time: 1000 + int64(i), From: first.User, Raw: fmt.Sprintf(`{"msg":"历史 %02d"}`, i),
			})
		}
		tt := ui.NewTester(a.view, 1180, 780)
		tt.Frame()
		privateReadMessage(tt, chat, 10)
		if chat.history.AtEnd() {
			t.Fatal("failed to scroll away from tail")
		}
		now := time.Unix(200, 0)
		// Baseline with UnreadKnown=false.
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{{User: first.User, Time: 1049, Summary: "历史 49", UnreadKnown: false}}},
			netease.PrivateHistoryPage{}, nil, now)
		// New arrival with UnreadKnown=false must trigger History.
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{{User: first.User, Time: 2000, Summary: "手机未读来信", UnreadKnown: false}}},
			netease.PrivateHistoryPage{}, nil, now.Add(privateFocusInterval))
		if in.pending == nil || in.pending.id != first.User.UserID {
			t.Fatal("UnreadKnown=false time advance did not queue selected History")
		}
		fresh := netease.PrivateMessage{ID: 100, Time: 2000, From: first.User, Raw: `{"msg":"手机未读来信"}`}
		a.privateInboxAnswered(in, a.accountGen, in.gen, *in.pending, netease.PrivateConversationPage{},
			netease.PrivateHistoryPage{Items: []netease.PrivateMessage{fresh}}, nil, now.Add(privateFocusInterval))
		tt.Frame()
		if chat.unread != 1 || !chat.hasNewTail {
			t.Fatalf("missing UnreadKnown did not produce local unread/jump hint: unread=%d hasNewTail=%t", chat.unread, chat.hasNewTail)
		}
		if _, ok := tt.Find("有新消息"); !ok {
			t.Fatal("有新消息 button did not show for scrolled-up arrival without UnreadKnown")
		}
		// Another device reads the conversation: authoritative server zero must clear the local estimate.
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{{User: first.User, Time: 2000, Summary: "手机未读来信", Unread: 0, UnreadKnown: true}}},
			netease.PrivateHistoryPage{}, nil, now.Add(2*privateFocusInterval))
		if chat.unread != 0 || chat.hasNewTail || in.pending != nil {
			t.Fatalf("authoritative server zero failed to clear local estimate or queued History: unread=%d hasNewTail=%t pending=%+v", chat.unread, chat.hasNewTail, in.pending)
		}
	})

	t.Run("same-ms-new-id-after-local-read", func(t *testing.T) {
		a, _ := privateTestApp(t)
		a.settings.Motion = "off"
		first := a.dm.contacts[0]
		a.openPrivateMessages(&first.User)
		t.Cleanup(a.closePrivateMessages)
		chat, in := a.dm.chats[first.User.UserID], a.dm.inbox
		chat.loading, a.dm.loading = false, false
		for i := range 40 {
			chat.messages = append(chat.messages, netease.PrivateMessage{
				ID: int64(i + 1), Time: 900 + int64(i), From: first.User, Raw: fmt.Sprintf(`{"msg":"旧 %02d"}`, i),
			})
		}
		msg10 := netease.PrivateMessage{ID: 100, Time: 1000, From: first.User, Raw: `{"msg":"同毫秒一"}`}
		chat.messages = append(chat.messages, msg10)
		chat.readAt, chat.serverUnreadAt = 1000, 1000

		tt := ui.NewTester(a.view, 1180, 780)
		tt.Frame()
		privateReadMessage(tt, chat, 5)
		now := time.Unix(300, 0)
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{{User: first.User, Time: 1000, Summary: "同毫秒一", Unread: 0, UnreadKnown: true}}},
			netease.PrivateHistoryPage{}, nil, now)
		// When History delivers a same-millisecond new message ID, local unread and "有新消息" update.
		msg11 := netease.PrivateMessage{ID: 101, Time: 1000, From: first.User, Raw: `{"msg":"同毫秒二"}`}
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{id: first.User.UserID, after: 1000}, netease.PrivateConversationPage{},
			netease.PrivateHistoryPage{Items: []netease.PrivateMessage{msg10, msg11}}, nil, now.Add(privateFocusInterval))
		tt.Frame()
		if chat.unread != 1 || !chat.hasNewTail {
			t.Fatalf("same-millisecond new ID was missed: unread=%d hasNewTail=%t", chat.unread, chat.hasNewTail)
		}
		if _, ok := tt.Find("有新消息"); !ok {
			t.Fatal("有新消息 button missing for same-ms new ID")
		}
		// Cross-device read at the same millisecond (1 -> 0) clears the estimate.
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{{User: first.User, Time: 1000, Summary: "同毫秒二", Unread: 0, UnreadKnown: true}}},
			netease.PrivateHistoryPage{}, nil, now.Add(2*privateFocusInterval))
		if chat.unread != 0 || chat.hasNewTail {
			t.Fatalf("same-millisecond cross-device zero did not clear unread: unread=%d hasNewTail=%t", chat.unread, chat.hasNewTail)
		}
		// Stale positive snapshot at the same timestamp cannot revive after read.
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{{User: first.User, Time: 1000, Summary: "同毫秒二", Unread: 5, UnreadKnown: true}}},
			netease.PrivateHistoryPage{}, nil, now.Add(3*privateFocusInterval))
		if chat.unread != 0 {
			t.Fatalf("stale same-ms positive snapshot revived unread=%d", chat.unread)
		}
	})

	// Regression (P1-3): after a positive server snapshot replaces a same-ms local estimate,
	// a later cross-device server zero at the same readAt must still clear unread and hasNewTail
	// (reporting changed=true), while stale positive and UnreadKnown=false snapshots stay rejected.
	t.Run("same-ms-positive-then-phone-zero-and-stale-positive", func(t *testing.T) {
		a, _ := privateTestApp(t)
		a.settings.Motion = "off"
		first := a.dm.contacts[0]
		a.openPrivateMessages(&first.User)
		t.Cleanup(a.closePrivateMessages)
		chat, in := a.dm.chats[first.User.UserID], a.dm.inbox
		chat.loading, a.dm.loading = false, false
		for i := range 40 {
			chat.messages = append(chat.messages, netease.PrivateMessage{
				ID: int64(i + 1), Time: 200 + int64(i), From: first.User, Raw: fmt.Sprintf(`{"msg":"旧 %02d"}`, i),
			})
		}
		msgA := netease.PrivateMessage{ID: 100, Time: 300, From: first.User, Raw: `{"msg":"已知A"}`}
		chat.messages = append(chat.messages, msgA)
		chat.readAt, chat.serverUnreadAt = 300, 300

		tt := ui.NewTester(a.view, 1180, 780)
		tt.Frame()
		privateReadMessage(tt, chat, 5)
		now := time.Unix(400, 0)
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{{User: first.User, Time: 300, Summary: "已知A", Unread: 0, UnreadKnown: true}}},
			netease.PrivateHistoryPage{}, nil, now)

		// H brings new peer B@300 -> local estimate unread=1, hasNewTail=true.
		msgB := netease.PrivateMessage{ID: 101, Time: 300, From: first.User, Raw: `{"msg":"新来信B"}`}
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{id: first.User.UserID, after: 300},
			netease.PrivateConversationPage{}, netease.PrivateHistoryPage{Items: []netease.PrivateMessage{msgA, msgB}}, nil, now)
		if chat.unread != 1 || !chat.unreadEstimated || !chat.hasNewTail {
			t.Fatalf("H arrival did not establish same-ms estimate: unread=%d est=%t hasNewTail=%t", chat.unread, chat.unreadEstimated, chat.hasNewTail)
		}

		// C replaces local estimate with authoritative positive unread=2 (unreadEstimated becomes false).
		if !a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{{User: first.User, Time: 300, Summary: "新来信B", Unread: 2, UnreadKnown: true}}},
			netease.PrivateHistoryPage{}, nil, now.Add(privateFocusInterval)) {
			t.Fatal("positive same-ms server count did not report change")
		}
		if chat.unread != 2 || chat.unreadEstimated || !chat.hasNewTail {
			t.Fatalf("positive C did not replace estimate cleanly: unread=%d est=%t hasNewTail=%t", chat.unread, chat.unreadEstimated, chat.hasNewTail)
		}

		// Phone reads the chat: C returns same-ms Unread=0 -> must clear unread & hasNewTail and report changed=true.
		if !a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{{User: first.User, Time: 300, Summary: "新来信B", Unread: 0, UnreadKnown: true}}},
			netease.PrivateHistoryPage{}, nil, now.Add(2*privateFocusInterval)) {
			t.Fatal("same-ms server zero after positive count did not report UI change")
		}
		if chat.unread != 0 || chat.hasNewTail {
			t.Fatalf("same-ms server zero after positive count failed to clear unread/hasNewTail: unread=%d hasNewTail=%t", chat.unread, chat.hasNewTail)
		}

		// Stale positive snapshot at 300 must NOT revive unread.
		if a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{{User: first.User, Time: 300, Summary: "新来信B", Unread: 2, UnreadKnown: true}}},
			netease.PrivateHistoryPage{}, nil, now.Add(3*privateFocusInterval)) || chat.unread != 0 {
			t.Fatalf("stale positive snapshot revived unread=%d", chat.unread)
		}

		// UnreadKnown=false must still be ignored.
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{{User: first.User, Time: 400, Summary: "新来信B", Unread: 5, UnreadKnown: false}}},
			netease.PrivateHistoryPage{}, nil, now.Add(4*privateFocusInterval))
		if chat.unread != 0 {
			t.Fatalf("UnreadKnown=false mutated unread=%d", chat.unread)
		}

		// Clearing hasNewTail when unread is already 0 must still report changed=true so the jump button redraws immediately.
		chat.hasNewTail = true
		if !a.applyPrivateUnreadSnapshot([]netease.PrivateConversation{{User: first.User, Time: 300, Unread: 0, UnreadKnown: true}}) || chat.hasNewTail {
			t.Fatal("clearing hasNewTail at unread=0 did not report changed=true")
		}
	})

	// Regression (P1-2): an unselected contact first seen in Conversations(0) with UnreadKnown=false
	// must record its contactAt watermark so clicking it and receiving a lagging History page does not
	// prematurely send a whole-conversation Read ACK before the latest message arrives.
	t.Run("first-enter-unselected-contact-without-unread-blocks-lagging-read", func(t *testing.T) {
		a, fake := privateTestApp(t)
		a.settings.Motion = "off"
		first, other := a.dm.contacts[0].User, a.dm.contacts[1].User
		delete(a.dm.chats, other.UserID) // Never opened before.
		backend := &privateReadBackend{fakePrivateMessages: fake, reads: make(chan int64, 4)}
		a.dm.client = backend
		a.openPrivateMessages(&first)
		t.Cleanup(a.closePrivateMessages)

		// First-page Conversations(0) includes unselected `other` at Time=300 without newMsgCount.
		a.privateContactsAnswered(a.accountGen, a.dm.epoch, 0, netease.PrivateConversationPage{
			Items: []netease.PrivateConversation{
				{User: first, Time: 100, Summary: "会话一", Unread: 0, UnreadKnown: true},
				{User: other, Time: 300, Summary: "尚未入历史的新来信B", UnreadKnown: false},
			},
		}, nil)
		if a.dm.inbox.pending != nil {
			t.Fatalf("unselected contact queued automatic History: %+v", a.dm.inbox.pending)
		}

		// User clicks `other`; explicit History lags and returns only older A@100.
		a.selectPrivateChat(other)
		otherChat := a.dm.chats[other.UserID]
		msgA := netease.PrivateMessage{ID: 1, Time: 100, From: other, Raw: `{"msg":"旧来信A"}`}
		a.privateHistoryAnswered(a.accountGen, a.dm.epoch, other.UserID, otherChat.gen, 0,
			netease.PrivateHistoryPage{Items: []netease.PrivateMessage{msgA}, Before: 100, Authoritative: true}, nil)

		tt := ui.NewTester(a.view, 1180, 780)
		tt.Frame()
		otherChat.history.ScrollToEnd()
		tt.Frame()
		tt.Advance(time.Millisecond)
		tt.Frame()
		select {
		case id := <-backend.reads:
			t.Fatalf("lagging History (100 < 300) prematurely sent Read for %d", id)
		default:
		}

		// When History catches up to B@300, Read is allowed and fires once.
		msgB := netease.PrivateMessage{ID: 2, Time: 300, From: other, Raw: `{"msg":"尚未入历史的新来信B"}`}
		a.privateHistoryAnswered(a.accountGen, a.dm.epoch, other.UserID, otherChat.gen, 0,
			netease.PrivateHistoryPage{Items: []netease.PrivateMessage{msgA, msgB}, Before: 100, Authoritative: true}, nil)
		tt.Frame()
		tt.Advance(time.Millisecond)
		tt.Frame()
		if got := receivePrivateRequest(t, backend.reads); got != other.UserID {
			t.Fatalf("catching up to B@300 did not trigger Read: got=%d", got)
		}
	})
}
