package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

// A footer cover whose URL artwork was evicted used to remain a gray box.
func TestPrivateStickerCoverRecoversAfterEviction(t *testing.T) {
	data, err := os.ReadFile("testdata/sticker-motion.webp")
	if err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write(data)
	}))
	defer cdn.Close()
	a, _ := privateTestApp(t)
	posted := make(chan func(), 1)
	a.art.post = func(fn func()) { posted <- fn }
	now := time.Unix(100, 0)
	a.art.now, a.art.limit = func() time.Time { return now }, 3*len(data)
	url := cdn.URL + "/cover.webp"
	a.dm.stickers.groups = []netease.PrivateStickerGroup{{ID: "2", Name: "一起听"}}
	a.dm.stickers.pages = map[string]netease.PrivateStickerPage{
		"2": {Items: []netease.PrivateSticker{{ID: "42", Group: "2", URL: url}}},
	}
	addBitmap := func(key string) {
		t.Helper()
		bmp, _, err := decodeArt(data)
		if err != nil {
			t.Fatal(err)
		}
		a.art.add(key, &artwork{bmp: bmp, used: now, size: len(data)})
	}
	addBitmap(url)
	if bmp, _ := a.privateStickerTabBitmap("2"); bmp == nil {
		t.Fatal("cover fixture was not cached")
	}
	for i := range 3 {
		now = now.Add(time.Second)
		addBitmap(fmt.Sprintf("other-%d", i))
	}
	if _, cached := a.art.art[url]; cached || len(a.art.gone) == 0 {
		t.Fatal("the cache budget did not evict the cover")
	}
	runtime.GC()
	if a.art.peek(url) != nil {
		t.Fatal("the evicted cover still had a live weak reference")
	}
	shown := true
	tt := ui.NewTester(func(c *ui.Context) {
		if shown {
			a.privateStickerTabs(c, a.inkOn(false))
		}
	}, 350, 80)
	for range 3 {
		tt.Frame()
	}
	if !a.art.busy[url] {
		t.Fatal("the footer did not start recovering its evicted cover")
	}
	select {
	case fn := <-posted:
		fn()
	case <-time.After(2 * time.Second):
		t.Fatal("the available cover did not finish loading")
	}
	tt.Frame()
	for range 2 {
		shown = false
		tt.Frame()
		shown = true
		tt.Frame()
	}
	if bmp, _ := a.privateStickerTabBitmap("2"); bmp == nil || requests.Load() != 1 {
		t.Fatalf("the footer did not recover with one request: %d requests", requests.Load())
	}
}

type privateStickerTestBackend struct {
	*fakePrivateMessages
	sends chan struct {
		peer int64
		item netease.PrivateSticker
	}
	pages       chan [2]string
	groupsAsked chan struct{}
}

func (b *privateStickerTestBackend) StickerGroups(_ context.Context) ([]netease.PrivateStickerGroup, error) {
	if b.groupsAsked != nil {
		b.groupsAsked <- struct{}{}
	}
	return nil, nil
}

func (b *privateStickerTestBackend) StickerPage(_ context.Context, group, cursor string) (netease.PrivateStickerPage, error) {
	b.pages <- [2]string{group, cursor}
	return netease.PrivateStickerPage{}, nil
}

func (b *privateStickerTestBackend) SendSticker(_ context.Context, id int64, sticker netease.PrivateSticker) error {
	b.sends <- struct {
		peer int64
		item netease.PrivateSticker
	}{id, sticker}
	return nil
}

