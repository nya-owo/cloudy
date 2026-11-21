package app

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

// Click feedback and failure transitions keep one local row, editable focus and the same bubble after late confirmation.
func TestPrivateOutgoingInteraction(t *testing.T) {
	a, backend := privateTestApp(t)
	a.openPrivateMessages(nil)
	a.selectPrivateChat(a.dm.contacts[0].User)
	chat := a.dm.chats[90002]
	tt := ui.NewTester(a.view, 1180, 780)
	tt.SetDark(true)
	tt.Frame()
	privateShowContents(tt)
	tt.Advance(time.Second)
	save := func(name string) {
		t.Helper()
		if dir := os.Getenv("CLOUDY_DM_R16_FRAMES"); dir != "" {
			f, err := os.Create(filepath.Join(dir, name+".png"))
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
	if err := tt.Click("私信内容"); err != nil {
		t.Fatal(err)
	}
	tt.Type("合成立即发送")
	tt.Frame()
	save("01-before")
	if err := tt.Click("发送"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	select {
	case <-backend.sent:
	case <-time.After(3 * time.Second):
		t.Fatal("no fake send")
	}
	if len(chat.outgoing) != 1 || len(chat.messages) != 0 || chat.draft != "" || !tt.Focused("私信内容") {
		t.Fatal("click failed to move input immediately into a separate local row with focus")
	}
	out := chat.outgoing[0]
	at := out.at
	if at != tt.Now() {
		t.Fatal("send animation did not start at click clock")
	}
	chat.prepareHistoryRows()
	var key privateHistoryKey
	for _, row := range chat.historyRows {
		if row.outgoing == out {
			key = row.key
		}
	}
	tt.Advance(80 * time.Millisecond)
	save("02-rise-80ms")
	if !tt.FramePending() {
		t.Fatal("unfinished local entrance stopped drawing")
	}
	tt.Advance(200 * time.Millisecond)
	save("03-normal")
	if _, ok := tt.Find("发送失败说明"); ok {
		t.Fatal("normal bubble has failure marker")
	}
	tt.Type("下一份草稿")
	tt.Frame()
	if chat.draft != "下一份草稿" {
		t.Fatal("in-flight send froze composer")
	}
	a.privateSendAnswered(a.accountGen, a.dm.epoch, 90002, out.key, context.DeadlineExceeded)
	tt.Frame()
	if out.failedAt != tt.Now() || out.at != at {
		t.Fatal("failure restarted send animation or lacks transition clock")
	}
	failedAt := out.failedAt
	tt.Advance(80 * time.Millisecond)
	save("04-failure-enter-80ms")
	if !tt.FramePending() {
		t.Fatal("failure transition stopped drawing")
	}
	tt.Advance(200 * time.Millisecond)
	save("05-failure-stable")
	icon, iconOK := tt.Find("发送失败说明")
	caption, captionOK := tt.Find("尚未送达")
	text, textOK := tt.Find("合成立即发送")
	viewport, _ := tt.Find("私信消息列表")
	if !iconOK || !captionOK || !textOK || icon.X <= text.X+text.W || caption.Y <= text.Y || caption.X+caption.W > icon.X || icon.X+icon.W > viewport.X+viewport.W {
		t.Fatalf("failure layout clipped or misplaced: text=%+v icon=%+v caption=%+v viewport=%+v", text, icon, caption, viewport)
	}
	tt.Frame()
	if out.failedAt != failedAt || out.at != at {
		t.Fatal("reconstruction replayed failure/send entrance")
	}
	if err := tt.Click("发送失败说明"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText(out.detail) {
		t.Fatal("failure marker did not explain uncertain result")
	}
	a.toast = ""
	tt.Move(0, 0)
	tt.Advance(6 * time.Second)
	chat.loading = false
	a.loadPrivateHistory(false)
	fresh := netease.PrivateMessage{ID: 71, Time: out.message.Time + 50, From: out.message.From, Raw: out.message.Raw}
	a.privateHistoryAnswered(a.accountGen, a.dm.epoch, 90002, chat.gen, 0, netease.PrivateHistoryPage{Items: []netease.PrivateMessage{fresh}}, nil)
	tt.Frame()
	tt.Advance(60 * time.Millisecond)
	save("06-confirmed-fading-60ms")
	if out.failed || out.confirmed == nil || out.clearedAt.IsZero() || out.at != at {
		t.Fatal("late confirmation restarted/discarded local presentation")
	}
	tt.Advance(120 * time.Millisecond)
	save("07-confirmed-stable")
	if _, ok := tt.Find("发送失败说明"); ok {
		t.Fatal("confirmed marker did not fade away")
	}
	count := 0
	for _, row := range chat.historyRows {
		if row.outgoing == out {
			count++
			if row.key != key {
				t.Fatal("confirmation replaced local list identity")
			}
		}
	}
	if count != 1 || len(chat.messages) != 1 || chat.draft != "下一份草稿" || !chat.history.AtEnd() {
		t.Fatal("confirmation duplicated bubble or lost draft/tail")
	}
	stable := append([]byte(nil), tt.Image().Pix...)
	chat.loading = false
	a.loadPrivateHistory(false)
	a.privateHistoryAnswered(a.accountGen, a.dm.epoch, 90002, chat.gen, 0, netease.PrivateHistoryPage{Items: []netease.PrivateMessage{fresh}}, nil)
	tt.Frame()
	if !bytes.Equal(stable, tt.Image().Pix) {
		t.Fatal("repeated history changed stable confirmed image")
	}
	select {
	case <-backend.sent:
		t.Fatal("failure/history triggered a resend")
	default:
	}
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	chat.draft = "合成即时失败"
	a.sendPrivateMessage(tt.Now())
	select {
	case <-backend.sent:
	case <-time.After(3 * time.Second):
		t.Fatal("no second fake send")
	}
	out = chat.outgoing[1]
	a.privateSendAnswered(a.accountGen, a.dm.epoch, 90002, out.key, &netease.PrivateMessageStatusError{Code: 250})
	tt.Frame()
	save("08-motion-off-failure")
	if _, ok := tt.Find("发送失败说明"); !ok {
		t.Fatal("ReduceMotion delayed failure")
	}
	if out.failedAt != tt.Now() || !out.failed {
		t.Fatal("immediate failure state lost")
	}
	chat.draft = "合成下一条"
	a.sendPrivateMessage(tt.Now())
	tt.Frame()
	below, ok := tt.Find("合成下一条")
	caption, _ = tt.Find("尚未送达")
	if !ok || below.Y <= caption.Y+caption.H {
		t.Fatal("failure caption overlaps next message")
	}
}

// Sequential same/different texts, stale callbacks and closing cannot claim another send or lose local text.
func TestPrivateOutgoingMultipleAndClose(t *testing.T) {
	a, backend := privateTestApp(t)
	a.openPrivateMessages(nil)
	a.selectPrivateChat(a.dm.contacts[0].User)
	chat := a.dm.chats[90002]
	send := func(text string) *privateOutgoing {
		chat.draft = text
		a.sendPrivateMessage(time.UnixMilli(1000 + int64(chat.nextOutgoing)))
		select {
		case <-backend.sent:
		case <-time.After(3 * time.Second):
			t.Fatal("no fake send")
		}
		return chat.activeOutgoing
	}
	first := send("same")
	a.sendPrivateMessage(time.UnixMilli(1000))
	if len(chat.outgoing) != 1 {
		t.Fatal("double click created another local message")
	}
	a.privateSendAnswered(a.accountGen, a.dm.epoch, 90002, first.key, nil)
	second := send("same")
	if second.snapshot == nil {
		t.Fatal("pending confirmation prevented the next Send baseline")
	}
	a.privateSendAnswered(a.accountGen, a.dm.epoch, 90002, first.key, nil)
	if chat.activeOutgoing != second || !chat.sending {
		t.Fatal("duplicate ACK completed a different send")
	}
	a.privateSendAnswered(a.accountGen, a.dm.epoch, 90002, second.key, nil)
	fresh := netease.PrivateMessage{ID: 90, From: first.message.From, Raw: first.message.Raw, Time: 1100}
	a.privateHistoryAnswered(a.accountGen, a.dm.epoch, 90002, chat.gen, 0, netease.PrivateHistoryPage{Items: []netease.PrivateMessage{fresh}}, nil)
	if first.confirmed != nil || second.confirmed != nil {
		t.Fatal("identical pending texts greedily claimed one server row")
	}
	chat.loading = true // another refresh may still be in flight when composing the next text
	third := send("different")
	a.selectPrivateChat(a.dm.contacts[1].User)
	a.selectPrivateChat(chat.user)
	account, epoch := a.accountGen, a.dm.epoch
	a.closePrivateMessages()
	if !third.failed || third.confirmed != nil || chat.sending || len(chat.outgoing) != 3 {
		t.Fatal("close discarded uncertain local text")
	}
	a.openPrivateMessages(nil)
	a.privateSendAnswered(account, epoch, 90002, third.key, nil)
	if !third.failed || chat.activeOutgoing != nil {
		t.Fatal("closed RPC revived after reopen")
	}
	chat.loading = false
	a.loadPrivateHistory(false)
	thirdServer := netease.PrivateMessage{ID: 91, Time: 1200, From: third.message.From, Raw: third.message.Raw}
	a.privateHistoryAnswered(a.accountGen, a.dm.epoch, 90002, chat.gen, 0, netease.PrivateHistoryPage{Items: []netease.PrivateMessage{fresh, thirdServer}}, nil)
	if third.confirmed == nil || *third.confirmed != privateMessageID(thirdServer) || third.failed || first.confirmed != nil || second.confirmed != nil {
		t.Fatal("late distinct send crossed another local identity")
	}
	a.accountGen++
	a.resetPrivateMessages()
	a.privateSendAnswered(account, epoch, 90002, third.key, nil)
	if len(a.dm.chats) != 0 {
		t.Fatal("account reset retained private outgoing data")
	}
	// A later distinct Send may confirm first; neither local key/order may move.
	b, _ := privateTestApp(t)
	b.openPrivateMessages(nil)
	b.selectPrivateChat(b.dm.contacts[0].User)
	c := b.dm.chats[90002]
	one := c.addOutgoing("earlier", time.UnixMilli(1000), 90001)
	two := c.addOutgoing("later", time.UnixMilli(1100), 90001)
	keys := func() []privateHistoryKey {
		c.prepareHistoryRows()
		var result []privateHistoryKey
		for _, row := range c.historyRows {
			result = append(result, row.key)
		}
		return result
	}
	before := keys()
	found := netease.PrivateMessage{ID: 99, Time: 1150, From: two.message.From, Raw: two.message.Raw}
	b.privateHistoryAnswered(b.accountGen, b.dm.epoch, 90002, c.gen, 0, netease.PrivateHistoryPage{Items: []netease.PrivateMessage{found}}, nil)
	if one.confirmed != nil || two.confirmed == nil || !slices.Equal(before, keys()) {
		t.Fatal("later confirmation reordered outstanding local rows")
	}
}

// A local tail entry and later failure/confirmation keep long-history virtualization and a reader's native anchor.
func TestPrivateOutgoingVirtualAnchor(t *testing.T) {
	a, backend := privateTestApp(t)
	a.settings.Motion = "off"
	a.openPrivateMessages(nil)
	a.selectPrivateChat(a.dm.contacts[0].User)
	chat := a.dm.chats[90002]
	for i := range 200 {
		chat.messages = append(chat.messages, netease.PrivateMessage{ID: int64(i + 1), Time: 1794801600000 + int64(i)*1000, From: chat.user, Raw: fmt.Sprintf(`{"msg":"合成锚点 %03d"}`, i)})
	}
	chat.bottom = true
	tt := ui.NewTester(a.view, 1180, 780)
	tt.Frame()
	chat.draft = "虚拟尾部发送"
	a.sendPrivateMessage(tt.Now())
	select {
	case <-backend.sent:
	case <-time.After(3 * time.Second):
		t.Fatal("no fake send")
	}
	tt.Frame()
	first, last := chat.history.Visible()
	if last-first >= 40 || !chat.history.AtEnd() {
		t.Fatal("local entry disabled virtualization or bottom following")
	}
	if _, ok := tt.Find("合成锚点 000"); ok {
		t.Fatal("local send built distant history")
	}
	privateReadMessage(tt, chat, 100)
	tt.Frame()
	anchor, ok := tt.Find("合成锚点 100")
	if !ok || chat.history.AtEnd() {
		t.Fatal("reader failed to leave tail")
	}
	out := chat.outgoing[0]
	a.privateSendAnswered(a.accountGen, a.dm.epoch, 90002, out.key, context.DeadlineExceeded)
	tt.Frame()
	if after, _ := tt.Find("合成锚点 100"); after != anchor {
		t.Fatal("offscreen failure moved reading anchor")
	}
	chat.loading = false
	a.loadPrivateHistory(false)
	fresh := netease.PrivateMessage{ID: 201, Time: out.message.Time, From: out.message.From, Raw: out.message.Raw}
	a.privateHistoryAnswered(a.accountGen, a.dm.epoch, 90002, chat.gen, 0, netease.PrivateHistoryPage{Items: []netease.PrivateMessage{fresh}}, nil)
	tt.Frame()
	if after, _ := tt.Find("合成锚点 100"); after != anchor || chat.history.AtEnd() || out.confirmed == nil || len(chat.messages) != 201 {
		t.Fatal("late confirmation moved reader or lost server history")
	}
}
