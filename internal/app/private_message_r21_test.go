package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

// Ambiguous same-text sends used to add local and server counts instead of pairing display rows one to one.
func TestPrivateRepeatedSendRows(t *testing.T) {
	for _, tc := range []struct {
		name            string
		locals, servers int
		loaded          bool
	}{
		{"two-one", 2, 1, true}, {"two-two", 2, 2, true}, {"unloaded", 1, 1, false}, {"surplus-server", 2, 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			chat := &privateChat{loaded: tc.loaded}
			for i := range tc.locals {
				chat.addOutgoing(" same ", time.UnixMilli(100000+int64(i)), 90001)
			}
			for i := range tc.servers {
				chat.messages = append(chat.messages, netease.PrivateMessage{ID: int64(i + 1), Time: 100100 + int64(i), From: netease.MessageUser{UserID: 90001}, Raw: `{"msg":"same"}`})
			}
			chat.outgoing[0].failed = true
			chat.prepareHistoryRows()
			count := 0
			keys := map[privateHistoryKey]bool{}
			for _, row := range chat.historyRows {
				if row.key.kind == privateHistoryOutgoing || row.key.kind == privateHistoryMessage {
					count++
					if keys[row.key] {
						t.Fatal("duplicate visible identity")
					}
					keys[row.key] = true
				}
			}
			if count != max(tc.locals, tc.servers) || len(chat.messages) != tc.servers || !chat.outgoing[0].failed {
				t.Fatalf("display/history/state count: %d", count)
			}
			for _, out := range chat.outgoing {
				if out.confirmed != nil {
					t.Fatal("display pair forged confirmation")
				}
			}
			extras := []netease.PrivateMessage{
				{ID: 80, Time: 99999, From: netease.MessageUser{UserID: 90001}, Raw: `{"msg":"same"}`},
				{ID: 81, Time: 100100, From: netease.MessageUser{UserID: 90002}, Raw: `{"msg":"same"}`},
				{ID: 82, Time: 140000, From: netease.MessageUser{UserID: 90001}, Raw: `{"msg":"same"}`},
			}
			chat.messages = append(chat.messages, extras...)
			chat.historyRows = nil
			chat.prepareHistoryRows()
			found := map[int64]bool{}
			for _, row := range chat.historyRows {
				if row.key.kind == privateHistoryMessage {
					found[chat.messages[row.message].ID] = true
				}
			}
			for _, msg := range extras {
				if !found[msg.ID] {
					t.Fatal("unrelated real message hidden")
				}
			}
			for key := range keys {
				exists := false
				for _, row := range chat.historyRows {
					exists = exists || row.key == key
				}
				if !exists {
					t.Fatal("growing history changed paired row key")
				}
			}
		})
	}
	// A recent identity already present at click time cannot be counted as a new send.
	chat := &privateChat{loaded: true, messages: []netease.PrivateMessage{{ID: 88, Time: 100100, From: netease.MessageUser{UserID: 90001}, Raw: `{"msg":"same"}`}}}
	chat.addOutgoing("same", time.UnixMilli(100000), 90001)
	chat.prepareHistoryRows()
	count := 0
	for _, row := range chat.historyRows {
		if row.key.kind == privateHistoryOutgoing || row.key.kind == privateHistoryMessage {
			count++
		}
	}
	if count != 2 {
		t.Fatal("baseline same-text message hidden")
	}
}

