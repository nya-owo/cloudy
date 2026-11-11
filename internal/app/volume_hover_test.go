package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

// The volume opens as the pointer comes over the speaker, which shows that
// the wheel there turns it, and closes a moment after it leaves. A click
// mutes, and another brings back the volume it had.
func TestVolumeOpensOnHover(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.pl.Cue([]netease.Track{{ID: 1, Name: "song", Duration: 200000}})
	tt := ui.NewTester(a.view, 1180, 780)
	tt.Frame()
	r, ok := tt.Find("音量")
	if !ok {
		t.Fatal("no speaker")
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if !a.volumeOpen {
		t.Fatal("the pointer over the speaker did not open the volume")
	}
	tt.Move(20, 400)
	tt.Frame()
	if !a.volumeOpen {
		t.Fatal("it closed at once, before the pointer could reach it")
	}
	tt.Advance(time.Second)
	tt.Frame()
	if a.volumeOpen {
		t.Fatal("it stayed open after the pointer left")
	}

	a.volume = 0.37
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if a.volume != 0 {
		t.Fatalf("a click left the volume at %v", a.volume)
	}
	tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
	if a.volume != 0.37 {
		t.Fatalf("a second click brought the volume to %v, not 0.37", a.volume)
	}
}
