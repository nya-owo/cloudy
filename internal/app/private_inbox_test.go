package app

import (
	"context"
	"errors"
	"fmt"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

// Manual refresh supersedes an older automatic History request for the same chat,
// cancelled replies are ignored, and an older History reply cannot consume a newer snapshot target.
func TestPrivateInboxConcurrencyAndYield(t *testing.T) {
	a, backend := privateTestApp(t)
	requests := &privateInboxBackend{fakePrivateMessages: backend, requested: make(chan privateInboxRequest, 2)}
	a.dm.client = requests
	a.dm.selected = 90002
	startTestPrivateInbox(a)
	defer a.stopPrivateInbox()
	in := a.dm.inbox
	now := time.Unix(100, 0)
	in.primed = true
	a.dm.chats[90002].contactAt = 2000
	in.queuePending(90002, -1)
	firstTarget := *in.pending
	a.tickPrivateInbox(now)
	if call := receivePrivateRequest(t, requests.requested); call.id != 90002 || call.before != 0 {
		t.Fatal("pending target did not dispatch History")
	}
	firstGen := in.gen
	// A newer Conversations snapshot arrives while History for 2000 is still in flight.
	a.privateContactsAnswered(a.accountGen, a.dm.epoch, 0, netease.PrivateConversationPage{
		Items: []netease.PrivateConversation{{User: a.dm.chats[90002].user, Time: 3000, Summary: "msg-2", Unread: 1, UnreadKnown: true}},
	}, nil)
	if !in.busy || in.gen != firstGen || in.pending == nil || a.dm.chats[90002].contactAt != 3000 {
		t.Fatal("a newer snapshot cancelled useful in-flight HTTP or failed to queue pending History")
	}
	// The older History response merges @2000 without consuming the newer @3000 pending check.
	msg2000 := netease.PrivateMessage{ID: 1, Time: 2000, From: a.dm.chats[90002].user, Raw: `{"msg":"msg-1"}`}
	a.privateInboxAnswered(in, a.accountGen, firstGen, firstTarget, netease.PrivateConversationPage{},
		netease.PrivateHistoryPage{Items: []netease.PrivateMessage{msg2000}, Before: 2000, Authoritative: true}, nil, now)
	if in.busy || len(a.dm.chats[90002].messages) != 1 || in.pending == nil {
		t.Fatalf("older History consumed newer pending check: pending=%+v msgs=%d", in.pending, len(a.dm.chats[90002].messages))
	}
	// Starting an automatic History for @3000 and then clicking manual refresh cancels the auto request cleanly.
	req2 := *in.pending
	a.tickPrivateInbox(now)
	receivePrivateRequest(t, requests.requested)
	autoGen := in.gen
	oldCtx, cancel := context.WithCancel(in.ctx)
	in.requestCancel = cancel
	a.loadPrivateHistory(false)
	if oldCtx.Err() == nil || in.busy || in.gen == autoGen {
		t.Fatal("manual refresh did not yield the in-flight automatic History")
	}
	// Delivering the cancelled auto reply must have zero effect.
	msg3000 := netease.PrivateMessage{ID: 2, Time: 3000, From: a.dm.chats[90002].user, Raw: `{"msg":"msg-2"}`}
	if a.privateInboxAnswered(in, a.accountGen, autoGen, req2, netease.PrivateConversationPage{},
		netease.PrivateHistoryPage{Items: []netease.PrivateMessage{msg2000, msg3000}}, nil, now) || len(a.dm.chats[90002].messages) != 1 {
		t.Fatal("cancelled automatic reply mutated chat state")
	}
}

// Unread stays inside the DM panel, and opening an unread chat clears only that conversation.
func TestPrivateUnreadNavigation(t *testing.T) {
	a, _ := privateTestApp(t)
	a.settings.Motion = "off"
	a.dm.chats[90002].unread, a.dm.chats[90003].unread = 2, 108
	tt := ui.NewTester(a.view, 1180, 780)
	tt.Frame()
	if _, shown := tt.Find("未读私信"); shown {
		t.Fatal("unread leaked onto the account entry")
	}
	if err := tt.Click("演示账号"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, shown := tt.Find("私信未读提醒"); shown {
		t.Fatal("unread leaked onto the account menu")
	}
	if err := tt.Click("私信"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, shown := tt.Find("会话未读私信"); !shown {
		t.Fatal("conversation unread badge is missing")
	}
	for index, user := range a.dm.contacts {
		if err := tt.Click("会话：" + user.User.Nickname); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		tt.Frame()
		if a.dm.selected != user.User.UserID || a.dm.chats[user.User.UserID].unread != 0 || index == 0 && a.dm.chats[90003].unread != 108 {
			t.Fatal("badge interfered with navigation or unread clearing")
		}
	}
	if a.dm.chats[90002].unread != 0 || a.dm.chats[90003].unread != 0 {
		t.Fatal("reading both conversations retained account unread")
	}
}

// Export the actual account/menu/conversation badge layout for visual review in both themes, without pixel assertions about appearance.
func TestPrivateUnreadFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip("NM_FRAMES is not set")
	}
	for _, dark := range []bool{false, true} {
		a, _ := privateTestApp(t)
		a.settings.Motion = "off"
		a.dm.chats[90002].unread, a.dm.chats[90003].unread = 2, 108
		tt := ui.NewTester(a.view, 1180, 780)
		tt.SetDark(dark)
		tt.Frame()
		save := func(name string) {
			f, err := os.Create(filepath.Join(dir, fmt.Sprintf("badge-%t-%s.png", dark, name)))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
		if err := tt.Click("演示账号"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		tt.Advance(time.Second)
		save("menu")
		if err := tt.Click("私信"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		tt.Advance(time.Second)
		save("conversations")
	}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Row(c).Grow(1).Padding(20).Gap(20).AlignItems(ui.Center).Background(ui.RGB(35, 35, 37)).Children(func() {
			for _, count := range []int{1, 2, 8, 10, 99, 108} {
				privateUnreadBadge(c, count, fmt.Sprint(count))
			}
		})
	}, 300, 60)
	tt.SetScale(4)
	tt.Frame()
	f, err := os.Create(filepath.Join(dir, "badge-details-4x.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

func startTestPrivateInbox(a *app) {
	a.dm.open = true
	a.dm.ctx, a.dm.cancel = context.WithCancel(context.Background())
	a.startPrivateInbox()
}

type privateInboxRequest struct {
	id, before int64
}

type privateInboxBackend struct {
	*fakePrivateMessages
	requested chan privateInboxRequest
}

func (b *privateInboxBackend) History(_ context.Context, id, before int64) (netease.PrivateHistoryPage, error) {
	b.requested <- privateInboxRequest{id, before}
	return netease.PrivateHistoryPage{}, nil
}

type privateInboxContactsBackend struct {
	*privateInboxBackend
	contacts chan context.Context
}

func (b *privateInboxContactsBackend) Conversations(ctx context.Context, _ int) (netease.PrivateConversationPage, error) {
	b.contacts <- ctx
	return netease.PrivateConversationPage{}, nil
}

func receivePrivateRequest[T any](t *testing.T, calls <-chan T) T {
	t.Helper()
	select {
	case call := <-calls:
		return call
	case <-time.After(3 * time.Second):
		t.Fatal("expected an offline inbox request")
		var zero T
		return zero
	}
}

// A signed-in main view leaves the inbox stopped until the DM entry opens it, and Escape cancels panel work.
func TestPrivateInboxPanelLifetime(t *testing.T) {
	a, fake := privateTestApp(t)
	backend := &privateInboxContactsBackend{
		privateInboxBackend: &privateInboxBackend{fakePrivateMessages: fake, requested: make(chan privateInboxRequest, 2)},
		contacts:            make(chan context.Context, 2),
	}
	a.dm.client = backend
	a.dm.selected = 90002
	tt := ui.NewTester(a.view, 1180, 780)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	a.startPrivateInbox()
	if a.dm.inbox != nil || len(backend.contacts) != 0 {
		t.Fatal("signed-in main view started the inbox")
	}
	if err := chooseItem(tt, "演示账号", "私信"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.closePrivateMessages)
	in := a.dm.inbox
	contacts := receivePrivateRequest(t, backend.contacts)
	if call := receivePrivateRequest(t, backend.requested); call.id != a.dm.selected || call.before != 0 {
		t.Fatal("opening did not load the selected conversation")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	a.tickPrivateInbox(in.syncAt)
	if a.dm.open || a.dm.inbox != nil || in.ctx.Err() == nil || contacts.Err() == nil || len(backend.contacts) != 0 {
		t.Fatal("closing left panel requests or the inbox running")
	}
}

// Focused (30s), unfocused visible (60s), minimized/hidden (0), Restore+Focus 5s deduplication,
// offset>0 baseline preservation, 429 cooldown, and foreground-only Toast rules.
func TestPrivateInboxScheduleAndWindowLifecycle(t *testing.T) {
	origQuery := privateWindowQuery
	defer func() { privateWindowQuery = origQuery }()
	ws := privateWindowState{visible: true, focused: true, hasWindow: true}
	privateWindowQuery = func(*app) privateWindowState { return ws }

	a, fake := privateTestApp(t)
	backend := &privateInboxContactsBackend{
		privateInboxBackend: &privateInboxBackend{fakePrivateMessages: fake, requested: make(chan privateInboxRequest, 4)},
		contacts:            make(chan context.Context, 8),
	}
	a.dm.client = backend
	a.dm.selected = 90002
	a.openPrivateMessages(nil)
	t.Cleanup(a.closePrivateMessages)
	// Opening already started Contacts(0); an immediate showMain()->OnFocus must NOT double-fetch.
	receivePrivateRequest(t, backend.contacts)
	receivePrivateRequest(t, backend.requested)
	in := a.dm.inbox
	a.privateWindowFocused(time.Now())
	if len(backend.contacts) != 0 || in.busy {
		t.Fatal("opening Focus triggered a duplicate Conversations(0) request")
	}
	// Complete initial load.
	first, other := a.dm.contacts[0], a.dm.contacts[1]
	first.Time, first.Unread, first.UnreadKnown = 1000, 0, true
	other.Time, other.Unread, other.UnreadKnown = 1000, 2, true
	a.privateContactsAnswered(a.accountGen, a.dm.epoch, 0, netease.PrivateConversationPage{Items: []netease.PrivateConversation{first, other}, More: true, Next: 2}, nil)
	a.privateHistoryAnswered(a.accountGen, a.dm.epoch, 90002, a.dm.chats[90002].gen, 0,
		netease.PrivateHistoryPage{Items: []netease.PrivateMessage{{ID: 1, Time: 1000, From: first.User}}}, nil)
	if a.toast != "" {
		t.Fatal("cold open fired an incoming Toast")
	}
	// Manual pagination (offset > 0) merges contacts without overwriting the offset=0 contactAt.
	third := netease.PrivateConversation{User: netease.MessageUser{UserID: 90004, Nickname: "翻页联系人"}, Time: 500}
	a.privateContactsAnswered(a.accountGen, a.dm.epoch, 2, netease.PrivateConversationPage{Items: []netease.PrivateConversation{third}}, nil)
	if a.dm.chats[90004] != nil || a.dm.chats[90002].contactAt != 1000 {
		t.Fatal("offset>0 pagination overwrote the first-page snapshot contactAt")
	}

	// 1) Focused interval is 30s.
	now := time.Unix(1000, 0)
	in.syncAt = now.Add(privateFocusInterval)
	in.lastContactsAt = now
	a.tickPrivateInbox(now.Add(privateFocusInterval - time.Millisecond))
	if in.busy {
		t.Fatal("focused poll fired before 30s")
	}
	now = now.Add(privateFocusInterval)
	a.tickPrivateInbox(now)
	receivePrivateRequest(t, backend.contacts)
	// Background snapshot advances non-selected peer unread while focused: foreground Toast fires!
	other.Time, other.Unread = 2000, 3
	a.dm.presentation.chat = true
	a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
		netease.PrivateConversationPage{Items: []netease.PrivateConversation{other, first}},
		netease.PrivateHistoryPage{}, nil, now)
	if a.toast != "收到新私信" || a.toastAs != "private-inbox" {
		t.Fatalf("foreground non-selected unread increase did not show Toast: %q (%q)", a.toast, a.toastAs)
	}
	if in.pending != nil {
		t.Fatal("non-selected peer change queued History")
	}

	// 2) Unfocused visible (second monitor) slows to 60s and suppresses Toast.
	a.toast, a.toastAs = "", ""
	ws.focused = false
	a.privateWindowBlur(now)
	if due, ok := a.privateInboxDue(in); !ok || due.Sub(now) != privateBlurInterval {
		t.Fatalf("unfocused visible interval = %v (ok=%t), want %v", due.Sub(now), ok, privateBlurInterval)
	}
	now = now.Add(privateBlurInterval)
	a.tickPrivateInbox(now)
	receivePrivateRequest(t, backend.contacts)
	other.Time, other.Unread = 3000, 4
	a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
		netease.PrivateConversationPage{Items: []netease.PrivateConversation{other, first}},
		netease.PrivateHistoryPage{}, nil, now)
	if a.toast != "" {
		t.Fatal("unfocused window fired an incoming Toast")
	}

	// 3) Minimized cancels in-flight auto request and schedules 0 new requests.
	now = now.Add(privateBlurInterval)
	a.tickPrivateInbox(now)
	inFlightCtx := receivePrivateRequest(t, backend.contacts)
	ws.minimized = true
	a.privateWindowPaused()
	if inFlightCtx.Err() == nil || in.busy {
		t.Fatal("minimizing did not cancel in-flight automatic request")
	}
	if _, ok := a.privateInboxDue(in); ok {
		t.Fatal("minimized window still scheduled automatic sync")
	}
	a.tickPrivateInbox(now.Add(10 * time.Minute))
	if in.busy || len(backend.contacts) != 0 {
		t.Fatal("minimized window started new automatic requests")
	}

	// 4) OnRestore + OnFocus back-to-back merge into a single Conversations(0) check; repeated focus within 5s is throttled.
	ws.minimized, ws.focused = false, true
	wokeAt := now.Add(10 * time.Second)
	a.privateWindowRestored(wokeAt)
	a.privateWindowFocused(wokeAt.Add(100 * time.Millisecond))
	receivePrivateRequest(t, backend.contacts)
	if len(backend.contacts) != 0 {
		t.Fatal("OnRestore + OnFocus sent duplicate Conversations(0) requests")
	}
	a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
		netease.PrivateConversationPage{Items: []netease.PrivateConversation{other, first}},
		netease.PrivateHistoryPage{}, nil, wokeAt.Add(200*time.Millisecond))
	a.privateWindowFocused(wokeAt.Add(privateWakeThrottle - time.Millisecond))
	if in.busy || len(backend.contacts) != 0 {
		t.Fatal("OnFocus bypassed the 5s wake throttle")
	}

	// 5) HTTP 429 holds both periodic ticks and OnFocus wake for a full 60s.
	failedAt := wokeAt.Add(privateWakeThrottle)
	a.privateWindowFocused(failedAt)
	receivePrivateRequest(t, backend.contacts)
	a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
		netease.PrivateConversationPage{}, netease.PrivateHistoryPage{}, &netease.PrivateMessageHTTPError{StatusCode: http.StatusTooManyRequests}, failedAt)
	a.privateWindowFocused(failedAt.Add(10 * time.Second))
	a.tickPrivateInbox(failedAt.Add(privateRateLimitBackoff - time.Millisecond))
	if in.busy || len(backend.contacts) != 0 {
		t.Fatal("429 cooldown was bypassed by Focus wake or periodic tick")
	}
	a.tickPrivateInbox(failedAt.Add(privateRateLimitBackoff))
	receivePrivateRequest(t, backend.contacts)
}

// Manual History and Contacts failures/429 delay automatic work, earlier 200 replies cannot erase
// a newer 429 backoff, and a manual History reply covering the target satisfies it without overlap.
func TestPrivateInboxManualOverlapAndBackoff(t *testing.T) {
	for _, tc := range []struct {
		name           string
		err            error
		backoff        time.Duration
		manualContacts bool
	}{
		{name: "history-success"},
		{name: "history-failed", err: errors.New("offline"), backoff: 4 * time.Second},
		{name: "history-rate-limited", err: &netease.PrivateMessageHTTPError{StatusCode: http.StatusTooManyRequests}, backoff: privateRateLimitBackoff},
		{name: "contacts-rate-limited", err: &netease.PrivateMessageHTTPError{StatusCode: http.StatusTooManyRequests}, backoff: privateRateLimitBackoff, manualContacts: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, fake := privateTestApp(t)
			backend := &privateInboxContactsBackend{
				privateInboxBackend: &privateInboxBackend{fakePrivateMessages: fake, requested: make(chan privateInboxRequest, 2)},
				contacts:            make(chan context.Context, 4),
			}
			a.dm.client, a.dm.selected = backend, 90002
			startTestPrivateInbox(a)
			t.Cleanup(a.closePrivateMessages)
			in, chat := a.dm.inbox, a.dm.chats[a.dm.selected]
			now := time.Now()
			in.syncAt = now.Add(time.Hour)
			if tc.manualContacts {
				a.loadPrivateContacts(false)
				receivePrivateRequest(t, backend.contacts)
				beforeReply := time.Now()
				a.privateContactsAnswered(a.accountGen, a.dm.epoch, 0, netease.PrivateConversationPage{}, tc.err)
				afterReply := time.Now()
				deadline := a.dm.retryAt
				if deadline.Before(beforeReply.Add(tc.backoff)) || deadline.After(afterReply.Add(tc.backoff)) {
					t.Fatal("manual Contacts 429 did not set automatic 60s backoff")
				}
				in.syncAt = beforeReply
				gen := in.gen
				a.tickPrivateInbox(deadline.Add(-time.Nanosecond))
				if in.gen != gen {
					t.Fatal("automatic poll bypassed manual Contacts 429 backoff")
				}
				return
			}

			rateLimited := tc.backoff == privateRateLimitBackoff
			if rateLimited {
				// Start an earlier automatic Contacts poll that arrives AFTER manual History reports 429.
				in.syncAt = now
				a.tickPrivateInbox(now)
				receivePrivateRequest(t, backend.contacts)
			}
			a.loadPrivateHistory(false)
			receivePrivateRequest(t, backend.requested)
			chat.contactAt = 200
			in.queuePending(a.dm.selected, -1)
			a.tickPrivateInbox(now)
			if in.busy && in.requestID == a.dm.selected {
				t.Fatal("automatic History overlapped manual History for the same chat")
			}
			beforeReply := time.Now()
			page := netease.PrivateHistoryPage{}
			if tc.err == nil {
				page.Items = []netease.PrivateMessage{{ID: 1, Time: 200, From: chat.user}}
			}
			a.privateHistoryAnswered(a.accountGen, a.dm.epoch, a.dm.selected, chat.gen, 0, page, tc.err)
			afterReply := time.Now()
			if tc.err == nil {
				if in.pending != nil {
					t.Fatal("manual History covering target did not consume the pending request")
				}
				return
			}
			deadline := a.dm.retryAt
			if deadline.Before(beforeReply.Add(tc.backoff)) || deadline.After(afterReply.Add(tc.backoff)) {
				t.Fatal("failed manual History did not set the expected automatic backoff")
			}
			if rateLimited {
				// An earlier in-flight Contacts 200 reply must NOT erase the newer 429 backoff.
				a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
					netease.PrivateConversationPage{}, netease.PrivateHistoryPage{}, nil, afterReply)
				if !a.dm.retryAt.Equal(deadline) {
					t.Fatal("earlier 200 reply erased newer manual 429 backoff")
				}
			}
			gen := in.gen
			a.tickPrivateInbox(deadline.Add(-time.Nanosecond))
			if in.gen != gen {
				t.Fatal("automatic work bypassed manual History backoff")
			}
			a.tickPrivateInbox(deadline)
			if !in.busy || in.requestID != a.dm.selected {
				t.Fatal("manual History failure lost the pending target")
			}
			if call := receivePrivateRequest(t, backend.requested); call.id != a.dm.selected || call.before != 0 {
				t.Fatal("retry requested the wrong history")
			}
		})
	}
}

