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
		// The whole heart, round, the count beside its upper right as a
		// superscript, rather than in a corner cut from it.
		a.heartButton(c.Key("heart"), tr, size, color, red, iconHeart, iconHeartFill)
		if s.known {
			label := s.value.Description
			if label == "" {
				label = count(s.value.Count)
			}
			ui.Text(c.Key("red-count"), label).FontSize(max(10, size*0.46)).FontWeight(700).FontFeatures("tnum").TextColor(shade).
				Absolute().Top(-2).Left(8 + size*0.86).SingleLine().Label(fmt.Sprintf("%d 人喜欢", s.value.Count))
		}
	})
	return wrap
}
