package app

import (
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

// Shuffling a list starts from any of its songs that play, not always the
// first, and never from one that does not.
func TestShuffleStartsAnywhere(t *testing.T) {
	tracks := make([]netease.Track, 20)
	for i := range tracks {
		tracks[i] = netease.Track{ID: int64(i + 1)}
	}
	tracks[0].Privilege = &netease.Privilege{St: -200} // taken down
	starts := map[int]bool{}
	for range 200 {
		at := randomPlayable(tracks)
		if at == 0 {
			t.Fatal("started from a song that does not play")
		}
		starts[at] = true
	}
	if len(starts) < 5 {
		t.Fatalf("started from only %v", starts)
	}
	a := newTestApp()
	a.playList(tracks, true)
	if a.pl.Mode() != player.ModeShuffle {
		t.Fatalf("shuffled play left the mode at %v", a.pl.Mode())
	}
	a.playList(tracks, false)
	if a.pl.Shuffle() {
		t.Fatal("playing in order stayed shuffled")
	}
}

// Turning the play mode over and over shows one toast, saying the mode
// chosen last, in place of the one before, not a stack of them.
func TestModeToastsReplaceEachOther(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.pl.Cue([]netease.Track{{ID: 1, Name: "song", Duration: 200000}, {ID: 2, Name: "two", Duration: 200000}})
	tt := ui.NewTester(a.view, 1180, 780)
	tt.Frame()
	for range 2 {
		r, ok := tt.Find(a.pl.Mode().Name())
		if !ok {
			t.Fatalf("no mode button for %s", a.pl.Mode().Name())
		}
		tt.ClickAt(r.X+r.W/2, r.Y+r.H/2)
		tt.Frame()
		tt.Frame()
	}
	// Two clicks: 列表循环, then 单曲循环. The button now says 单曲循环, and
	// so does the one toast; 列表循环 shows nowhere.
	if a.pl.Mode() != player.ModeOne {
		t.Fatalf("mode %v after two clicks", a.pl.Mode())
	}
	if _, ok := tt.Find("列表循环"); ok {
		t.Fatal("the first toast still shows beside the second")
	}
}