// Closing the picker leaves an inert exit animation on screen before it disappears.
func TestPrivateStickerPickerClosesWithMotion(t *testing.T) {
	a, _ := privateTestApp(t)
	a.settings.GlobalGlass, a.settings.GlassPanels = false, false
	a.openPrivateMessages(&a.dm.contacts[0].User)
	a.dm.stickers.groups = []netease.PrivateStickerGroup{{ID: "1", Name: "自定义表情"}}
	a.dm.stickers.group = "1"
	a.dm.stickers.emoji = true
	tt := ui.NewTester(a.view, 1180, 780)
	privateShowContents(tt)
	tt.Advance(time.Second)
	initial := *tt.Image()
	initial.Pix = append([]byte(nil), initial.Pix...)
	clickAnchor := func() {
		t.Helper()
		if err := tt.Click("网易云表情"); err != nil {
			t.Fatal(err)
		}
	}
	openPicker := func() ui.Rect {
		t.Helper()
		clickAnchor()
		tt.Advance(motionPane.Duration)
		tt.Frame()
		r, ok := tt.Find("选择网易云表情")
		if !ok {
			t.Fatal("the picker did not open")
		}
		return r
	}
	assertClosing := func(r ui.Rect) {
		t.Helper()
		if _, ok := tt.Find("插入 Emoji：😀"); ok || a.dm.stickers.open {
			t.Fatal("closing left the picker interactive")
		}
		x, y := int(r.X+32), int(r.Y+62)
		bg := initial.RGBAAt(x, y)
		if tt.Image().RGBAAt(x, y) == bg {
			t.Fatal("the picker disappeared on the close frame")
		}
		tt.Advance(motionPane.Duration)
		tt.Frame()
		if tt.Image().RGBAAt(x, y) != bg {
			t.Fatal("the picker remained visible after its exit settled")
		}
	}
	r := openPicker()
	tt.Key(0, ui.KeyEscape)
	assertClosing(r)
	r = openPicker()
	clickAnchor()
	assertClosing(r)
	r = openPicker()
	tt.Press(r.X+r.W+30, r.Y+62)
	tt.Release(r.X+r.W+30, r.Y+62)
	assertClosing(r)
}

