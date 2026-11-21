package app

import (
	"context"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

type privateReadBackend struct {
	*fakePrivateMessages
	reads chan int64
}

func (b *privateReadBackend) Read(_ context.Context, id int64) error {
	b.reads <- id
	return nil
}

// Only a loaded visible tail acknowledges its peer once; newer arrivals, failed requests and stale panel replies cannot claim another read.
func TestPrivateReadConversation(t *testing.T) {
	a, fake := privateTestApp(t)
	backend := &privateReadBackend{fakePrivateMessages: fake, reads: make(chan int64, 8)}
	a.dm.client = backend
	a.settings.Motion = "off"
	chat := a.dm.chats[90002]
	for i := range 60 {
		chat.messages = append(chat.messages, netease.PrivateMessage{ID: int64(i + 1), Time: int64(1000 + i), From: chat.user, Raw: `{"msg":"合成已读历史"}`})
	}
	noRequest := func() {
		t.Helper()
		select {
		case <-backend.reads:
			t.Fatal("unexpected read request")
		default:
		}
	}
	request := func() privateReadKey {
		t.Helper()
		// Marking busy is synchronous; workers deliver through this fake channel.
		if !chat.read.busy || chat.read.attempt == nil {
			t.Fatal("reading the loaded tail did not start a server acknowledgement")
		}
		select {
		case id := <-backend.reads:
			if id != 90002 {
				t.Fatal("read targeted a different conversation")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("read did not reach the backend")
		}
		return *chat.read.attempt
	}
	load := func() {
		t.Helper()
		a.privateHistoryAnswered(a.accountGen, a.dm.epoch, 90002, chat.gen, 0, netease.PrivateHistoryPage{Items: chat.messages}, nil)
	}
	a.markPrivateRead(chat)
	noRequest() // Closed panels and background cached history cannot mark read.
	a.openPrivateMessages(&chat.user)
	tt := ui.NewTester(a.view, 1180, 780)
	tt.Frame()
	noRequest() // Local loaded cache is insufficient while latest History loads.
	chat.history.ScrollTo(0, ui.Start)
	tt.Frame()
	load()
	tt.Frame()
	noRequest() // Successful history alone is insufficient in the middle.
	chat.history.ScrollToEnd()
	tt.Frame()
	tt.Frame()
	first := request() // Local unread was zero: old history still needs a read.
	for range 3 {
		tt.Frame()
	}
	noRequest()
	a.selectPrivateChat(a.dm.contacts[1].User)
	fresh := netease.PrivateMessage{ID: 99, Time: 2000, From: chat.user, Raw: `{"msg":"在旧请求期间到达"}`}
	in := a.dm.inbox
	a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{id: 90002, after: 1059}, netease.PrivateConversationPage{}, netease.PrivateHistoryPage{Items: []netease.PrivateMessage{fresh}}, nil, time.Unix(100, 0))
	a.privateReadAnswered(a.accountGen, 90002, first, nil)
	if chat.unread != 1 || chat.read.confirmed == nil || *chat.read.confirmed == privateMessageID(fresh) {
		t.Fatal("old read reply cleared or confirmed a newer unread message")
	}
	a.selectPrivateChat(chat.user)
	load()
	tt.Frame()
	tt.Frame()
	second := request()
	a.privateReadAnswered(a.accountGen, 90002, second, context.DeadlineExceeded)
	for range 3 {
		tt.Frame()
	}
	noRequest()
	if *chat.read.confirmed == privateMessageID(fresh) {
		t.Fatal("failed request was treated as server-confirmed")
	}
	a.loadPrivateHistory(false)
	load()
	tt.Frame()
	retry := request() // A new successful load permits one retry after failure.
	a.closePrivateMessages()
	a.openPrivateMessages(nil)
	load()
	tt.Frame()
	tt.Frame()
	latest := request()
	a.privateReadAnswered(a.accountGen, 90002, retry, nil)
	if !chat.read.busy {
		t.Fatal("closed panel's reply completed the new read")
	}
	a.privateReadAnswered(a.accountGen, 90002, latest, nil)
	tt.Frame()
	tt.Frame()
	noRequest()
	if chat.read.busy || chat.unread != 0 || *chat.read.confirmed != privateMessageID(fresh) {
		t.Fatal("latest read did not confirm once and settle")
	}
	a.loadPrivateHistory(false)
	load()
	tt.Frame()
	noRequest() // A successful business ACK survives a later refresh.
	a.closePrivateMessages()
	a.openPrivateMessages(nil)
	load()
	tt.Frame()
	tt.Frame()
	noRequest() // Reopening the same already-read identity needs no second ACK.

	// The newest peer message can sit above a batch of own messages. Its
	// pending entrance outside the viewport must not stall the tail read.
	delayed := fresh
	delayed.ID++
	delayed.Time++
	items := []netease.PrivateMessage{delayed}
	for i := range 30 {
		items = append(items, netease.PrivateMessage{ID: 200 + int64(i), Time: delayed.Time + 1 + int64(i),
			From: netease.MessageUser{UserID: a.profile.UserID}, Raw: `{"msg":"合成跨端发送"}`})
	}
	a.updatePrivateHistory(90002, 0, netease.PrivateHistoryPage{Items: items}, true, -1)
	tt.Frame()
	tt.Advance(20 * time.Millisecond)
	got := request()
	if got.message != privateMessageID(delayed) {
		t.Fatal("an offscreen arrival blocked the latest peer read")
	}
	a.privateReadAnswered(a.accountGen, 90002, got, nil)

	// A same-millisecond peer arrival with a smaller ID sorts before the
	// confirmed tail ID, yet must still trigger a new read acknowledgement.
	sameMsSmaller := delayed
	sameMsSmaller.ID = 98 // Smaller than delayed.ID (100) and distinct from fresh.ID (99).
	sameMsSmaller.Raw = `{"msg":"同毫秒较小 ID 来信"}`
	a.updatePrivateHistory(90002, 0, netease.PrivateHistoryPage{Items: []netease.PrivateMessage{sameMsSmaller, delayed}}, true, -1)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if sameMs := request(); sameMs.message != privateMessageID(sameMsSmaller) {
		t.Fatalf("same-ms smaller ID did not trigger Read: got %+v", sameMs)
	} else {
		a.privateReadAnswered(a.accountGen, 90002, sameMs, nil)
	}
	tt.Frame()
	noRequest()
}
