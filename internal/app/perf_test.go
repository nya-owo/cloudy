package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

// transitionPlaylist supplies enough rows to expose work on the hidden page.
func transitionPlaylist() *app {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.GlobalGlass = true
	a.settings.BarSpin = false
	tracks := make([]netease.Track, 200)
	for i := range tracks {
		tracks[i] = netease.Track{ID: int64(i + 1), Name: fmt.Sprintf("Track %03d", i+1), Duration: 200000}
	}
	a.route = route{page: pagePlaylist, id: 7}
	a.playlists[7] = &netease.Playlist{ID: 7, Name: "Playlist", Tracks: tracks, TrackCount: len(tracks)}
	a.pl.Cue(tracks[:2])
	a.pl.SetLyrics(nil)
	return a
}

// BenchmarkNowPlayingTransition measures rebuilding a warm, partially open
// playback view over a playlist; it does not measure GPU or display cadence.
func BenchmarkNowPlayingTransition(b *testing.B) {
	a := transitionPlaylist()
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	tt.Advance(time.Second)
	a.nowPlaying = true
	tt.Frame()
	tt.Advance(120 * time.Millisecond)
	tt.Frame()
	if a.npK < npTextStart || a.npK >= 1 {
		b.Fatalf("expected a transition with invisible page text, got %v", a.npK)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		tt.Frame()
	}
}

// Closing or reversing Now Playing must restore the scrolled playlist so
// that clicking its first visible song still plays that song.
func TestNowPlayingRestoresHiddenPlaylist(t *testing.T) {
	a := transitionPlaylist()
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	tt.Advance(time.Second)
	a.trackList.ScrollTo(25, ui.Start)
	tt.Frame()
	first, _ := a.trackList.Visible()
	if first <= 0 {
		t.Fatal("playlist did not scroll")
	}
	for _, wait := range []time.Duration{120 * time.Millisecond, time.Second} {
		a.nowPlaying = true
		tt.Frame()
		tt.Advance(wait)
		tt.Frame()
		a.nowPlaying = false
		tt.Frame()
		tt.Advance(time.Second)
		tt.Frame()
		if got, _ := a.trackList.Visible(); got != first {
			t.Fatalf("playlist first visible row changed from %v to %v", first, got)
		}
	}
	// Reverse a closing animation while the playlist is still invisible.
	a.nowPlaying = true
	tt.Frame()
	tt.Advance(time.Second)
	a.nowPlaying = false
	tt.Frame()
	tt.Advance(80 * time.Millisecond)
	a.nowPlaying = true
	tt.Frame()
	tt.Advance(time.Second)
	// A different track and zoom rebuild the playback layout while covered.
	a.pl.Next()
	tt.SetZoom(1.1)
	tt.Frame()
	tt.Key(0, ui.KeyEscape)
	tt.Advance(time.Second)
	tt.SetZoom(1)
	tt.Frame()
	if a.nowPlaying {
		t.Fatal("Escape did not close the reversed playback view")
	}
	if got, _ := a.trackList.Visible(); got != first {
		t.Fatalf("reversal or zoom lost scroll: first=%d, now=%d", first, got)
	}
	for i := 20; i < 40; i++ {
		name := fmt.Sprintf("Track %03d", i)
		if r, ok := tt.Find(name); ok && r.Y > 180 && r.Y+r.H < 650 {
			if err := tt.Click(name); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if a.pl.Current().Name != name {
				t.Fatalf("clicked %s, playing %s", name, a.pl.Current().Name)
			}
			return
		}
	}
	t.Fatal("no visible playlist song after closing")
}

// playingApp is an app playing a track with lyrics, at a position the test
// sets with pl.Seek, and the number of times its view was built.
func playingApp() (*app, *ui.Tester, *int) {
	a := newTestApp()
	a.settings = defaultSettings()
	tr := netease.Track{ID: 7, Name: "宝船", Artists: []netease.Artist{{Name: "DOUDOU"}}, Duration: 200000}
	a.pl.Cue([]netease.Track{tr})
	// A line every 10 seconds, sung over the first 5 of them.
	var lrc string
	for i := range 12 {
		lrc += "[0" + string(rune('0'+i*10/60)) + ":" + string(rune('0'+i*10%60/10)) + "0.00]一二三四五六\n"
	}
	lines := lyrics.Build(lrc, "", "")
	for i := range lines {
		l := &lines[i]
		l.End = l.At + 5*time.Second
		l.Words = lyrics.SpreadWords(l.Text, l.At, l.End)
	}
	a.pl.SetLyrics(lines)
	a.pl.Hold(0)
	builds := 0
	tt := ui.NewTester(func(c *ui.Context) { builds++; a.view(c) }, 1200, 800)
	return a, tt, &builds
}