// Background polls request a redraw only when a reply changes the displayed inbox;
// metadata-only Unread/UnreadKnown differences without visible change must not redraw.
func TestPrivateInboxRedrawOnChange(t *testing.T) {
	a, _ := privateTestApp(t)
	startTestPrivateInbox(a)
	defer a.stopPrivateInbox()
	in := a.dm.inbox
	now := time.Unix(100, 0)
	item := a.dm.contacts[0]
	contactPage := netease.PrivateConversationPage{Items: []netease.PrivateConversation{item}}
	contactReply := func(err error) bool {
		in.busy = true
		return a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{}, contactPage, netease.PrivateHistoryPage{}, err, now)
	}
	if contactReply(nil) || in.busy {
		t.Fatal("an identical contact reply redrew the window or left the inbox busy")
	}
	if contactReply(errors.New("network unavailable")) || in.busy || a.dm.failures != 1 {
		t.Fatal("a failed poll redrew or failed to record its backoff")
	}
	contactPage.Items[0].User.Nickname = "更新后的昵称"
	if !contactReply(nil) || a.dm.contacts[0] != contactPage.Items[0] {
		t.Fatal("changed contact details did not request a redraw")
	}

	chat := a.dm.chats[item.User.UserID]
	request := privateInboxChange{id: chat.user.UserID, after: item.Time}
	message := netease.PrivateMessage{ID: 1, Time: item.Time + 1, From: contactPage.Items[0].User, Raw: `{"msg":"合成新来信"}`}
	page := netease.PrivateHistoryPage{Items: []netease.PrivateMessage{message}}
	historyReply := func(gen int) bool {
		return a.privateInboxAnswered(in, a.accountGen, gen, request, netease.PrivateConversationPage{}, page, nil, now)
	}
	if historyReply(in.gen-1) || chat.unread != 0 || len(chat.messages) != 0 {
		t.Fatal("a stale reply changed the inbox")
	}
	if !historyReply(in.gen) || chat.unread != 1 || len(chat.messages) != 1 || a.dm.contacts[0].Summary != message.Text() {
		t.Fatal("a new message did not update history, unread and the contact preview")
	}
	chat.prepareHistoryRows()
	if historyReply(in.gen) || chat.unread != 1 || chat.historyRows == nil {
		t.Fatal("an identical history reply redrew or discarded the committed rows")
	}
	// A matching Contacts snapshot whose UnreadKnown/Unread metadata agrees with chat.unread must not redraw.
	meta := a.dm.contacts[0]
	meta.Unread, meta.UnreadKnown = 1, true
	contactPage = netease.PrivateConversationPage{Items: []netease.PrivateConversation{meta}}
	if contactReply(nil) {
		t.Fatal("metadata-only contact reply with unchanged badge/preview requested a redraw")
	}
	if historyReply(in.gen) {
		t.Fatal("History contact sync after UnreadKnown snapshot triggered a metadata-only redraw")
	}
}

