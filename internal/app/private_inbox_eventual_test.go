package app

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

type eventualPrivateHistory struct {
	*fakePrivateMessages
	pages    []netease.PrivateHistoryPage
	reply    chan netease.PrivateHistoryPage
	contacts chan int
	reads    chan int64
}

func (b *eventualPrivateHistory) Conversations(_ context.Context, offset int) (netease.PrivateConversationPage, error) {
	if b.contacts != nil {
		b.contacts <- offset
	}
	return netease.PrivateConversationPage{}, nil
}

func (b *eventualPrivateHistory) History(context.Context, int64, int64) (netease.PrivateHistoryPage, error) {
	page := b.pages[0]
	b.pages = b.pages[1:]
	b.reply <- page
	return page, nil
}

func (b *eventualPrivateHistory) Read(_ context.Context, id int64) error {
	if b.reads != nil {
		b.reads <- id
	}
	return nil
}

// Conversations may advance ahead of History visibility: a lagging HTTP 200 History page must not prune
// cached messages or trigger fast-retry storms, and the next low-frequency Conversations(0) slot must
// still re-check History even when the Conversations snapshot itself is unchanged.
func TestPrivateInboxEventualHistory(t *testing.T) {
	t.Run("lagging-history-recovers-on-next-low-frequency-poll", func(t *testing.T) {
		a, fake := privateTestApp(t)
		user := a.dm.contacts[0].User
		a.dm.selected = user.UserID
		old := netease.PrivateMessage{ID: 1, Time: 1000, From: user, Raw: `{"msg":"已有记录"}`}
		fresh := old
		fresh.ID, fresh.Time, fresh.Raw = 2, 2000, `{"msg":"后来出现的目标消息"}`
		stalePage := netease.PrivateHistoryPage{Items: []netease.PrivateMessage{old}, Before: old.Time, Authoritative: true}
		freshPage := netease.PrivateHistoryPage{Items: []netease.PrivateMessage{old, fresh}, Before: old.Time, Authoritative: true}
		backend := &eventualPrivateHistory{
			fakePrivateMessages: fake,
			reply:               make(chan netease.PrivateHistoryPage, 2),
			contacts:            make(chan int, 2),
			pages:               []netease.PrivateHistoryPage{stalePage, freshPage},
		}
		a.dm.client = backend
		startTestPrivateInbox(a)
		defer a.stopPrivateInbox()
		chat, in := a.dm.chats[user.UserID], a.dm.inbox
		chat.messages = []netease.PrivateMessage{old}
		now := time.Unix(100, 0)
		in.syncAt = now.Add(privateFocusInterval)

		// Initial Conversations(0) at @1000 matches cached tail and queues no History.
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{{User: user, Time: 1000, Summary: "已有记录", Unread: 0, UnreadKnown: true}}},
			netease.PrivateHistoryPage{}, nil, now)
		if in.pending != nil {
			t.Fatal("initial snapshot matching chatTail unexpectedly queued History")
		}

		// Conversations(0) advances to @2000 -> queues one immediate History check.
		snap2000 := netease.PrivateConversation{User: user, Time: 2000, Summary: "后来出现的目标消息", Unread: 1, UnreadKnown: true}
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{snap2000}},
			netease.PrivateHistoryPage{}, nil, now)
		if in.pending == nil || in.pending.id != user.UserID {
			t.Fatalf("advanced snapshot did not queue selected History: %+v", in.pending)
		}

		req1 := *in.pending
		a.tickPrivateInbox(now)
		page1 := receivePrivateRequest(t, backend.reply)
		a.privateInboxAnswered(in, a.accountGen, in.gen, req1, netease.PrivateConversationPage{}, page1, nil, now)
		// First History lagged at @1000: must not prune @1000, must not schedule fast retries,
		// and must keep Read blocked while chatTail (1000) < contactAt (2000).
		if len(chat.messages) != 1 || in.pending != nil || !a.privateHasPendingTarget(user.UserID) {
			t.Fatalf("lagging History pruned cache, queued fast retry, or unblocked Read: msgs=%d pending=%+v readBlocked=%t",
				len(chat.messages), in.pending, a.privateHasPendingTarget(user.UserID))
		}
		a.tickPrivateInbox(now.Add(privateFocusInterval - time.Millisecond))
		if in.busy {
			t.Fatal("inbox fired before the next 30s low-frequency slot")
		}

		// Next 30s slot runs Conversations(0) first, returning the EXACT SAME snapshot @2000.
		now = now.Add(privateFocusInterval)
		a.tickPrivateInbox(now)
		if !in.busy || in.requestID != 0 {
			t.Fatalf("low-frequency slot did not run Conversations(0): busy=%t reqID=%d", in.busy, in.requestID)
		}
		receivePrivateRequest(t, backend.contacts)
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{snap2000}},
			netease.PrivateHistoryPage{}, nil, now)
		if in.pending == nil || in.pending.id != user.UserID {
			t.Fatalf("unchanged C@2000 ahead of chatTail@1000 did not re-queue History: %+v", in.pending)
		}

		// Follow-up History in the same slot receives @2000 and converges.
		req2 := *in.pending
		a.tickPrivateInbox(now)
		page2 := receivePrivateRequest(t, backend.reply)
		a.privateInboxAnswered(in, a.accountGen, in.gen, req2, netease.PrivateConversationPage{}, page2, nil, now)
		if in.pending != nil || len(chat.messages) != 2 || chat.messages[1].ID != 2 || chat.unread != 1 || a.privateHasPendingTarget(user.UserID) {
			t.Fatalf("low-frequency recovery did not merge @2000 cleanly: pending=%+v msgs=%d unread=%d",
				in.pending, len(chat.messages), chat.unread)
		}

		// Subsequent unchanged snapshot @2000 after chatTail caught up schedules no extra History.
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{snap2000}},
			netease.PrivateHistoryPage{}, nil, now.Add(privateFocusInterval))
		if in.pending != nil {
			t.Fatal("unchanged snapshot queued redundant History after convergence")
		}
	})

	// A transient Conversations(0) rollback below an unfulfilled snapshot time must not trigger
	// extra History, and when C returns @400 on the next low-frequency slot it still recovers B@400.
	t.Run("rollback-then-recover", func(t *testing.T) {
		a, fake := privateTestApp(t)
		user := a.dm.contacts[0].User
		a.dm.selected = user.UserID
		msgA := netease.PrivateMessage{ID: 1, Time: 300, From: user, Raw: `{"msg":"消息A"}`}
		msgB := netease.PrivateMessage{ID: 2, Time: 400, From: user, Raw: `{"msg":"消息B"}`}
		stalePage := netease.PrivateHistoryPage{Items: []netease.PrivateMessage{msgA}, Before: msgA.Time, Authoritative: true}
		freshPage := netease.PrivateHistoryPage{Items: []netease.PrivateMessage{msgA, msgB}, Before: msgA.Time, Authoritative: true}
		backend := &eventualPrivateHistory{
			fakePrivateMessages: fake,
			reply:               make(chan netease.PrivateHistoryPage, 2),
			contacts:            make(chan int, 2),
			pages:               []netease.PrivateHistoryPage{stalePage, freshPage},
		}
		a.dm.client = backend
		startTestPrivateInbox(a)
		defer a.stopPrivateInbox()
		chat, in := a.dm.chats[user.UserID], a.dm.inbox
		chat.loaded = true
		chat.messages = []netease.PrivateMessage{msgA}
		now := time.Unix(100, 0)
		slowAt := now.Add(privateFocusInterval)
		in.syncAt = slowAt

		snap300 := netease.PrivateConversation{User: user, Time: 300, Summary: "消息A", Unread: 0, UnreadKnown: true}
		snap400 := netease.PrivateConversation{User: user, Time: 400, Summary: "消息B", Unread: 1, UnreadKnown: true}
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{snap300}},
			netease.PrivateHistoryPage{}, nil, now)
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{snap400}},
			netease.PrivateHistoryPage{}, nil, now)
		if in.pending == nil {
			t.Fatal("advanced snapshot did not queue @400 History")
		}
		req1 := *in.pending
		a.tickPrivateInbox(now)
		page1 := receivePrivateRequest(t, backend.reply)
		a.privateInboxAnswered(in, a.accountGen, in.gen, req1, netease.PrivateConversationPage{}, page1, nil, now)

		// Transient Conversations(0) rollback to 300 must not trigger an extra History request.
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{snap300}},
			netease.PrivateHistoryPage{}, nil, now)
		if in.pending != nil {
			t.Fatalf("transient C rollback queued extra History: %+v", in.pending)
		}
		a.tickPrivateInbox(now)
		if in.busy {
			t.Fatal("transient C rollback dispatched a request before slowAt")
		}

		// At the next low-frequency slot, C recovers 400 and History fetches B@400.
		now = slowAt
		a.tickPrivateInbox(now)
		receivePrivateRequest(t, backend.contacts)
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{snap400}},
			netease.PrivateHistoryPage{}, nil, now)
		if in.pending == nil {
			t.Fatal("C recovery to 400 did not re-queue History")
		}
		req2 := *in.pending
		a.tickPrivateInbox(now)
		page2 := receivePrivateRequest(t, backend.reply)
		a.privateInboxAnswered(in, a.accountGen, in.gen, req2, netease.PrivateConversationPage{}, page2, nil, now)
		if in.pending != nil || len(chat.messages) != 2 || chat.messages[1].ID != 2 {
			t.Fatalf("low-frequency slot failed to recover B@400 after C rollback: pending=%+v msgs=%d", in.pending, len(chat.messages))
		}
	})

	// Regression (P1-1): a loaded empty conversation whose first History returns a lagging empty page
	// must still re-query History on the next low-frequency Conversations(0) poll when C remains @400.
	t.Run("empty-chat-lagging-empty-page-recovers-on-next-poll", func(t *testing.T) {
		a, fake := privateTestApp(t)
		user := a.dm.contacts[0].User
		a.dm.selected = user.UserID
		firstMsg := netease.PrivateMessage{ID: 1, Time: 400, From: user, Raw: `{"msg":"首条来信B"}`}
		emptyPage := netease.PrivateHistoryPage{Items: []netease.PrivateMessage{}, Authoritative: true}
		freshPage := netease.PrivateHistoryPage{Items: []netease.PrivateMessage{firstMsg}, Before: firstMsg.Time, Authoritative: true}
		backend := &eventualPrivateHistory{
			fakePrivateMessages: fake,
			reply:               make(chan netease.PrivateHistoryPage, 2),
			contacts:            make(chan int, 2),
			pages:               []netease.PrivateHistoryPage{emptyPage, freshPage},
		}
		a.dm.client = backend
		startTestPrivateInbox(a)
		defer a.stopPrivateInbox()
		chat, in := a.dm.chats[user.UserID], a.dm.inbox
		chat.loaded, chat.loading, chat.messages = true, false, nil
		now := time.Unix(100, 0)
		in.syncAt = now.Add(privateFocusInterval)

		snap400 := netease.PrivateConversation{User: user, Time: 400, Summary: "首条来信B", Unread: 1, UnreadKnown: true}
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{snap400}},
			netease.PrivateHistoryPage{}, nil, now)
		if in.pending == nil || in.pending.id != user.UserID {
			t.Fatalf("initial C@400 on loaded empty chat did not queue History: %+v", in.pending)
		}
		req1 := *in.pending
		a.tickPrivateInbox(now)
		page1 := receivePrivateRequest(t, backend.reply)
		a.privateInboxAnswered(in, a.accountGen, in.gen, req1, netease.PrivateConversationPage{}, page1, nil, now)
		if len(chat.messages) != 0 || in.pending != nil {
			t.Fatalf("unexpected state after lagging empty page: msgs=%d pending=%+v", len(chat.messages), in.pending)
		}

		// Next 30s slot: C still reports @400; empty chat (chatTail=0 < 400) must re-queue History and recover B@400.
		now = now.Add(privateFocusInterval)
		a.tickPrivateInbox(now)
		receivePrivateRequest(t, backend.contacts)
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{snap400}},
			netease.PrivateHistoryPage{}, nil, now)
		if in.pending == nil || in.pending.id != user.UserID {
			t.Fatalf("unchanged C@400 on loaded empty chat did not re-queue History: %+v", in.pending)
		}
		req2 := *in.pending
		a.tickPrivateInbox(now)
		page2 := receivePrivateRequest(t, backend.reply)
		a.privateInboxAnswered(in, a.accountGen, in.gen, req2, netease.PrivateConversationPage{}, page2, nil, now)
		if len(chat.messages) != 1 || chat.messages[0].ID != firstMsg.ID || in.pending != nil {
			t.Fatalf("empty chat did not recover B@400 on second poll: msgs=%d pending=%+v", len(chat.messages), in.pending)
		}

		// Once caught up (chatTail=400), another unchanged C@400 must not re-queue History.
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{snap400}},
			netease.PrivateHistoryPage{}, nil, now.Add(privateFocusInterval))
		if in.pending != nil {
			t.Fatal("caught-up chat queued redundant History on unchanged C@400")
		}
	})
}

