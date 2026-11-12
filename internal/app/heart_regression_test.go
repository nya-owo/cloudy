package app

import (
	"errors"
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

// Repeated clicks before submission collapse to one final state, including cancelling a whole burst.
func TestLikeBurstCoalescesBeforeRequest(t *testing.T) {
	old := likeSong
	calls := make(chan bool, 8)
	likeSong = func(_ int64, like bool) error { calls <- like; return nil }
	defer func() { likeSong = old }()
	a, pl := likedTestPage()
	tr := pl.Tracks[10]
	for range 20 {
		a.toggleLike(&tr)
	}
	if len(a.likeEdits) != 0 || len(calls) != 0 || !a.liked[tr.ID] {
		t.Fatal("an even click burst did not cancel its unsent request")
	}
	for range 21 {
		a.toggleLike(&tr)
	}
	if len(calls) != 0 || a.liked[tr.ID] {
		t.Fatal("click burst submitted before the quiet interval or lost its final state")
	}
	flushTestLike(a, tr.ID)
	if wait(t, calls, "the final unlike") {
		t.Fatal("final request was not an unlike")
	}
	a.likeAnswered(a.accountGen, tr.ID, false, nil)
	if len(a.likeEdits) != 0 || len(calls) != 0 {
		t.Fatal("click burst left extra requests or pending state")
	}
}

// A failed request releases its edit so a later unlike and like can both succeed.
func TestLikeFailureDoesNotLockFurtherActions(t *testing.T) {
	old := likeSong
	calls := make(chan bool, 4)
	likeSong = func(_ int64, like bool) error { calls <- like; return nil }
	defer func() { likeSong = old }()
	a, pl := likedTestPage()
	tr := pl.Tracks[10]
	a.toggleLike(&tr)
	flushTestLike(a, tr.ID)
	if wait(t, calls, "first unlike") {
		t.Fatal("expected unlike")
	}
	a.likeAnswered(a.accountGen, tr.ID, false, errors.New("rate limited"))
	if len(a.likeEdits) != 0 || !a.liked[tr.ID] {
		t.Fatal("failed request left the heart locked or failed to restore its state")
	}
	a.toggleLike(&tr)
	flushTestLike(a, tr.ID)
	if wait(t, calls, "retry unlike") {
		t.Fatal("retry was not an unlike")
	}
	a.likeAnswered(a.accountGen, tr.ID, false, nil)
	a.toggleLike(&tr)
	flushTestLike(a, tr.ID)
	if !wait(t, calls, "like after retry") {
		t.Fatal("like could not run after a failure")
	}
	a.likeAnswered(a.accountGen, tr.ID, true, nil)
	if !a.liked[tr.ID] || len(a.likeEdits) != 0 {
		t.Fatal("heart did not recover after failure")
	}
}

// A cancelled timer must not send through a new edit, and different songs share the request interval.
func TestLikeRequestOwnershipAndSpacing(t *testing.T) {
	old := likeSong
	calls := make(chan int64, 4)
	likeSong = func(id int64, _ bool) error { calls <- id; return nil }
	defer func() { likeSong = old }()
	a, pl := likedTestPage()
	x, y := pl.Tracks[10], pl.Tracks[20]
	a.toggleLike(&x)
	stale := a.likeEdits[x.ID]
	a.toggleLike(&x)
	a.toggleLike(&x)
	now := time.Unix(1700000000, 0)
	a.flushLike(x.ID, stale, now)
	if len(calls) != 0 {
		t.Fatal("cancelled timer submitted a new edit")
	}
	a.flushLike(x.ID, a.likeEdits[x.ID], now)
	if wait(t, calls, "first song") != x.ID {
		t.Fatal("wrong first song")
	}
	a.toggleLike(&y)
	a.flushLike(y.ID, a.likeEdits[y.ID], now.Add(likeRequestGap/2))
	if len(calls) != 0 || a.likeEdits[y.ID].busy {
		t.Fatal("second song ignored the request interval")
	}
	a.flushLike(y.ID, a.likeEdits[y.ID], now.Add(likeRequestGap))
	if wait(t, calls, "second song") != y.ID {
		t.Fatal("second song did not resume after the interval")
	}
	a.likeAnswered(a.accountGen, x.ID, false, nil)
	a.likeAnswered(a.accountGen, y.ID, false, nil)
}

// A successful heart click and even a failed count refresh keep the last visible number.
func TestRedCountDoesNotDisappearDuringInteraction(t *testing.T) {
	a, pl := likedTestPage()
	tr := pl.Tracks[10]
	s := &redCountState{known: true, value: netease.RedCount{Count: 270001, Description: "27w+"}, at: time.Unix(1, 0)}
	a.redCounts = map[int64]*redCountState{tr.ID: s}
	e := a.newLikeEdit(&tr)
	e.desired, e.busy = false, true
	a.likeEdits = map[int64]*likeEdit{tr.ID: e}
	a.applyLike(e, false)
	a.likeAnswered(a.accountGen, tr.ID, false, nil)
	if a.redCounts[tr.ID] != s || !s.known || s.value.Description != "27w+" {
		t.Fatal("heart response removed its visible count")
	}
	a.redCountAnswered(tr.ID, s, netease.RedCount{}, errors.New("offline"))
	if !s.known || s.value.Count != 270001 {
		t.Fatal("failed refresh blanked the last count")
	}
	a.redCountAnswered(tr.ID, s, netease.RedCount{Count: 270002, Description: "27w+"}, nil)
	if !s.known || s.value.Count != 270002 {
		t.Fatal("fresh count did not replace the stale value")
	}
}

// Removing a favorite moves its surviving neighbor over time instead of snapping to its final position.
func TestUnlikeRowsGlideIntoGap(t *testing.T) {
	for _, reduced := range []bool{false, true} {
		t.Run(fmt.Sprintf("reduced-%v", reduced), func(t *testing.T) {
			a, pl := likedTestPage()
			if reduced {
				a.settings.Motion = "off"
			}
			tt := ui.NewTester(a.view, 1200, 800)
			tt.Frame()
			tt.Advance(time.Second)
			a.trackList.ScrollTo(25, ui.Start)
			tt.Frame()
			tt.Advance(time.Second)
			tt.Frame()
			index := -1
			var before ui.Rect
			for i := 1; i < 79; i++ {
				if r, ok := tt.Find(fmt.Sprintf("歌曲%03d", i)); ok && r.Y > 280 && r.Y < 450 {
					index = i - 1
					before, _ = tt.Find(fmt.Sprintf("歌曲%03d", i+1))
					break
				}
			}
			if index < 0 {
				t.Fatal("no visible test row")
			}
			name := pl.Tracks[index+1].Name
			e := a.newLikeEdit(&pl.Tracks[index])
			a.applyLike(e, false)
			tt.Frame()
			start, _ := tt.Find(name)
			tt.Advance(60 * time.Millisecond)
			tt.Frame()
			middle, _ := tt.Find(name)
			tt.Advance(400 * time.Millisecond)
			tt.Frame()
			end, _ := tt.Find(name)
			t.Logf("neighbor Y: before=%.2f start=%.2f middle=%.2f end=%.2f", before.Y, start.Y, middle.Y, end.Y)
			if math.Abs(float64(before.Y-end.Y-54)) > 1 {
				t.Fatal("neighbor did not fill the removed row")
			}
			if reduced {
				if math.Abs(float64(start.Y-end.Y)) > 1 {
					t.Fatal("disabled motion still animated the move")
				}
			} else if math.Abs(float64(start.Y-before.Y)) > 1 || middle.Y >= start.Y || middle.Y <= end.Y+1 {
				t.Fatal("row snapped or did not progress through the move")
			}
		})
	}
}

// With NM_FRAMES set, preview the cutout heart against the reference's dark background.
func TestHeartReferenceFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip("set NM_FRAMES to inspect the reference style")
	}
	a := newTestApp()
	a.settings = defaultSettings()
	a.liked = map[int64]bool{1: true}
	a.redCounts = map[int64]*redCountState{
		1: {known: true, value: netease.RedCount{Count: 270000, Description: "27w+"}},
		2: {known: true, value: netease.RedCount{Count: 270000, Description: "27w+"}},
	}
	tt := ui.NewTester(func(c *ui.Context) {
		a.pal = paletteFor(c.Theme())
		ui.Row(c).Fill().Center().Gap(100).Background(ui.Hex("#180006")).Children(func() {
			a.likeCountButton(c.Key("liked"), &netease.Track{ID: 1}, 56, ui.Hex("#c4b9bd"))
			a.likeCountButton(c.Key("unliked"), &netease.Track{ID: 2}, 56, ui.Hex("#c4b9bd"))
		})
	}, 600, 220)
	tt.SetDark(true)
	tt.Frame()
	f, err := os.Create(filepath.Join(dir, "heart-reference.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}
