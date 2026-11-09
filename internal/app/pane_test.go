package app

import (
	"encoding/json"
	"image"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// The glass of the panes is on by default, for settings kept before it was
// there, and turning it off stays off; the sliders start where the glass
// was tuned.
func TestGlassPanelsOffIsKept(t *testing.T) {
	s := defaultSettings()
	json.Unmarshal([]byte(`{"quality":"exhigh"}`), &s)
	if !s.GlassPanels || s.GlassClear != 0.6 || s.GlassBend != 0.3 || s.GlowLevel != 0.5 {
		t.Errorf("settings from before the options load as %+v", s)
	}
	s.GlassPanels, s.GlassClear = false, 0
	b, _ := json.Marshal(s)
	back := defaultSettings()
	json.Unmarshal(b, &back)
	if back.GlassPanels || back.GlassClear != 0 {
		t.Error("turning the glass down did not survive being saved")
	}
}

// The settings open over the window, and Escape closes them.
func TestSettingsOverTheWindow(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	a.openSettings()
	tt.Frame()
	if _, ok := tt.Find("设置分类：账号"); !ok {
		t.Fatalf("the settings did not open: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEscape)
	tt.Advance(time.Second)
	tt.Frame()
	if _, ok := tt.Find("设置分类：账号"); a.settingsView.open || ok {
		t.Error("Escape did not close the settings")
	}
}

// A menu stays in the window: under what opened it where it fits, and over
// it where it doesn't, as at the foot of a list.
func TestMenuStaysInTheWindow(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	acts := [][]menuAction{{{label: "第一项", do: func() {}}, {label: "第二项", do: func() {}}}}
	for _, y := range []float32{100, 740} {
		a.openMenu(1090, y, y, false, acts, nil)
		tt.Frame()
		r, ok := tt.Find("第二项")
		if !ok {
			t.Fatalf("the menu did not open: %q", tt.Texts())
		}
		if r.X+r.W > 1100 || r.Y+r.H > 760 || r.Y < 0 {
			t.Errorf("opened at y %v, its row is at %+v", y, r)
		}
		a.menu.open = false
		tt.Frame()
	}
}

// A press outside a menu closes it, and the press that closes it on the
// button that opened it doesn't open it again.
func TestMenuClosesOnPressOutside(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.pl.Cue([]netease.Track{{ID: 1, Name: "宝船", Duration: 200000}})
	a.pl.SetLyrics(nil)
	a.nowPlaying = true
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Advance(time.Second)
	tt.Frame()
	if err := tt.Click("更多"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.menu.open {
		t.Fatal("the dots did not open the menu")
	}
	if err := tt.Click("更多"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.menu.open {
		t.Error("a second click on the dots left the menu open")
	}
}

// Now Playing grows out of the bar over a moment, and folds back into it, and
// at rest it covers the window; with motion off it is there at once.
func TestNowPlayingGrowsFromTheBar(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.pl.Cue([]netease.Track{{ID: 1, Name: "宝船", Duration: 200000}})
	a.pl.SetLyrics(nil)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	a.nowPlaying = true
	tt.Frame()
	tt.Advance(120 * time.Millisecond)
	tt.Frame()
	if a.npK <= 0 || a.npK >= 1 {
		t.Errorf("a moment in, it is %v of the way open", a.npK)
	}
	tt.Advance(time.Second)
	tt.Frame()
	if a.npK != 1 {
		t.Errorf("at rest, it is %v of the way open", a.npK)
	}
	a.nowPlaying = false
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if a.npK != 0 {
		t.Errorf("closed, it is %v of the way open", a.npK)
	}
	a.settings.Motion = "off"
	a.nowPlaying = true
	tt.Frame()
	tt.Frame()
	if a.npK != 1 {
		t.Errorf("with motion off, it took its time: %v", a.npK)
	}
}

// A cover flies from its card to its page and lands, and doesn't hang there
// for a page that takes its time to load.
func TestCoverFlight(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.playlists[7] = &netease.Playlist{ID: 7, Name: "钢琴曲", CoverURL: "https://example.com/7.jpg"}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	a.flight = coverFlight{url: a.playlists[7].Cover(), from: ui.Rect{X: 300, Y: 300, W: 200, H: 200}, asked: tt.Now()}
	a.route = route{page: pagePlaylist, id: 7}
	tt.Frame()
	tt.Frame()
	if a.flight.to.W == 0 {
		t.Fatal("the cover found no place to land")
	}
	tt.Advance(time.Second)
	tt.Frame()
	if a.flight.url != "" {
		t.Error("the cover did not land")
	}
	a.route = route{page: pageCharts}
	tt.Frame()
	a.flight = coverFlight{url: "nowhere", from: ui.Rect{W: 100, H: 100}, asked: tt.Now()}
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if a.flight.url != "" {
		t.Error("the cover hangs over a page that didn't come")
	}
}

// A page says which way it was reached, for the way it comes in: deeper,
// back, or across the sidebar's.
func TestPageDirections(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.open(route{page: pageCharts})
	if a.pageDir != 0 {
		t.Errorf("across the sidebar's pages, the way is %d", a.pageDir)
	}
	a.open(route{page: pagePlaylist, id: 1})
	if a.pageDir != 1 {
		t.Errorf("into a playlist, the way is %d", a.pageDir)
	}
	a.back()
	if a.pageDir != -1 {
		t.Errorf("back, the way is %d", a.pageDir)
	}
	a.goForward()
	if a.pageDir != 1 {
		t.Errorf("forward, the way is %d", a.pageDir)
	}
}

// With NM_MOTION set, logs how much Now Playing changes from one frame to the
// next as it opens: a jump at the end of its way shows as a spike.
func TestOpenMotionFrames(t *testing.T) {
	if os.Getenv("NM_MOTION") == "" {
		t.Skip("set NM_MOTION to log the frames")
	}
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.Smoke = false
	url := "https://example.com/album.jpg"
	markArt(a, url, 120)
	a.art.add(netease.Sized(url, 600)+"#backdrop", a.art.peek(netease.Sized(url, 120)))
	tr := netease.Track{ID: 1, Name: "被神明写的歌", Duration: 180000}
	tr.Album.PicURL = url
	a.pl.Cue([]netease.Track{tr})
	a.pl.SetLyrics(nil)
	a.pl.Seek(60 * time.Second)
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	tt.Advance(100 * time.Millisecond)
	tt.Frame()
	// Once, for where everything lands.
	a.nowPlaying = true
	tt.Frame()
	tt.Advance(2 * time.Second)
	tt.Frame()
	a.nowPlaying = false
	tt.Frame()
	tt.Advance(2 * time.Second)
	tt.Frame()
	a.nowPlaying = true
	var last *image.RGBA
	for i := 0; i < 96; i++ {
		if i == 48 {
			a.nowPlaying = false
			t.Log("closing")
		}
		tt.Frame()
		img := tt.Image()
		diff := 0
		if last != nil {
			s := img.Bounds().Dx() / 1100
			for y := 100; y < 700; y += 2 {
				for x := 60; x < 520; x += 2 {
					p, q := img.RGBAAt(x*s, y*s), last.RGBAAt(x*s, y*s)
					d := int(p.G) - int(q.G)
					if d < 0 {
						d = -d
					}
					diff += d
				}
			}
		}
		last = image.NewRGBA(img.Bounds())
		copy(last.Pix, img.Pix)
		fly := " "
		if a.npFlying() {
			fly = "F"
		}
		m := findMark(img, img.Bounds().Dx()/1100)
		t.Logf("%3d ms  k %.4f %s diff %4d  x %6.1f y %6.1f area %6d", i*16, a.npK, fly, diff/1000, m.x, m.y, m.area)
		tt.Advance(16 * time.Millisecond)
	}
}
