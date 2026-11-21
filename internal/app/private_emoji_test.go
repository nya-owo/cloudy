package app

import (
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

// Emoji tiles insert complete Unicode sequences at the composer selection without sending.
func TestPrivateEmojiPickerInsertsText(t *testing.T) {
	a, fake := privateTestApp(t)
	user := a.dm.contacts[0].User
	a.openPrivateMessages(&user)
	chat := a.dm.chats[user.UserID]
	a.dm.stickers.groups = []netease.PrivateStickerGroup{{ID: "3001", Name: "一起听"}}
	a.dm.stickers.group = "3001"
	chat.draft = "前后"
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
	click("私信内容")
	tt.Key(0, ui.KeyEnd)
	tt.Key(0, ui.KeyLeft)
	click("网易云表情")
	tt.Advance(250 * time.Millisecond)
	click("Emoji 分类")
	if _, ok := tt.Find("表情分组：Emoji"); !ok {
		t.Fatal("the selected Emoji category did not set its title")
	}
	click("插入 Emoji：😀")
	if chat.draft != "前😀后" || len(fake.sent) != 0 || chat.sending || a.dm.stickers.open {
		t.Fatalf("unexpected insertion result %q", chat.draft)
	}
	tt.Type("X")
	if chat.draft != "前😀X后" {
		t.Fatalf("the composer did not retain focus/caret after insertion: %q", chat.draft)
	}

	chooseEmoji := func(glyph string) {
		t.Helper()
		click("网易云表情")
		tt.Advance(250 * time.Millisecond)
		click("插入 Emoji：" + glyph)
	}
	for _, glyph := range []string{"❤️", "👍", "🎵"} {
		chooseEmoji(glyph)
		if !strings.Contains(chat.draft, glyph) || len(fake.sent) != 0 || chat.sending {
			t.Fatalf("Unicode sequence %q was split or sent", glyph)
		}
	}
	raw, err := json.Marshal(map[string]string{"msg": chat.draft})
	if err != nil {
		t.Fatal(err)
	}
	message := netease.PrivateMessage{Raw: string(raw)}
	if message.Text() != chat.draft {
		t.Fatal("received message text did not retain the Unicode sequences")
	}
	tooFull := strings.Repeat("a", 999)
	chat.draft = tooFull
	tt.Frame()
	chooseEmoji("❤️")
	if chat.draft != tooFull || !a.dm.stickers.open {
		t.Fatal("inserting an Emoji exceeded the rune limit or truncated the sequence")
	}
}

// A small emoji tile inserts at the caret and uses the regular text send action.
func TestPrivateSmallEmojiSendsOriginalMarker(t *testing.T) {
	a, backend := privateTestApp(t)
	user := a.dm.contacts[0].User
	a.openPrivateMessages(&user)
	chat := a.dm.chats[user.UserID]
	chat.draft = "前后"
	for _, url := range privateSmallEmojiURLs {
		a.art.add(url, &artwork{bmp: ui.NewBitmap(coverTestImage()), used: time.Now()})
	}
	tt := ui.NewTester(a.view, 1180, 780)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	privateShowContents(tt)
	tt.Advance(time.Second)
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	click("私信内容")
	tt.Key(0, ui.KeyEnd)
	tt.Key(0, ui.KeyLeft)
	click("网易云表情")
	click("多多西西")
	click("插入表情：[西西疑问]")
	if chat.draft != "前[西西疑问]后" || chat.sending || len(backend.sent) != 0 {
		t.Fatalf("tile did not insert the raw marker at the caret: %q", chat.draft)
	}
	tt.Type("!")
	tt.Key(0, ui.KeyEnter)
	select {
	case request := <-backend.sent:
		if request.id != user.UserID || request.text != "前[西西疑问]!后" {
			t.Fatalf("wrong text send request: %+v", request)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("text send did not reach the backend")
	}
}

// Opening the picker requests the cold small emoji tab cover once, without visiting its grid.
func TestPrivateSmallEmojiTabLoadsCoverOnOpen(t *testing.T) {
	fill := color.RGBA{G: 255, A: 255}
	img := image.NewRGBA(image.Rect(0, 0, 26, 26))
	draw.Draw(img, img.Bounds(), &image.Uniform{C: fill}, image.Point{}, draw.Src)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_ = png.Encode(w, img)
	}))
	defer server.Close()
	marker := privateSmallEmojiMarkers[0]
	previous := privateSmallEmojiURLs[marker]
	privateSmallEmojiURLs[marker] = server.URL
	t.Cleanup(func() { privateSmallEmojiURLs[marker] = previous })

	a, _ := privateTestApp(t)
	post := make(chan func(), 1)
	a.art.post = func(fn func()) { post <- fn }
	a.openPrivateMessages(&a.dm.contacts[0].User)
	a.dm.stickers.groups = []netease.PrivateStickerGroup{
		{ID: "2", Name: "多多西西大表情"},
		{ID: "1", Name: "自定义表情"},
		{ID: "3", Name: "一起听"},
	}
	a.dm.stickers.group = "1"
	tt := ui.NewTester(a.view, 1180, 780)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	privateShowContents(tt)
	if len(a.art.busy) != 0 {
		t.Fatal("the closed picker requested artwork")
	}
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	click("网易云表情")
	if len(a.art.busy) != 1 || !a.art.busy[server.URL] {
		t.Fatal("opening the picker did not request just the cold tab cover")
	}
	var previousTab ui.Rect
	for i, label := range []string{"Emoji 分类", "自定义表情", "多多西西", "多多西西大表情", "一起听"} {
		tab, ok := tt.Find(label)
		if !ok || i > 0 && tab.X < previousTab.X+previousTab.W {
			t.Fatalf("tab %q did not follow the previous tab", label)
		}
		previousTab = tab
	}
	select {
	case apply := <-post:
		apply()
	case <-time.After(5 * time.Second):
		t.Fatal("the local cover did not load")
	}
	checkCover := func() {
		t.Helper()
		tt.Frame()
		r, ok := tt.Find("多多西西")
		if !ok || tt.Image().RGBAAt(int(r.X+r.W/2), int(r.Y+r.H/2)) != fill {
			t.Fatal("the fetched image was not drawn in the small emoji tab")
		}
		if requests.Load() != 1 || len(a.art.busy) != 0 {
			t.Fatal("the picker did not reuse the loaded tab cover")
		}
	}
	checkCover()
	click("Emoji 分类")
	click("自定义表情")
	checkCover()
	click("网易云表情")
	click("网易云表情")
	checkCover()
}

