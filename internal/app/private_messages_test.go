package app

import (
	"context"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/audio"
	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

type privateSendCall struct {
	id   int64
	text string
}
type fakePrivateMessages struct{ sent chan privateSendCall }

func (f *fakePrivateMessages) Conversations(context.Context, int) (netease.PrivateConversationPage, error) {
	return netease.PrivateConversationPage{}, nil
}
func (f *fakePrivateMessages) History(context.Context, int64, int64) (netease.PrivateHistoryPage, error) {
	return netease.PrivateHistoryPage{}, nil
}
func (f *fakePrivateMessages) Send(_ context.Context, id int64, text string) error {
	f.sent <- privateSendCall{id, text}
	return nil
}

func (f *fakePrivateMessages) Read(context.Context, int64) error { return nil }

func (f *fakePrivateMessages) StickerGroups(context.Context) ([]netease.PrivateStickerGroup, error) {
	return nil, nil
}
func (f *fakePrivateMessages) StickerPage(context.Context, string, string) (netease.PrivateStickerPage, error) {
	return netease.PrivateStickerPage{}, nil
}
func (f *fakePrivateMessages) SendSticker(context.Context, int64, netease.PrivateSticker) error {
	return nil
}

func privateTestApp(t *testing.T) (*app, *fakePrivateMessages) {
	t.Helper()
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.path = filepath.Join(t.TempDir(), "settings.json")
	a.profile = &netease.Profile{UserID: 90001, Nickname: "演示账号"}
	f := &fakePrivateMessages{sent: make(chan privateSendCall, 8)}
	a.dm = privateMessagesState{loaded: true, client: f, chats: map[int64]*privateChat{}}
	for _, u := range []netease.MessageUser{{UserID: 90002, Nickname: "演示联系人甲"}, {UserID: 90003, Nickname: "演示联系人乙"}} {
		a.dm.contacts = append(a.dm.contacts, netease.PrivateConversation{User: u, Summary: "仅用于离线界面演示", Time: 1794801600000})
		a.dm.chats[u.UserID] = &privateChat{user: u, loaded: true}
	}
	return a, f
}

// The account menu opens a separate pane and Escape closes it without changing settings.
func TestPrivateMessagesMenuAndEscape(t *testing.T) {
	a, _ := privateTestApp(t)
	tt := ui.NewTester(a.view, 1180, 780)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	if err := chooseItem(tt, "演示账号", "私信"); err != nil {
		t.Fatal(err)
	}
	if !a.dm.open || a.accountOpen || a.settingsView.open {
		t.Fatal("menu did not open an independent DM pane")
	}
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.dm.open {
		t.Fatal("Escape did not close DM")
	}
}

// Selecting A→B→A keeps contact rows and names fixed; selected-only borders used to shift their contents.
func TestPrivateContactSelectionKeepsBounds(t *testing.T) {
	a, _ := privateTestApp(t)
	a.openPrivateMessages(nil)
	a.selectPrivateChat(a.dm.contacts[0].User)
	tt := ui.NewTester(a.view, 1180, 780)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	tt.Advance(time.Second)
	want := map[string]ui.Rect{}
	for _, contact := range a.dm.contacts {
		for _, label := range []string{"会话：" + contact.User.Nickname, contact.User.Nickname} {
			r, ok := tt.Find(label)
			if !ok {
				t.Fatalf("missing contact geometry: %s", label)
			}
			want[label] = r
		}
	}
	checkBounds := func() {
		t.Helper()
		for label, before := range want {
			if after, ok := tt.Find(label); !ok || after != before {
				t.Fatalf("contact moved after selection: %s: %+v → %+v", label, before, after)
			}
		}
	}
	b := a.dm.contacts[1].User
	row := want["会话："+b.Nickname]
	// Sample only the row's empty right padding to check background coverage.
	x, y := int(row.X+row.W-4), int(row.Y+row.H/2)
	neutral := tt.Image().RGBAAt(x, y)
	tt.Move(row.X+row.W/2, row.Y+row.H/2)
	tt.Frame()
	checkBounds()
	hover := tt.Image().RGBAAt(x, y)
	if hover == neutral {
		t.Fatal("contact hover lost its background")
	}
	for _, user := range []netease.MessageUser{b, a.dm.contacts[0].User} {
		if err := tt.Click("会话：" + user.Nickname); err != nil {
			t.Fatal(err)
		}
		tt.Move(0, 0)
		tt.Frame()
		if a.dm.selected != user.UserID {
			t.Fatal("contact click did not select its chat")
		}
		checkBounds()
		if user.UserID == b.UserID && tt.Image().RGBAAt(x, y) != hover {
			t.Fatal("selected contact lost its background after pointer exit")
		}
	}
}

// Composer Enter sends once, Shift+Enter breaks a line, and IME Enter leaves the draft unsent.
func TestPrivateMessagesComposerKeys(t *testing.T) {
	a, f := privateTestApp(t)
	a.openPrivateMessages(nil)
	a.selectPrivateChat(a.dm.contacts[0].User)
	out := audio.NewPlayer(&silence{})
	out.SetVolume(0)
	out.Play()
	a.pl.Attach(out)
	defer out.Close()
	tt := ui.NewTester(a.view, 1180, 780)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	if err := tt.Click("私信内容"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Type("测试")
	tt.TypeKey(0, ui.KeySpace, " ")
	tt.Frame()
	chat := a.dm.chats[90002]
	if chat.draft != "测试 " || !a.pl.Playing() {
		t.Fatalf("Space changed playback or lost input: %q", chat.draft)
	}
	tt.Key(ui.Ctrl, ui.KeyEnter)
	tt.Key(ui.Alt, ui.KeyEnter)
	if chat.sending || chat.draft != "测试 " {
		t.Fatal("modified Enter sent or changed the draft")
	}
	tt.Compose("输入法", 3)
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if chat.sending {
		t.Fatal("IME Enter sent a message")
	}
	tt.Compose("", 0)
	tt.Command("selectAll")
	tt.Type("测试 草稿")
	tt.Key(ui.Shift, ui.KeyEnter)
	tt.Frame()
	if chat.sending || chat.draft != "测试 草稿\n" {
		t.Fatalf("Shift+Enter did not insert newline: %q", chat.draft)
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	select {
	case call := <-f.sent:
		if call.id != 90002 || call.text != "测试 草稿\n" {
			t.Fatalf("wrong send: %+v", call)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Enter did not send")
	}
	if chat.draft != "" || !chat.sending || len(chat.outgoing) != 1 || !tt.Focused("私信内容") {
		t.Fatal("send did not immediately move input into a local bubble")
	}
	tt.Type("继续输入")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if chat.draft != "继续输入" || len(chat.outgoing) != 1 {
		t.Fatal("in-flight Enter changed the draft or sent again")
	}
	a.privateSendAnswered(a.accountGen, a.dm.epoch, 90002, chat.outgoing[0].key, errors.New("offline"))
	tt.Frame()
	if chat.draft != "继续输入" || !chat.outgoing[0].failed {
		t.Fatal("failure lost new input or the local failed message")
	}
	if err := tt.Click("发送"); err != nil {
		t.Fatal(err)
	}
	select {
	case call := <-f.sent:
		if call.text != "继续输入" {
			t.Fatalf("button sent %q", call.text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("button did not send")
	}
	a.privateSendAnswered(a.accountGen, a.dm.epoch, 90002, chat.outgoing[1].key, nil)
	tt.Type("  ")
	tt.Key(0, ui.KeyEnter)
	if chat.draft != "  " || chat.sending || len(chat.outgoing) != 2 || !tt.Focused("私信内容") {
		t.Fatal("blank Enter sent, inserted a newline or lost focus")
	}
	a.closePrivateMessages()
	tt.Frame()
	tt.Key(0, ui.KeySpace)
	tt.Frame()
	if a.pl.Playing() {
		t.Fatal("Space did not return to playback after closing DM")
	}
}

// Switching contacts preserves per-chat drafts and sends only once until acknowledged.
func TestPrivateMessagesSwitchAndSend(t *testing.T) {
	a, f := privateTestApp(t)
	a.openPrivateMessages(nil)
	tt := ui.NewTester(a.view, 1180, 780)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	click("会话：演示联系人甲")
	click("私信内容")
	tt.Type("保留的草稿")
	tt.Frame()
	click("会话：演示联系人乙")
	click("私信内容")
	tt.Type("另一份草稿")
	tt.Frame()
	click("会话：演示联系人甲")
	click("发送")
	a.sendPrivateMessage(tt.Now())
	select {
	case call := <-f.sent:
		if call.id != 90002 || call.text != "保留的草稿" {
			t.Fatalf("wrong send %+v", call)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no send")
	}
	select {
	case <-f.sent:
		t.Fatal("duplicate submission")
	default:
	}
	chat := a.dm.chats[90002]
	a.privateSendAnswered(a.accountGen, a.dm.epoch, 90002, chat.outgoing[0].key, errors.New("offline"))
	if chat.draft != "" || chat.sending || !chat.outgoing[0].failed {
		t.Fatal("failed send lost its local text or stayed busy")
	}
	a.privateSendAnswered(a.accountGen, a.dm.epoch, 90002, chat.outgoing[0].key, nil)
	if chat.draft != "" || a.dm.chats[90003].draft != "另一份草稿" {
		t.Fatal("acknowledgement cleared the wrong draft")
	}
}

// A 250 rejection retains the local message without claiming success or starting a retry.
func TestPrivateMessagesRejected250KeepsMessage(t *testing.T) {
	a, f := privateTestApp(t)
	a.openPrivateMessages(nil)
	a.dm.selected = 90002
	chat := a.dm.chats[90002]
	out := chat.addOutgoing("synthetic draft", time.UnixMilli(100), 90001)
	chat.sending, chat.activeOutgoing = true, out
	a.privateSendAnswered(a.accountGen, a.dm.epoch, 90002, out.key, &netease.PrivateMessageStatusError{Code: 250})
	if out.confirmed != nil || chat.sending || !chat.loaded || !out.failed || !strings.Contains(out.detail, "250") || !strings.Contains(out.detail, "服务器拒绝") {
		t.Fatal("250 lost draft, acknowledged delivery or misclassified failure")
	}
	select {
	case <-f.sent:
		t.Fatal("250 started an automatic retry")
	default:
	}
}

// Responses are scoped to a chat, request generation, panel lifetime and account.
func TestPrivateMessagesStaleAndPagination(t *testing.T) {
	a, _ := privateTestApp(t)
	a.openPrivateMessages(nil)
	a.dm.selected = 90003
	account, epoch := a.accountGen, a.dm.epoch
	m1 := netease.PrivateMessage{ID: 1, Time: 100, Raw: `{"type":1,"msg":"虚构旧消息"}`}
	m2 := netease.PrivateMessage{ID: 2, Time: 200, Raw: `{"type":1,"msg":"虚构新消息"}`}
	page := netease.PrivateHistoryPage{Items: []netease.PrivateMessage{m2, m1}, More: true, Before: 100}
	a.dm.chats[90002].gen = 3
	a.privateHistoryAnswered(account, epoch, 90002, 2, 0, page, nil)
	if len(a.dm.chats[90002].messages) != 0 {
		t.Fatal("superseded request applied")
	}
	a.privateHistoryAnswered(account, epoch, 90002, 3, 0, page, nil)
	chat := a.dm.chats[90002]
	if len(chat.messages) != 2 || chat.messages[0].ID != 1 || !chat.more || len(a.dm.chats[90003].messages) != 0 {
		t.Fatal("history wrote into wrong chat or failed ordering")
	}
	a.privateHistoryAnswered(account, epoch, 90002, 3, 100, netease.PrivateHistoryPage{Items: []netease.PrivateMessage{m1}, More: true, Before: 100}, nil)
	if len(chat.messages) != 2 || chat.more {
		t.Fatal("duplicate/non-advancing page admitted")
	}
	chat.draft = "待确认"
	out := chat.addOutgoing("取消中的消息", time.UnixMilli(100), 90001)
	chat.sending, chat.activeOutgoing = true, out
	a.closePrivateMessages()
	a.openPrivateMessages(nil)
	a.privateSendAnswered(account, epoch, 90002, out.key, nil)
	if chat.draft != "待确认" || !out.failed || out.confirmed != nil {
		t.Fatal("closed request cleared pending input")
	}
	a.accountGen++
	a.resetPrivateMessages()
	a.privateContactsAnswered(account, epoch, 0, netease.PrivateConversationPage{Items: []netease.PrivateConversation{{User: netease.MessageUser{UserID: 90004}}}}, nil)
	if len(a.dm.contacts) != 0 || len(a.dm.chats) != 0 || a.dm.open {
		t.Fatal("account change retained private data")
	}
}

// A user page can start a new chat without sending, even without a recent conversation.
func TestPrivateMessagesUserEntry(t *testing.T) {
	a, _ := privateTestApp(t)
	a.route = route{page: pageUser, id: 90004}
	a.users[90004] = &userData{user: &netease.User{UserID: 90004, Nickname: "演示新联系人"}}
	tt := ui.NewTester(a.view, 1180, 780)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	if err := tt.Click("发私信"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.dm.open || a.dm.selected != 90004 || a.dm.chats[90004].sending {
		t.Fatal("user entry did not start a blank chat")
	}
}

// With NM_FRAMES set, renders synthetic data only for visual review.
func TestPrivateMessagesFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	for _, size := range []struct {
		w, h int
		name string
	}{{1180, 780, "private-messages.png"}, {880, 580, "private-messages-compact.png"}} {
		a, _ := privateTestApp(t)
		a.openPrivateMessages(nil)
		a.selectPrivateChat(a.dm.contacts[0].User)
		a.dm.loaded, a.dm.loading = true, false // the synthetic contact request has completed
		chat := a.dm.chats[90002]
		chat.draft = "这是一份未发送的演示草稿。"
		chat.messages = []netease.PrivateMessage{{ID: 1, Time: 1794801600000, From: chat.user, Raw: `{"type":1,"msg":"你好，这里是私信界面的离线演示。"}`}, {ID: 2, Time: 1794801660000, From: netease.MessageUser{UserID: 90001}, Raw: `{"type":1,"msg":"会话、草稿和消息仅保存在本次运行中。"}`}}
		tt := ui.NewTester(a.view, size.w, size.h)
		tt.SetDark(true)
		tt.SetPreferences(ui.Preferences{ReduceMotion: true})
		tt.Frame()
		f, err := os.Create(filepath.Join(dir, size.name))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, tt.Image())
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}
