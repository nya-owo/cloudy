package app

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// Enabling IgnoreSystemMotion lets Cloudy and MyGo animations keep running
// when the OS has reduced motion turned on, while Cloudy's own "off" mode
// still disables motion.
func TestIgnoreSystemMotionEnablesAnimationsUnderSystemReduceMotion(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.Motion = ""
	a.pl.Cue([]netease.Track{{ID: 1, Name: "Song", Artists: []netease.Artist{{Name: "Artist"}}, Duration: 180000}})

	tt := ui.NewTester(a.view, 1180, 780)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	if a.motion != motionOff {
		t.Fatalf("with system ReduceMotion and IgnoreSystemMotion=false, motion = %d, want motionOff", a.motion)
	}

	// Opening Now Playing jumps to 1 immediately while respecting system ReduceMotion.
	a.nowPlaying = true
	tt.Frame()
	if a.npK != 1 {
		t.Fatalf("with system ReduceMotion, npK = %v, want 1", a.npK)
	}
	a.nowPlaying = false
	tt.Frame()

	// Toggle "无视系统动效设置" in Settings -> 外观 -> 动效.
	a.settingsView.open = true
	a.settingsView.tab = settingsAppearance
	a.settingsView.scroll[settingsAppearance].Y = 320
	tt.Frame()
	if dir := os.Getenv("NM_FRAMES"); dir != "" {
		if f, err := os.Create(filepath.Join(dir, "settings-ignore-motion-before.png")); err == nil {
			_ = png.Encode(f, tt.Image())
			f.Close()
		}
	}
	if err := tt.Click("切换无视系统动效设置"); err != nil {
		t.Fatalf("clicking 无视系统动效设置 toggle failed: %v", err)
	}
	tt.Frame()
	if !a.settings.IgnoreSystemMotion {
		t.Fatal("IgnoreSystemMotion did not turn on")
	}
	if a.motion != motionFull {
		t.Fatalf("after enabling IgnoreSystemMotion, motion = %d, want motionFull", a.motion)
	}
	// Turning motion back on while Settings is already open must keep Settings
	// visible without replaying its open animation from 0.
	if _, ok := tt.Find("切换无视系统动效设置"); !ok {
		t.Fatal("settings dialog vanished right after enabling IgnoreSystemMotion")
	}
	if dir := os.Getenv("NM_FRAMES"); dir != "" {
		if f, err := os.Create(filepath.Join(dir, "settings-ignore-motion-after.png")); err == nil {
			_ = png.Encode(f, tt.Image())
			f.Close()
		}
	}

	// With IgnoreSystemMotion on, Now Playing animates smoothly even though the
	// OS still reports ReduceMotion=true.
	a.settingsView.open = false
	tt.Frame()
	a.nowPlaying = true
	tt.Frame()
	tt.Advance(80 * time.Millisecond)
	if a.npK <= 0 || a.npK >= 1 {
		t.Fatalf("with IgnoreSystemMotion=true, Now Playing did not animate mid-way: npK=%v", a.npK)
	}
	tt.Advance(time.Second)
	if a.npK != 1 {
		t.Fatalf("Now Playing did not settle to 1: npK=%v", a.npK)
	}

	// Floating windows sharing floatTheme also see ReduceMotion=false.
	var floatReduced bool
	ft := ui.NewTester(func(c *ui.Context) {
		a.floatTheme(c)
		floatReduced = c.Preferences().ReduceMotion
		ui.Box(c).Fill()
	}, 200, 100)
	ft.SetPreferences(ui.Preferences{ReduceMotion: true})
	ft.Frame()
	if floatReduced {
		t.Fatal("floatTheme did not apply IgnoreSystemMotion")
	}

	// Cloudy's own "off" motion setting still turns animations off even when
	// IgnoreSystemMotion is enabled.
	a.settings.Motion = "off"
	a.nowPlaying = false
	tt.Frame()
	if a.motion != motionOff || a.npK != 0 {
		t.Fatalf("Motion=off did not stay off: motion=%d npK=%v", a.motion, a.npK)
	}
}
