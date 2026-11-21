package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/audio"
	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

// Hiding the main window used to keep its panel, inbox and history work alive.
func TestPrivateClosureHideMain(t *testing.T) {
	a, _ := privateTestApp(t)
	a.openPrivateMessages(&a.dm.contacts[0].User)
	t.Cleanup(a.closePrivateMessages)
	out := audio.NewPlayer(&silence{})
	out.SetVolume(0)
	out.Play()
	a.pl.Attach(out)
	defer out.Close()
	in, panel := a.dm.inbox, a.dm.ctx
	history, cancelHistory := context.WithCancel(in.ctx)
	defer cancelHistory()
	in.busy, in.requestCancel = true, cancelHistory
	in.timer = time.NewTimer(time.Hour)
	defer in.timer.Stop()

	a.hideMain()
	if a.dm.open || a.dm.inbox != nil || panel.Err() == nil || in.ctx.Err() == nil || history.Err() == nil {
		t.Fatal("hidden main window kept the private panel or its requests alive")
	}
	if in.timer.Stop() {
		t.Fatal("hidden main window kept the automatic timer armed")
	}
	if !a.pl.Playing() {
		t.Fatal("hiding the main window interrupted existing playback")
	}
	epoch := a.dm.epoch
	a.closePrivateMessages()
	if a.dm.epoch != epoch {
		t.Fatal("closing a hidden panel a second time changed its epoch")
	}
	a.openPrivateMessages(nil)
	if a.dm.inbox == nil || a.dm.inbox == in || a.dm.inbox.ctx.Err() != nil {
		t.Fatal("reopening the panel did not start a fresh inbox")
	}
}

// HTTP 429 rate limiting must hold automatic retries for at least a minute across close/reopen,
// and closing the panel immediately stops timers and cancels in-flight context.
func TestPrivateClosureRateLimitBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		a, _ := privateTestApp(t)
		a.openPrivateMessages(nil)
		defer a.closePrivateMessages()
		a.win = &mygo.Window{} // Arm production timers without creating a native window.
		in := a.dm.inbox
		a.dm.loading = false
		now := time.Now()
		limited := &netease.PrivateMessageHTTPError{StatusCode: http.StatusTooManyRequests}
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{}, netease.PrivateHistoryPage{}, limited, now)
		if due, ok := a.privateInboxDue(in); !ok || due.Sub(now) != privateRateLimitBackoff {
			t.Fatalf("HTTP 429 due=%v ok=%t, want %s", due.Sub(now), ok, privateRateLimitBackoff)
		}
		// Closing and reopening the same account preserves the 429 cooldown instead of resetting it.
		timer := in.timer
		a.closePrivateMessages()
		if timer != nil && timer.Stop() {
			t.Fatal("closing the panel did not stop the backoff timer")
		}
		if in.ctx.Err() == nil {
			t.Fatal("closing the panel did not cancel the inbox context")
		}
		a.win = nil
		a.openPrivateMessages(nil)
		a.dm.loading = false
		if due, ok := a.privateInboxDue(a.dm.inbox); !ok || !due.Equal(now.Add(privateRateLimitBackoff)) {
			t.Fatalf("reopening cleared active 429 cooldown: due=%v want=%v", due, now.Add(privateRateLimitBackoff))
		}
	})
}