// Two summaries for the same message timestamp must not alternate or redraw on idle polls,
// while History preview stays authoritative.
func TestPrivateInboxStableContactPreviews(t *testing.T) {
	a, _ := privateTestApp(t)
	first, second := a.dm.contacts[0], a.dm.contacts[1]
	a.openPrivateMessages(&first.User)
	a.dm.loaded = true
	defer a.stopPrivateInbox()
	in, now := a.dm.inbox, time.Unix(100, 0)
	reply := func(item netease.PrivateConversation) bool {
		in.busy = true
		page := netease.PrivateConversationPage{Items: []netease.PrivateConversation{item}}
		return a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{}, page, netease.PrivateHistoryPage{}, nil, now)
	}
	if reply(first) {
		t.Fatal("an identical snapshot triggered a redraw")
	}
	tt := ui.NewTester(a.view, 1180, 780)
	privateShowContents(tt)
	tt.Advance(time.Second)
	label := "会话：" + first.User.Nickname
	bounds, ok := tt.Find(label)
	if !ok {
		t.Fatal("the conversation row is missing")
	}
	alternate := first
	alternate.Summary = "服务端另一种摘要"
	if reply(alternate) || a.dm.contacts[0].Summary != first.Summary {
		t.Fatal("first same-time alternate summary flickered visible summary or redrew")
	}
	changed := false
	for range 3 {
		changed = reply(alternate) || changed
		tt.Frame()
		if current, _ := tt.Find(label); current != bounds || a.dm.contacts[0].User.UserID != first.User.UserID {
			t.Fatal("identical same-time snapshots reordered or rebuilt the conversation row")
		}
	}
	if changed || a.dm.contacts[0].Summary != first.Summary || in.pending != nil {
		t.Fatal("idle snapshots alternated the visible summary or queued new work")
	}
	alternate.User.Nickname = "新昵称"
	alternate.User.AvatarURL = "fixture-avatar.webp"
	a.art.add(alternate.User.AvatarURL, &artwork{failed: true, retryAt: time.Now().Add(time.Hour)})
	if !reply(alternate) || a.dm.contacts[0].User != alternate.User || a.dm.contacts[0].Summary != first.Summary {
		t.Fatal("same-time profile changes were not applied independently of the summary")
	}
	message := netease.PrivateMessage{
		ID: 1, Time: first.Time, From: alternate.User, Raw: `{"msg":"历史中的原文"}`,
	}
	history := netease.PrivateHistoryPage{Items: []netease.PrivateMessage{message}}
	a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{id: first.User.UserID, after: -1}, netease.PrivateConversationPage{}, history, nil, now)
	if a.dm.contacts[0].Summary != message.Text() || reply(alternate) || in.pending != nil {
		t.Fatal("a same-time snapshot overrode authoritative message history or queued redundant History")
	}
	second.Time = first.Time + 1
	second.Summary = "新来信"
	if !reply(second) || a.dm.contacts[0] != second {
		t.Fatal("a newer message did not move its conversation to the top")
	}
}

