package app

import (
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

type trackRowLayout struct {
	route     route
	first     *netease.Track
	rows      int
	keys      []any
	positions []float32
}

// Cache the identities and content positions, rather than rebuilding a
// map of a long favorite list on every animation frame.
func (a *app) movingTrackRows(tracks []netease.Track, items []int, groups []albumGroup, rows int) []float32 {
	var first *netease.Track
	if len(tracks) > 0 {
		first = &tracks[0]
	}
	l := &a.trackLayout
	if l.route != a.route || l.first != first || l.rows != rows || l.keys == nil {
		*l = trackRowLayout{route: a.route, first: first, rows: rows, keys: make([]any, rows), positions: make([]float32, len(items))}
		l.keys[0] = "header"
		seen := map[[2]int64]int{}
		top := float32(0)
		for n, item := range items {
			l.positions[n] = top
			if item < 0 {
				g := -1 - item
				l.keys[n+1] = "group:" + groups[g].key
				if g == 0 {
					top += 56
				} else {
					top += 70
				}
			} else {
				t := tracks[item]
				id := [2]int64{t.ID, t.ProgramID}
				l.keys[n+1] = struct {
					ID         [2]int64
					Occurrence int
				}{id, seen[id]}
				seen[id]++
				top += 54
			}
		}
		for n := len(items) + 1; n < rows; n++ {
			l.keys[n] = n
		}
	}
	keys := l.keys
	a.trackList.Key = func(n int) any { return keys[n] }
	a.trackList.KeepOffset = true
	return l.positions
}

type trackRowPlace struct {
	top      float32
	revision int
}

// A surviving song keeps its old visual position, then slides into the gap
// left by an unlike. A later edit can retarget an unfinished move.
func (a *app) trackRowShift(row ui.Element, top float32) float32 {
	place := ui.Local(row, "song-place", func() trackRowPlace { return trackRowPlace{top, a.likeRevision} })
	anim := ui.Animation{Duration: 280 * time.Millisecond, Ease: ui.CubicBezier(0.22, 0.8, 0.25, 1)}
	shift := row.AnimateTo("song-shift", 0, anim)
	if a.motion == motionFull && place.revision != a.likeRevision && place.top != top {
		row.Jump("song-shift", shift+place.top-top)
		shift = row.AnimateTo("song-shift", 0, anim)
	} else if a.motion != motionFull {
		row.Jump("song-shift", 0)
		shift = 0
	}
	*place = trackRowPlace{top, a.likeRevision}
	return shift
}
