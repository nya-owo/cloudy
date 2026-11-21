package app

import (
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// The bar of glass is on by default, for settings kept before it was there,
// and turning it off stays off: dropped from the file, as omitempty would
// have false, it would load as the default again.
func TestLiquidGlassOffIsKept(t *testing.T) {
	s := defaultSettings()
	json.Unmarshal([]byte(`{"quality":"exhigh"}`), &s)
	if !s.LiquidGlass {
		t.Error("settings from before the option turned the glass off")
	}
	s.LiquidGlass = false
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	back := defaultSettings()
	json.Unmarshal(b, &back)
	if back.LiquidGlass {
		t.Error("turning the glass off did not survive being saved")
	}
}

// The capsule leaves the room beside it to the page, whose columns reach to
// the window's edge, and the bar on glass opens Now Playing as the one along
// the foot does.
func TestGlassBarOpensNowPlaying(t *testing.T) {
	a := newTestApp()
	a.settings.LiquidGlass = true
	a.pl.Cue([]netease.Track{{ID: 1, Name: "Echo of You"}})
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	if err := tt.Click("打开正在播放"); err != nil {
		t.Fatal(err)
	}
	if !a.nowPlaying {
		t.Error("the cover on the glass did not open Now Playing")
	}
}

// Paused, the bar of glass asks for no frames: each of them would read and
// blur what is under the pane again.
func TestGlassBarIdle(t *testing.T) {
	a := newTestApp()
	a.settings.LiquidGlass = true
	a.pl.Cue([]netease.Track{{ID: 1, Name: "Echo of You", Duration: 125000}})
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	tt.Advance(2 * time.Second)
	if tt.FramePending() {
		t.Error("the bar of glass keeps drawing while nothing changes")
	}
}

// With NM_FRAMES set, saves the foot of the window with the bar of glass,
// over a page of covers, in the light and in the dark.
func TestGlassBarFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip("set NM_FRAMES to a directory to save frames")
	}
	for _, dark := range []bool{false, true} {
		a := newTestApp()
		a.settings.LiquidGlass = true
		a.settings.Smoke = true
		tr := netease.Track{ID: 1, Name: "Echo of You", Duration: 125000}
		a.pl.Cue([]netease.Track{tr})
		a.pl.Seek(25 * time.Second)
		tt := ui.NewTester(a.view, 1180, 700)
		tt.SetDark(dark)
		tt.Frame()
		img := tt.Image()
		s := float32(img.Bounds().Dx()) / 1180
		name := "glassbar-light.png"
		if dark {
			name = "glassbar-dark.png"
		}
		sub := img.SubImage(image.Rect(0, int(500*s), img.Bounds().Dx(), img.Bounds().Dy()))
		f, _ := os.Create(filepath.Join(dir, name))
		png.Encode(f, sub)
		f.Close()
	}
}

// The bar's own glass is compiled the first time a window shows it on a GPU,
// a panic there: a mistake in it must show here.
func TestBarGlassCompiles(t *testing.T) {
	barGlassShader()
}

// The selection glass used to cover and dim the playlist label beneath it.
func TestSidebarSelectionKeepsTextAboveGlass(t *testing.T) {
	a := newTestApp()
	a.settings.GlobalGlass = true
	lists := []netease.Playlist{{ID: 7, Name: "收藏的歌曲"}}
	a.route = route{page: pagePlaylist, id: lists[0].ID}
	tt := ui.NewTester(func(c *ui.Context) {
		a.pal = paletteFor(c.Theme())
		c.Root().Background(a.pal.bg)
		ui.Column(c).Fill().Children(func() { a.playlistRows(c, lists, true) })
	}, 220, 80)
	tt.SetDark(true)
	tt.Frame()
	img := tt.Image()
	scale := float32(img.Bounds().Dx()) / 220
	peak := uint8(0)
	for y := int(8 * scale); y < int(26*scale); y++ {
		for x := int(42 * scale); x < int(150*scale); x++ {
			peak = max(peak, img.RGBAAt(x, y).R)
		}
	}
	if peak < 225 {
		t.Fatalf("selected playlist text is dimmed by the glass (brightest red: %d)", peak)
	}
}

// The main surface leaves room for the desktop only when its glass is on,
// and a settled glass playback page leaves that room even without the main page.
func TestGlobalGlassWindowCoverage(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.SidebarGlass, a.settings.MainGlass = false, false
	a.pl.Cue([]netease.Track{{ID: 1, Name: "Echo of You", Duration: 125000}})
	tt := ui.NewTester(a.view, 1100, 700)
	tt.SetDark(true)
	tt.SetVibrancy(true)
	coverage := func(wantGlass bool) {
		t.Helper()
		tt.Frame()
		tt.Advance(time.Second)
		img := tt.Image()
		alpha := img.RGBAAt(img.Bounds().Dx()*9/10, img.Bounds().Dy()*3/4).A
		if alpha == 0 || (alpha < 255) != wantGlass {
			t.Fatalf("main region alpha = %d, want glass = %v", alpha, wantGlass)
		}
	}
	coverage(false)
	a.settings.SidebarGlass = true
	coverage(false)
	a.settings.SidebarGlass, a.settings.MainGlass = false, true
	coverage(true)
	a.settings.MainGlass, a.settings.GlobalGlass = false, true
	coverage(true)
	// The two rounded panels used to leave thinner glass at the seam
	// and in all four corners.
	img := tt.Image()
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	seam := int(a.sideWidth(1100) * float32(w) / 1100)
	for _, y := range []int{4, h - 5} {
		want := img.RGBAAt(w/10, y).A
		for _, x := range []int{4, seam - 4, seam + 4, w - 5} {
			if got := img.RGBAAt(x, y).A; got != want {
				t.Errorf("glass coverage at (%d, %d) = %d, want %d", x, y, got, want)
			}
		}
	}
	a.nowPlaying = true
	coverage(true)
}

