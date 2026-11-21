package app

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

// Reconciliation accepts only a unique new self identity, never ACK alone or a stale history response.
func TestPrivateOutgoingReconciliation(t *testing.T) {
	for _, scenario := range []string{"id", "fallback", "duplicate-id", "duplicate-fallback", "incoming", "ambiguous", "initial", "pre-ack", "send-error", "history-error", "older", "manual-refresh", "leave", "leave-return", "stale-gen", "stale-account", "stale-epoch"} {
		t.Run(scenario, func(t *testing.T) {
			a, backend := privateTestApp(t)
			a.openPrivateMessages(nil)
			a.selectPrivateChat(a.dm.contacts[0].User)
			chat := a.dm.chats[90002]
			old := netease.PrivateMessage{ID: 11, Time: 100, From: netease.MessageUser{UserID: 90001}, Raw: `{"msg":"合成旧消息"}`}
			fresh := netease.PrivateMessage{ID: 12, Time: 200, From: old.From, Raw: `{"msg":"合成新消息"}`}
			if scenario == "fallback" || scenario == "duplicate-fallback" {
				old.ID, fresh.ID = 0, 0
			}
			if scenario == "duplicate-id" || scenario == "duplicate-fallback" {
				old = fresh
			}
			chat.messages = []netease.PrivateMessage{old}
			chat.draft = "合成新消息"
			if scenario == "initial" {
				chat.loaded = false
			}
			a.sendPrivateMessage(time.UnixMilli(150))
			select {
			case <-backend.sent:
			case <-time.After(3 * time.Second):
				t.Fatal("no fake send")
			}
			out := chat.outgoing[0]
			if out.confirmed != nil || len(chat.messages) != 1 || chat.draft != "" {
				t.Fatal("local send polluted history or retained draft")
			}
			if scenario != "pre-ack" {
				var sendErr error
				if scenario == "send-error" {
					sendErr = errors.New("synthetic timeout")
				}
				a.privateSendAnswered(a.accountGen, a.dm.epoch, 90002, out.key, sendErr)
				if out.confirmed != nil {
					t.Fatal("ACK claimed server identity")
				}
			}
			if scenario == "incoming" {
				fresh.From.UserID = 90002
			}
			items := []netease.PrivateMessage{old, fresh, fresh}
			if scenario == "ambiguous" {
				other := fresh
				other.ID++
				items = append(items, other)
			}
			account, epoch, gen := a.accountGen, a.dm.epoch, chat.gen
			before := int64(0)
			var historyErr error
			switch scenario {
			case "history-error":
				historyErr = errors.New("synthetic offline")
			case "older":
				before = 100
			case "manual-refresh":
				chat.loading = false
				a.loadPrivateHistory(false)
				gen = chat.gen
			case "leave", "leave-return":
				a.selectPrivateChat(a.dm.contacts[1].User)
				if scenario == "leave-return" {
					a.selectPrivateChat(a.dm.contacts[0].User)
				}
			case "stale-gen":
				gen--
			case "stale-account":
				a.accountGen++
			case "stale-epoch":
				a.closePrivateMessages()
			}
			a.privateHistoryAnswered(account, epoch, 90002, gen, before, netease.PrivateHistoryPage{Items: items}, historyErr)
			want := scenario == "id" || scenario == "fallback" || scenario == "pre-ack" || scenario == "send-error" || scenario == "manual-refresh" || scenario == "leave" || scenario == "leave-return"
			if want {
				if out.confirmed == nil || *out.confirmed != privateMessageID(fresh) || out.failed {
					t.Fatal("unique authoritative history did not reconcile")
				}
				chat.prepareHistoryRows()
				count := 0
				for _, row := range chat.historyRows {
					if row.key.kind == privateHistoryOutgoing && row.key.local == out.key {
						count++
					}
				}
				if count != 1 {
					t.Fatal("alias duplicated or lost its local key")
				}
			} else if out.confirmed != nil {
				t.Fatalf("%s incorrectly reconciled", scenario)
			}
		})
	}
}

func checkPrivateVisibleEntrance(t *testing.T, a *app, chat *privateChat) int {
	t.Helper()
	first, last := chat.history.Visible()
	if !chat.history.AtEnd() || last < first || a.dm.messagesIn.pending {
		t.Fatal("the committed tail did not start its entrance")
	}
	count := 0
	for _, entry := range chat.historyRows[first : last+1] {
		if entry.key.kind != privateHistoryMessage {
			continue
		}
		if order, ok := a.dm.messagesIn.order[entry.key.message]; !ok || order != count {
			t.Fatalf("visible message %d skipped its screen order %d", entry.key.message.id, count)
		}
		count++
	}
	if count == 0 || len(a.dm.messagesIn.order) != count {
		t.Fatal("the entrance omitted visible bubbles or included offscreen rows")
	}
	return count
}