// Cold baseline does not alert; snapshot changes trigger only selected History; duplicate/older rows
// merge cleanly; account reset cancels the inbox and rejects stale replies.
func TestPrivateInboxLifecycleAndBaseline(t *testing.T) {
	a, backend := privateTestApp(t)
	requests := &privateInboxBackend{fakePrivateMessages: backend, requested: make(chan privateInboxRequest, 2)}
	a.dm.client = requests
	a.dm.selected = 90002
	startTestPrivateInbox(a)
	defer a.stopPrivateInbox()
	in := a.dm.inbox
	now := time.Unix(100, 0)
	answer := func(request privateInboxChange, contacts netease.PrivateConversationPage, history netease.PrivateHistoryPage) {
		in.gen++
		a.privateInboxAnswered(in, a.accountGen, in.gen, request, contacts, history, nil, now)
	}
	user := a.dm.contacts[0].User
	chat := a.dm.chats[90002]
	chat.loaded = false
	answer(privateInboxChange{}, netease.PrivateConversationPage{Items: []netease.PrivateConversation{{User: user, Time: 1000, Summary: "基线"}}}, netease.PrivateHistoryPage{})
	if a.dm.chats[90002].unread != 0 || a.dm.chats[90003].unread != 0 || a.toast != "" || in.pending != nil {
		t.Fatal("initial baseline alerted on existing messages or queued redundant History")
	}
	// Identical poll queues nothing.
	answer(privateInboxChange{}, netease.PrivateConversationPage{Items: []netease.PrivateConversation{{User: user, Time: 1000, Summary: "基线"}}}, netease.PrivateHistoryPage{})
	if in.pending != nil {
		t.Fatal("unchanged snapshot queued History")
	}
	// Selected chat advances -> queues History(90002, 0).
	answer(privateInboxChange{}, netease.PrivateConversationPage{Items: []netease.PrivateConversation{{User: user, Time: 2000, Summary: "合成来信一", Unread: 1, UnreadKnown: true}}}, netease.PrivateHistoryPage{})
	if in.pending == nil || in.pending.id != 90002 {
		t.Fatal("selected snapshot change did not queue History")
	}
	req1 := *in.pending
	a.tickPrivateInbox(now)
	if requested := receivePrivateRequest(t, requests.requested); requested.id != 90002 || requested.before != 0 {
		t.Fatal("dirty target did not request the latest authoritative history")
	}
	msg1 := netease.PrivateMessage{ID: 1, Time: 2000, From: chat.user, Raw: `{"msg":"合成来信一"}`}
	answer(req1, netease.PrivateConversationPage{}, netease.PrivateHistoryPage{Items: []netease.PrivateMessage{msg1}})
	if in.pending != nil || chat.unread != 1 || len(chat.messages) != 1 || a.toast != "" {
		t.Fatal("first selected History did not merge and consume target cleanly")
	}
	// Replaying the same page does not duplicate messages or unread.
	answer(privateInboxChange{id: 90002, after: -1}, netease.PrivateConversationPage{}, netease.PrivateHistoryPage{Items: []netease.PrivateMessage{msg1}})
	if chat.unread != 1 || len(chat.messages) != 1 {
		t.Fatal("replayed history duplicated messages or unread")
	}
	// Merging older + newer rows with after=-1 preserves all rows without guessing unread.
	old := msg1
	old.ID, old.Time = 3, 500
	newer := msg1
	newer.ID, newer.Time = 4, 2002
	answer(privateInboxChange{id: 90002, after: -1}, netease.PrivateConversationPage{}, netease.PrivateHistoryPage{Items: []netease.PrivateMessage{old, newer}})
	if chat.unread != 1 || len(chat.messages) != 3 {
		t.Fatal("history merge lost rows or invented unread")
	}
	account, gen := a.accountGen, in.gen
	a.resetPrivateMessages()
	if in.ctx.Err() == nil || a.dm.inbox != nil {
		t.Fatal("account reset did not cancel its inbox")
	}
	a.privateInboxAnswered(in, account, gen, req1, netease.PrivateConversationPage{}, netease.PrivateHistoryPage{Items: []netease.PrivateMessage{msg1}}, nil, now)
	if len(a.dm.chats) != 0 {
		t.Fatal("old account response repopulated private data")
	}
}

// Distinct loaded snapshots can disambiguate same-text sends transitively; the list must contain exactly two aliases, not extra server bubbles.
func TestPrivateOutgoingDistinctSnapshots(t *testing.T) {
	chat := &privateChat{loaded: true}
	first := chat.addOutgoing("相同正文", time.UnixMilli(1000), 90001)
	one := first.message
	one.ID = 100
	chat.messages = []netease.PrivateMessage{one}
	second := chat.addOutgoing("相同正文", time.UnixMilli(2000), 90001)
	two := second.message
	two.ID = 101
	chat.messages = append(chat.messages, two)
	if !chat.reconcileOutgoing(90001) || first.confirmed == nil || second.confirmed == nil || *first.confirmed == *second.confirmed {
		t.Fatal("distinct snapshot constraints did not disambiguate both sends")
	}
	chat.prepareHistoryRows()
	bubbles := 0
	for _, row := range chat.historyRows {
		if row.key.kind == privateHistoryMessage {
			t.Fatal("confirmed text rendered a second server bubble")
		}
		if row.key.kind == privateHistoryOutgoing {
			bubbles++
		}
	}
	if bubbles != 2 || len(chat.messages) != 2 || chat.reconcileOutgoing(90001) {
		t.Fatal("alias count, authoritative history or stable reconciliation changed")
	}
}

// Both sides enter once on first visibility, including new IDs inside a scrolled viewport.
func TestPrivateIncomingUI(t *testing.T) {
	a, _ := privateTestApp(t)
	a.settings.Motion = "full"
	a.openPrivateMessages(nil)
	defer a.stopPrivateInbox()
	a.selectPrivateChat(a.dm.contacts[0].User)
	chat := a.dm.chats[90002]
	chat.loading, a.dm.loading = false, false
	for i := range 60 {
		chat.messages = append(chat.messages, netease.PrivateMessage{ID: int64(i + 1), Time: 1000 + int64(i), From: chat.user, Raw: fmt.Sprintf(`{"msg":"合成历史 %02d"}`, i)})
	}
	tt := ui.NewTester(a.view, 1180, 780)
	tt.Frame()
	privateShowContents(tt)
	tt.Advance(time.Second)
	newMessage := netease.PrivateMessage{ID: 100, Time: 2000, From: chat.user, Raw: `{"msg":"合成自然来信"}`}
	in := a.dm.inbox
	a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{id: 90002, after: 1059}, netease.PrivateConversationPage{}, netease.PrivateHistoryPage{Items: []netease.PrivateMessage{newMessage}}, nil, time.Now())
	entrance := chat.arriving[privateMessageID(newMessage)]
	if entrance == nil || !entrance.at.IsZero() || chat.unread != 0 || a.toast != "" {
		t.Fatal("active reader was notified or incoming entrance was not prepared")
	}
	tt.Frame()
	if _, visible := tt.Find("合成自然来信"); !visible {
		t.Fatal("merged tail did not construct its new row on the next frame")
	}
	tt.Advance(80 * time.Millisecond)
	if entrance.at.IsZero() || entrance.done || !tt.FramePending() {
		t.Fatal("incoming entrance did not run on visible frames")
	}
	tt.Advance(time.Second)
	if !entrance.done || len(chat.arriving) != 0 {
		t.Fatal("incoming entrance did not settle")
	}
	privateReadMessage(tt, chat, 20)
	anchor, ok := tt.Find("合成历史 20")
	if !ok || chat.history.AtEnd() {
		t.Fatal("could not establish middle reading anchor")
	}
	// These are new IDs, but their server times place them inside the
	// current viewport, rather than after the cached tail.
	synced := netease.PrivateMessage{ID: 101, Time: 1021, From: netease.MessageUser{UserID: a.profile.UserID}, Raw: `{"msg":"合成跨端同步"}`}
	second := netease.PrivateMessage{ID: 102, Time: 1022, From: chat.user, Raw: `{"msg":"合成对方同步"}`}
	syncedPage := netease.PrivateHistoryPage{Items: []netease.PrivateMessage{synced, second}}
	a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{id: 90002, after: 2000}, netease.PrivateConversationPage{}, syncedPage, nil, time.Now())
	own := chat.arriving[privateMessageID(synced)]
	other := chat.arriving[privateMessageID(second)]
	if own == nil || other == nil || !own.at.IsZero() || chat.unread != 0 {
		t.Fatal("visible arrivals were missed, alerted or started before layout")
	}
	tt.Frame()
	for _, label := range []string{"合成跨端同步", "合成对方同步"} {
		if r, ok := tt.Find(label); !ok || r.H == 0 {
			t.Fatalf("new row never reached the scrolled viewport: %s", label)
		}
	}
	tt.Advance(280 * time.Millisecond)
	if own.at.IsZero() || own.done || other.at.IsZero() || other.done || chat.history.AtEnd() {
		t.Fatal("new bubbles did not enter inside the scrolled viewport")
	}
	tt.Advance(privateBubbleDuration)
	a.updatePrivateHistory(90002, 0, syncedPage, true, -1)
	if !own.done || !other.done || len(chat.arriving) != 0 {
		t.Fatal("duplicate history replayed an entrance")
	}
	newMessage.ID, newMessage.Time = 104, 2003
	a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{id: 90002, after: 2000}, netease.PrivateConversationPage{}, netease.PrivateHistoryPage{Items: []netease.PrivateMessage{newMessage}}, nil, time.Now())
	waiting := chat.arriving[privateMessageID(newMessage)]
	tt.Frame()
	if current, ok := tt.Find("合成历史 20"); !ok || current != anchor || chat.unread != 1 || chat.history.AtEnd() || waiting == nil || !waiting.at.IsZero() {
		t.Fatal("incoming history pulled the middle reader or lost unread")
	}
	if err := tt.Click("有新消息"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Advance(time.Second)
	if !chat.history.AtEnd() || chat.unread != 0 || !waiting.done {
		t.Fatal("unread action did not reveal the pending arrival at the tail")
	}
	older := synced
	older.ID, older.Time = 103, 999
	a.updatePrivateHistory(90002, 1000, netease.PrivateHistoryPage{Items: []netease.PrivateMessage{older}}, true, -1)
	if len(chat.arriving) != 0 {
		t.Fatal("paging older history started a live entrance")
	}
	otherChat := a.dm.chats[90003]
	a.selectPrivateChat(otherChat.user)
	otherChat.loaded = false
	a.updatePrivateHistory(90003, 0, syncedPage, true, -1)
	if len(otherChat.arriving) != 0 {
		t.Fatal("the first history was animated as a live sync")
	}
	a.selectPrivateChat(chat.user)
	synced.ID, synced.Time = 105, 2004
	a.privateHistoryAnswered(a.accountGen, a.dm.epoch, 90002, chat.gen, 0, netease.PrivateHistoryPage{Items: []netease.PrivateMessage{synced}}, nil)
	if len(chat.arriving) != 0 {
		t.Fatal("history loaded on conversation switch started a live entrance")
	}
	a.closePrivateMessages()
	tt.Frame()
	a.openPrivateMessages(nil)
	tt.Frame()
	privateShowContents(tt)
	tt.Advance(time.Second)
	// An opening refresh may return after the pane is already fully visible.
	synced.ID, synced.Time = 106, 2005
	a.privateHistoryAnswered(a.accountGen, a.dm.epoch, 90002, chat.gen, 0, netease.PrivateHistoryPage{Items: []netease.PrivateMessage{synced}}, nil)
	if len(chat.arriving) != 0 {
		t.Fatal("opening history was animated as a live sync")
	}
}

