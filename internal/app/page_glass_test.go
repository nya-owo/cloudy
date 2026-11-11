package app

import (
	"image"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// As the song changes, the cover backgrounds keep the cover before until
// the next is ready, and fade from one to the other, rather than showing
// the plain page between them.
func TestPageGlassKeepsCoverTillNextIsReady(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.motion = motionFull
	cover := func(tint ui.Color) *artwork {
		bmp := ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 16, 16)))
		return &artwork{bmp: bmp, backdrop: bmp, tint: tint, used: time.Now(), size: 1}
	}
	first := cover(ui.RGB(200, 40, 40))
	a.art.add(netease.Sized("https://example.invalid/a.jpg", 600)+"#backdrop", first)
	a.pl.Cue([]netease.Track{
		{ID: 1, Name: "a", Duration: 60000, Album: netease.Album{PicURL: "https://example.invalid/a.jpg"}},
		{ID: 2, Name: "b", Duration: 60000, Album: netease.Album{PicURL: "https://example.invalid/b.jpg"}},
	})
	tt := ui.NewTester(func(c *ui.Context) { a.glassPageBackdrop(ui.Box(c).Fill()) }, 200, 100)
	tt.Frame()
	if a.pageGlass.shown != first {
		t.Fatal("the cover of the song playing is not shown")
	}
	a.pl.Next()
	tt.Frame()
	if a.pageGlass.shown != first {
		t.Fatal("the cover before left before the next was ready")
	}
	second := cover(ui.RGB(40, 40, 200))
	a.art.add(netease.Sized("https://example.invalid/b.jpg", 600)+"#backdrop", second)
	tt.Frame()
	if a.pageGlass.shown != second || a.pageGlass.before != first {
		t.Fatalf("the next cover does not fade over the one before: %+v", a.pageGlass)
	}
}