// Entering a conversation jumps to its tail and animates the visible bubbles once, while navigation within it preserves the reading position.
func TestPrivateMotionKeepsChatState(t *testing.T) {
	a, _ := privateTestApp(t)
	a.settings.Motion = "full"
	tt := ui.NewTester(a.view, 1600, 1180)
	a.openPrivateMessages(nil)
	a.privateContactsAnswered(a.accountGen, a.dm.epoch, 0, netease.PrivateConversationPage{Items: a.dm.contacts}, nil)
	for i, contact := range a.dm.contacts {
		chat := a.dm.chats[contact.User.UserID]
		chat.draft = fmt.Sprintf("合成草稿%d", i)
		for j := range 100 {
			text := fmt.Sprintf("会话%d 历史%d", i, j)
			if i == 1 {
				text += strings.Repeat(" 长消息换行测试", 30)
			}
			chat.messages = append(chat.messages, netease.PrivateMessage{ID: int64(j + 1), Time: int64(100 + j), From: contact.User, Raw: fmt.Sprintf(`{"msg":%q}`, text)})
		}
	}
	chat, other := a.dm.chats[90002], a.dm.chats[90003]
	chat.more, chat.before = true, 100
	a.selectPrivateChat(chat.user)
	tt.Frame()
	privateShowContents(tt)
	tt.Advance(35 * time.Millisecond)
	tall := checkPrivateVisibleEntrance(t, a, chat)
	if tall <= 8 || a.dm.messagesIn.at.Sub(a.dm.contactsIn.at) < rowStep+80*time.Millisecond {
		t.Fatalf("the large viewport admitted only %d bubbles or skipped the contact lead-in", tall)
	}
	tt.Advance(time.Second)
	privateReadMessage(tt, chat, 65)
	tt.Frame()
	anchorLabel := chat.messages[65].Content().Text
	anchor, ok := tt.Find(anchorLabel)
	if !ok || chat.history.AtEnd() {
		t.Fatal("could not establish a middle reading position")
	}
	staysInMiddle := func() {
		t.Helper()
		tt.Frame()
		if rect, ok := tt.Find(anchorLabel); !ok || rect != anchor || chat.history.AtEnd() ||
			a.dm.messagesIn.pending || len(a.dm.messagesIn.order) != 0 {
			t.Fatal("navigation within the same conversation moved the anchor or replayed entry")
		}
	}
	if err := tt.Click("会话：" + chat.user.Nickname); err != nil {
		t.Fatal(err)
	}
	staysInMiddle()
	chat.loading = false
	a.loadPrivateHistory(false)
	a.privateHistoryAnswered(a.accountGen, a.dm.epoch, chat.user.UserID, chat.gen, 0, netease.PrivateHistoryPage{Items: chat.messages}, nil)
	staysInMiddle()
	a.loadPrivateHistory(true)
	old := netease.PrivateMessage{ID: 101, Time: 99, From: chat.user, Raw: `{"msg":"更早一页"}`}
	a.privateHistoryAnswered(a.accountGen, a.dm.epoch, chat.user.UserID, chat.gen, 100, netease.PrivateHistoryPage{Items: []netease.PrivateMessage{old}, Before: 99}, nil)
	staysInMiddle()

	tt.SetSize(1180, 780)
	tt.Frame()
	if a.dm.messagesIn.pending || len(a.dm.messagesIn.order) != 0 {
		t.Fatal("resize restarted the entrance")
	}
	if err := tt.Click("会话：" + other.user.Nickname); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Advance(35 * time.Millisecond)
	short := checkPrivateVisibleEntrance(t, a, other)
	if short >= tall {
		t.Fatal("the smaller viewport with taller bubbles reused the large batch")
	}
	t.Logf("visible entrance covered %d short bubbles in the large viewport and %d tall bubbles in the small viewport", tall, short)
	if tt.HasText("会话0 历史") {
		t.Fatal("the prior chat was retained after switching")
	}
	if err := tt.Click("会话：" + chat.user.Nickname); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Advance(35 * time.Millisecond)
	checkPrivateVisibleEntrance(t, a, chat)
	if chat.draft != "合成草稿0" || other.draft != "合成草稿1" || len(chat.messages) != 101 {
		t.Fatal("switching discarded a draft or history")
	}
	tt.Advance(time.Second)
	privateReadMessage(tt, chat, 65)
	a.closePrivateMessages()
	tt.Frame()
	a.openPrivateMessages(nil)
	tt.Frame()
	privateShowContents(tt)
	tt.Advance(35 * time.Millisecond)
	checkPrivateVisibleEntrance(t, a, chat)
}

