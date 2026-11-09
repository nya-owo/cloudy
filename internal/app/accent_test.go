package app

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// The color chosen is kept, and settings kept before it load as no color.
func TestAccentKept(t *testing.T) {
	s := defaultSettings()
	json.Unmarshal([]byte(`{"quality":"exhigh"}`), &s)
	if s.Accent != "" {
		t.Errorf("settings from before the color took %q", s.Accent)
	}
	s.Accent = "#3D7BD9"
	b, _ := json.Marshal(s)
	back := defaultSettings()
	json.Unmarshal(b, &back)
	if back.Accent != "#3D7BD9" {
		t.Errorf("the color saved as %q", back.Accent)
	}
}

// The color chosen tints the window and the sidebar, not only the buttons.
func TestAccentCoversWindow(t *testing.T) {
	frame := func(accent string) *image.RGBA {
		a := newTestApp()
		accentHome(a)
		a.settings.Accent = accent
		tt := ui.NewTester(a.view, 1200, 800)
		tt.SetDark(false)
		tt.Frame()
		tt.Advance(time.Second)
		tt.Frame()
		return tt.Image()
	}
	plain, blue := frame(""), frame("#3D7BD9")
	// A point of the content, and one of the sidebar, clear of what sits on
	// them.
	cPlain, cBlue := plain.RGBAAt(900, 620), blue.RGBAAt(900, 620)
	sPlain, sBlue := plain.RGBAAt(80, 620), blue.RGBAAt(80, 620)
	if cPlain == cBlue {
		t.Error("the content kept its color")
	}
	if sPlain == sBlue {
		t.Error("the sidebar kept its color")
	}
	if cBlue.B <= cBlue.R || sBlue.B <= sBlue.R {
		t.Errorf("a blue accent left the window cold: content %v, sidebar %v", cBlue, sBlue)
	}
}

// The sidebar keeps the theme's color, whatever cover plays.
func TestSidebarDropsCover(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.pal = paletteFor(&ui.Theme{Dark: true})
	a.npArt = &artwork{tint: ui.RGB(255, 0, 0)}
	if got := a.sidebarColor(); got != a.pal.sidebar {
		t.Errorf("the sidebar took the cover playing: %v over %v", got, a.pal.sidebar)
	}
}

// accentHome is a home page with something on it, for the frames below.
func accentHome(a *app) {
	a.settings = defaultSettings()
	a.daily = []netease.Track{
		{ID: 1, Name: "第一首", Duration: 180000, Album: netease.Album{Name: "专辑一"}},
		{ID: 2, Name: "第二首", Duration: 240000, Album: netease.Album{Name: "专辑二"}},
	}
	for i := range 8 {
		a.recPlaylists = append(a.recPlaylists, netease.Playlist{ID: int64(10 + i), Name: fmt.Sprintf("推荐歌单 %d", i)})
	}
	a.charts = []netease.Playlist{{ID: 99, Name: "新歌榜"}}
}

// With NM_FRAMES set, saves the home page in each color and appearance, and
// the settings that choose them, for looking at them.
func TestAccentFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip("set NM_FRAMES to a directory to save frames")
	}
	save := func(name string, tt *ui.Tester) {
		tt.Advance(time.Second)
		tt.Frame()
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
	for _, dark := range []bool{false, true} {
		appearance := "light"
		if dark {
			appearance = "dark"
		}
		for name, accent := range map[string]string{"red": "", "clay": "#D97757", "blue": "#3D7BD9"} {
			a := newTestApp()
			accentHome(a)
			a.settings.Accent = accent
			tt := ui.NewTester(a.view, 1280, 820)
			tt.SetDark(dark)
			save("accent-"+name+"-"+appearance, tt)
		}
		a := newTestApp()
		accentHome(a)
		a.settings.Accent = "#D97757"
		a.settingsView.open, a.settingsView.tab = true, settingsAppearance
		tt := ui.NewTester(a.view, 1280, 820)
		tt.SetDark(dark)
		save("accent-settings-"+appearance, tt)
		if err := tt.Click("自定义主题色"); err == nil {
			save("accent-picker-"+appearance, tt)
		}
	}
}