// Disabling either background must survive reopening even though both default on.
func TestGlassBackgroundOffIsKept(t *testing.T) {
	s := defaultSettings()
	s.SidebarGlass, s.MainGlass = false, false
	s.path = filepath.Join(t.TempDir(), "settings.json")
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	loaded := loadSettingsFrom(s.path)
	if loaded.SidebarGlass || loaded.MainGlass {
		t.Fatal("disabled cover backgrounds were enabled after reopening")
	}
}

// The cached cover background must stop asking for frames once its tint has settled.
func TestGlassBackgroundRests(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	tr := netease.Track{ID: 1, Duration: 120000}
	tr.Album.PicURL = "cached-cover"
	a.pl.Cue([]netease.Track{tr})
	a.art.art[netease.Sized(tr.Album.PicURL, 600)+"#backdrop"] = &artwork{
		backdrop: ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 16, 16))),
		tint:     ui.RGB(90, 120, 210),
	}
	tt := ui.NewTester(func(c *ui.Context) {
		a.pal = paletteFor(c.Theme())
		a.glassPageBackdrop(ui.Box(c).Fill())
	}, 220, 160)
	tt.Frame()
	tt.Advance(backdropFade + time.Second)
	tt.Frame()
	for range 10 {
		tt.Advance(100 * time.Millisecond)
		if tt.FramePending() {
			t.Fatal("static cover background is still asking for frames")
		}
	}
}

// Liquid glass used to barely blur panes and left a near-clear middle, letting
// underlying text clash with settings, menus and floating bars.
func TestLiquidGlassStaysFrosted(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.GlobalGlass = true
	paneBox := ui.Rect{W: 900, H: 680}
	barBox := ui.Rect{W: 900, H: 70}

	for _, dark := range []bool{false, true} {
		for _, tc := range []struct {
			name    string
			surface bool
			box     ui.Rect
			radius  float32
			minBlur float32
			minVeil float32
		}{
			{"pane", true, paneBox, 26, 10, 0.30},
			{"bar", false, barBox, 35, 6, 0.20},
		} {
			var prevBlur, prevVeil float32
			for i, clear := range []float64{0, 0.6, 1} {
				a.settings.GlassClear = clear
				g := a.floatGlass(dark)
				if tc.surface {
					g = a.interfaceGlass(dark)
				}
				opts := g.shaderOptions(tc.box, tc.radius)
				if opts.BackdropBlur < tc.minBlur {
					t.Errorf("%s dark=%v clear=%.1f blur=%v, want >= %v", tc.name, dark, clear, opts.BackdropBlur, tc.minBlur)
				}
				veil := opts.Uniforms["Veil"].(float32)
				if veil < tc.minVeil {
					t.Errorf("%s dark=%v clear=%.1f veil=%v, want >= %v", tc.name, dark, clear, veil, tc.minVeil)
				}
				tone := opts.Uniforms["Tone"].([]float32)
				if dark && tone[1] > 0.55 {
					t.Errorf("%s dark clear=%.1f tone high=%v washes out white text", tc.name, clear, tone[1])
				}
				if !dark && tone[0] < 0.45 {
					t.Errorf("%s light clear=%.1f tone low=%v washes out dark text", tc.name, clear, tone[0])
				}
				if i > 0 && (opts.BackdropBlur >= prevBlur || veil >= prevVeil) {
					t.Errorf("%s dark=%v clear=%.1f did not lighten frost monotonically", tc.name, dark, clear)
				}
				prevBlur, prevVeil = opts.BackdropBlur, veil
			}
		}
	}
}

// Global glass used to ask for VibrancyGlassClear, whose barely-frosted
// backdrop hurt readability on macOS.
func TestGlobalGlassUsesFrostedWindowMaterial(t *testing.T) {
	s := defaultSettings()
	if got := s.mainWindowMaterial(); got != mygo.VibrancySidebar {
		t.Fatalf("default material = %q, want %q", got, mygo.VibrancySidebar)
	}
	s.GlobalGlass = true
	if got := s.mainWindowMaterial(); got != mygo.VibrancyGlass {
		t.Fatalf("global glass material = %q, want %q", got, mygo.VibrancyGlass)
	}
	s.PowerSaver = true
	if got := s.mainWindowMaterial(); got != mygo.VibrancyNone {
		t.Fatalf("power saver material = %q, want %q", got, mygo.VibrancyNone)
	}
}
