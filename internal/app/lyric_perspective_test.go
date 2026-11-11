package app

import (
	"encoding/json"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

// The arc keeps the sung line in the middle of the window and tilted rows still seek after reflow.
func TestLyricPerspectiveSeeksAfterReflow(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.LyricPerspective = true
	a.nowPlaying = true
	a.pl.Cue([]netease.Track{{ID: 1, Name: "x", Duration: 60000}})
	a.pl.SetLyrics(lyrics.Build("[00:00]第一句歌词\n[00:05]第二句歌词\n[00:10]最后一句歌词", "[00:10]翻译", ""))
	tt := ui.NewTester(a.view, 1100, 760)
	tt.SetScale(0.25) // Geometry and input stay in DIPs; render fewer pixels.
	settle := func() {
		tt.Frame()
		tt.Advance(2 * time.Second)
		tt.Frame()
	}
	checkFocus := func(text string, height float32) {
		t.Helper()
		settle()
		line, ok := tt.Find(text)
		if !ok || abs32(line.Y+line.H/2-height*0.53) > 3 {
			t.Fatalf("%q did not settle in the lyric band: %+v", text, line)
		}
	}
	seek := func(text string, want time.Duration) {
		t.Helper()
		settle()
		if err := tt.Click(text); err != nil {
			t.Fatal(err)
		}
		if pos := a.pl.Position(); pos != want {
			t.Fatalf("clicking %q sought to %v, want %v", text, pos, want)
		}
	}
	checkFocus("第一句歌词", 760)
	seek("最后一句歌词", 10*time.Second)
	tt.SetSize(1300, 860)
	checkFocus("最后一句歌词", 860)
	seek("第二句歌词", 5*time.Second)
	a.settings.LyricScale = 1.5
	a.settings.LyricTrans = false
	seek("最后一句歌词", 10*time.Second)
	a.settings.LyricTilt = 0
	settle()
	last, ok := tt.Find("最后一句歌词")
	if !ok || last.Y+last.H/2 >= 860*0.53-20 {
		t.Fatalf("zero tilt did not return the active line to the flat list: %+v", last)
	}
	a.settings.LyricTilt = 0.8
	checkFocus("最后一句歌词", 860)
}

// Old settings keep the original arc, and setting either slider to zero
// must survive reopening the app.
func TestLyricPerspectiveSettingsKeepZero(t *testing.T) {
	s := defaultSettings()
	if err := json.Unmarshal([]byte(`{"lyricPerspective":true}`), &s); err != nil {
		t.Fatal(err)
	}
	if s.LyricTilt != 0.5 {
		t.Fatalf("the old arc loaded at %v tilt, want 0.5", s.LyricTilt)
	}
	s.LyricTilt, s.LyricBounce = 0, 0
	s.path = filepath.Join(t.TempDir(), "settings.json")
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	loaded := loadSettingsFrom(s.path)
	if loaded.LyricTilt != 0 || loaded.LyricBounce != 0 {
		t.Fatalf("zero settings reloaded as tilt %v, bounce %v", loaded.LyricTilt, loaded.LyricBounce)
	}
}

// The spring can overshoot the sung line, while zero strength approaches
// the line from the same side throughout the animation.
func TestLyricGlideBounce(t *testing.T) {
	overshoot := func(strength float32) float64 {
		var glide lyricGlide
		var scroll ui.ScrollState
		tt := ui.NewTester(func(c *ui.Context) {
			if glide.start.IsZero() {
				glide.to(c, &scroll, 100, strength)
			}
		}, 100, 100)
		tt.Frame()
		var peak float64
		for i := 0; i <= 100; i++ {
			lag, _ := glide.at(glide.start.Add(time.Duration(i) * 10 * time.Millisecond))
			peak = max(peak, lag)
		}
		return peak
	}
	if peak := overshoot(0); peak != 0 {
		t.Errorf("the smooth scroll overshot by %v DIPs", peak)
	}
	mid, full := overshoot(0.5), overshoot(1)
	if mid <= 1 || full <= mid || full > 20 {
		t.Errorf("unexpected overshoot at 50%% and 100%%: %v, %v DIPs", mid, full)
	}
}

// The wheel over the middle values makes fine adjustments, kept once it
// rests, while clicking them still resets what was set either way.
func TestLyricToolsWheelValues(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.path = filepath.Join(t.TempDir(), "settings.json")
	a.npPointer = true
	const id = 1
	tt := ui.NewTester(func(c *ui.Context) {
		a.lyricTools(c, id, ui.Rect{X: 20, Y: 60, W: 430, H: 32})
	}, 470, 160)
	tt.Frame()
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	wheel := func(label string, notches int) {
		t.Helper()
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("no %q button", label)
		}
		before := loadSettingsFrom(a.settings.path)
		for range max(notches, -notches) {
			dy := float32(-100)
			if notches < 0 {
				dy = 100
			}
			tt.Wheel(r.X+r.W/2, r.Y+r.H/2, 0, dy)
		}
		tt.Frame()
		during := loadSettingsFrom(a.settings.path)
		if during.LyricScale != before.LyricScale || during.LyricShift[id] != before.LyricShift[id] {
			t.Fatal("the wheel saved settings before it rested")
		}
		a.lyricWheelAt = time.Now().Add(-lyricWheelRest)
		tt.Frame()
	}
	click("歌词字号增大")
	wheel("恢复歌词字号", -3)
	if got := a.lyricScale(); math.Abs(float64(got)-1.07) > 0.00001 {
		t.Errorf("the wheel set scale to %v, want 107%%", got)
	}
	click("恢复歌词字号")
	if got := a.lyricScale(); got != 1 {
		t.Errorf("click reset scale to %v, want 100%%", got)
	}
	click("歌词提前 0.1 秒")
	wheel("恢复歌词时间", 3)
	if got := a.lyricShift(id); got != 130*time.Millisecond {
		t.Errorf("the wheel shifted lyrics by %v, want 130 ms", got)
	}
	if got := loadSettingsFrom(a.settings.path).LyricShift[id]; got != 130 {
		t.Errorf("saved shift is %d ms, want 130 ms", got)
	}
	click("恢复歌词时间")
	if got := a.lyricShift(id); got != 0 {
		t.Errorf("click reset shift to %v, want 0", got)
	}
}