// Selecting a sticker sends it once without clearing the draft, and the server rows render the actual image.
func TestPrivateStickerPickerAndHistory(t *testing.T) {
	a, fake := privateTestApp(t)
	backend := &privateStickerTestBackend{fakePrivateMessages: fake, pages: make(chan [2]string, 2), sends: make(chan struct {
		peer int64
		item netease.PrivateSticker
	}, 2)}
	a.dm.client = backend
	user := a.dm.contacts[0].User
	a.openPrivateMessages(&user)
	chat := a.dm.chats[user.UserID]
	chat.draft = "保留草稿"
	chat.loading = false
	sticker := netease.PrivateSticker{
		ID: "109951166199016469", Group: "100000000000000003", Name: "合成海豹",
		URL: "https://p1.music.126.net/sticker-fixture.png", Width: 160, Height: 160, Format: "png",
		AlternateURL: "https://p4.music.126.net/sticker-fallback.png",
	}
	ink := color.RGBA{R: 25, G: 194, B: 106, A: 255}
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: ink}, image.Point{}, draw.Src)
	art := &artwork{bmp: ui.NewBitmap(img), used: time.Now()}
	a.art.add(sticker.AlternateURL, art)
	a.art.add(sticker.AlternateURL+"#sticker", art)
	failed := &artwork{failed: true, used: time.Now(), retryAt: time.Now().Add(time.Hour)}
	a.art.add(sticker.URL, failed)
	broken := sticker
	broken.ID, broken.Name, broken.URL, broken.AlternateURL = "11", "合成坏图", "https://p4.music.126.net/sticker-failed.png", ""
	a.art.add(broken.URL, failed)
	previous := sticker
	previous.ID, previous.Name = "12", "合成旧图"
	a.dm.stickers.groups = []netease.PrivateStickerGroup{
		{ID: "1", Name: "自定义表情"}, {ID: sticker.Group, Name: "内置表情"},
	}
	a.dm.stickers.group = "1"
	tt := ui.NewTester(a.view, 1180, 780)
	tt.Frame()
	privateShowContents(tt)
	tt.Advance(time.Second)
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	awaitPage := func(want [2]string) {
		t.Helper()
		select {
		case req := <-backend.pages:
			if req != want {
				t.Fatalf("page request %v, want %v", req, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no sticker page request")
		}
	}
	click("网易云表情")
	tt.Advance(motionPane.Duration)
	tt.Frame()
	checkSize := func() ui.Rect {
		t.Helper()
		r, ok := tt.Find("选择网易云表情")
		if !ok || r.W != 350 || r.H != 310 {
			t.Fatalf("picker changed size: %+v", r)
		}
		return r
	}
	panel := checkSize()
	emoji, haveEmoji := tt.Find("Emoji 分类")
	favorites, haveFavorites := tt.Find("自定义表情")
	inset := emoji.X - panel.X
	if !haveEmoji || !haveFavorites || inset < privateStickerPanelPadding-1 || inset > privateStickerPanelPadding+1 ||
		favorites.X != emoji.X+emoji.W+6 || favorites.W != 40 || favorites.H != 40 {
		t.Fatal("the bottom tabs were not left-aligned and evenly spaced")
	}
	click("内置表情")
	awaitPage([2]string{sticker.Group.String(), ""})
	answerPage := func(cursor string, page netease.PrivateStickerPage) {
		a.privateStickerPageAnswered(a.accountGen, a.dm.epoch, a.dm.stickers.group, cursor, page, nil)
	}
	answerPage("", netease.PrivateStickerPage{Items: []netease.PrivateSticker{previous, broken}, Cursor: "next"})
	tt.Frame()
	click("加载更多表情")
	awaitPage([2]string{sticker.Group.String(), "next"})
	checkSize()
	next := netease.PrivateStickerPage{Cursor: "tail"}
	for i := range 20 {
		item := sticker
		item.ID, item.Name = json.Number(fmt.Sprint(100+i)), ""
		next.Items = append(next.Items, item)
	}
	answerPage("next", next)
	tt.Frame()
	tt.Advance(40 * time.Millisecond)
	title, haveTitle := tt.Find("表情分组：内置表情")
	tile, haveTile := tt.Find("表情：合成旧图")
	tab, haveTab := tt.Find("内置表情")
	if !haveTitle || !haveTile || !haveTab || title.Y+title.H > tile.Y || tile.Y+tile.H > tab.Y {
		t.Fatal("the title, sticker grid and image tabs are out of order")
	}
	if got := tt.Image().RGBAAt(int(tab.X+tab.W/2), int(tab.Y+tab.H/2)); got != ink {
		t.Fatalf("the group tab did not display its actual sticker artwork: %v", got)
	}
	if a.dm.stickers.loading {
		t.Fatal("a full grid paged before scrolling")
	}
	r := checkSize()
	tt.Scroll(r.X+100, r.Y+120, 0, 800)
	tt.Advance(400 * time.Millisecond)
	awaitPage([2]string{sticker.Group.String(), "tail"})
	tt.Frame()
	if a.dm.stickers.scroll.Y <= 0 || !a.dm.stickers.loading || len(backend.pages) != 0 {
		t.Fatal("scrolling failed or triggered another in-flight page")
	}
	a.dm.stickers.scroll.Y = 36
	answerPage("tail", netease.PrivateStickerPage{Cursor: "old-next"})
	tt.Frame()
	if cached := a.dm.stickers.pages[sticker.Group.String()]; len(cached.Items) != 22 || cached.Cursor != "old-next" {
		t.Fatal("the group cache did not retain appended pages and their cursor")
	}
	click("自定义表情")
	awaitPage([2]string{"1", ""})
	if a.dm.stickers.scroll.Y != 0 {
		t.Fatal("switching groups kept the old scroll offset")
	}
	if got := tt.Image().RGBAAt(int(tab.X+tab.W/2), int(tab.Y+tab.H/2)); got != ink {
		t.Fatalf("the group cover was dimmed during loading: %v", got)
	}
	old, visible := tt.Find("表情：合成旧图")
	if !visible || old.H <= 0 {
		t.Fatal("the old grid disappeared before the new first page arrived")
	}
	if got := tt.Image().RGBAAt(int(old.X+old.W/2), int(old.Y+old.H/2)); got != ink {
		t.Fatalf("the old thumbnail was not painted at %+v: got %v, want %v", old, got, ink)
	}
	tt.Press(old.X+old.W/2, old.Y+old.H/2)
	tt.Release(old.X+old.W/2, old.Y+old.H/2)
	if chat.sending || !a.dm.stickers.open || len(backend.sends) != 0 {
		t.Fatal("the retained grid allowed a send during the switch")
	}
	oldPanel := checkSize()
	tt.Scroll(oldPanel.X+100, oldPanel.Y+120, 0, 800)
	tt.Advance(400 * time.Millisecond)
	if a.dm.stickers.scroll.Y <= 0 {
		t.Fatal("the retained grid did not scroll")
	}
	answerPage("", netease.PrivateStickerPage{Items: []netease.PrivateSticker{sticker, broken}})
	tt.Frame()
	checkSize()
	if _, visible := tt.Find("表情：合成旧图"); visible || len(a.dm.stickers.page.Items) != 2 || a.dm.stickers.scroll.Y != 0 {
		t.Fatal("the first page did not replace the old grid and reset its scroll")
	}
	if _, ok := tt.Find("图片加载失败"); !ok {
		t.Fatal("a failed tile stayed blank")
	}
	r, ok := tt.Find("表情：合成海豹")
	if !ok || r.H <= 0 || tt.Image().RGBAAt(int(r.X+r.W/2), int(r.Y+r.H/2)) != ink {
		t.Fatal("failed primary URL did not fall back to the cached picture")
	}
	click("发送表情：合成海豹")
	sticker.URL = sticker.AlternateURL
	select {
	case sent := <-backend.sends:
		if sent.peer != user.UserID || sent.item != sticker {
			t.Fatalf("wrong sticker send: %+v", sent)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no native sticker send")
	}
	if a.dm.stickers.open || !chat.sending || chat.draft != "保留草稿" || len(chat.outgoing) != 0 || len(fake.sent) != 0 {
		t.Fatal("sticker selection altered the draft, text send, or busy state")
	}
	a.sendPrivateSticker(chat, sticker, tt.Now())
	if len(backend.sends) != 0 {
		t.Fatal("in-flight sticker send was repeated")
	}
	a.privateStickerAnswered(a.accountGen, a.dm.epoch, user.UserID, nil)
	if chat.sending || !chat.refreshingSent {
		t.Fatal("confirmation did not trigger the existing history refresh")
	}

	fromWire := func(record map[string]any) netease.PrivateMessage {
		t.Helper()
		raw, _ := json.Marshal(record)
		var msg netease.PrivateMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			t.Fatal(err)
		}
		return msg
	}
	echo := fromWire(map[string]any{
		"id": 7101, "time": 20, "fromUser": netease.MessageUser{UserID: a.profile.UserID},
		"msgType": 1, "msg": fmt.Sprintf(`{"body":{"url":%q,"name":"合成海豹"}}`, sticker.URL),
	})
	incoming := fromWire(map[string]any{
		"id": 7102, "time": 21, "fromUser": user,
		"msg": fmt.Sprintf(`{"picInfo":{"picUrl":%q,"name":"合成来信","emojiId":0}}`, sticker.URL),
	})
	page := netease.PrivateHistoryPage{Items: []netease.PrivateMessage{echo, incoming}}
	a.privateHistoryAnswered(a.accountGen, a.dm.epoch, user.UserID, chat.gen, 0, page, nil)
	entry := chat.arriving[privateMessageID(echo)]
	if entry == nil || len(chat.arriving) != 2 {
		t.Fatal("R23 did not schedule the two server rows")
	}
	tt.Frame()
	tt.Advance(time.Second)
	for _, label := range []string{"表情：合成海豹", "表情：合成来信"} {
		r, ok := tt.Find(label)
		if !ok || r.H <= 0 || tt.Image().RGBAAt(int(r.X+r.W/2), int(r.Y+r.H/2)) != ink {
			t.Fatalf("image was not painted for %s", label)
		}
	}
	a.updatePrivateHistory(user.UserID, 0, page, true, -1)
	if !entry.done || len(chat.arriving) != 0 || len(chat.messages) != 2 {
		t.Fatal("same history replayed a sticker entrance or duplicated its row")
	}
}

// Opening DM warms each first page once and picking an in-flight group shares its request.
func TestPrivateStickerPickerSharesPreload(t *testing.T) {
	a, fake := privateTestApp(t)
	backend := &privateStickerTestBackend{
		fakePrivateMessages: fake,
		pages:               make(chan [2]string, 16),
		groupsAsked:         make(chan struct{}, 4),
	}
	a.dm.client = backend
	user := a.dm.contacts[0].User
	groups := []netease.PrivateStickerGroup{
		{ID: "2", Name: "一起听"}, {ID: "1", Name: "自定义表情"},
		{ID: "3", Name: "另一个分组"}, {ID: "4", Name: "最后一个分组"},
	}
	path, err := filepath.Abs("testdata/sticker-motion.webp")
	if err != nil {
		t.Fatal(err)
	}
	page := netease.PrivateStickerPage{Items: []netease.PrivateSticker{
		{ID: "42", Group: "2", Name: "共享贴图", URL: path},
	}}
	posted := make(chan func(), 4)
	a.art.post = func(fn func()) { posted <- fn }
	tt := ui.NewTester(a.view, 1180, 780)
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	awaitPage := func(id, cursor string) {
		t.Helper()
		select {
		case req := <-backend.pages:
			if req != ([2]string{id, cursor}) {
				t.Fatalf("unexpected sticker request %v, want %s/%s", req, id, cursor)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("no request for group %s", id)
		}
	}
	answer := func(group string, err error) {
		a.privateStickerPageAnswered(a.accountGen, a.dm.epoch, group, "", page, err)
	}
	openMessages := func() {
		a.openPrivateMessages(&user)
		tt.Frame()
		privateShowContents(tt)
		tt.Advance(time.Second)
	}
	openPicker := func() {
		click("网易云表情")
		tt.Advance(motionPane.Duration)
		tt.Frame()
	}
	expectReady := func(group string) {
		t.Helper()
		p := &a.dm.stickers
		r, ok := tt.Find("表情：共享贴图")
		if !ok || p.loading || p.group != group || p.pageGroup != group {
			t.Fatalf("the cached page for group %s was not shown", group)
		}
		if got := tt.Image().RGBAAt(int(r.X+r.W/2), int(r.Y+r.H/2)); got != (color.RGBA{R: 255, A: 255}) {
			t.Fatalf("the cached thumbnail did not paint immediately: %v", got)
		}
	}

	openMessages()
	select {
	case <-backend.groupsAsked:
	case <-time.After(2 * time.Second):
		t.Fatal("opening private messages did not request the sticker directory")
	}
	a.privateStickerGroupsAnswered(a.accountGen, a.dm.epoch, groups, nil)
	awaitPage("2", "")
	answer("2", nil)
	awaitPage("1", "")
	if a.dm.stickers.open || a.dm.stickers.group != "" || !a.art.busy[path] {
		t.Fatal("warmup did not start a thumbnail load independently of the picker")
	}
	select {
	case fn := <-posted:
		fn()
	case <-time.After(2 * time.Second):
		t.Fatal("the preloaded artwork did not finish")
	}
	if bmp, _ := a.privateStickerTabBitmap("2"); bmp == nil {
		t.Fatal("the unvisited group had no ready cover")
	}
	cachedImage := a.art.peek(path)
	// The visible small-emoji tab already has its own cached icon.
	a.art.add(privateSmallEmojiURLs[privateSmallEmojiMarkers[0]], &artwork{bmp: cachedImage.bmp, used: time.Now()})
	openPicker()
	expectReady("2")
	click("自定义表情")
	if p := &a.dm.stickers; p.group != "1" || !p.loading || p.pageGroup != "2" {
		t.Fatal("selecting the in-flight group failed to retain the previous page")
	}
	click("另一个分组")
	awaitPage("3", "")
	a.dm.stickers.scroll.Y = 12
	answer("1", nil)
	if p := &a.dm.stickers; p.group != "3" || p.pageGroup != "2" || !p.loading || p.scroll.Y != 12 {
		t.Fatal("a background reply changed the selected group, grid or scroll")
	}
	answer("3", nil)
	awaitPage("4", "")
	tt.Frame()
	expectReady("3")
	click("Emoji 分类")
	click("自定义表情")
	expectReady("1")
	first := page
	first.Cursor = "tail"
	a.privateStickerPageAnswered(a.accountGen, a.dm.epoch, "4", "", first, nil)
	tt.Frame()
	click("最后一个分组")
	awaitPage("4", "tail")
	if p := &a.dm.stickers; p.group != "4" || p.pageGroup != "4" || !p.loading || p.page.Cursor != "tail" {
		t.Fatal("the selected group did not join its in-flight second page")
	}
	click("自定义表情")
	expectReady("1")

	oldEpoch, oldContext := a.dm.epoch, a.dm.ctx
	a.closePrivateMessages()
	if oldContext.Err() == nil {
		t.Fatal("closing DM did not cancel its prefetch context")
	}
	tt.Frame()
	tt.Advance(time.Second)
	openMessages()
	openPicker()
	expectReady("2")
	click("最后一个分组")
	awaitPage("4", "tail")
	a.privateStickerPageAnswered(a.accountGen, oldEpoch, "4", "tail", netease.PrivateStickerPage{}, nil)
	if p := &a.dm.stickers; !p.pageRequests["4"].pending || p.pages["4"].Cursor != "tail" {
		t.Fatal("a stale reply populated the cache or consumed the resumed request")
	}
	a.privateStickerPageAnswered(a.accountGen, a.dm.epoch, "4", "tail", netease.PrivateStickerPage{}, errors.New("synthetic page failure"))
	if request := a.dm.stickers.pageRequests["4"]; !request.failed || request.pending {
		t.Fatal("a failed preload was queued automatically again")
	}
	tt.Frame()
	expectReady("4")
	click("重试加载表情")
	awaitPage("4", "tail")
	a.privateStickerPageAnswered(a.accountGen, a.dm.epoch, "4", "tail", netease.PrivateStickerPage{}, nil)
	tt.Frame()
	expectReady("4")
	if len(backend.groupsAsked) != 0 || len(backend.pages) != 0 || a.art.peek(path) != cachedImage || len(a.art.busy) != 0 {
		t.Fatal("warmup or reentry duplicated the directory, a page or image request")
	}
	a.resetPrivateMessages()
	if p := &a.dm.stickers; len(p.pages) != 0 || len(p.groups) != 0 || len(p.pageRequests) != 0 || p.groupsLoaded {
		t.Fatal("resetting the account retained its sticker metadata")
	}
}

// Inactive groups warm only their cover until selected, and the active group
// keeps its three-page warmup without requesting page four before a scroll.
func TestPrivateStickerPickerWarmsFullFirstScreen(t *testing.T) {
	data, err := os.ReadFile("testdata/sticker-motion.webp")
	if err != nil {
		t.Fatal(err)
	}
	var imageRequests atomic.Int32
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		imageRequests.Add(1)
		_, _ = w.Write(data)
	}))
	defer cdn.Close()
	a, fake := privateTestApp(t)
	tabCover, _, err := decodeArt(data)
	if err != nil {
		t.Fatal(err)
	}
	// Cache the tab icon so the callback queue counts only the group's images.
	a.art.add(privateSmallEmojiURLs[privateSmallEmojiMarkers[0]], &artwork{bmp: tabCover, used: time.Now()})
	backend := &privateStickerTestBackend{fakePrivateMessages: fake, pages: make(chan [2]string, 16)}
	a.dm.client = backend
	posted := make(chan func(), 64)
	a.art.post = func(fn func()) { posted <- fn }
	a.openPrivateMessages(&a.dm.contacts[0].User)
	a.privateStickerGroupsAnswered(a.accountGen, a.dm.epoch, []netease.PrivateStickerGroup{
		{ID: "2", Name: "一起听"}, {ID: "3", Name: "其他分组"},
	}, nil)
	p := &a.dm.stickers
	awaitPage := func(group, cursor string) {
		t.Helper()
		select {
		case req := <-backend.pages:
			if req != ([2]string{group, cursor}) {
				t.Fatalf("unexpected page request %v, want %s/%s", req, group, cursor)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("no page request for %s/%s", group, cursor)
		}
	}
	makePage := func(group string, start, count int, next string) netease.PrivateStickerPage {
		page := netease.PrivateStickerPage{Cursor: next}
		for i := start; i < start+count; i++ {
			page.Items = append(page.Items, netease.PrivateSticker{
				ID: json.Number(fmt.Sprint(i + 1)), Group: json.Number(group),
				Name:  fmt.Sprintf("%s-%d", group, i+1),
				URL:   fmt.Sprintf("%s/%s/%d.webp", cdn.URL, group, i+1),
				Width: 40, Height: 40,
			})
		}
		return page
	}
	answer := func(group, cursor string, page netease.PrivateStickerPage) {
		a.privateStickerPageAnswered(a.accountGen, a.dm.epoch, group, cursor, page, nil)
	}
	for _, group := range []string{"2", "3"} {
		awaitPage(group, "")
		answer(group, "", makePage(group, 0, 10, "2"))
	}
	fillScreen := func(group string) {
		t.Helper()
		for i := 2; i <= 3; i++ {
			cursor := fmt.Sprint(i)
			awaitPage(group, cursor)
			answer(group, cursor, makePage(group, 10*(i-1), 10, fmt.Sprint(i+1)))
		}
	}
	waitForImages := func(count int) {
		t.Helper()
		for range count {
			select {
			case fn := <-posted:
				fn()
			case <-time.After(2 * time.Second):
				t.Fatal("a first-screen bitmap was never published")
			}
		}
	}
	fillScreen("2")
	waitForImages(31)
	if p.open || p.group != "" || len(p.page.Items) != 0 || len(backend.pages) != 0 {
		t.Fatal("background pages selected a group or exceeded the warmup budget")
	}
	if page := p.pages["2"]; len(page.Items) != 30 || page.Cursor != "4" {
		t.Fatal("the default group did not retain its three pages and cursor")
	}
	for _, sticker := range p.pages["2"].Items {
		if bitmap(a.art.peek(sticker.URL)) == nil {
			t.Fatalf("thumbnail %s was not ready before the picker was built", sticker.Name)
		}
	}
	inactive := p.pages["3"]
	if len(inactive.Items) != 10 || inactive.Cursor != "2" || bitmap(a.art.peek(inactive.Items[0].URL)) == nil ||
		a.art.peek(inactive.Items[1].URL) != nil || len(a.art.busy) != 0 || imageRequests.Load() != 31 {
		t.Fatal("the inactive group did not stay within one cover and one page")
	}
	tt := ui.NewTester(a.view, 1180, 780)
	privateShowContents(tt)
	tt.Advance(time.Second)
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	checkGrid := func(group string, images int32) {
		t.Helper()
		r, ok := tt.Find("表情：" + group + "-21")
		if !ok || p.group != group || p.loading || len(backend.pages) != 0 || imageRequests.Load() != images {
			t.Fatalf("opening group %s did not reuse the ready grid", group)
		}
		if got := tt.Image().RGBAAt(int(r.X+r.W/2), int(r.Y+r.H/2)); got != (color.RGBA{R: 255, A: 255}) {
			t.Fatalf("group %s did not paint an image from its third page: %v", group, got)
		}
	}
	click("网易云表情")
	tt.Advance(motionPane.Duration)
	tt.Frame()
	checkGrid("2", 31)
	click("其他分组")
	fillScreen("3")
	waitForImages(29)
	tt.Frame()
	checkGrid("3", 60)
	panel, _ := tt.Find("选择网易云表情")
	tt.Scroll(panel.X+100, panel.Y+120, 0, 800)
	tt.Advance(400 * time.Millisecond)
	awaitPage("3", "4")
	click("一起听")
	answer("3", "4", makePage("3", 30, 1, ""))
	tt.Frame()
	if p.group != "2" || p.pageGroup != "2" || p.page.Cursor != "4" || p.pages["3"].Cursor != "" || len(p.pages["3"].Items) != 31 {
		t.Fatal("pagination changed the wrong group or lost its cached items")
	}
}
