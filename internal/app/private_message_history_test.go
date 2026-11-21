package app

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

// A cold long history builds visible rows, keeps date keys and holds the reader while older pages prepend.
func TestPrivateVirtualHistory(t *testing.T) {
	a, _ := privateTestApp(t)
	a.settings.Motion = "off"
	a.openPrivateMessages(nil)
	a.selectPrivateChat(a.dm.contacts[0].User)
	chat := a.dm.chats[90002]
	chat.more = true
	chat.before = 1794801600000
	chat.bottom = true
	for i := range 200 {
		chat.messages = append(chat.messages, netease.PrivateMessage{ID: int64(i + 1), Time: 1794801600000 + int64(i)*6*60*1000, From: chat.user, Raw: fmt.Sprintf(`{"msg":"长历史消息 %03d"}`, i)})
	}
	tt := ui.NewTester(a.view, 1180, 780)
	tt.Frame()
	first, last := chat.history.Visible()
	if last-first >= 40 || !chat.history.AtEnd() {
		t.Fatal("cold history did not virtualize/follow its tail")
	}
	if _, ok := tt.Find("长历史消息 000"); ok {
		t.Fatal("cold history built the distant head")
	}
	date, ok := tt.Find(privateMessageTime(chat.messages[199].Time))
	tail, tailOK := tt.Find("长历史消息 199")
	if !ok || !tailOK || date.Y >= tail.Y {
		t.Fatal("virtual date divider lost its following message")
	}
	chat.history.ScrollTo(0, ui.Start)
	tt.Frame()
	if err := tt.Click("加载更早消息"); err != nil {
		t.Fatal(err)
	}
	if !chat.loading {
		t.Fatal("older-row action did not request history")
	}
	privateReadMessage(tt, chat, 100)
	before, ok := tt.Find("长历史消息 100")
	if !ok || chat.history.AtEnd() {
		t.Fatal("reader did not leave the bottom")
	}
	page := netease.PrivateHistoryPage{More: true, Before: 1794801600000 - 20*6*60*1000}
	for i := range 20 {
		page.Items = append(page.Items, netease.PrivateMessage{ID: int64(1000 + i), Time: 1794801600000 - int64(20-i)*6*60*1000, From: chat.user, Raw: fmt.Sprintf(`{"msg":"前插旧消息 %d"}`, i)})
	}
	a.privateHistoryAnswered(a.accountGen, a.dm.epoch, 90002, chat.gen, chat.before, page, nil)
	tt.Frame()
	after, ok := tt.Find("长历史消息 100")
	if !ok || after != before || chat.history.AtEnd() || len(chat.messages) != 220 {
		t.Fatal("prepending older history moved the reading anchor")
	}
	chat.history.ScrollToEnd()
	tt.Frame()
	if !chat.history.AtEnd() {
		t.Fatal("returning to the tail did not resume following")
	}
	// Missing-art metadata work is limited to rows the native list actually builds.
	oldLoad := loadPrivateMediaDetail
	defer func() { loadPrivateMediaDetail = oldLoad }()
	loadPrivateMediaDetail = func(kind string, id int64) (netease.PrivateMessageMedia, error) {
		return netease.PrivateMessageMedia{Kind: kind, ID: id}, nil
	}
	b, _ := privateTestApp(t)
	oldDetail := loadPrivateMediaDetail
	defer func() { loadPrivateMediaDetail = oldDetail }()
	loadPrivateMediaDetail = func(kind string, id int64) (netease.PrivateMessageMedia, error) {
		return netease.PrivateMessageMedia{Kind: kind, ID: id, Title: "离线合成详情"}, nil
	}
	b.settings.Motion = "off"
	b.openPrivateMessages(nil)
	b.selectPrivateChat(b.dm.contacts[0].User)
	c := b.dm.chats[90002]
	c.bottom = true
	for i := range 200 {
		c.messages = append(c.messages, netease.PrivateMessage{ID: int64(i + 1), From: c.user, Raw: fmt.Sprintf(`{"album":{"id":%d,"name":"无封面合成专辑"}}`, i+1)})
	}
	u := ui.NewTester(b.view, 1180, 780)
	u.Frame()
	if b.dm.media == nil || len(b.dm.media.items) == 0 || len(b.dm.media.items) >= 40 {
		t.Fatal("offscreen history eagerly requested all media details")
	}
}