// While History lags behind Conversations(0), unseen older rows and same-ms self rows merge without
// pruning or unlocking Read early, and an older in-flight History cannot consume a newer pending snapshot.
func TestPrivateInboxSameMsTargetCoverage(t *testing.T) {
	t.Run("older-unseen-and-self-do-not-unlock-read-before-snapshot-time", func(t *testing.T) {
		a, fake := privateTestApp(t)
		a.settings.Motion = "off"
		user := a.dm.contacts[0].User
		msgA := netease.PrivateMessage{ID: 10, Time: 300, From: user, Raw: `{"msg":"已知A"}`}
		msgOld := netease.PrivateMessage{ID: 5, Time: 200, From: user, Raw: `{"msg":"未知旧消息O"}`}
		msgSelf := netease.PrivateMessage{ID: 11, Time: 300, From: netease.MessageUser{UserID: a.profile.UserID}, Raw: `{"msg":"同毫秒自己发"}`}
		msgB := netease.PrivateMessage{ID: 12, Time: 400, From: user, Raw: `{"msg":"真正B"}`}

		backend := &eventualPrivateHistory{
			fakePrivateMessages: fake,
			reply:               make(chan netease.PrivateHistoryPage, 4),
			contacts:            make(chan int, 4),
			reads:               make(chan int64, 4),
			pages: []netease.PrivateHistoryPage{
				// First H has unseen older O@200 + known A@300, still lagging behind C@400.
				{Items: []netease.PrivateMessage{msgOld, msgA}, Before: 200, Authoritative: true},
				// Second H adds an unrelated self@300, still lagging behind C@400.
				{Items: []netease.PrivateMessage{msgOld, msgA, msgSelf}, Before: 200, Authoritative: true},
				// Third H finally contains peer B@400.
				{Items: []netease.PrivateMessage{msgOld, msgA, msgSelf, msgB}, Before: 200, Authoritative: true},
			},
		}
		a.dm.client = backend
		a.dm.selected = user.UserID
		chat := a.dm.chats[user.UserID]
		chat.loaded = true
		chat.messages = []netease.PrivateMessage{msgA}
		chat.readAt, chat.serverUnreadAt = 300, 300
		a.dm.open = true
		a.dm.loaded = true
		a.dm.ctx, a.dm.cancel = context.WithCancel(context.Background())
		a.startPrivateInbox()
		defer a.closePrivateMessages()
		in, now := a.dm.inbox, time.Unix(200, 0)
		in.syncAt = now.Add(privateFocusInterval)

		// Render at bottom so Read is armed whenever privateChatReadReady becomes true.
		tt := ui.NewTester(a.view, 1180, 780)
		tt.Frame()
		chat.history.ScrollToEnd()
		tt.Frame()
		if chat.read.attempt != nil {
			a.privateReadAnswered(a.accountGen, user.UserID, *chat.read.attempt, nil)
		}
		for len(backend.reads) > 0 {
			<-backend.reads
		}

		// Baseline C@300 -> C@400 unread=1 queues History.
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{{User: user, Time: 300, Summary: "已知A", Unread: 0, UnreadKnown: true}}},
			netease.PrivateHistoryPage{}, nil, now)
		snapUnread := netease.PrivateConversation{User: user, Time: 400, Summary: "真正B", Unread: 1, UnreadKnown: true}
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{snapUnread}},
			netease.PrivateHistoryPage{}, nil, now)

		// Step 1: H returns unseen older O@200 + A@300 (no B@400). O@200 must merge, and Read MUST stay blocked.
		req1 := *in.pending
		a.tickPrivateInbox(now)
		page1 := receivePrivateRequest(t, backend.reply)
		a.privateInboxAnswered(in, a.accountGen, in.gen, req1, netease.PrivateConversationPage{}, page1, nil, now)
		if len(chat.messages) != 2 || chat.messages[0].ID != msgOld.ID || !a.privateHasPendingTarget(user.UserID) {
			t.Fatalf("older O@200 did not merge cleanly or unblocked Read early: msgs=%d blocked=%t", len(chat.messages), a.privateHasPendingTarget(user.UserID))
		}
		tt.Frame()
		select {
		case <-backend.reads:
			t.Fatal("O@200 unlocked whole-conversation Read before B@400 arrived")
		default:
		}

		// Step 2: Next poll C@400 -> H adds self@300 (still no B@400). Must merge self@300 and keep Read blocked.
		now = now.Add(privateFocusInterval)
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{snapUnread}},
			netease.PrivateHistoryPage{}, nil, now)
		req2 := *in.pending
		a.tickPrivateInbox(now)
		page2 := receivePrivateRequest(t, backend.reply)
		a.privateInboxAnswered(in, a.accountGen, in.gen, req2, netease.PrivateConversationPage{}, page2, nil, now)
		if len(chat.messages) != 3 || !a.privateHasPendingTarget(user.UserID) {
			t.Fatalf("self@300 did not merge or unblocked Read early: msgs=%d blocked=%t", len(chat.messages), a.privateHasPendingTarget(user.UserID))
		}
		tt.Frame()
		select {
		case <-backend.reads:
			t.Fatal("self@300 unlocked Read before peer B@400 arrived")
		default:
		}

		// Step 3: Next poll C@400 -> H finally returns peer B@400 -> merges B and unlocks Read.
		now = now.Add(privateFocusInterval)
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{snapUnread}},
			netease.PrivateHistoryPage{}, nil, now)
		req3 := *in.pending
		a.tickPrivateInbox(now)
		page3 := receivePrivateRequest(t, backend.reply)
		a.privateInboxAnswered(in, a.accountGen, in.gen, req3, netease.PrivateConversationPage{}, page3, nil, now)
		if in.pending != nil || len(chat.messages) != 4 || chat.messages[3].ID != msgB.ID || a.privateHasPendingTarget(user.UserID) {
			t.Fatalf("peer B@400 did not complete catch-up: pending=%+v msgs=%d", in.pending, len(chat.messages))
		}
		tt.Frame()
		tt.Advance(time.Millisecond)
		tt.Frame()
		if got := receivePrivateRequest(t, backend.reads); got != user.UserID {
			t.Fatalf("Read did not unlock after B@400 arrived: got=%d", got)
		}
	})

	t.Run("older-in-flight-history-cannot-consume-newer-snapshot", func(t *testing.T) {
		a, fake := privateTestApp(t)
		user := a.dm.contacts[0].User
		msgA := netease.PrivateMessage{ID: 10, Time: 300, From: user, Raw: `{"msg":"已知A"}`}
		msgB1 := netease.PrivateMessage{ID: 11, Time: 400, From: user, Raw: `{"msg":"新消息B1"}`}
		msgB2 := netease.PrivateMessage{ID: 12, Time: 500, From: user, Raw: `{"msg":"新消息B2"}`}

		backend := &eventualPrivateHistory{
			fakePrivateMessages: fake,
			reply:               make(chan netease.PrivateHistoryPage, 4),
			pages: []netease.PrivateHistoryPage{
				{Items: []netease.PrivateMessage{msgA, msgB1}, Before: 300, Authoritative: true},
				{Items: []netease.PrivateMessage{msgA, msgB1, msgB2}, Before: 300, Authoritative: true},
			},
		}
		a.dm.client = backend
		a.dm.selected = user.UserID
		startTestPrivateInbox(a)
		defer a.stopPrivateInbox()
		chat, in := a.dm.chats[user.UserID], a.dm.inbox
		chat.loaded = true
		chat.messages = []netease.PrivateMessage{msgA}
		now := time.Unix(200, 0)

		// C@400 dispatches H #1.
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{{User: user, Time: 400, Summary: "新消息B1", Unread: 1, UnreadKnown: true}}},
			netease.PrivateHistoryPage{}, nil, now)
		req1 := *in.pending
		a.tickPrivateInbox(now)
		page1 := receivePrivateRequest(t, backend.reply)
		gen1 := in.gen

		// While H #1 is in flight, C advances to @500 -> re-queues in.pending.
		a.privateContactsAnswered(a.accountGen, a.dm.epoch, 0, netease.PrivateConversationPage{
			Items: []netease.PrivateConversation{{User: user, Time: 500, Summary: "新消息B2", Unread: 2, UnreadKnown: true}},
		}, nil)
		if in.pending == nil {
			t.Fatal("newer snapshot while H was in flight did not queue pending History")
		}

		// H #1 returns B1@400 (< contactAt 500): merges B1 without clearing in.pending!
		a.privateInboxAnswered(in, a.accountGen, gen1, req1, netease.PrivateConversationPage{}, page1, nil, now)
		if len(chat.messages) != 2 || in.pending == nil {
			t.Fatalf("older H@400 consumed newer @500 pending request: msgs=%d pending=%+v", len(chat.messages), in.pending)
		}

		// Follow-up H #2 returns B2@500 -> merges B2 and clears in.pending.
		req2 := *in.pending
		a.tickPrivateInbox(now)
		page2 := receivePrivateRequest(t, backend.reply)
		a.privateInboxAnswered(in, a.accountGen, in.gen, req2, netease.PrivateConversationPage{}, page2, nil, now)
		if in.pending != nil || len(chat.messages) != 3 || chat.messages[2].ID != msgB2.ID {
			t.Fatalf("follow-up H@500 did not settle B2@500: pending=%+v msgs=%d", in.pending, len(chat.messages))
		}
	})
}