// A mixed received message draws both inline images while keeping its text and order.
func TestPrivateSmallEmojiDrawsInlineInReceivedText(t *testing.T) {
	a, _ := privateTestApp(t)
	red, green := color.RGBA{R: 255, A: 255}, color.RGBA{G: 255, A: 255}
	for marker, fill := range map[string]color.RGBA{"[多多大笑]": red, "[西西疑问]": green} {
		img := image.NewRGBA(image.Rect(0, 0, 20, 20))
		draw.Draw(img, img.Bounds(), &image.Uniform{C: fill}, image.Point{}, draw.Src)
		a.art.add(privateSmallEmojiURLs[marker], &artwork{bmp: ui.NewBitmap(img), used: time.Now()})
	}
	const raw = `{"msg":"前😀[多多大笑][普通文字][西西疑问]后"}`
	msg := netease.PrivateMessage{Raw: raw}
	tt := ui.NewTester(func(c *ui.Context) { a.privateMessageBubble(c, msg, false) }, 420, 90)
	frame := tt.Image()
	first := func(fill color.RGBA) image.Point {
		for y := range frame.Bounds().Dy() {
			for x := range frame.Bounds().Dx() {
				if frame.RGBAAt(x, y) == fill {
					return image.Pt(x, y)
				}
			}
		}
		t.Fatalf("missing inline image for %v", fill)
		return image.Point{}
	}
	x, y := first(red), first(green)
	if x.Y != y.Y || y.X-x.X <= 20 || len(a.art.busy) != 0 {
		t.Fatalf("positions %v and %v did not preserve the text between images", x, y)
	}
}
