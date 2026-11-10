package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

// The arc keeps the sung line in the middle of the window and tilted rows still seek after reflow.
func TestLyricPerspectiveSeeksAfterReflow(t *testing.T) {
	a := newTestApp()
	a.settings.LyricPerspective = true
	a.nowPlaying = true
	a.pl.Cue([]netease.Track{{ID: 1, Name: "x", Duration: 60000}})
	a.pl.SetLyrics(lyrics.Build("[00:00]第一句歌词\n[00:05]第二句歌词\n[00:10]最后一句歌词", "[00:10]翻译", ""))
	tt := ui.NewTester(a.view, 1100, 760)
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
}