// Send refresh preserves old rows and the reader's anchor; entrance is decided before the first painted message.
func TestPrivateHistoryAndFirstPaintStability(t *testing.T) {
	makeMessages := func(count int, from netease.MessageUser) []netease.PrivateMessage {
		var messages []netease.PrivateMessage
		for i := range count {
			messages = append(messages, netease.PrivateMessage{ID: int64(i + 1), Time: int64(i+1) * 1000, From: from,
				Raw: fmt.Sprintf(`{"msg":"合成历史 %02d"}`, i+1)})
		}
		return messages
	}
	for _, position := range []string{"bottom", "reading", "scroll-during-request", "history-error"} {
		t.Run(position, func(t *testing.T) {
			a, backend := privateTestApp(t)
			a.settings.Motion = "off" // isolate actual layout and scroll from animation phase
			a.openPrivateMessages(nil)
			a.selectPrivateChat(a.dm.contacts[0].User)
			chat := a.dm.chats[90002]
			chat.messages = makeMessages(40, chat.user)
			chat.more, chat.before, chat.bottom = true, 10, true
			chat.loading = false // The cached initial refresh has completed in this fixture.
			chat.draft = "确认后的新消息"
			tt := ui.NewTester(a.view, 1180, 780)
			tt.Frame()
			if position == "reading" {
				privateReadMessage(tt, chat, 10)
				tt.Frame()
			}
			bounds := map[string]ui.Rect{}
			for _, label := range []string{"私信", "查看主页", "私信消息列表", "私信内容"} {
				var ok bool
				bounds[label], ok = tt.Find(label)
				if !ok {
					t.Fatalf("missing layout anchor %s", label)
				}
			}
			checkBounds := func() {
				t.Helper()
				for label, before := range bounds {
					if after, ok := tt.Find(label); !ok || after != before {
						t.Fatalf("Send moved %s: %+v → %+v", label, before, after)
					}
				}
			}
			beforeRequestAnchor, _ := tt.Find("合成历史 10")
			beforeRequestEnd := chat.history.AtEnd()
			a.sendPrivateMessage(tt.Now())
			select {
			case <-backend.sent:
			case <-time.After(3 * time.Second):
				t.Fatal("synthetic send did not run")
			}
			tt.Frame()
			checkBounds()
			// History confirmation should preserve the list image, independent of composer controls.
			historyBox := bounds["私信消息列表"]
			localImage := privateViewPixels(tt, historyBox)
			a.privateSendAnswered(a.accountGen, a.dm.epoch, 90002, chat.outgoing[0].key, nil)
			if !chat.loaded || !chat.refreshingSent || len(chat.messages) != 40 {
				t.Fatal("Send acknowledgement discarded cached history")
			}
			tt.Frame()
			checkBounds()
			if anchor, _ := tt.Find("合成历史 10"); anchor != beforeRequestAnchor || chat.history.AtEnd() != beforeRequestEnd {
				t.Fatal("waiting for send history inserted a row or moved the reading anchor")
			}
			if position == "scroll-during-request" {
				privateReadMessage(tt, chat, 10)
				tt.Frame()
			}
			anchor, _ := tt.Find("合成历史 10")
			wasAtEnd := chat.history.AtEnd()
			fresh := netease.PrivateMessage{ID: 41, Time: 41000, From: netease.MessageUser{UserID: 90001}, Raw: `{"msg":"确认后的新消息"}`}
			page := netease.PrivateHistoryPage{Items: append(append([]netease.PrivateMessage(nil), chat.messages[30:]...), fresh), Before: 31000}
			var historyErr error
			if position == "history-error" {
				historyErr = errors.New("synthetic offline")
			}
			a.privateHistoryAnswered(a.accountGen, a.dm.epoch, 90002, chat.gen, 0, page, historyErr)
			if historyErr == nil {
				if len(chat.messages) != 41 || chat.messages[0].ID != 1 || chat.outgoing[0].confirmed == nil || *chat.outgoing[0].confirmed != privateMessageID(fresh) || !chat.more || chat.before != 10 {
					t.Fatal("send refresh replaced old pages, lost pagination or failed unique confirmation")
				}
			} else if len(chat.messages) != 40 || !chat.refreshSent {
				t.Fatal("failed refresh discarded old messages or forgot the pending refresh")
			}
			tt.Frame()
			checkBounds()
			if position == "reading" || position == "scroll-during-request" {
				if after, _ := tt.Find("合成历史 10"); after != anchor || chat.history.AtEnd() != wasAtEnd {
					t.Fatal("incremental send refresh moved the reader's anchor")
				}
			} else if !chat.history.AtEnd() {
				t.Fatal("bottom reader did not stay at the new bottom")
			}
			if position == "bottom" && !bytes.Equal(localImage, privateViewPixels(tt, historyBox)) {
				t.Fatal("unique confirmation changed the normal local bubble image")
			}
		})
	}
	for _, count := range []int{2, 40, 0} {
		t.Run(fmt.Sprintf("first-paint-%d", count), func(t *testing.T) {
			a, _ := privateTestApp(t)
			a.openPrivateMessages(nil) // contacts deliberately remain pending
			a.selectPrivateChat(a.dm.contacts[0].User)
			chat := a.dm.chats[90002]
			chat.messages = makeMessages(count, chat.user)
			chat.loaded, chat.loading = count != 0, true
			tt := ui.NewTester(a.view, 1180, 780)
			tt.Frame()
			privateShowContents(tt)
			if count == 0 {
				if !a.dm.messagesIn.pending || len(a.dm.messagesIn.order) != 0 {
					t.Fatal("empty loading chat prematurely consumed its entrance")
				}
				a.privateHistoryAnswered(a.accountGen, a.dm.epoch, 90002, chat.gen, 0,
					netease.PrivateHistoryPage{Items: makeMessages(40, chat.user)}, nil)
				tt.Frame()
			}
			tt.Advance(35 * time.Millisecond)
			checkPrivateVisibleEntrance(t, a, chat)
			at := a.dm.messagesIn.at
			tt.Advance(120 * time.Millisecond)
			a.privateContactsAnswered(a.accountGen, a.dm.epoch, 0, netease.PrivateConversationPage{Items: a.dm.contacts}, nil)
			a.privateHistoryAnswered(a.accountGen, a.dm.epoch, 90002, chat.gen, 0,
				netease.PrivateHistoryPage{Items: chat.messages}, nil)
			tt.Frame()
			if a.dm.messagesIn.at != at || a.dm.messagesIn.pending {
				t.Fatal("later contacts/history response reset visible messages to hidden entrance")
			}
		})
	}
}