// Confirmed local rows used to miss the history cascade on reopening; clicking Send still uses its original entrance.
func TestPrivateLocalReopenCascade(t *testing.T) {
	a, backend := privateTestApp(t)
	chat := a.dm.chats[90002]
	chat.messages = []netease.PrivateMessage{{ID: 1, From: chat.user, Raw: `{"msg":"older"}`, Time: 1000}}
	a.openPrivateMessages(&chat.user)
	var oldProgress, localProgress float32
	var out *privateOutgoing
	tt := ui.NewTester(func(c *ui.Context) {
		a.view(c)
		if out != nil {
			oldProgress = a.dm.messagesIn.progress(c, privateMessageID(chat.messages[0]), a.motion)
			localProgress = a.dm.messagesIn.progress(c, privateMessageIdentity{local: out.key}, a.motion)
		}
	}, 1180, 780)
	tt.Frame()
	privateShowContents(tt)
	tt.Advance(time.Second)
	chat.draft = "new"
	if err := tt.Click("发送"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-backend.sent:
	case <-time.After(3 * time.Second):
		t.Fatal("no fake send")
	}
	out = chat.outgoing[0]
	tt.Frame()
	tt.Advance(40 * time.Millisecond)
	tt.Advance(privateBubbleDuration)
	if !out.entrance.done {
		t.Fatal("click entrance failed to finish")
	}
	server := netease.PrivateMessage{ID: 2, Time: out.message.Time + 1, From: out.message.From, Raw: out.message.Raw}
	chat.messages = append(chat.messages, server)
	chat.reconcileOutgoing(90001)
	chat.historyRows = nil
	tt.Frame()
	if out.confirmed == nil {
		t.Fatal("fixture did not confirm")
	}
	a.privateSendAnswered(a.accountGen, a.dm.epoch, chat.user.UserID, out.key, nil)
	at, entrance := out.at, out.entrance.at
	a.requestPrivateClose()
	tt.Frame()
	tt.Advance(time.Second)
	a.openPrivateMessages(nil)
	tt.Frame()
	privateShowContents(tt)
	if a.dm.messagesIn.order[privateMessageIdentity{local: out.key}] != 1 || len(a.dm.messagesIn.order) != 2 {
		t.Fatal("local row missing from ordered history batch")
	}
	tt.Advance(100 * time.Millisecond)
	if oldProgress <= localProgress || localProgress >= 1 {
		t.Fatal("local did not follow older row in cascade")
	}
	tt.Advance(time.Second)
	if localProgress != 1 || out.at != at || out.entrance.at != entrance || len(chat.outgoing) != 1 {
		t.Fatal("reopen replayed sending or lost row")
	}
	select {
	case <-backend.sent:
		t.Fatal("reopen resent")
	default:
	}
}

// Deep contact reopening used to skip animation; preserve the viewport and smooth reversal with draft/focus.
func TestPrivateScrolledContactReopen(t *testing.T) {
	a, _ := privateTestApp(t)
	a.dm.contacts = nil
	for i := range 40 {
		a.dm.contacts = append(a.dm.contacts, netease.PrivateConversation{User: netease.MessageUser{UserID: int64(90002 + i), Nickname: fmt.Sprintf("contact%02d", i)}, Summary: "synthetic"})
	}
	chat := a.dm.chats[90002]
	chat.draft = "keep draft"
	a.openPrivateMessages(&chat.user)
	var viewport ui.Rect
	tt := ui.NewTester(func(c *ui.Context) { a.view(c); viewport = a.dm.listScroll.Bounds(c) }, 1180, 780)
	tt.Frame()
	privateShowContents(tt)
	tt.Advance(time.Second)
	first, _ := tt.Find("会话：contact00")
	tt.Scroll(first.X+20, first.Y+20, 0, 1100)
	tt.Frame()
	tt.Advance(time.Second)
	y := a.dm.listScroll.Y
	if y < 500 {
		t.Fatalf("fixture failed to scroll: %v", y)
	}
	if err := tt.Click("私信内容"); err != nil {
		t.Fatal(err)
	}
	a.requestPrivateClose()
	tt.Frame()
	tt.Advance(40 * time.Millisecond)
	before, _ := tt.Find("私信")
	a.openPrivateMessages(nil)
	tt.Frame()
	tt.Advance(50 * time.Millisecond)
	if after, ok := tt.Find("私信"); !ok || after.W <= before.W || chat.draft != "keep draft" || !tt.Focused("私信内容") {
		t.Fatal("quick reversal did not grow the pane or preserve focus and draft")
	}
	a.requestPrivateClose()
	tt.Frame()
	tt.Advance(time.Second)
	a.openPrivateMessages(nil)
	tt.Frame()
	privateShowContents(tt)
	if a.dm.listScroll.Y != y {
		t.Fatal("reopening did not restore the scrolled contact viewport")
	}
	checkPrivateContactEntrance(t, tt, a, &viewport)
	at := a.dm.contactsIn.at
	tt.Scroll(viewport.X+20, viewport.Y+20, 0, 100)
	tt.Frame()
	if a.dm.contactsIn.at != at {
		t.Fatal("active scrolling restarted entrance")
	}
	a.dm.query = "contact"
	tt.Frame()
	if a.dm.contactsIn.at != at || len(a.dm.contactsIn.order) != 0 {
		t.Fatal("search restarted the entrance")
	}
	a.requestPrivateClose()
	tt.Frame()
	tt.Advance(time.Second)
	a.settings.Motion = "off"
	a.openPrivateMessages(nil)
	tt.Frame()
	if a.dm.listScroll.Y == 0 || len(a.dm.contactsIn.order) != 0 || a.dm.contactsIn.pending {
		t.Fatal("no-motion reopen changed scroll or delayed display")
	}
}
