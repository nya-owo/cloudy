package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

// The picker decodes GIF thumbnails once even when their full animations would exceed the cache budget.
func TestPrivateStickerPickerCachesThumbnails(t *testing.T) {
	const stickers = 10
	// These ten GIFs decode to about 95 MiB, beyond the default cache budget.
	var payload bytes.Buffer
	frames := make([]*image.Paletted, 30)
	delays := make([]int, len(frames))
	colors := color.Palette{color.RGBA{R: 255, A: 255}, color.RGBA{B: 255, A: 255}}
	for i := range frames {
		frames[i] = image.NewPaletted(image.Rect(0, 0, 288, 288), colors[i%2:i%2+1])
		delays[i] = 8
	}
	if err := gif.EncodeAll(&payload, &gif.GIF{Image: frames, Delay: delays}); err != nil {
		t.Fatal(err)
	}
	data := payload.Bytes()
	var requests atomic.Int32
	cdn := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Write(data)
	}))
	defer cdn.Close()

	a, _ := privateTestApp(t)
	// The visible small-emoji tab already has an icon in this fixture.
	a.art.add(privateSmallEmojiURLs[privateSmallEmojiMarkers[0]], &artwork{bmp: ui.NewBitmap(frames[0]), used: time.Now()})
	a.settings.GlobalGlass, a.settings.GlassPanels = true, true
	a.openPrivateMessages(&a.dm.contacts[0].User)
	replies := make(chan func(), 3*stickers)
	a.art.post = func(fn func()) { replies <- fn }
	a.dm.stickers.groups = []netease.PrivateStickerGroup{{ID: "1", Name: "一起听"}}
	a.dm.stickers.group = "1"
	for i := range stickers {
		a.dm.stickers.page.Items = append(a.dm.stickers.page.Items, netease.PrivateSticker{
			ID: json.Number(fmt.Sprint(i + 1)), Group: "1", URL: fmt.Sprintf("%s/%d.gif", cdn.URL, i),
		})
	}
	tt := ui.NewTester(a.view, 1180, 780)
	tt.Frame()
	privateShowContents(tt)
	if err := tt.Click("网易云表情"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(500 * time.Millisecond)
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for i := range stickers {
		select {
		case fn := <-replies:
			fn()
		case <-deadline.C:
			t.Fatalf("only %d sticker loads completed", i)
		}
		tt.Frame()
	}
	for _, sticker := range a.dm.stickers.page.Items {
		art := a.art.art[sticker.URL]
		if art == nil || art.bmp == nil || art.sticker != nil {
			t.Fatalf("picker did not cache a still for %s", sticker.URL)
		}
	}
	for range 2 {
		runtime.GC()
		tt.Frame()
		if pending := len(a.art.busy); pending != 0 {
			t.Fatalf("redrawing visible stickers queued %d extra loads", pending)
		}
	}
	if got := requests.Load(); got != stickers {
		t.Fatalf("got %d HTTP requests for %d sticker URLs", got, stickers)
	}
}
