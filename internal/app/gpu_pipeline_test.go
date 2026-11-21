package app

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// The GPU notch shader fills the housing body and top flares in black while
// leaving the exterior transparent across compact and expanded sizes.
func TestNotchShaderGeometry(t *testing.T) {
	w, h, flare, foot := float32(220), float32(90), float32(10), float32(20)
	cx := float32(160)
	tt := ui.NewTester(func(c *ui.Context) {
		c.Root().Background(ui.Transparent)
		ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
			paintNotch(p, cx, w, h, flare, foot)
		})
	}, 320, 120)
	tt.Frame()
	img := tt.Image()
	alpha := func(x, y int) uint8 { return img.RGBAAt(x, y).A }
	if a := alpha(int(cx), 20); a != 255 {
		t.Fatalf("housing interior alpha = %d, want 255", a)
	}
	// Just inside the left top flare (between l-flare and l, near y=0).
	if a := alpha(int(cx-w/2-3), 1); a < 200 {
		t.Fatalf("left flare alpha = %d, want opaque", a)
	}
	// Outside the concave curve of the left flare.
	if a := alpha(int(cx-w/2-flare+1), int(flare-1)); a > 20 {
		t.Fatalf("outside flare curve alpha = %d, want transparent", a)
	}
	// Outside the bottom-right rounded corner.
	if a := alpha(int(cx+w/2-1), int(h-1)); a > 20 {
		t.Fatalf("outside rounded foot corner alpha = %d, want transparent", a)
	}
	// Below the housing.
	if a := alpha(int(cx), int(h+5)); a != 0 {
		t.Fatalf("below housing alpha = %d, want 0", a)
	}
	if dir := os.Getenv("NM_FRAMES"); dir != "" {
		savePNG(t, filepath.Join(dir, "gpu-notch.png"), img)
	}
}

// The GPU disc shader draws a rotated cover inside its circular boundary and
// leaves the corners outside the circle transparent.
func TestDiscShaderCoversCircle(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			c := color.RGBA{R: 230, G: 70, B: 90, A: 255}
			if x >= 32 {
				c = color.RGBA{R: 60, G: 170, B: 235, A: 255}
			}
			src.SetRGBA(x, y, c)
		}
	}
	bmp := ui.NewBitmap(src)
	tt := ui.NewTester(func(c *ui.Context) {
		c.Root().Background(ui.Transparent)
		ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
			paintDisc(p, ui.Rect{X: 10, Y: 10, W: 80, H: 80}, bmp, 90, 1)
		})
	}, 100, 100)
	tt.Frame()
	img := tt.Image()
	// Corners of the bounding square lie outside the circle.
	if a := img.RGBAAt(12, 12).A; a != 0 {
		t.Fatalf("corner outside disc alpha = %d, want 0", a)
	}
	// Rotated +90° clockwise: the left red half moves to the top half, right blue half to the bottom.
	top := img.RGBAAt(50, 28)
	bot := img.RGBAAt(50, 72)
	if top.A < 250 || top.R <= top.B {
		t.Fatalf("top of 90°-rotated disc = %+v, want red half", top)
	}
	if bot.A < 250 || bot.B <= bot.R {
		t.Fatalf("bottom of 90°-rotated disc = %+v, want blue half", bot)
	}
	if dir := os.Getenv("NM_FRAMES"); dir != "" {
		savePNG(t, filepath.Join(dir, "gpu-disc.png"), img)
	}
}

// The heart-like burst paints its expanding SDF ring without leaving pixels
// behind once the burst duration has elapsed.
func TestLikeBurstDrawsAndStops(t *testing.T) {
	var ongoing bool
	render := func(elapsed time.Duration) *image.RGBA {
		tt := ui.NewTester(func(c *ui.Context) {
			c.Root().Background(ui.Transparent)
			ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) {
				ongoing = drawBurst(p, r, 24, elapsed, ui.Hex("#ff3b43"))
			})
		}, 120, 120)
		tt.Frame()
		return tt.Image()
	}
	mid := render(120 * time.Millisecond)
	if !ongoing {
		t.Fatal("burst stopped before burstTime")
	}
	lit := 0
	for i := 3; i < len(mid.Pix); i += 4 {
		if mid.Pix[i] > 20 {
			lit++
		}
	}
	if lit < 80 {
		t.Fatalf("mid-burst drew only %d visible pixels", lit)
	}
	if dir := os.Getenv("NM_FRAMES"); dir != "" {
		savePNG(t, filepath.Join(dir, "gpu-burst-early.png"), mid)
		savePNG(t, filepath.Join(dir, "gpu-burst-late.png"), render(360*time.Millisecond))
	}
	done := render(burstTime + time.Millisecond)
	if ongoing {
		t.Fatal("burst still reported ongoing after burstTime")
	}
	for i := 3; i < len(done.Pix); i += 4 {
		if done.Pix[i] != 0 {
			t.Fatal("burst drew pixels after burstTime")
		}
	}
}