// Delaying construction must not consume the entrance; each successive send has its own visible clock and confirmation never replays it.
func TestPrivateOutgoingFirstVisible(t *testing.T) {
	a, backend := privateTestApp(t)
	a.openPrivateMessages(nil)
	a.selectPrivateChat(a.dm.contacts[0].User)
	chat := a.dm.chats[90002]
	chat.loading, a.dm.loading = false, false
	tt := ui.NewTester(a.view, 1180, 780)
	tt.Frame()
	privateShowContents(tt)
	tt.Advance(time.Second)
	for i := range 3 {
		chat.draft = fmt.Sprintf("合成连续发送 %d", i)
		clickClock := tt.Now().Add(-2 * time.Second)
		a.sendPrivateMessage(clickClock)
		select {
		case <-backend.sent:
		case <-time.After(3 * time.Second):
			t.Fatal("no synthetic send")
		}
		out := chat.activeOutgoing
		tt.Frame()
		tt.Advance(40 * time.Millisecond)
		if out.entrance.at.Before(tt.Now().Add(-80*time.Millisecond)) || out.entrance.done || !tt.FramePending() {
			t.Fatal("delayed first paint consumed the local entrance")
		}
		tt.Advance(privateBubbleDuration)
		if !out.entrance.done {
			t.Fatal("local entrance did not settle")
		}
		started := out.entrance.at
		a.privateSendAnswered(a.accountGen, a.dm.epoch, 90002, out.key, context.DeadlineExceeded)
		chat.loading = false
		confirmed := out.message
		confirmed.ID = int64(100 + i)
		page := netease.PrivateHistoryPage{Items: []netease.PrivateMessage{confirmed}}
		if i == 2 {
			// A visual pair without a unique confirmation still occupies only
			// one local row. A second same-text server ID stands on its own.
			synced := confirmed
			synced.ID, synced.Time = 200, synced.Time+1
			page.Items = append(page.Items, synced)
			a.updatePrivateHistory(90002, 0, page, true, -1)
			if chat.arriving[privateMessageID(confirmed)] != nil || chat.arriving[privateMessageID(synced)] == nil || out.confirmed != nil {
				t.Fatal("visual echo replayed or hid the independent same-text sync")
			}
			break
		}
		a.updatePrivateHistory(90002, 0, page, true, -1)
		if len(chat.arriving) != 0 {
			t.Fatal("confirmed local echo was scheduled for a second entrance")
		}
		tt.Frame()
		tt.Advance(time.Second)
		if out.confirmed == nil || out.entrance.at != started || !out.entrance.done {
			t.Fatal("authoritative confirmation replayed or replaced the settled bubble")
		}
	}
}

type pagedPrivateHistoryBackend struct {
	*fakePrivateMessages
	onHistory func(ctx context.Context, id, before int64) (netease.PrivateHistoryPage, error)
}

func (b *pagedPrivateHistoryBackend) History(ctx context.Context, id, before int64) (netease.PrivateHistoryPage, error) {
	return b.onHistory(ctx, id, before)
}

