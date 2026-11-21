package app

import (
	"errors"
	"fmt"
	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
	"strings"
	"testing"
	"time"
)

func privateReadMessage(tt *ui.Tester, chat *privateChat, index int) {
	chat.prepareHistoryRows()
	for i, entry := range chat.historyRows {
		if entry.key.kind == privateHistoryMessage && entry.message == index {
			chat.history.ScrollTo(i, ui.Start)
			tt.Frame()
			return
		}
	}
	panic("test message missing")
}

// Native history keeps its anchor through media growth and returns to the tail after a conversation switch.
func TestPrivatePanelTransitionLayout(t *testing.T) {
	a, _ := privateTestApp(t)
	a.settings.GlassPanels, a.settings.GlobalGlass = false, false
	chat := a.dm.chats[90002]
	cover := "https://p1.music.126.net/layout-fixture.png"
	a.art.add(netease.Sized(cover, 120), &artwork{bmp: ui.NewBitmap(coverTestImage()), used: time.Now(), size: 1})
	a.dm.media = &privateMediaCache{items: map[privateMediaKey]*privateMediaEntry{{"album", 88}: {media: netease.PrivateMessageMedia{Kind: "album", ID: 88, Title: "首条卡片", CoverURL: cover}}}}
	for i := range 80 {
		raw := fmt.Sprintf(`{"msg":"合成消息 %d：固定内容与换行测试。"}`, i)
		if i%7 == 0 {
			raw = `{"album":{"name":"合成卡片","picUrl":"` + cover + `"},"msg":"合成分享"}`
		}
		if i == 0 {
			raw = `{"album":{"id":88},"msg":"首条分享附言"}`
		}
		id := int64(i + 1)
		if i == 1 || i == 2 {
			id = 0
		}
		chat.messages = append(chat.messages, netease.PrivateMessage{ID: id, From: chat.user, Raw: raw, Time: 1794801600000})
	}
	chat.draft, chat.bottom = "保留草稿", true
	tt := ui.NewTester(a.view, 1180, 780)
	tt.SetDark(true)
	tt.Frame()
	a.openPrivateMessages(nil)
	a.selectPrivateChat(chat.user)
	tt.Frame()
	privateShowContents(tt)
	tt.Advance(time.Second)
	tail := chat.messages[len(chat.messages)-1].Content().Text
	if _, ok := tt.Find(tail); !ok || !chat.history.AtEnd() {
		t.Fatal("initial history did not show its visible tail")
	}
	if _, ok := tt.Find(chat.messages[1].Content().Text); ok {
		t.Fatal("virtual history instantiated distant text")
	}
	privateReadMessage(tt, chat, 0)
	tt.Advance(32 * time.Millisecond)
	cardBefore, ok := tt.Find("首条分享附言")
	if !ok {
		t.Fatal("scrolling back lost the media row")
	}
	nextBefore, _ := tt.Find(chat.messages[1].Content().Text)
	mediaTopBefore, _ := tt.Find("分享专辑：首条卡片")
	for _, msg := range chat.messages[1:3] {
		if _, ok := tt.Find(msg.Content().Text); !ok {
			t.Fatal("ID-less rows collided")
		}
	}
	a.dm.media.items[privateMediaKey{"album", 88}].media.Subtitle = strings.Repeat("新到达的艺术家信息 ", 12)
	tt.Frame()
	tt.Advance(32 * time.Millisecond)
	cardAfter, _ := tt.Find("首条分享附言")
	nextAfter, _ := tt.Find(chat.messages[1].Content().Text)
	mediaTopAfter, _ := tt.Find("分享专辑：首条卡片")
	// Added subtitle height moves the caption, while the anchored card's top remains fixed.
	if mediaTopAfter.Y != mediaTopBefore.Y || nextAfter.Y <= nextBefore.Y || cardAfter.Y <= cardBefore.Y {
		t.Fatal("late media metadata did not remeasure the visible row")
	}
	oldAlbum := albumDetail
	defer func() { albumDetail = oldAlbum }()
	played := make(chan int64, 1)
	albumDetail = func(id int64) (*netease.AlbumProfile, error) {
		played <- id
		return nil, errors.New("synthetic offline")
	}
	if err := tt.Click("播放分享专辑"); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-played:
		if id != 88 {
			t.Fatal("restored media played wrong resource")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("restored card lost play action")
	}
	a.privatePlayAnswered(a.dm.play, nil, errors.New("synthetic offline"))
	privateReadMessage(tt, chat, 10)
	tt.Advance(32 * time.Millisecond)
	b := a.dm.chats[90003]
	b.messages = []netease.PrivateMessage{{Raw: `{"msg":"另一会话消息"}`}}
	b.draft = "另一会话草稿"
	a.selectPrivateChat(b.user)
	tt.Frame()
	tt.Advance(240 * time.Millisecond)
	a.selectPrivateChat(chat.user)
	tt.Frame()
	tt.Advance(240 * time.Millisecond)
	if _, ok := tt.Find(tail); !ok || !chat.history.AtEnd() || chat.draft != "保留草稿" || b.draft != "另一会话草稿" {
		t.Fatal("switching chats did not return to the tail with drafts intact")
	}
	if err := tt.Click("私信内容"); err != nil {
		t.Fatal(err)
	}
	a.requestPrivateClose()
	tt.Frame()
	tt.Advance(50 * time.Millisecond)
	if _, ok := tt.Find(tail); !ok || !chat.history.AtEnd() {
		t.Fatal("closing lost the tail")
	}
	a.openPrivateMessages(nil)
	tt.Frame()
	tt.Advance(50 * time.Millisecond)
	if !tt.Focused("私信内容") {
		t.Fatal("quick reversal lost composer focus")
	}
	tt.Type("R15")
	tt.Frame()
	if !strings.Contains(chat.draft, "R15") || len(chat.messages) != 80 {
		t.Fatal("quick reopen changed data or draft")
	}
	tt.SetSize(1020, 740)
	tt.Advance(time.Second)
	if _, ok := tt.Find(tail); !ok || !chat.history.AtEnd() {
		t.Fatal("resize lost tail following")
	}
	a.closePrivateMessages()
	tt.Frame()
	tt.Advance(time.Second)
	if _, ok := tt.Find("私信内容"); ok {
		t.Fatal("closed panel retained input")
	}
}
