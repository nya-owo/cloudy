package app

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

// An open picker with several cached thumbnails leaves the view idle after it settles.
func TestPrivateStickerIdleRepaint(t *testing.T) {
	data, err := os.ReadFile("testdata/sticker-motion.webp")
	if err != nil {
		t.Fatal(err)
	}
	thumbnail, _, err := decodeArt(data)
	if err != nil {
		t.Fatal(err)
	}

	a, _ := privateTestApp(t)
	// The visible small-emoji tab already has an icon in this fixture.
	a.art.add(privateSmallEmojiURLs[privateSmallEmojiMarkers[0]], &artwork{bmp: thumbnail, used: time.Now()})
	a.settings.GlobalGlass, a.settings.GlassPanels = true, true
	user := a.dm.contacts[0].User
	a.openPrivateMessages(&user)
	a.dm.stickers.groups = []netease.PrivateStickerGroup{{ID: "1", Name: "一起听"}}
	a.dm.stickers.group = "1"
	for i := range 10 {
		url := fmt.Sprintf("fixture-%d.webp", i)
		a.art.add(url, &artwork{bmp: thumbnail, used: time.Now()})
		a.dm.stickers.page.Items = append(a.dm.stickers.page.Items, netease.PrivateSticker{
			ID: json.Number(fmt.Sprint(i + 1)), Group: "1", URL: url, Name: fmt.Sprintf("样本%d", i),
		})
	}
	builds := 0
	tt := ui.NewTester(func(c *ui.Context) { builds++; a.view(c) }, 1180, 780)
	tt.Frame()
	privateShowContents(tt)
	if err := tt.Click("网易云表情"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(2 * time.Second)
	if _, ok := tt.Find("表情：样本0"); !ok {
		t.Fatal("the picker did not display its cached thumbnail")
	}

	before := builds
	still := sha256.Sum256(tt.Image().Pix)
	for range 12 {
		tt.Advance(37 * time.Millisecond)
		if sha256.Sum256(tt.Image().Pix) != still {
			t.Fatal("the picker or the surrounding interface changed while idle")
		}
	}
	if builds != before || len(a.art.busy) != 0 {
		t.Fatal("the idle picker rebuilt the view or requested more artwork")
	}
}