// Offscreen contact rows used to fetch missing artwork and refresh cached artwork on every frame.
func TestPrivateClosureOffscreenAvatars(t *testing.T) {
	a, _ := privateTestApp(t)
	a.settings.Motion = "off"
	a.openPrivateMessages(nil)
	defer a.closePrivateMessages()
	lastUse, now := time.Unix(1, 0), time.Unix(2, 0)
	a.art.now = func() time.Time { return now }

	img := image.NewRGBA(image.Rect(0, 0, 40, 40))
	green := color.RGBA{G: 255, A: 255}
	for y := range 40 {
		for x := range 40 {
			img.SetRGBA(x, y, green)
		}
	}
	path := filepath.Join(t.TempDir(), "avatar.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	err = png.Encode(file, img)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("could not write the avatar fixture: %v / %v", err, closeErr)
	}

	cached := &artwork{bmp: ui.NewBitmap(img), used: lastUse}
	a.art.add("offscreen-cached", cached)
	for i := range 26 {
		a.dm.contacts = append(a.dm.contacts, netease.PrivateConversation{User: netease.MessageUser{
			UserID: int64(91000 + i), Nickname: fmt.Sprintf("联系人 %02d", i),
		}})
	}
	a.dm.contacts[len(a.dm.contacts)-2].User.AvatarURL = "offscreen-cached"
	last := &a.dm.contacts[len(a.dm.contacts)-1].User
	last.AvatarURL = path
	key := netease.Sized(path, 114)
	posts := make(chan func(), 1)
	a.art.post = func(fn func()) { posts <- fn }

	tt := ui.NewTester(a.view, 1180, 780)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	privateShowContents(tt)
	tt.Frame()
	if a.art.busy[key] || cached.used != lastUse {
		t.Fatalf("offscreen avatar accessed the cache: request=%t, last used=%v", a.art.busy[key], cached.used)
	}

	a.dm.listScroll.Y = a.dm.listScroll.MaxY
	tt.Frame()
	tt.Advance(time.Millisecond)
	select {
	case apply := <-posts:
		apply()
	case <-time.After(3 * time.Second):
		t.Fatal("avatar did not start loading when scrolled into view")
	}
	tt.Frame()
	tt.Advance(artFade.Duration)
	row, ok := tt.Find("会话：" + last.Nickname)
	if !ok || tt.Image().RGBAAt(int(row.X+28), int(row.Y+row.H/2)) != green || cached.used != now {
		t.Fatal("scrolling the contact into view did not paint and touch its artwork")
	}
}

// Retrying an empty history used to leave the stale retry row interactive while the request was pending.
func TestPrivateClosureEmptyHistoryRetry(t *testing.T) {
	t.Run("first load", func(t *testing.T) { testPrivateClosureEmptyHistoryRetry(t, false) })
	t.Run("refresh empty chat", func(t *testing.T) { testPrivateClosureEmptyHistoryRetry(t, true) })
}

