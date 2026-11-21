package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

// Waiting areas stay empty rather than drawing the former skeleton, regardless of cached or slow data.
func TestPrivateClosureWaiting(t *testing.T) {
	for _, kind := range []string{"no-selection", "cached", "slow"} {
		t.Run(kind, func(t *testing.T) {
			a, _ := privateTestApp(t)
			a.settings.GlassPanels, a.settings.GlobalGlass = false, false
			chat := a.dm.chats[90002]
			if kind == "cached" {
				chat.messages = []netease.PrivateMessage{{ID: 1, Raw: `{"msg":"已有真实消息"}`, From: chat.user}}
			}
			if kind == "slow" {
				a.dm.contacts = nil
				chat.loaded = false
			}
			tt := ui.NewTester(a.view, 1180, 780)
			tt.SetDark(true)
			var user *netease.MessageUser
			if kind != "no-selection" {
				user = &chat.user
			}
			a.openPrivateMessages(user)
			tt.Frame()
			tt.Advance(120 * time.Millisecond)
			if _, ok := tt.Find("私信内容"); ok {
				t.Fatal("content entered before shell completion")
			}
			for step := 0; !a.dm.presentation.contents && step < 20; step++ {
				tt.Advance(17 * time.Millisecond)
			}
			if !a.dm.presentation.contents || a.dm.presentation.chat {
				t.Fatal("missing left-only admission stage")
			}
			if kind == "no-selection" {
				if _, ok := tt.Find("选择一个会话开始聊天"); !ok {
					t.Fatal("no-selection stage showed a fake loading state")
				}
			} else {
				panel, _ := tt.Find("私信")
				privateAssertEmptyArea(t, tt, ui.Rect{X: panel.X + 300, Y: panel.Y + 150, W: panel.W - 350, H: panel.H - 250})
			}
			tt.Advance(time.Second)
			if kind == "slow" {
				history, ok := tt.Find("私信消息列表")
				if !ok {
					t.Fatal("pending chat lost its stable viewport")
				}
				privateAssertEmptyArea(t, tt, history)
				if !chat.history.AtEnd() || !a.dm.messagesIn.pending {
					t.Fatal("waiting changed history or consumed its entrance")
				}
				a.privateHistoryAnswered(a.accountGen, a.dm.epoch, 90002, chat.gen, 0, netease.PrivateHistoryPage{Items: []netease.PrivateMessage{{ID: 1, From: chat.user, Raw: `{"msg":"刚到达的真实消息"}`}}}, nil)
				tt.Frame()
				tt.Advance(35 * time.Millisecond)
				if a.dm.messagesIn.pending || len(chat.messages) != 1 {
					first, last := chat.history.Visible()
					t.Fatalf("first server data did not enter directly: pending=%v bottom=%v range=%d..%d end=%v age=%v", a.dm.messagesIn.pending, chat.bottom, first, last, chat.history.AtEnd(), tt.Now().Sub(a.dm.messagesIn.at))
				}
			}
			if kind == "cached" {
				if _, ok := tt.Find("已有真实消息"); !ok {
					t.Fatal("cached data did not enter directly")
				}
			}
		})
	}
}

func privateAssertEmptyArea(t *testing.T, tt *ui.Tester, r ui.Rect) {
	t.Helper()
	if r.W <= 20 || r.H <= 20 {
		t.Fatal("missing pending-area geometry")
	}
	im := tt.Image()
	bg := im.RGBAAt(int(r.X)+8, int(r.Y)+8)
	for y := int(r.Y) + 8; y < int(r.Y+r.H)-8; y += 9 {
		for x := int(r.X) + 8; x < int(r.X+r.W)-8; x += 9 {
			if im.RGBAAt(x, y) != bg {
				t.Fatal("a placeholder layer covered the empty waiting area")
			}
		}
	}
}

func checkPrivateContactEntrance(t *testing.T, tt *ui.Tester, a *app, viewport *ui.Rect) int {
	t.Helper()
	batch := a.dm.contactsIn.order
	tt.Advance(time.Second) // Match the batch against untransformed on-screen bounds.
	count := 0
	for _, contact := range a.dm.contacts {
		bounds, found := tt.Find("会话：" + contact.User.Nickname)
		visible := found && bounds.H > 0 && bounds.Y < viewport.Y+viewport.H && bounds.Y+bounds.H > viewport.Y
		order, included := batch[privateMessageIdentity{id: contact.User.UserID}]
		if included != visible || included && order != count {
			t.Fatalf("contact %s: visible %t, in entrance %t at %d, next visible index %d", contact.User.Nickname, visible, included, order, count)
		}
		if visible {
			count++
		}
	}
	if count == 0 || count != len(batch) {
		t.Fatalf("entrance had %d contacts for %d visible rows", len(batch), count)
	}
	return count
}

// Every visible contact enters in screen order even when the viewport fits more than fourteen.
func TestPrivateContactEntranceFirstScreen(t *testing.T) {
	for _, height := range []int{1440, 780} {
		t.Run(fmt.Sprintf("height-%d", height), func(t *testing.T) {
			a, _ := privateTestApp(t)
			a.dm.contacts = nil
			for i := range 40 {
				a.dm.contacts = append(a.dm.contacts, netease.PrivateConversation{User: netease.MessageUser{UserID: int64(90002 + i), Nickname: fmt.Sprintf("首屏联系人%02d", i+1)}, Summary: "合成会话"})
			}
			var viewport ui.Rect
			tt := ui.NewTester(func(c *ui.Context) { a.view(c); viewport = a.dm.listScroll.Bounds(c) }, 1180, height)
			a.openPrivateMessages(nil)
			t.Cleanup(a.closePrivateMessages)
			tt.Frame()
			privateShowContents(tt)
			count := checkPrivateContactEntrance(t, tt, a, &viewport)
			if height == 1440 && count <= 14 || height == 780 && count >= 14 {
				t.Fatalf("unexpected viewport batch of %d contacts at height %d", count, height)
			}
			t.Logf("%d visible contacts joined the entrance", count)
		})
	}
}
