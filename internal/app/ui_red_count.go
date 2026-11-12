package app

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

var fetchRedCount = netease.SongRedCount

type redCountState struct {
	value       netease.RedCount
	known, busy bool
	failed      bool
	at          time.Time
}

// Refresh a count without removing its last successful value from the UI.
func (a *app) redCountChanged(id int64) {
	if s := a.redCounts[id]; s != nil {
		s.at = time.Time{}
	}
}

// askRedCount shares the current song's count between the bar and playback
// page. Ordinary list rows do not fetch counts.
func (a *app) askRedCount(id int64) *redCountState {
	if a.redCounts == nil {
		a.redCounts = map[int64]*redCountState{}
	}
	s := a.redCounts[id]
	if s == nil {
		if len(a.redCounts) >= 128 {
			clear(a.redCounts)
		}
		s = &redCountState{}
		a.redCounts[id] = s
	}
	gap := time.Minute
	if s.known && !s.failed {
		gap = 10 * time.Minute
	}
	if a.win == nil || s.busy || time.Since(s.at) < gap {
		return s
	}
	s.busy, s.at = true, time.Now()
	gen := a.accountGen
	go func() {
		value, err := fetchRedCount(id)
		a.post(func() {
			if gen != a.accountGen || a.redCounts[id] != s {
				return
			}
			a.redCountAnswered(id, s, value, err)
		})
	}()
	return s
}

func (a *app) redCountAnswered(id int64, s *redCountState, value netease.RedCount, err error) {
	if a.redCounts[id] != s {
		return
	}
	s.busy = false
	s.failed = err != nil
	if err == nil {
		s.value, s.known = value, true
	}
}

// The open upper-right corner is part of the vector, so a count fits into
// it on glass and cover backgrounds without an opaque masking patch.
var (
	iconCountHeart     = strokeIcon(`<path d="M14.5 4.3C13.7 4.6 12.9 5.2 12 6.1 10.5 4.5 9.2 3.7 7.5 3.7 4.3 3.7 2 6 2 9.1c0 2.3 1.4 4 3.2 5.7L12 21l6.8-6.2c1.7-1.6 3.2-3.4 3.2-5.3"/>`)
	iconCountHeartFill = fillIcon(`<path d="M15.8 3.5c-1.4.2-2.7 1.2-3.8 2.3-1.5-1.7-2.8-2.6-4.5-2.6A5.6 5.6 0 0 0 2 8.8c0 2.3 1.5 4.1 3.2 5.8L12 21l6.8-6.3c1.2-1.2 2.5-2.8 3-4.7-3.7.8-7-.5-7-3.7 0-1 .4-1.9 1-2.8Z"/>`)
)

func (a *app) likeCountButton(c *ui.Context, tr *netease.Track, size float32, color ui.Color) ui.Element {
	if tr.LocalPath != "" || tr.ProgramID != 0 {
		return a.likeButton(c, tr, size, color)
	}
	s := a.askRedCount(tr.ID)
	red := ui.Hex("#d92d43")
	if a.pal.dark || a.nowPlaying {
		red = ui.Hex("#ff3b43")
	}
	shade := color
	if a.isLiked(tr) {
		shade = red
	}
	wrap := ui.Box(c).Width(size + max(44, size*1.3)).Height(size + 16).AlignItems(ui.Start)
	wrap.Children(func() {
		outline, filled := iconHeart, iconHeartFill
		if s.known {
			outline, filled = iconCountHeart, iconCountHeartFill
		}
		a.heartButton(c.Key("heart"), tr, size, color, red, outline, filled)
		if s.known {
			label := s.value.Description
			if label == "" {
				label = count(s.value.Count)
			}
			ui.Text(c.Key("red-count"), label).FontSize(max(10, size*0.46)).FontWeight(700).FontFeatures("tnum").TextColor(shade).
				Absolute().Top(-1).Left(8 + size*0.72).SingleLine().Label(fmt.Sprintf("%d 人喜欢", s.value.Count))
		}
	})
	return wrap
}
