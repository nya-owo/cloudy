package app

import (
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

func likedTestPage() (*app, *netease.Playlist) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.profile = &netease.Profile{UserID: 1}
	pl := &netease.Playlist{ID: 99, Name: "我喜欢的音乐", SpecialType: 5, TrackCount: 80}
	pl.Creator.UserID = 1
	a.liked = map[int64]bool{}
	for i := 1; i <= 80; i++ {
		pl.Tracks = append(pl.Tracks, netease.Track{ID: int64(i), Name: fmt.Sprintf("歌曲%03d", i), Duration: 200000})
		a.liked[int64(i)] = true
	}
	a.mine = []netease.Playlist{*pl}
	a.playlists[99] = pl
	a.playlistAt = map[int64]time.Time{99: time.Now()}
	a.route = route{page: pagePlaylist, id: 99}
	return a, pl
}

func flushTestLike(a *app, id int64) {
	if e := a.likeEdits[id]; e != nil {
		now := time.Unix(1700000000, 0)
		if a.likeNext.After(now) {
			now = a.likeNext
		}
		a.flushLike(id, e, now)
	}
}

// Unliking a visible song used to clear the page; it now removes only that row and rolls it back in place on failure.
func TestUnlikeKeepsPlaylistAndScroll(t *testing.T) {
	old := likeSong
	calls := make(chan int64, 2)
	likeSong = func(id int64, like bool) error { calls <- id; return nil }
	defer func() { likeSong = old }()
	a, pl := likedTestPage()
	original := slices.Clone(pl.Tracks)
	a.plView = plView{route: a.route, query: "歌曲", sort: 1}
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	tt.Advance(time.Second)
	a.trackList.ScrollTo(40, ui.Start)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	first, _ := a.trackList.Visible()
	if first < 10 {
		t.Fatalf("test did not scroll: first=%d", first)
	}
	clicked := false
	for i := 1; i <= 80; i++ {
		if r, ok := tt.Find(fmt.Sprintf("歌曲%03d", i)); ok && r.Y > 240 && r.Y+r.H < 600 {
			// The first virtual row can be under the sticky heading. Use a
			// visible row's heart, beside its duration column.
			tt.ClickAt(r.X+r.W-86, r.Y+r.H/2)
			clicked = true
			break
		}
	}
	if !clicked {
		t.Fatal("no visible song to unlike")
	}
	for id := range a.likeEdits {
		flushTestLike(a, id)
	}
	id := wait(t, calls, "the unlike")
	tt.Frame()
	after, _ := a.trackList.Visible()
	if a.playlists[99] != pl || a.loadingPl[99] || len(pl.Tracks) != 79 || a.liked[id] {
		t.Fatal("unlike emptied or reloaded the page instead of removing one row")
	}
	if after < first-1 || after > first+1 || a.plView.query != "歌曲" || a.plView.sort != 1 {
		t.Fatalf("lost scroll/search/sort: before=%d after=%d", first, after)
	}
	a.likeAnswered(a.accountGen, id, false, errors.New("offline"))
	tt.Frame()
	if a.playlists[99] != pl || !slices.EqualFunc(original, pl.Tracks, func(x, y netease.Track) bool { return x.ID == y.ID }) || a.mine[0].TrackCount != 80 {
		t.Fatal("failed unlike did not restore just that song in its old position")
	}
}

// Likes edited in a page must not shorten the queue that was started from its old slice.
func TestUnlikeKeepsPlaybackQueue(t *testing.T) {
	a, pl := likedTestPage()
	a.pl.Cue(pl.Tracks)
	e := a.newLikeEdit(&pl.Tracks[10])
	a.applyLike(e, false)
	session, ok := a.pl.Session()
	if !ok || len(session.Queue) != 80 || session.Queue[10].ID != 11 || len(pl.Tracks) != 79 {
		t.Fatal("removing a favorite also changed playback queue")
	}
}