func testPrivateClosureEmptyHistoryRetry(t *testing.T, alreadyLoaded bool) {
	t.Helper()
	a, _ := privateTestApp(t)
	a.settings.Motion = "off"
	user := a.dm.contacts[0].User
	chat := a.dm.chats[user.UserID]
	chat.loaded = alreadyLoaded
	a.openPrivateMessages(&user)
	defer a.closePrivateMessages()
	a.privateHistoryAnswered(a.accountGen, a.dm.epoch, user.UserID, chat.gen, 0,
		netease.PrivateHistoryPage{}, errors.New("fixture history failed"))

	tt := ui.NewTester(a.view, 1180, 780)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	privateShowContents(tt)
	if err := tt.Click("重试消息"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if _, found := tt.Find("重试消息"); found || !chat.loading || (len(chat.historyRows) != 1 || chat.historyRows[0].key.kind != privateHistoryEnd) {
		t.Fatal("retry kept the stale error row or failed to begin loading")
	}
	gen := chat.gen
	if err := tt.Click("刷新消息"); err != nil {
		t.Fatal(err)
	}
	if chat.gen != gen {
		t.Fatal("clicking refresh during retry started another history request")
	}
	a.privateHistoryAnswered(a.accountGen, a.dm.epoch, user.UserID, gen, 0, netease.PrivateHistoryPage{}, nil)
	tt.Frame()
	if _, found := tt.Find("暂无消息"); !found || chat.loading {
		t.Fatal("successful retry did not bring back the empty history state")
	}
}

type sendLagBackend struct {
	*fakePrivateMessages
	sendCalls    atomic.Int32
	stickerCalls atomic.Int32
	sendCh       chan struct{}
	stickerCh    chan struct{}
	historyCalls chan int64
}

func (b *sendLagBackend) Send(ctx context.Context, id int64, text string) error {
	b.sendCalls.Add(1)
	if b.sendCh != nil {
		b.sendCh <- struct{}{}
	}
	return b.fakePrivateMessages.Send(ctx, id, text)
}

func (b *sendLagBackend) SendSticker(ctx context.Context, id int64, s netease.PrivateSticker) error {
	b.stickerCalls.Add(1)
	if b.stickerCh != nil {
		b.stickerCh <- struct{}{}
	}
	return b.fakePrivateMessages.SendSticker(ctx, id, s)
}

func (b *sendLagBackend) History(_ context.Context, id, _ int64) (netease.PrivateHistoryPage, error) {
	if b.historyCalls != nil {
		b.historyCalls <- id
	}
	return netease.PrivateHistoryPage{}, nil
}

// A send ACK after switching chats must invalidate stale history and keep refreshSent for switching back
// without stalling the inbox scheduler; post-send History lag preserves local outgoing bubbles without
// auto-resending or falsely confirming unrelated/ambiguous same-text sends, reconciling on subsequent C/H.
func TestPrivateClosureSendACKAndLag(t *testing.T) {
	t.Run("switch-chat-ack-and-rearm", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			a, fake := privateTestApp(t)
			backend := &sendLagBackend{fakePrivateMessages: fake, historyCalls: make(chan int64, 4)}
			a.dm.client = backend
			user := a.dm.contacts[0].User
			other := a.dm.contacts[1].User
			a.openPrivateMessages(&user)
			defer a.closePrivateMessages()
			receivePrivateRequest(t, backend.historyCalls)
			chat := a.dm.chats[user.UserID]
			stale := chat.gen
			chat.draft = "fixture send"
			a.sendPrivateMessage(time.Now())
			a.selectPrivateChat(other)
			receivePrivateRequest(t, backend.historyCalls)
			synctest.Wait()

			in := a.dm.inbox
			a.dm.loading = false
			a.win = &mygo.Window{}
			in.syncAt = time.Now().Add(time.Minute)
			a.privateSendAnswered(a.accountGen, a.dm.epoch, user.UserID, chat.activeOutgoing.key, nil)
			a.privateHistoryAnswered(a.accountGen, a.dm.epoch, user.UserID, stale, 0,
				netease.PrivateHistoryPage{Items: []netease.PrivateMessage{{ID: 42, From: user}}}, nil)

			if chat.loading || chat.sending || chat.gen == stale || !chat.refreshSent || len(chat.messages) != 0 || in.pending != nil {
				t.Fatal("send ACK or stale history mutated the wrong chat or queued unselected background History")
			}
			if in.timer == nil {
				t.Fatal("inbox scheduler timer was not re-armed after send ACK")
			}
			// Returning to the original chat consumes refreshSent and requests History.
			a.selectPrivateChat(user)
			if got := receivePrivateRequest(t, backend.historyCalls); got != user.UserID {
				t.Fatalf("returning to sent chat requested history for %d, want %d", got, user.UserID)
			}
		})
	})

	t.Run("post-send-history-lag-and-same-text-reconciliation", func(t *testing.T) {
		a, fake := privateTestApp(t)
		backend := &sendLagBackend{
			fakePrivateMessages: fake,
			sendCh:              make(chan struct{}, 8),
			stickerCh:           make(chan struct{}, 4),
			historyCalls:        make(chan int64, 16),
		}
		a.dm.client = backend
		user := a.dm.contacts[0].User
		a.openPrivateMessages(&user)
		defer a.closePrivateMessages()
		receivePrivateRequest(t, backend.historyCalls)
		chat, in := a.dm.chats[user.UserID], a.dm.inbox
		self := netease.MessageUser{UserID: a.profile.UserID}
		old := netease.PrivateMessage{ID: 1, Time: 1000, From: user, Raw: `{"msg":"旧消息"}`}
		a.privateHistoryAnswered(a.accountGen, a.dm.epoch, user.UserID, chat.gen, 0,
			netease.PrivateHistoryPage{Items: []netease.PrivateMessage{old}, Before: old.Time, Authoritative: true}, nil)

		// 1) Text send ACK triggers one immediate History check. If that check lags or returns an unrelated
		// cross-device self message, the local bubble remains intact and unconfirmed without fast retries.
		chat.draft = "文本A"
		a.sendPrivateMessage(time.UnixMilli(2000))
		receivePrivateRequest(t, backend.sendCh)
		outA := chat.activeOutgoing
		a.privateSendAnswered(a.accountGen, a.dm.epoch, user.UserID, outA.key, nil)
		receivePrivateRequest(t, backend.historyCalls)

		otherSelf := netease.PrivateMessage{ID: 201, Time: 1500, From: self, Raw: `{"msg":"另一设备文本B"}`}
		a.privateHistoryAnswered(a.accountGen, a.dm.epoch, user.UserID, chat.gen, 0,
			netease.PrivateHistoryPage{Items: []netease.PrivateMessage{old, otherSelf}, Before: old.Time, Authoritative: true}, nil)
		if in.pending != nil || outA.confirmed != nil || outA.failed || len(chat.messages) != 2 {
			t.Fatalf("unrelated self text falsely confirmed outA or queued fast retry: pending=%+v confirmed=%v", in.pending, outA.confirmed)
		}

		// When the next low-frequency Conversations(0) poll advances to @2000, History reconciles outA.
		now := time.Unix(200, 0)
		a.privateInboxAnswered(in, a.accountGen, in.gen, privateInboxChange{},
			netease.PrivateConversationPage{Items: []netease.PrivateConversation{{User: user, Time: 2000, Summary: "文本A"}}},
			netease.PrivateHistoryPage{}, nil, now)
		if in.pending == nil {
			t.Fatal("advanced C@2000 did not queue History")
		}
		req := *in.pending
		a.tickPrivateInbox(now)
		receivePrivateRequest(t, backend.historyCalls)
		msgA := outA.message
		msgA.ID = 202
		a.privateInboxAnswered(in, a.accountGen, in.gen, req, netease.PrivateConversationPage{},
			netease.PrivateHistoryPage{Items: []netease.PrivateMessage{old, otherSelf, msgA}, Before: old.Time, Authoritative: true}, nil, now)
		if outA.confirmed == nil || backend.sendCalls.Load() != 1 {
			t.Fatalf("matching text A did not reconcile or retried Send: confirmed=%v sends=%d", outA.confirmed, backend.sendCalls.Load())
		}

		// 2) Two identical "相同内容" sends both ACK'd before H sees either:
		// H with only 1 server row leaves both unconfirmed (no greedy confirmation) while visual pairing shows 2 bubbles.
		chat.draft = "相同内容"
		a.sendPrivateMessage(time.UnixMilli(3000))
		receivePrivateRequest(t, backend.sendCh)
		same1 := chat.activeOutgoing
		a.privateSendAnswered(a.accountGen, a.dm.epoch, user.UserID, same1.key, nil)
		receivePrivateRequest(t, backend.historyCalls)

		chat.draft = "相同内容"
		a.sendPrivateMessage(time.UnixMilli(3001))
		receivePrivateRequest(t, backend.sendCh)
		same2 := chat.activeOutgoing
		a.privateSendAnswered(a.accountGen, a.dm.epoch, user.UserID, same2.key, nil)
		receivePrivateRequest(t, backend.historyCalls)

		srv1 := same1.message
		srv1.ID, srv1.Time = 301, 3000
		a.privateHistoryAnswered(a.accountGen, a.dm.epoch, user.UserID, chat.gen, 0,
			netease.PrivateHistoryPage{Items: []netease.PrivateMessage{old, otherSelf, msgA, srv1}, Before: old.Time, Authoritative: true}, nil)
		if same1.confirmed != nil || same2.confirmed != nil || backend.sendCalls.Load() != 3 {
			t.Fatalf("first of two identical texts greedily confirmed or retried Send: c1=%v c2=%v sends=%d", same1.confirmed, same2.confirmed, backend.sendCalls.Load())
		}

		// 3) Sticker ACK triggers one immediate History refresh and never auto-resends SendSticker.
		sticker := netease.PrivateSticker{ID: json.Number("71"), Group: "10", Name: "海豹一", URL: "https://p1.music.126.net/s1.png"}
		a.sendPrivateSticker(chat, sticker, time.UnixMilli(4000))
		receivePrivateRequest(t, backend.stickerCh)
		a.privateStickerAnswered(a.accountGen, a.dm.epoch, user.UserID, nil)
		receivePrivateRequest(t, backend.historyCalls)
		if in.pending != nil || backend.stickerCalls.Load() != 1 {
			t.Fatalf("sticker ACK queued background fast retry or retried SendSticker: pending=%+v calls=%d", in.pending, backend.stickerCalls.Load())
		}
	})

	// Regression (P1-3): when a chat already displays A@100 and a confirmed self message S1@200
	// (with contactAt=200), a lagging Authoritative History page (only A@100) returned for the
	// post-send refresh of S2 must not prune S1@200, whereas a genuine manual refresh still can.
	t.Run("post-send-lagging-authoritative-page-preserves-confirmed-tail", func(t *testing.T) {
		a, fake := privateTestApp(t)
		backend := &sendLagBackend{
			fakePrivateMessages: fake,
			sendCh:              make(chan struct{}, 4),
			historyCalls:        make(chan int64, 8),
		}
		a.dm.client = backend
		user := a.dm.contacts[0].User
		a.openPrivateMessages(&user)
		defer a.closePrivateMessages()
		receivePrivateRequest(t, backend.historyCalls)
		chat := a.dm.chats[user.UserID]
		self := netease.MessageUser{UserID: a.profile.UserID}
		msgA := netease.PrivateMessage{ID: 1, Time: 100, From: user, Raw: `{"msg":"对方消息A"}`}
		chat.contactAt = 200
		a.dm.contacts[0].Time = 200
		a.privateHistoryAnswered(a.accountGen, a.dm.epoch, user.UserID, chat.gen, 0,
			netease.PrivateHistoryPage{Items: []netease.PrivateMessage{msgA}, Before: 100, Authoritative: true}, nil)

		// Send S1@200 and confirm it in History.
		outS1 := chat.addOutgoing("第一条自发S1", time.UnixMilli(200), self.UserID)
		msgS1 := outS1.message
		msgS1.ID = 2
		a.privateHistoryAnswered(a.accountGen, a.dm.epoch, user.UserID, chat.gen, 0,
			netease.PrivateHistoryPage{Items: []netease.PrivateMessage{msgA, msgS1}, Before: 100, Authoritative: true}, nil)
		if outS1.confirmed == nil || len(chat.messages) != 2 {
			t.Fatalf("S1@200 did not confirm cleanly: confirmed=%v msgs=%d", outS1.confirmed, len(chat.messages))
		}

		// Send S2@300 -> ACK triggers post-send History refresh, which lags and returns only Authoritative [msgA@100].
		chat.draft = "第二条自发S2"
		a.sendPrivateMessage(time.UnixMilli(300))
		receivePrivateRequest(t, backend.sendCh)
		outS2 := chat.activeOutgoing
		a.privateSendAnswered(a.accountGen, a.dm.epoch, user.UserID, outS2.key, nil)
		receivePrivateRequest(t, backend.historyCalls)

		laggingPage := netease.PrivateHistoryPage{Items: []netease.PrivateMessage{msgA}, Before: 100, Authoritative: true}
		a.privateHistoryAnswered(a.accountGen, a.dm.epoch, user.UserID, chat.gen, 0, laggingPage, nil)
		if len(chat.messages) != 2 || chat.messages[1].ID != msgS1.ID || outS2.confirmed != nil || backend.sendCalls.Load() != 1 {
			t.Fatalf("post-send lagging Authoritative page pruned S1@200 or mis-confirmed S2: msgs=%d s2Confirmed=%v sends=%d",
				len(chat.messages), outS2.confirmed, backend.sendCalls.Load())
		}

		// A subsequent genuine manual refresh returning Authoritative [msgA@100] still prunes deleted S1@200.
		a.loadPrivateHistory(false)
		receivePrivateRequest(t, backend.historyCalls)
		a.privateHistoryAnswered(a.accountGen, a.dm.epoch, user.UserID, chat.gen, 0, laggingPage, nil)
		if len(chat.messages) != 1 || chat.messages[0].ID != msgA.ID {
			t.Fatalf("genuine manual refresh did not prune deleted tail S1@200: msgs=%d", len(chat.messages))
		}
	})
}
