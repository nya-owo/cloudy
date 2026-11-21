package app

import (
	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
	"testing"
	"time"
)

// Advance through the real panel completion, next-frame contact mount and chat stagger.
func privateShowContents(tt *ui.Tester) {
	tt.Advance(motionPane.Duration)
	tt.Advance(rowStep)
	tt.Advance(rowStep)
}

// Shell admission, cached replies and closing reversals must not expose stale trees or cancel twice.
func TestPrivateStagedPresentation(t *testing.T) {
	a, _ := privateTestApp(t)
	chat := a.dm.chats[90002]
	chat.messages = []netease.PrivateMessage{{ID: 1, From: chat.user, Raw: `{"msg":"阶段测试正文"}`}}
	chat.draft, chat.bottom = "阶段草稿", true
	tt := ui.NewTester(a.view, 1180, 780)
	a.openPrivateMessages(&chat.user)
	epoch := a.dm.epoch
	tt.Frame()
	tt.Advance(120 * time.Millisecond)
	if a.dm.presentation.contents || !a.dm.messagesIn.pending || !chat.bottom {
		t.Fatal("shell consumed content or scroll intent")
	}
	if _, ok := tt.Find("私信内容"); ok {
		t.Fatal("composer mounted during pane expansion")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.dm.open || a.dm.epoch != epoch+1 {
		t.Fatal("shell Escape did not cancel exactly once")
	}
	a.requestPrivateClose()
	if a.dm.epoch != epoch+1 {
		t.Fatal("duplicate close changed request generation")
	}
	a.privateContactsAnswered(a.accountGen, epoch, 0, netease.PrivateConversationPage{}, nil)
	if len(a.dm.contacts) != 2 {
		t.Fatal("stale close reply replaced cached contacts")
	}
	a.openPrivateMessages(nil)
	tt.Frame()
	privateShowContents(tt)
	if !a.dm.presentation.contents || !a.dm.presentation.chat {
		t.Fatalf("completed pane did not admit content: %+v open=%v", a.dm.presentation, a.dm.open)
	}
	if _, ok := tt.Find("私信内容"); !ok {
		t.Fatal("chat did not mount after contacts")
	}
	at := a.dm.contactsIn.at
	a.privateContactsAnswered(a.accountGen, a.dm.epoch, 0, netease.PrivateConversationPage{Items: a.dm.contacts}, nil)
	tt.Frame()
	if at.IsZero() || a.dm.contactsIn.at != at {
		t.Fatal("late contact refresh replayed cached entrance")
	}
	tt.Advance(time.Second)
	if err := tt.Click("私信内容"); err != nil {
		t.Fatal(err)
	}
	before, _ := tt.Find("私信")
	out := chat.addOutgoing("合成入场", tt.Now(), 90001)
	started := out.at
	a.requestPrivateClose()
	epoch = a.dm.epoch
	tt.Frame()
	tt.Advance(50 * time.Millisecond)
	settings := newTestApp()
	settings.settings = a.settings
	settings.settingsView.open = true
	st := ui.NewTester(func(c *ui.Context) {
		settings.pal = paletteFor(c.Theme())
		settings.motion = settings.motionLevel(c)
		settings.settingsPanel(c)
	}, 1180, 780)
	st.Frame()
	st.Advance(time.Second)
	settings.settingsView.open = false
	st.Frame()
	st.Advance(50 * time.Millisecond)
	want, settingsVisible := st.Find("设置")
	if after, visible := tt.Find("私信"); !visible || !settingsVisible || after.W >= before.W || after != want {
		t.Fatalf("private and settings panes closed differently: private=%+v settings=%+v", after, want)
	}
	if len(chat.outgoing) != 1 || out.at != started {
		t.Fatal("closing discarded an already visible local animation")
	}
	if _, ok := tt.Find("私信内容"); !ok {
		t.Fatal("content vanished before exit fade")
	}
	a.openPrivateMessages(nil)
	tt.Frame()
	tt.Advance(60 * time.Millisecond)
	if !tt.Focused("私信内容") || chat.draft != "阶段草稿" {
		t.Fatal("fade reversal lost focus or draft")
	}
	if err := tt.Click("关闭私信"); err != nil {
		t.Fatal(err)
	}
	if a.dm.epoch != epoch+2 {
		t.Fatal("X performed duplicate cancellation")
	}
	tt.Advance(120 * time.Millisecond)
	if _, ok := tt.Find("私信内容"); !ok {
		t.Fatal("content vanished before the shared pane exit finished")
	}
	tt.Advance(motionPane.Duration)
	if _, ok := tt.Find("私信内容"); ok || a.dm.presentation.contents {
		t.Fatal("the closed pane retained its content")
	}
	a.openPrivateMessages(nil)
	tt.Frame()
	privateShowContents(tt)
	a.resetPrivateMessages()
	tt.Frame()
	if _, ok := tt.Find("阶段测试正文"); ok {
		t.Fatal("forced close retained prior account content")
	}

	b, _ := privateTestApp(t)
	b.dm.contacts = nil
	c := b.dm.chats[90002]
	c.loaded = false
	u := ui.NewTester(b.view, 1180, 780)
	b.openPrivateMessages(&c.user)
	u.Frame()
	privateShowContents(u)
	if len(c.messages) != 0 || !c.history.AtEnd() || !b.dm.messagesIn.pending {
		t.Fatal("empty pending history changed geometry or consumed entrance")
	}
	b.privateHistoryAnswered(b.accountGen, b.dm.epoch, 90002, c.gen, 0, netease.PrivateHistoryPage{Items: chat.messages}, nil)
	u.Frame()
	u.Advance(35 * time.Millisecond)
	if b.dm.messagesIn.pending {
		first, last := c.history.Visible()
		t.Fatalf("first history failed to enter after pending state: pending=%v bottom=%v range=%d..%d end=%v age=%v", b.dm.messagesIn.pending, c.bottom, first, last, c.history.AtEnd(), u.Now().Sub(b.dm.messagesIn.at))
	}
	u.SetPreferences(ui.Preferences{ReduceMotion: true})
	b.closePrivateMessages()
	u.Frame()
	b.openPrivateMessages(nil)
	u.Frame()
	if !b.dm.presentation.chat {
		t.Fatal("ReduceMotion retained staged waiting")
	}
	b.closePrivateMessages()
	b.settings.Motion = "lite"
	u.SetPreferences(ui.Preferences{})
	b.openPrivateMessages(nil)
	u.Frame()
	privateShowContents(u)
	if !b.dm.presentation.chat {
		t.Fatal("Lite did not finish content admission")
	}
	epoch = b.dm.epoch
	u.Press(2, 2)
	u.Release(2, 2)
	u.Frame()
	if b.dm.open || b.dm.epoch != epoch+1 {
		t.Fatal("backdrop did not cancel exactly once")
	}
}