// Rapid clicks keep their immediate state while like requests for the same song run in order.
func TestLikeRapidClicksSerialize(t *testing.T) {
	old := likeSong
	calls := make(chan bool, 4)
	likeSong = func(_ int64, like bool) error { calls <- like; return nil }
	defer func() { likeSong = old }()
	a, pl := likedTestPage()
	tr := pl.Tracks[20]
	a.toggleLike(&tr)
	flushTestLike(a, tr.ID)
	if wait(t, calls, "unlike") {
		t.Fatal("first request was not an unlike")
	}
	a.toggleLike(&tr)
	if !a.liked[tr.ID] || len(pl.Tracks) != 80 || len(calls) != 0 {
		t.Fatal("rapid like was not immediate or started a parallel request")
	}
	a.likeAnswered(a.accountGen, tr.ID, false, nil)
	flushTestLike(a, tr.ID)
	if !wait(t, calls, "like after unlike reply") {
		t.Fatal("latest desired like was not sent")
	}
	a.likeAnswered(a.accountGen, tr.ID, true, nil)
	if !a.liked[tr.ID] || len(a.likeEdits) != 0 || a.playlists[99] != pl {
		t.Fatal("latest like did not settle in place")
	}
}

// A background response may sync another device, but must never overwrite a later local click or another login.
func TestLibrarySyncRejectsStaleReplies(t *testing.T) {
	a, pl := likedTestPage()
	r := libraryReply{liked: []int64{999}, made: []netease.Playlist{{ID: 99, SpecialType: 5, TrackCount: 1}}}
	a.likeRevision = 4
	a.libraryAnswered(a.accountGen, 3, r)
	if !a.liked[1] || a.liked[999] {
		t.Fatal("old refresh overwrote local edits")
	}
	a.libraryAnswered(a.accountGen-1, 4, r)
	if a.liked[999] {
		t.Fatal("another login's refresh was accepted")
	}
	a.libraryAnswered(a.accountGen, 4, r)
	if a.liked[1] || !a.liked[999] || a.mine[0].TrackCount != 1 || a.playlists[99] != pl {
		t.Fatal("current refresh did not update personal data without emptying the page")
	}
}

// Re-entering a stale cached playlist requests fresh data while retaining its old content during the request.
func TestCachedPlaylistRefreshKeepsContent(t *testing.T) {
	old := fetchPlaylist
	calls := make(chan int64, 2)
	fetchPlaylist = func(id int64) (*netease.Playlist, error) { calls <- id; return nil, errors.New("offline") }
	defer func() { fetchPlaylist = old }()
	a, pl := likedTestPage()
	a.playlistAt[99] = time.Now().Add(-time.Minute)
	a.loadPlaylist(99, false)
	if wait(t, calls, "playlist refresh") != 99 || !a.loadingPl[99] || a.playlists[99] != pl {
		t.Fatal("cached page was not refreshed in the background")
	}
	a.playlistAnswered(a.accountGen, 99, nil, errors.New("offline"), false)
	if a.playlists[99] != pl || a.loadingPl[99] {
		t.Fatal("failed refresh discarded readable content")
	}
	next := &netease.Playlist{ID: 99, Tracks: []netease.Track{{ID: 999}}}
	a.playlistAnswered(a.accountGen, 99, next, nil, false)
	if a.playlists[99] != next {
		t.Fatal("fresh playlist was not applied")
	}
	a.loadPlaylist(99, false)
	if a.loadingPl[99] {
		t.Fatal("immediate re-entry ignored the short throttle")
	}
}

// With NM_FRAMES set, draw the new count in the bar and playback page in both appearances.
func TestRedCountFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip("set NM_FRAMES to inspect count placement")
	}
	for _, dark := range []bool{false, true} {
		for _, expanded := range []bool{false, true} {
			a := newTestApp()
			a.settings = defaultSettings()
			a.pl.Cue([]netease.Track{{ID: 186016, Name: "红心人数显示预览", Artists: []netease.Artist{{Name: "Cloudy"}}, Duration: 200000}})
			a.redCounts = map[int64]*redCountState{186016: {known: true, value: netease.RedCount{Count: 6822849, Description: "100w+"}}}
			a.nowPlaying = expanded
			tt := ui.NewTester(a.view, 1200, 800)
			tt.SetDark(dark)
			tt.Frame()
			tt.Advance(time.Second)
			tt.Frame()
			f, err := os.Create(filepath.Join(dir, fmt.Sprintf("red-count-dark-%v-expanded-%v.png", dark, expanded)))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}