// framesAt counts the frames the view asks for over a second, at 60 a
// second, with playback held at pos.
func framesAt(a *app, tt *ui.Tester, pos time.Duration) int {
	a.pl.Seek(pos)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	asked := 0
	for range 60 {
		tt.Advance(time.Second / 60)
		if tt.FramePending() {
			asked++
			tt.Frame()
		}
	}
	return asked
}

// The bar's cover turning asks for a few frames a second, which only paint it:
// the view isn't built for it, as a pose at the display's rate would have the
// window painted, for what doesn't change.
func TestBarDiscFrames(t *testing.T) {
	a, tt, builds := playingApp()
	a.pl.TogglePlay()
	tt.Frame()
	if !a.pl.Playing() {
		t.Skip("the fixture doesn't play")
	}
	tt.Advance(time.Second)
	tt.Frame()
	was := *builds
	if n := framesAt(a, tt, 12*time.Second); n > 16 {
		t.Errorf("it asks for %d frames a second", n)
	}
	if n := *builds - was; n > 4 {
		t.Errorf("it built the view %d times", n)
	}
}

// Now Playing asks for frames only as fast as what it shows changes. The
// Tester runs timers as it settles a frame, so this counts only what does
// not change for a while: a frame log of the app (MYGO_FRAME_STATS) measures
// the rest.
func TestNowPlayingFrameBudget(t *testing.T) {
	a, tt, _ := playingApp()
	a.settings.BarSpin = false
	if n := framesAt(a, tt, 12*time.Second); n > 3 {
		t.Errorf("a page under the player: %d frames a second", n)
	}
	// Its rows still, the wall asks for no more than the lyrics beside it
	// do: it moves only as it is moved.
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	a.nowPlaying, a.npPane = true, paneLyrics
	lyricsFrames := framesAt(a, tt, 12*time.Second)
	a.npPane = paneWall
	if n := framesAt(a, tt, 12*time.Second); n > lyricsFrames+3 {
		t.Errorf("the wall, still: %d frames a second, the lyrics %d", n, lyricsFrames)
	}
	tt.SetPreferences(ui.Preferences{})
	a.npPane = paneStage
	a.settings.StageSparks = true
	for _, s := range stageStyles {
		a.settings.StageStyle = s.id
		if n := framesAt(a, tt, 18*time.Second); n > 12 {
			t.Errorf("the stage between lines, in %s: %d frames a second", s.label, n)
		}
	}
}

// Another song's backdrop fades in over the last one's, which is then let
// go, so that the backdrop stops asking for frames.
func TestBackdropFadeEnds(t *testing.T) {
	a, tt, _ := coverApp(t)
	a.art.art[netease.Sized("a", 600)+"#backdrop"] = &artwork{tint: ui.RGB(200, 40, 40)}
	a.art.art[netease.Sized("b", 600)+"#backdrop"] = &artwork{tint: ui.RGB(40, 40, 200)}
	q := a.pl.Queue()
	q[1].Album.PicURL, q[2].Album.PicURL = "a", "b"
	tt.Frame()
	a.pl.Next()
	tt.Frame()
	if a.backdropCache.before == nil {
		t.Fatal("the backdrop changed songs without fading")
	}
	tt.Advance(backdropFade + 100*time.Millisecond)
	tt.Frame()
	if a.backdropCache.before != nil {
		t.Error("the last song's backdrop still fades after the fade")
	}
}

// The FM stack stops requesting frames after its covers have changed places.
func TestFMCoverRestFrames(t *testing.T) {
	a := newTestApp()
	a.pl.Cue([]netease.Track{{ID: 1}, {ID: 2}, {ID: 3}})
	a.pl.AttachRadio(fmRadio(netease.FMDefault), func(func([]netease.Track)) {})
	tt := ui.NewTester(func(c *ui.Context) { a.fmCovers(c, a.pl.Current()) }, 360, 300)
	tt.Frame()
	a.pl.Next()
	tt.Frame()
	tt.Advance(3 * time.Second)
	tt.Frame()
	for range 10 {
		tt.Advance(100 * time.Millisecond)
		if tt.FramePending() {
			t.Fatal("the resting FM stack still asks for frames")
		}
	}
}

// The category capsule asks for no frames once it has reached its selected label.
func TestPlaylistFilterRestFrames(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.motion = motionFull
	a.featured = []netease.Playlist{{ID: 1}}
	tt := ui.NewTester(func(c *ui.Context) {
		a.pal = paletteFor(c.Theme())
		a.recommendedPlaylists(c)
	}, 1180, 700)
	tt.Frame()
	tt.Frame()
	tt.Advance(3 * time.Second)
	tt.Frame()
	for range 10 {
		tt.Advance(100 * time.Millisecond)
		if tt.FramePending() {
			t.Fatal("the settled category capsule still asks for frames")
		}
	}
}