// A short synthetic smoke exports one row-level sample and reports cost without a hardware-specific performance gate.
func TestPrivateMotionSmoke(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	a, _ := privateTestApp(t)
	for i, contact := range a.dm.contacts {
		chat := a.dm.chats[contact.User.UserID]
		chat.bottom = true
		for j := range 40 {
			chat.messages = append(chat.messages, netease.PrivateMessage{ID: int64(j + 1), From: contact.User,
				Raw: fmt.Sprintf(`{"msg":"合成会话 %d，消息 %d"}`, i+1, j+1)})
		}
	}
	tt := ui.NewTester(a.view, 1180, 780)
	tt.SetDark(true)
	tt.Frame()
	a.openPrivateMessages(nil)
	a.privateContactsAnswered(a.accountGen, a.dm.epoch, 0, netease.PrivateConversationPage{Items: a.dm.contacts}, nil)
	a.selectPrivateChat(a.dm.contacts[0].User)
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	f, err := os.Create(filepath.Join(dir, "dm-row-motion-r9.png"))
	if err != nil {
		t.Fatal(err)
	}
	err = png.Encode(f, tt.Image())
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	for frame := range 60 {
		if frame == 10 || frame == 30 {
			id := int64(90002)
			if frame == 10 {
				id = 90003
			}
			a.selectPrivateChat(a.dm.chats[id].user)
		}
		tt.Advance(time.Second / 60)
	}
	t.Logf("synthetic 60-step sweep: %s (informational, not a 165Hz acceptance result)", time.Since(started))
	if len(a.dm.messagesIn.order) != 0 || len(a.dm.contactsIn.order) != 0 {
		t.Fatal("finished cascades kept their row batches")
	}
}
