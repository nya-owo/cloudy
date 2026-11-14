package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// A sidebar omitted while transparent must restore scrolling and group
// choices, and Cmd+F must still focus its search after reversing the reveal.
func TestNowPlayingRestoresTransparentSidebar(t *testing.T) {
	a := transitionPlaylist()
	a.settings.SidebarAll = true
	a.query, a.savedCollapsed = "saved query", true
	for i := range 40 {
		a.mine = append(a.mine, netease.Playlist{ID: int64(i + 20), Name: fmt.Sprintf("Playlist %02d", i)})
	}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	tt.Advance(time.Second)
	a.navScroll.Y = 500
	tt.Frame()
	y := a.navScroll.Y
	if y <= 0 {
		t.Fatal("sidebar did not scroll")
	}
	a.nowPlaying = true
	tt.Frame()
	tt.Advance(120 * time.Millisecond)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Advance(time.Second)
	tt.Frame()
	if a.nowPlaying || !tt.Focused("搜索") {
		t.Fatal("Cmd+F failed to close playback and focus sidebar search")
	}
	if a.query != "saved query" || !a.savedCollapsed || a.navScroll.Y != y {
		t.Fatalf("lost sidebar state: query=%q collapsed=%v scroll=%v (was %v)", a.query, a.savedCollapsed, a.navScroll.Y, y)
	}
	a.navScroll.Y = 0
	tt.Frame()
	if err := tt.Click("最近播放"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.route.page != pageRecent {
		t.Fatal("restored sidebar navigation did not handle the click")
	}
}