// scrollEdgeFade fades the top and bottom edges in a single GPU ColorEffect
// pass while keeping the middle of the scrolled layer opaque.
func TestScrollEdgeFadeSoftensEdges(t *testing.T) {
	tt := ui.NewTester(func(c *ui.Context) {
		c.Root().Background(ui.Transparent)
		ui.Box(c).Absolute().Left(0).Top(0).Size(120, 200).
			Background(ui.RGB(255, 255, 255)).
			Effect(scrollEdgeFade{depth: 40, top: 1, bottom: 1})
	}, 120, 200)
	tt.Frame()
	img := tt.Image()
	top := img.RGBAAt(60, 2).A
	mid := img.RGBAAt(60, 100).A
	bot := img.RGBAAt(60, 197).A
	if mid < 250 {
		t.Fatalf("middle alpha = %d, want opaque", mid)
	}
	if top >= 40 || bot >= 40 {
		t.Fatalf("edges not faded: top=%d bottom=%d", top, bot)
	}
	if dir := os.Getenv("NM_FRAMES"); dir != "" {
		savePNG(t, filepath.Join(dir, "gpu-scrollfade.png"), img)
	}
}

// Opening Settings scales its cached body cleanly while animating and leaves
// its controls interactive once settled.
func TestSettingsDialogScalesAndSettles(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.Motion = "full"
	a.settingsView.open = true
	a.settingsView.tab = settingsAppearance
	tt := ui.NewTester(a.view, 1180, 780)
	tt.Frame()
	tt.Advance(100 * time.Millisecond)
	tt.Frame()
	if dir := os.Getenv("NM_FRAMES"); dir != "" {
		savePNG(t, filepath.Join(dir, "gpu-settings-mid.png"), tt.Image())
	}
	tt.Advance(time.Second)
	tt.Frame()
	if dir := os.Getenv("NM_FRAMES"); dir != "" {
		savePNG(t, filepath.Join(dir, "gpu-settings-settled.png"), tt.Image())
	}
	if err := tt.Click("设置分类：播放"); err != nil {
		t.Fatalf("clicking settings category after settle failed: %v", err)
	}
	tt.Frame()
	if a.settingsView.tab != settingsPlayback {
		t.Fatalf("tab = %v, want settingsPlayback", a.settingsView.tab)
	}
}

// Mid-flight desktop lyrics render visible glyphs via the GPU canvas strip
// and reuse the cached strip across frames without reshaping every frame.
func TestDeskFlyMidFlightRendersAndCaches(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.DeskStyle = "outline"
	a.settings.DeskWords = false
	m := deskTestLine(false)
	f := &deskFlight{
		opening:     true,
		line:        m,
		from:        ui.Rect{X: 700, Y: 260, W: 32, H: 32},
		desk:        ui.Point{X: 40, Y: 10},
		at:          ui.Rect{X: 80, Y: 40, W: 520, H: 45},
		deskOpacity: -1,
	}
	gone := 160 * time.Millisecond
	tt := ui.NewTester(func(c *ui.Context) {
		a.pal = paletteFor(c.Theme())
		a.floatTheme(c)
		ui.Box(c).Fill().Draw(func(p *ui.Painter, _ ui.Rect) {
			a.paintDeskFly(p, f, gone)
		})
	}, 800, 320)
	tt.Frame()
	firstCanvas := f.strip.canvas
	if firstCanvas == nil || !f.strip.ready {
		t.Fatal("mid-flight desktop lyrics did not prepare GPU canvas strip")
	}
	img := tt.Image()
	lit := 0
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] > 20 {
			lit++
		}
	}
	if lit < 200 {
		t.Fatalf("mid-flight desktop lyrics drew only %d visible pixels", lit)
	}
	gone = 220 * time.Millisecond
	tt.Frame()
	if f.strip.canvas != firstCanvas {
		t.Fatal("desktop lyrics recreated GPU canvas strip between frames")
	}
	if dir := os.Getenv("NM_FRAMES"); dir != "" {
		savePNG(t, filepath.Join(dir, "gpu-deskfly-mid.png"), tt.Image())
	}
}

// Flipping the home calendar caches the 3D-tilted card face and still completes
// cleanly onto the next page.
func TestDeckFlipCompletesWithCachedFace(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.Motion = "full"
	accentHome(a)
	a.profile = &netease.Profile{UserID: 1, Nickname: "Test"}
	a.settings.DeckOff = []string{"fm", "radar", "week"}
	tr := netease.Track{ID: 1, Name: "Song", Artists: []netease.Artist{{Name: "Artist"}}, Duration: 180000}
	a.style.loaded = true
	a.style.songs = netease.StyleSongs{
		Picked: netease.StyleCategory{ID: 1, Name: "流行", Tags: []netease.StyleTag{{ID: 1, Name: "R&B"}}},
		Songs:  []netease.Track{tr},
	}
	tt := ui.NewTester(a.view, 1180, 780)
	tt.Advance(time.Second)
	tt.Frame()
	if err := tt.Click("翻到风格推荐"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(100 * time.Millisecond)
	if !a.deck.flipping || a.deck.front != deckStyle {
		t.Fatalf("mid-flip: flipping=%v front=%v gone=%v", a.deck.flipping, a.deck.front, a.deck.gone)
	}
	if dir := os.Getenv("NM_FRAMES"); dir != "" {
		savePNG(t, filepath.Join(dir, "gpu-deck-midflip.png"), tt.Image())
	}
	tt.Advance(time.Second)
	if a.deck.flipping || a.deck.front != deckStyle {
		t.Fatalf("after flip: flipping=%v front=%v, want false and deckStyle", a.deck.flipping, a.deck.front)
	}
}

func savePNG(t *testing.T, path string, img *image.RGBA) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}