// Multi-page HTTPS catch-up preserves the last valid cursor, caps pages safely,
// pre-caches message content off the main thread, and handles errors/cancellation.
func TestPrivateInboxMultiPageCatchUp(t *testing.T) {
	makeMsg := func(id, at int64, from netease.MessageUser, text string) netease.PrivateMessage {
		return netease.PrivateMessage{ID: id, Time: at, From: from, Raw: fmt.Sprintf(`{"msg":%q}`, text)}
	}

	t.Run("cold-two-pages-and-content-cache", func(t *testing.T) {
		a, fake := privateTestApp(t)
		user := a.dm.contacts[0].User
		chat := a.dm.chats[user.UserID]
		chat.loaded, chat.messages, chat.serverUnreadAt, chat.unread = false, nil, 100, 1
		var cursors []int64
		raw1 := makeMsg(20, 160, user, "第一页较新")
		raw2 := makeMsg(19, 140, user, "第一页较旧")
		raw3 := makeMsg(18, 130, user, "第二页较新")
		raw4 := makeMsg(17, 95, user, "第二页已覆盖")
		backend := &pagedPrivateHistoryBackend{
			fakePrivateMessages: fake,
			onHistory: func(_ context.Context, _ int64, before int64) (netease.PrivateHistoryPage, error) {
				cursors = append(cursors, before)
				switch before {
				case 0:
					return netease.PrivateHistoryPage{Items: []netease.PrivateMessage{raw1, raw2}, Before: 140, More: true, Authoritative: true}, nil
				case 140:
					return netease.PrivateHistoryPage{Items: []netease.PrivateMessage{raw3, raw4}, Before: 95, More: true}, nil
				default:
					return netease.PrivateHistoryPage{Items: []netease.PrivateMessage{makeMsg(16, 80, user, "更早页")}, Before: 80, More: false}, nil
				}
			},
		}
		a.dm.client = backend
		startTestPrivateInbox(a)
		defer a.stopPrivateInbox()
		in := a.dm.inbox
		now := time.Unix(100, 0)
		in.queuePending(user.UserID, 100)
		request := *in.pending
		history, err := fetchPrivateInboxHistory(in.ctx, backend, request.id, request.after)
		if err != nil {
			t.Fatal(err)
		}
		if len(cursors) != 2 || cursors[0] != 0 || cursors[1] != 140 {
			t.Fatalf("unexpected catch-up cursors: %v", cursors)
		}
		if history.Before != 95 || !history.More || history.Authoritative || len(history.Items) != 4 {
			t.Fatalf("catch-up did not preserve last page cursor or non-authoritative status: %+v", history)
		}
		for i, item := range history.Items {
			raw := []netease.PrivateMessage{raw1, raw2, raw3, raw4}[i]
			if item == raw || item != item.WithContentCache() {
				t.Fatalf("item %d did not populate content cache in background", i)
			}
		}
		in.busy = true
		if !a.privateInboxAnswered(in, a.accountGen, in.gen, request, netease.PrivateConversationPage{}, history, nil, now) {
			t.Fatal("cold multi-page catch-up did not report UI change")
		}
		if chat.before != 95 || !chat.more || chat.unread != 4 || len(chat.messages) != 4 {
			t.Fatalf("cold multi-page state wrong: before=%d more=%t unread=%d count=%d", chat.before, chat.more, chat.unread, len(chat.messages))
		}
		// Manual "load older" must continue from 95, not re-fetch Page 2 from 140.
		manualCalled := make(chan int64, 1)
		backend.onHistory = func(_ context.Context, _ int64, before int64) (netease.PrivateHistoryPage, error) {
			manualCalled <- before
			return netease.PrivateHistoryPage{}, nil
		}
		a.dm.selected = user.UserID
		a.loadPrivateHistory(true)
		if got := receivePrivateRequest(t, manualCalled); got != 95 {
			t.Fatalf("manual older paging requested before=%d, want 95", got)
		}
	})

	t.Run("invalid-or-non-advancing-cursor-clears-more-without-exhausted", func(t *testing.T) {
		for _, badBefore := range []int64{0, 140, 150} {
			a, fake := privateTestApp(t)
			user := a.dm.contacts[0].User
			backend := &pagedPrivateHistoryBackend{
				fakePrivateMessages: fake,
				onHistory: func(_ context.Context, _ int64, before int64) (netease.PrivateHistoryPage, error) {
					if before == 0 {
						return netease.PrivateHistoryPage{Items: []netease.PrivateMessage{makeMsg(2, 160, user, "a"), makeMsg(1, 140, user, "b")}, Before: 140, More: true, Authoritative: true}, nil
					}
					return netease.PrivateHistoryPage{Items: []netease.PrivateMessage{makeMsg(3, 140, user, "dup")}, Before: badBefore, More: true}, nil
				},
			}
			history, err := fetchPrivateInboxHistory(context.Background(), backend, user.UserID, 100)
			if err != nil {
				t.Fatal(err)
			}
			if history.More || history.Exhausted || history.Before != 140 || history.Authoritative {
				t.Fatalf("badBefore=%d left distorted pagination: %+v", badBefore, history)
			}
			// Merging this stalled catch-up into a hot chat (oldTail=100) must open a gap and preserve before=140.
			chat := a.dm.chats[user.UserID]
			chat.loaded = true
			chat.messages = []netease.PrivateMessage{makeMsg(9, 100, user, "old")}
			a.updatePrivateHistory(user.UserID, 0, history, true, -1)
			if chat.gapUntil != 100 || !chat.more || chat.before != 140 {
				t.Fatalf("stalled bad cursor did not keep gap/before: gap=%d more=%t before=%d", chat.gapUntil, chat.more, chat.before)
			}
		}
	})

	t.Run("mid-page-error-and-timeout-preserve-hot-cache", func(t *testing.T) {
		for _, timeout := range []bool{false, true} {
			a, fake := privateTestApp(t)
			user := a.dm.contacts[0].User
			chat := a.dm.chats[user.UserID]
			old1, old2 := makeMsg(1, 50, user, "旧一"), makeMsg(2, 100, user, "旧二")
			chat.loaded, chat.messages, chat.before, chat.more = true, []netease.PrivateMessage{old1, old2}, 50, true
			startTestPrivateInbox(a)
			defer a.stopPrivateInbox()
			in := a.dm.inbox
			now := time.Unix(100, 0)
			in.queuePending(user.UserID, 100)
			request := *in.pending
			ctx, cancel := context.WithCancel(in.ctx)
			defer cancel()
			backend := &pagedPrivateHistoryBackend{
				fakePrivateMessages: fake,
				onHistory: func(_ context.Context, _ int64, before int64) (netease.PrivateHistoryPage, error) {
					if before == 0 {
						if timeout {
							cancel()
						}
						return netease.PrivateHistoryPage{Items: []netease.PrivateMessage{makeMsg(20, 200, user, "新")}, Before: 160, More: true, Authoritative: true}, nil
					}
					return netease.PrivateHistoryPage{}, errors.New("network interrupted")
				},
			}
			history, err := fetchPrivateInboxHistory(ctx, backend, request.id, 100)
			if err == nil || len(history.Items) != 0 || history.Authoritative || history.More {
				t.Fatalf("mid-page failure (timeout=%t) returned partial data: %+v %v", timeout, history, err)
			}
			in.busy = true
			if a.privateInboxAnswered(in, a.accountGen, in.gen, request, netease.PrivateConversationPage{}, history, err, now) {
				t.Fatal("failed multi-page catch-up reported UI change")
			}
			if len(chat.messages) != 2 || chat.messages[0].ID != 1 || chat.messages[1].ID != 2 ||
				chat.before != 50 || !chat.more || chat.unread != 0 {
				t.Fatalf("mid-page failure mutated cached chat: msgs=%d before=%d more=%t unread=%d", len(chat.messages), chat.before, chat.more, chat.unread)
			}
		}
	})

	t.Run("page-limit-keeps-hot-history-unread-and-manual-gap-resume", func(t *testing.T) {
		a, fake := privateTestApp(t)
		user := a.dm.contacts[0].User
		chat := a.dm.chats[user.UserID]
		old1, old2 := makeMsg(1, 50, user, "原旧一"), makeMsg(2, 100, user, "原旧二")
		chat.loaded, chat.messages, chat.before, chat.more, chat.serverUnreadAt = true, []netease.PrivateMessage{old1, old2}, 50, true, 100
		var cursors []int64
		pages := map[int64]netease.PrivateHistoryPage{
			0:   {Items: []netease.PrivateMessage{makeMsg(10, 200, user, "m10"), makeMsg(9, 190, user, "m9")}, Before: 190, More: true, Authoritative: true},
			190: {Items: []netease.PrivateMessage{makeMsg(8, 180, user, "m8"), makeMsg(7, 170, user, "m7")}, Before: 170, More: true},
			170: {Items: []netease.PrivateMessage{makeMsg(6, 160, user, "m6"), makeMsg(5, 150, user, "m5")}, Before: 150, More: true},
			150: {Items: []netease.PrivateMessage{makeMsg(4, 140, user, "m4"), makeMsg(3, 130, user, "m3")}, Before: 130, More: true},
			130: {Items: []netease.PrivateMessage{makeMsg(12, 120, user, "gap12"), makeMsg(11, 110, user, "gap11")}, Before: 110, More: true},
		}
		backend := &pagedPrivateHistoryBackend{
			fakePrivateMessages: fake,
			onHistory: func(_ context.Context, _ int64, before int64) (netease.PrivateHistoryPage, error) {
				cursors = append(cursors, before)
				return pages[before], nil
			},
		}
		a.dm.client = backend
		startTestPrivateInbox(a)
		defer a.stopPrivateInbox()
		in := a.dm.inbox
		now := time.Unix(100, 0)
		in.queuePending(user.UserID, 100)
		request := *in.pending
		history, err := fetchPrivateInboxHistory(in.ctx, backend, request.id, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(cursors) != privateInboxMaxHistoryPages || history.Before != 130 || !history.More || history.Authoritative {
			t.Fatalf("page cap behaved unexpectedly: cursors=%v history=%+v", cursors, history)
		}
		in.busy = true
		if !a.privateInboxAnswered(in, a.accountGen, in.gen, request, netease.PrivateConversationPage{}, history, nil, now) {
			t.Fatal("capped catch-up did not update chat")
		}
		if len(chat.messages) != 10 || chat.messages[0].ID != 1 || chat.messages[1].ID != 2 ||
			chat.before != 130 || !chat.more || chat.unread != 8 || in.pending != nil {
			t.Fatalf("capped catch-up lost hot history, cursor or unread: msgs=%d before=%d more=%t unread=%d", len(chat.messages), chat.before, chat.more, chat.unread)
		}
		// Replaying the same page must not double-count unread.
		in.busy = true
		if a.privateInboxAnswered(in, a.accountGen, in.gen, request, netease.PrivateConversationPage{}, history, nil, now) || chat.unread != 8 {
			t.Fatalf("replayed catch-up redrew or recounted unread: %d", chat.unread)
		}
		// Manual "load older" must load the intermediate gap from 130 without recounting unread.
		a.dm.selected = user.UserID
		gapCalled := make(chan int64, 1)
		backend.onHistory = func(_ context.Context, _ int64, before int64) (netease.PrivateHistoryPage, error) {
			gapCalled <- before
			return pages[before], nil
		}
		a.loadPrivateHistory(true)
		if got := receivePrivateRequest(t, gapCalled); got != 130 {
			t.Fatalf("manual gap resume requested before=%d, want 130", got)
		}
		a.privateHistoryAnswered(a.accountGen, a.dm.epoch, user.UserID, chat.gen, 130, pages[130], nil)
		if len(chat.messages) != 12 || chat.before != 110 || !chat.more || chat.unread != 8 {
			t.Fatalf("manual gap resume failed: msgs=%d before=%d more=%t unread=%d", len(chat.messages), chat.before, chat.more, chat.unread)
		}
	})

	t.Run("hot-no-gap-keeps-earlier-before", func(t *testing.T) {
		a, fake := privateTestApp(t)
		user := a.dm.contacts[0].User
		chat := a.dm.chats[user.UserID]
		chat.loaded, chat.messages, chat.before, chat.more = true, []netease.PrivateMessage{makeMsg(1, 50, user, "a"), makeMsg(2, 100, user, "b")}, 50, true
		backend := &pagedPrivateHistoryBackend{
			fakePrivateMessages: fake,
			onHistory: func(_ context.Context, _ int64, before int64) (netease.PrivateHistoryPage, error) {
				if before == 0 {
					return netease.PrivateHistoryPage{Items: []netease.PrivateMessage{makeMsg(4, 140, user, "d"), makeMsg(3, 120, user, "c")}, Before: 120, More: true, Authoritative: true}, nil
				}
				return netease.PrivateHistoryPage{Items: []netease.PrivateMessage{makeMsg(2, 100, user, "b"), makeMsg(5, 95, user, "x")}, Before: 95, More: true}, nil
			},
		}
		history, err := fetchPrivateInboxHistory(context.Background(), backend, user.UserID, 100)
		if err != nil {
			t.Fatal(err)
		}
		a.updatePrivateHistory(user.UserID, 0, history, true, 100)
		if chat.before != 50 || !chat.more || len(chat.messages) != 5 {
			t.Fatalf("contiguous hot catch-up regressed earlier cursor: before=%d more=%t msgs=%d", chat.before, chat.more, len(chat.messages))
		}
	})

	t.Run("window-and-account-cancel-block-old-reply", func(t *testing.T) {
		a, fake := privateTestApp(t)
		user := a.dm.contacts[0].User
		a.dm.chats[user.UserID].serverUnreadAt = 100
		page2Ctx := make(chan context.Context, 1)
		backend := &pagedPrivateHistoryBackend{
			fakePrivateMessages: fake,
			onHistory: func(ctx context.Context, _ int64, before int64) (netease.PrivateHistoryPage, error) {
				if before == 0 {
					return netease.PrivateHistoryPage{Items: []netease.PrivateMessage{makeMsg(20, 200, user, "新")}, Before: 180, More: true}, nil
				}
				page2Ctx <- ctx
				<-ctx.Done()
				return netease.PrivateHistoryPage{}, ctx.Err()
			},
		}
		a.dm.client = backend
		startTestPrivateInbox(a)
		in := a.dm.inbox
		now := time.Unix(100, 0)
		in.queuePending(user.UserID, 100)
		request := *in.pending
		a.tickPrivateInbox(now)
		ctx2 := receivePrivateRequest(t, page2Ctx)
		account, gen := a.accountGen, in.gen
		a.closePrivateMessages()
		if ctx2.Err() == nil || a.dm.inbox != nil {
			t.Fatal("closing DM panel did not cancel in-flight multi-page catch-up")
		}
		stale := netease.PrivateHistoryPage{Items: []netease.PrivateMessage{makeMsg(20, 200, user, "不应复活")}, Before: 200}
		if a.privateInboxAnswered(in, account, gen, request, netease.PrivateConversationPage{}, stale, nil, now) || len(a.dm.chats[user.UserID].messages) != 0 {
			t.Fatal("stale catch-up reply mutated state after panel close")
		}
		a.accountGen++
		a.resetPrivateMessages()
		if a.privateInboxAnswered(in, account, gen, request, netease.PrivateConversationPage{}, stale, nil, now) || len(a.dm.chats) != 0 {
			t.Fatal("stale catch-up reply resurrected state after account switch")
		}
	})
}

// Partial history or an unknown peer must never write a placeholder nickname into
// chat/contact models; fallback text belongs only in the UI.
func TestPrivatePlaceholderDoesNotOverwriteNickname(t *testing.T) {
	a, _ := privateTestApp(t)
	a.settings.Motion = "off"
	known := netease.MessageUser{UserID: 90002, Nickname: "真实昵称", AvatarURL: "real-avatar"}
	a.dm.contacts = []netease.PrivateConversation{{User: known, Summary: "旧消息", Time: 1000}}
	delete(a.dm.chats, known.UserID)
	startTestPrivateInbox(a)
	defer a.stopPrivateInbox()
	in, now := a.dm.inbox, time.Unix(100, 0)

	// 1) Existing contact without an open chat retains its real nickname/avatar.
	chat := a.privateChatFor(netease.MessageUser{UserID: known.UserID})
	if chat == nil || chat.user != known {
		t.Fatalf("privateChatFor overwrote known contact profile: got %+v, want %+v", chat, known)
	}
	partialMsg := netease.PrivateMessage{ID: 10, Time: 2000, From: netease.MessageUser{UserID: known.UserID}, Raw: `{"msg":"新来信"}`}
	a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{id: known.UserID, after: -1}, netease.PrivateConversationPage{}, netease.PrivateHistoryPage{Items: []netease.PrivateMessage{partialMsg}}, nil, now)
	if chat.user != known || a.dm.contacts[0].User != known {
		t.Fatalf("partial history lost real nickname or avatar: chat=%+v contact=%+v", chat.user, a.dm.contacts[0].User)
	}

	// 2) Unknown peer keeps empty Nickname in models while UI shows fallback label.
	const unknownID int64 = 90099
	unknownChat := a.privateChatFor(netease.MessageUser{UserID: unknownID})
	if unknownChat == nil || unknownChat.user.UserID != unknownID || unknownChat.user.Nickname != "" {
		t.Fatalf("unknown peer stored placeholder in chat model: %+v", unknownChat)
	}
	unknownMsg := netease.PrivateMessage{ID: 20, Time: 3000, From: netease.MessageUser{UserID: unknownID}, Raw: `{"msg":"未知联系人来信"}`}
	a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{id: unknownID, after: -1}, netease.PrivateConversationPage{}, netease.PrivateHistoryPage{Items: []netease.PrivateMessage{unknownMsg}}, nil, now)
	if unknownChat.user.Nickname != "" || a.dm.contacts[0].User.UserID != unknownID || a.dm.contacts[0].User.Nickname != "" {
		t.Fatalf("placeholder leaked into chat or contact model: chat=%+v contact=%+v", unknownChat.user, a.dm.contacts[0].User)
	}
	a.dm.loaded = true
	tt := ui.NewTester(a.view, 1180, 780)
	tt.Frame()
	if err := tt.Click("会话：私信联系人"); err != nil {
		t.Fatalf("UI fallback label missing for unknown contact: %v", err)
	}
	tt.Frame()
	if _, ok := tt.Find("私信联系人"); !ok {
		t.Fatal("chat header did not display fallback nickname")
	}
	if unknownChat.user.Nickname != "" || a.dm.contacts[0].User.Nickname != "" {
		t.Fatal("rendering UI wrote fallback nickname back into model")
	}

	// 3) Later full profile replaces the unknown state cleanly.
	full := netease.MessageUser{UserID: unknownID, Nickname: "后续完整昵称", AvatarURL: "later-avatar"}
	a.mergePrivateContacts([]netease.PrivateConversation{{User: full, Summary: "未知联系人来信", Time: 3000}}, false)
	if unknownChat.user != full || a.dm.contacts[0].User != full {
		t.Fatalf("full profile did not replace unknown state: chat=%+v contact=%+v", unknownChat.user, a.dm.contacts[0].User)
	}
}

