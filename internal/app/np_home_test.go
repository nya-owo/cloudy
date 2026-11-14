package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// Closing playback must restore the home scroll and let a recommendation
// open its own playlist, even after reversing the closing animation.
func TestClosingPlaybackRestoresHomeCards(t *testing.T) {
	a := transitionPlaylist()
	a.route = route{page: pageHome}
	a.playlistAt = map[int64]time.Time{}
	for i := range 30 {
		pl := netease.Playlist{ID: int64(i + 100), Name: "Recommendation " + string(rune('A'+i))}
		a.featured = append(a.featured, pl)
		a.recPlaylists = append(a.recPlaylists, pl)
		a.playlists[pl.ID] = &pl
		a.playlistAt[pl.ID] = time.Now()
	}
	a.charts = a.featured[:4]
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	tt.Advance(time.Second)
	a.pageScroll.Y = 250
	tt.Frame()
	y := a.pageScroll.Y
	if y <= 0 {
		t.Fatal("home did not scroll")
	}
	a.nowPlaying = true
	tt.Frame()
	tt.Advance(time.Second)
	a.nowPlaying = false
	tt.Frame()
	tt.Advance(150 * time.Millisecond)
	a.nowPlaying = true
	tt.Frame()
	tt.Advance(80 * time.Millisecond)
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Advance(time.Second)
	tt.Frame()
	if a.nowPlaying || !tt.Focused("搜索") || a.pageScroll.Y != y {
		t.Fatalf("home restore: open=%v search=%v scroll=%v (was %v)", a.nowPlaying, tt.Focused("搜索"), a.pageScroll.Y, y)
	}
	for _, pl := range a.recPlaylists[:8] {
		if r, ok := tt.Find(pl.Name); ok && r.Y > 60 && r.Y+r.H < 680 {
			if err := tt.Click(pl.Name); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if a.route != (route{page: pagePlaylist, id: pl.ID}) {
				t.Fatalf("card opened %+v, want playlist %d", a.route, pl.ID)
			}
			return
		}
	}
	t.Fatal("no visible recommendation after returning home")
}
