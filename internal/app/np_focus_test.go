package app

import (
	"fmt"
	"github.com/egoist/mygo/audio"
	"github.com/egoist/mygo/ui"
	"testing"
	"time"
)

// Menu opening must ignore a hidden search focus without stealing visible editing.
func TestNowPlayingSpaceWithRetainedSearchFocus(t *testing.T) {
	for _, glass := range []bool{false, true} {
		t.Run(fmt.Sprintf("glass=%v", glass), func(t *testing.T) {
			a := transitionPlaylist()
			a.settings.GlobalGlass = glass
			out := audio.NewPlayer(&silence{})
			out.SetVolume(0)
			out.Play()
			a.pl.Attach(out)
			defer out.Close()
			defer a.dismissSuggestions()
			tt := ui.NewTester(a.view, 1200, 800)
			tt.Frame()
			tt.Advance(time.Second)
			tt.Key(ui.Cmd, ui.KeyF)
			tt.Frame()
			if !tt.Focused("搜索") {
				t.Fatal("search not focused")
			}
			// Equivalent to menu.go's Ctrl+L action; it does not move internal focus.
			a.nowPlaying = !a.nowPlaying && a.pl.Current() != nil
			tt.Frame()
			tt.Advance(time.Second)
			tt.Frame()
			if !a.nowPlaying {
				t.Fatal("playback did not open")
			}
			tt.Key(0, ui.KeySpace)
			tt.Frame()
			if a.pl.Playing() {
				t.Fatal("Space did not pause with hidden search focus")
			}
			tt.Key(0, ui.KeySpace)
			tt.Frame()
			if !a.pl.Playing() {
				t.Fatal("Space did not resume exactly once")
			}
			a.nowPlaying = false
			tt.Frame()
			tt.Advance(70 * time.Millisecond)
			a.nowPlaying = true
			tt.Frame()
			tt.Advance(40 * time.Millisecond)
			tt.Key(ui.Cmd, ui.KeyF)
			tt.Advance(time.Second)
			tt.Frame()
			if a.nowPlaying || !tt.Focused("搜索") {
				t.Fatal("Ctrl+F failed to restore search")
			}
			before := a.query
			tt.TypeKey(0, ui.KeySpace, " ")
			tt.Frame()
			if a.query != before+" " || !a.pl.Playing() {
				t.Fatal("visible search Space was intercepted")
			}
			tt.Compose("输入法", 2)
			tt.TypeKey(0, ui.KeySpace, " ")
			tt.Frame()
			if !a.pl.Playing() {
				t.Fatal("composition Space paused playback")
			}
			tt.Compose("", 0)
			a.nowPlaying = true
			tt.Frame()
			tt.Advance(time.Second)
			tt.Key(0, ui.KeyEscape)
			tt.Advance(time.Second)
			tt.Frame()
			if a.nowPlaying {
				t.Fatal("Escape failed to close")
			}
		})
	}
}

// Playback's Space must leave a foreground dialog and shortcut recording alone.
func TestNowPlayingSpaceLeavesForegroundInput(t *testing.T) {
	a := transitionPlaylist()
	out := audio.NewPlayer(&silence{})
	out.SetVolume(0)
	out.Play()
	a.pl.Attach(out)
	defer out.Close()
	tt := ui.NewTester(a.view, 1200, 800)
	a.nowPlaying = true
	tt.Frame()
	tt.Advance(time.Second)
	a.naming.open = true
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	if !tt.Focused("歌单名称") {
		t.Fatal("dialog input not focused")
	}
	before := a.naming.name
	tt.TypeKey(0, ui.KeySpace, " ")
	tt.Frame()
	if a.naming.name != before+" " || !a.pl.Playing() {
		t.Fatal("dialog Space was intercepted")
	}
	a.naming.open = false
	tt.Frame()
	tt.Advance(time.Second)
	a.keys.setup.open = true
	a.keys.setup.recording = "play"
	tt.Frame()
	tt.Advance(time.Second)
	tt.Key(0, ui.KeySpace)
	tt.Frame()
	if !a.pl.Playing() {
		t.Fatal("recording Space paused playback")
	}
}