type gapReadHistoryBackend struct {
	*fakePrivateMessages
	onHistory func(ctx context.Context, id, before int64) (netease.PrivateHistoryPage, error)
	reads     chan int64
}

func (b *gapReadHistoryBackend) History(ctx context.Context, id, before int64) (netease.PrivateHistoryPage, error) {
	return b.onHistory(ctx, id, before)
}

func (b *gapReadHistoryBackend) Read(_ context.Context, id int64) error {
	b.reads <- id
	return nil
}

// A bounded 4-page catch-up gap must let a server snapshot correct missing unread, block
// whole-conversation Read across empty/non-advancing older pages until manually bridged,
// resume paging cleanly, reject stale snapshots after reading, and guard cold stale tails.
func TestPrivateInboxGapUnreadAndReadBoundary(t *testing.T) {
	a, fake := privateTestApp(t)
	a.settings.Motion = "off"
	user := a.dm.contacts[0].User
	makeMsg := func(id, at int64, text string) netease.PrivateMessage {
		return netease.PrivateMessage{ID: id, Time: at, From: user, Raw: fmt.Sprintf(`{"msg":%q}`, text)}
	}
	pages := map[int64]netease.PrivateHistoryPage{
		0:   {Items: []netease.PrivateMessage{makeMsg(10, 200, "m10"), makeMsg(9, 190, "m9")}, Before: 190, More: true, Authoritative: true},
		190: {Items: []netease.PrivateMessage{makeMsg(8, 180, "m8"), makeMsg(7, 170, "m7")}, Before: 170, More: true},
		170: {Items: []netease.PrivateMessage{makeMsg(6, 160, "m6"), makeMsg(5, 150, "m5")}, Before: 150, More: true},
		150: {Items: []netease.PrivateMessage{makeMsg(4, 140, "m4"), makeMsg(3, 130, "m3")}, Before: 130, More: true},
		130: {Items: []netease.PrivateMessage{makeMsg(12, 120, "gap12"), makeMsg(11, 110, "gap11")}, Before: 110, More: true},
		110: {Items: []netease.PrivateMessage{makeMsg(2, 100, "old2"), makeMsg(1, 50, "old1")}, Before: 50, More: true},
		50:  {Items: []netease.PrivateMessage{makeMsg(13, 40, "older13")}, Before: 40, More: false, Exhausted: true},
	}
	historyReq := make(chan int64, 8)
	backend := &gapReadHistoryBackend{
		fakePrivateMessages: fake,
		reads:               make(chan int64, 4),
		onHistory: func(_ context.Context, _ int64, before int64) (netease.PrivateHistoryPage, error) {
			historyReq <- before
			return pages[before], nil
		},
	}
	a.dm.client = backend
	chat := a.dm.chats[user.UserID]
	chat.loaded = true
	chat.messages = []netease.PrivateMessage{makeMsg(1, 50, "old1"), makeMsg(2, 100, "old2")}
	chat.before, chat.more = 50, true
	chat.serverUnreadAt, chat.readAt, chat.unread = 100, 100, 0
	a.dm.open = true
	a.dm.loaded = true
	a.dm.selected = user.UserID
	a.dm.ctx, a.dm.cancel = context.WithCancel(context.Background())
	a.startPrivateInbox()
	defer a.closePrivateMessages()
	in, now := a.dm.inbox, time.Unix(100, 0)

	// 1) Hot chat hits the 4-page cap at T=200 (gap [110, 120] missing).
	in.queuePending(user.UserID, 100)
	request := *in.pending
	history, err := fetchPrivateInboxHistory(in.ctx, backend, request.id, 100)
	if err != nil {
		t.Fatal(err)
	}
	in.busy = true
	a.privateInboxAnswered(in, a.accountGen, in.gen, request, netease.PrivateConversationPage{}, history, nil, now)
	if chat.unread != 8 || chat.readAt != 100 || chat.gapUntil != 100 || !chat.unreadEstimated || chat.before != 130 || !chat.more {
		t.Fatalf("capped catch-up state wrong: unread=%d readAt=%d gap=%d est=%t before=%d more=%t",
			chat.unread, chat.readAt, chat.gapUntil, chat.unreadEstimated, chat.before, chat.more)
	}

	// 2) Server snapshot (Time=200, Unread=10) aligns the capped local estimate without losing gapUntil.
	if !a.applyPrivateUnreadSnapshot([]netease.PrivateConversation{{User: user, Time: 200, Unread: 10, UnreadKnown: true}}) ||
		chat.unread != 10 || chat.serverUnreadAt != 200 || chat.unreadEstimated || chat.gapUntil != 100 {
		t.Fatalf("server snapshot did not correct capped unread: unread=%d serverAt=%d est=%t gap=%d",
			chat.unread, chat.serverUnreadAt, chat.unreadEstimated, chat.gapUntil)
	}
	// A later arrival at T=205 adds +1 without double-counting the aligned snapshot or dropping gap cursor.
	msg205 := makeMsg(14, 205, "m14")
	in.busy = true
	a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{id: user.UserID, after: 200}, netease.PrivateConversationPage{},
		netease.PrivateHistoryPage{Items: []netease.PrivateMessage{msg205, makeMsg(10, 200, "m10")}, Before: 200, More: true}, nil, now)
	if chat.unread != 11 || chat.gapUntil != 100 || chat.before != 130 {
		t.Fatalf("post-snapshot arrival double-counted or dropped gap cursor: unread=%d gap=%d before=%d", chat.unread, chat.gapUntil, chat.before)
	}

	// 3) While the gap is still open, viewing the bottom must neither clear unread nor send Read ACK.
	tt := ui.NewTester(a.view, 1180, 780)
	tt.Frame()
	chat.history.ScrollToEnd()
	tt.Frame()
	tt.Advance(time.Millisecond)
	tt.Frame()
	select {
	case <-backend.reads:
		t.Fatal("markPrivateRead confirmed whole conversation before the gap was loaded")
	default:
	}
	if chat.unread != 11 || chat.read.busy || chat.read.confirmed != nil {
		t.Fatalf("open gap cleared unread or started Read: unread=%d busy=%t confirmed=%v", chat.unread, chat.read.busy, chat.read.confirmed)
	}

	// 3b) Empty, duplicate, or bad-cursor older pages — and lagging before==0 empty Exhausted pages —
	// MUST NOT clear gapUntil, clear chat.more, or unlock Read (P0-A / T05).
	chat.contactAt = 205
	a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{id: user.UserID, after: -1},
		netease.PrivateConversationPage{}, netease.PrivateHistoryPage{Items: nil, More: false, Authoritative: true, Exhausted: true}, nil, now)
	if chat.gapUntil != 100 || chat.before != 130 || !chat.more {
		t.Fatalf("lagging before==0 Exhausted empty page broke gap protection: gap=%d before=%d more=%t", chat.gapUntil, chat.before, chat.more)
	}
	for _, bad := range []netease.PrivateHistoryPage{
		{Items: nil, Before: 0, More: false},
		{Items: []netease.PrivateMessage{makeMsg(3, 130, "dup130")}, Before: 130, More: false},
		{Items: []netease.PrivateMessage{makeMsg(4, 140, "bad140")}, Before: 140, More: false},
	} {
		a.updatePrivateHistory(user.UserID, 130, bad, false, -1)
		if chat.gapUntil != 100 || chat.before != 130 || !chat.more {
			t.Fatalf("non-advancing older page %+v broke gap protection: gap=%d before=%d more=%t", bad, chat.gapUntil, chat.before, chat.more)
		}
		tt.Frame()
		select {
		case <-backend.reads:
			t.Fatal("non-advancing older page unlocked Read")
		default:
		}
	}

	// 4) Manual paging loads the gap (130 -> 110 -> 50); Read remains blocked until gapUntil is bridged.
	for len(historyReq) > 0 {
		<-historyReq
	}
	a.loadPrivateHistory(true)
	if got := receivePrivateRequest(t, historyReq); got != 130 {
		t.Fatalf("first manual gap page requested before=%d, want 130", got)
	}
	a.privateHistoryAnswered(a.accountGen, a.dm.epoch, user.UserID, chat.gen, 130, pages[130], nil)
	if chat.before != 110 || !chat.more || chat.gapUntil != 100 || chat.unread != 11 {
		t.Fatalf("intermediate gap page closed gap prematurely or mutated unread: before=%d more=%t gap=%d unread=%d",
			chat.before, chat.more, chat.gapUntil, chat.unread)
	}
	tt.Frame()
	select {
	case <-backend.reads:
		t.Fatal("markPrivateRead fired before bridging down to gapUntil")
	default:
	}

	a.loadPrivateHistory(true)
	if got := receivePrivateRequest(t, historyReq); got != 110 {
		t.Fatalf("second manual gap page requested before=%d, want 110", got)
	}
	a.privateHistoryAnswered(a.accountGen, a.dm.epoch, user.UserID, chat.gen, 110, pages[110], nil)
	if chat.before != 50 || !chat.more || chat.gapUntil != 0 {
		t.Fatalf("bridging past gapUntil did not clear gap or keep earlier cursor: before=%d more=%t gap=%d", chat.before, chat.more, chat.gapUntil)
	}
	// Continuing manual paging reaches older history before 50 cleanly.
	a.loadPrivateHistory(true)
	if got := receivePrivateRequest(t, historyReq); got != 50 {
		t.Fatalf("post-gap manual paging requested before=%d, want 50", got)
	}
	a.privateHistoryAnswered(a.accountGen, a.dm.epoch, user.UserID, chat.gen, 50, pages[50], nil)
	if chat.more || chat.before != 40 || chat.messages[0].ID != 13 {
		t.Fatalf("post-gap older paging failed: more=%t before=%d first=%d", chat.more, chat.before, chat.messages[0].ID)
	}

	// 5) After the gap is closed, reading at the bottom clears unread and sends Read ACK once.
	chat.history.ScrollToEnd()
	tt.Frame()
	tt.Advance(time.Millisecond)
	tt.Frame()
	if got := receivePrivateRequest(t, backend.reads); got != user.UserID || chat.unread != 0 || chat.read.attempt == nil {
		t.Fatalf("closed gap did not clear unread and trigger Read: got=%d unread=%d attempt=%v", got, chat.unread, chat.read.attempt)
	}
	a.privateReadAnswered(a.accountGen, user.UserID, *chat.read.attempt, nil)
	if chat.read.confirmed == nil || *chat.read.confirmed != privateMessageID(msg205) {
		t.Fatalf("Read ACK did not confirm latest incoming message: %+v", chat.read.confirmed)
	}

	// 6) Stale snapshots arriving after confirmed read must never revive the unread badge.
	for _, stale := range []netease.PrivateConversation{
		{User: user, Time: 200, Unread: 10, UnreadKnown: true},
		{User: user, Time: 205, Unread: 11, UnreadKnown: true},
	} {
		if a.applyPrivateUnreadSnapshot([]netease.PrivateConversation{stale}) || chat.unread != 0 {
			t.Fatalf("stale post-read snapshot %+v revived unread=%d", stale, chat.unread)
		}
	}

	// 7) Cold 50-unread chat (T11): stale tail < serverUnreadAt blocks Read; latest 30 reaching serverUnreadAt allows Read.
	coldUser := a.dm.contacts[1].User
	coldChat := a.dm.chats[coldUser.UserID]
	coldChat.loaded, coldChat.messages = false, nil
	a.applyPrivateUnreadSnapshot([]netease.PrivateConversation{{User: coldUser, Time: 500, Unread: 50, UnreadKnown: true}})
	a.selectPrivateChat(coldUser)
	// Stale cold History reaching only Time=400 < 500 must NOT clear unread or send Read.
	a.privateHistoryAnswered(a.accountGen, a.dm.epoch, coldUser.UserID, coldChat.gen, 0,
		netease.PrivateHistoryPage{Items: []netease.PrivateMessage{{ID: 1, Time: 400, From: coldUser}}, Before: 400, More: true}, nil)
	tt.Frame()
	tt.Advance(time.Millisecond)
	tt.Frame()
	if coldChat.unread != 50 || coldChat.read.busy {
		t.Fatalf("stale cold tail cleared unread or started Read: unread=%d busy=%t", coldChat.unread, coldChat.read.busy)
	}
	// Latest 30 reaching serverUnreadAt=500 allows reading the tail (current product contract).
	var latest30 []netease.PrivateMessage
	for i := range 30 {
		latest30 = append(latest30, netease.PrivateMessage{ID: int64(21 + i), Time: int64(210 + i*10), From: coldUser, Raw: `{"msg":"冷未读"}`})
	}
	a.privateHistoryAnswered(a.accountGen, a.dm.epoch, coldUser.UserID, coldChat.gen, 0,
		netease.PrivateHistoryPage{Items: latest30, Before: 210, More: true, Authoritative: true}, nil)
	tt.Frame()
	tt.Advance(time.Millisecond)
	tt.Frame()
	if got := receivePrivateRequest(t, backend.reads); got != coldUser.UserID || coldChat.unread != 0 {
		t.Fatalf("cold latest-30 covering serverUnreadAt did not clear unread/Read: got=%d unread=%d", got, coldChat.unread)
	}

	// 8) Regression (P2-1): while a hot gap remains open, cursor progress with omitted `more`
	// (More=false, Exhausted=false) must keep chat.more=true so manual paging stays reachable,
	// whereas an explicit Exhausted=true page still closes both gapUntil and chat.more cleanly.
	chat.gapUntil, chat.before, chat.more = 100, 130, true
	a.updatePrivateHistory(user.UserID, 130, netease.PrivateHistoryPage{
		Items:  []netease.PrivateMessage{makeMsg(15, 120, "omitted-more")},
		Before: 120, More: false, Exhausted: false,
	}, false, -1)
	if chat.gapUntil != 100 || chat.before != 120 || !chat.more {
		t.Fatalf("omitted more during open gap dropped manual paging entry: gap=%d before=%d more=%t", chat.gapUntil, chat.before, chat.more)
	}
	a.updatePrivateHistory(user.UserID, 120, netease.PrivateHistoryPage{
		Items:  []netease.PrivateMessage{makeMsg(16, 115, "explicit-end")},
		Before: 115, More: false, Exhausted: true,
	}, false, -1)
	if chat.gapUntil != 0 || chat.before != 115 || chat.more {
		t.Fatalf("explicit Exhausted=true did not close gap and chat.more cleanly: gap=%d before=%d more=%t", chat.gapUntil, chat.before, chat.more)
	}
}