// Non-selected conversations update contact/unread metadata only and never scan History or leak timestamps across chats.
func TestPrivateInboxNonSelectedIsolation(t *testing.T) {
	a, _ := privateTestApp(t)
	first, other := a.dm.contacts[0], a.dm.contacts[1]
	a.dm.selected = first.User.UserID
	a.dm.chats[first.User.UserID].messages = []netease.PrivateMessage{{ID: 1, Time: 1000, From: first.User, Raw: `{"msg":"基线"}`}}
	startTestPrivateInbox(a)
	defer a.stopPrivateInbox()
	in := a.dm.inbox
	now := time.Unix(100, 0)
	first.Time, other.Time = 1000, 1000
	a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
		netease.PrivateConversationPage{Items: []netease.PrivateConversation{first, other}},
		netease.PrivateHistoryPage{}, nil, now)
	other.Time, other.Summary, other.Unread, other.UnreadKnown = 9000, "非当前来信", 2, true
	a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
		netease.PrivateConversationPage{Items: []netease.PrivateConversation{other, first}},
		netease.PrivateHistoryPage{}, nil, now.Add(privateFocusInterval))
	if in.pending != nil {
		t.Fatalf("non-selected conversation change queued background History: %+v", in.pending)
	}
	if a.dm.chats[other.User.UserID].unread != 2 || a.dm.chats[first.User.UserID].unread != 0 || a.dm.chats[first.User.UserID].serverUnreadAt != 0 {
		t.Fatal("non-selected conversation leaked unread or timestamp into selected chat")
	}
}

// Refreshing an older contact must not move it ahead of newer messages; equal timestamps keep order and stale pages cannot roll time back.
func TestPrivateContactsStableTimeOrder(t *testing.T) {
	a, _ := privateTestApp(t)
	first, second := a.dm.contacts[0], a.dm.contacts[1]
	first.Time, second.Time = 2000, 1000
	a.dm.contacts = []netease.PrivateConversation{first, second}
	a.mergePrivateContacts([]netease.PrivateConversation{second}, false)
	if a.dm.contacts[0].User.UserID != first.User.UserID {
		t.Fatal("refresh order replaced message-time order")
	}
	second.Time = 2000
	a.mergePrivateContacts([]netease.PrivateConversation{second, first}, false)
	if a.dm.contacts[0].User.UserID != first.User.UserID {
		t.Fatal("equal timestamps reordered existing contacts")
	}
	second.Time = 3000
	a.mergePrivateContacts([]netease.PrivateConversation{second}, false)
	second.Time = 500
	a.mergePrivateContacts([]netease.PrivateConversation{second}, false)
	if a.dm.contacts[0].User.UserID != second.User.UserID || a.dm.contacts[0].Time != 3000 {
		t.Fatal("new messages did not move the contact, or a stale page rolled it back")
	}
}
