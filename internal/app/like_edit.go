package app

import (
	"slices"
	"time"

	"netease-music-mygo/internal/netease"
)

// likeEdit keeps a song in its old position if an unlike is refused. Rapid
// clicks change desired while the requests for this song run in order.
type likeEdit struct {
	track                    netease.Track
	confirmed, desired, busy bool
	positions                map[int64]int
	timer                    *time.Timer
}

const (
	likeDebounce   = 400 * time.Millisecond
	likeRequestGap = time.Second
)

func (a *app) newLikeEdit(tr *netease.Track) *likeEdit {
	e := &likeEdit{track: *tr, confirmed: a.liked[tr.ID], positions: map[int64]int{}}
	for id, pl := range a.playlists {
		if a.isLikedPlaylist(id) {
			e.positions[id] = slices.IndexFunc(pl.Tracks, func(t netease.Track) bool { return t.ID == tr.ID })
		}
	}
	return e
}

func (a *app) editLike(tr *netease.Track) {
	if a.likeEdits == nil {
		a.likeEdits = map[int64]*likeEdit{}
	}
	e := a.likeEdits[tr.ID]
	if e == nil {
		e = a.newLikeEdit(tr)
		a.likeEdits[tr.ID] = e
	}
	e.desired = !a.liked[tr.ID]
	a.applyLike(e, e.desired)
	a.sendLike(tr.ID, e)
}

func (a *app) sendLike(id int64, e *likeEdit) {
	if e.timer != nil {
		e.timer.Stop()
		e.timer = nil
	}
	if e.busy {
		return
	}
	if e.desired == e.confirmed {
		delete(a.likeEdits, id)
		return
	}
	// A burst is one desired state, rather than a string of requests that
	// can rate-limit the account. The heart still responds immediately.
	gen := a.accountGen
	e.timer = time.AfterFunc(likeDebounce, func() {
		a.post(func() {
			if gen == a.accountGen {
				a.flushLike(id, e, time.Now())
			}
		})
	})
}

func (a *app) flushLike(id int64, e *likeEdit, now time.Time) {
	if a.likeEdits[id] != e || e.busy {
		return
	}
	if e.timer != nil {
		e.timer.Stop()
		e.timer = nil
	}
	if e.desired == e.confirmed {
		delete(a.likeEdits, id)
		return
	}
	if wait := a.likeNext.Sub(now); wait > 0 {
		gen := a.accountGen
		e.timer = time.AfterFunc(wait, func() {
			a.post(func() {
				if gen == a.accountGen {
					a.flushLike(id, e, time.Now())
				}
			})
		})
		return
	}
	e.busy = true
	a.likeNext = now.Add(likeRequestGap)
	gen, like := a.accountGen, e.desired
	go func() {
		err := likeSong(id, like)
		a.post(func() {
			if a.likeEdits[id] == e {
				a.likeAnswered(gen, id, like, err)
			}
		})
	}()
}

// applyLike changes just the liked lists' songs. Their object, route,
// scroll state, sorting and search survive, as does the player's queue.
func (a *app) applyLike(e *likeEdit, like bool) {
	id := e.track.ID
	if a.liked[id] == like {
		return
	}
	a.setLiked(id, like)
	a.likeRevision++
	a.librarySync.at = time.Now()
	delta := -1
	if like {
		delta = 1
	}
	for i := range a.mine {
		if a.mine[i].SpecialType == 5 {
			a.mine[i].TrackCount = max(0, a.mine[i].TrackCount+delta)
		}
	}
	for pid, pl := range a.playlists {
		if !a.isLikedPlaylist(pid) {
			continue
		}
		oldCount := max(pl.TrackCount, len(pl.Tracks))
		at := slices.IndexFunc(pl.Tracks, func(t netease.Track) bool { return t.ID == id })
		if like && at < 0 {
			pos, known := e.positions[pid]
			if !known || pos < 0 {
				pos = 0
			}
			pl.Tracks = slices.Insert(slices.Clone(pl.Tracks), min(pos, len(pl.Tracks)), e.track)
			entry := struct {
				ID int64 `json:"id"`
			}{ID: id}
			pl.TrackIDs = slices.Insert(slices.Clone(pl.TrackIDs), min(pos, len(pl.TrackIDs)), entry)
		} else if !like && at >= 0 {
			pl.Tracks = slices.Delete(slices.Clone(pl.Tracks), at, at+1)
			pl.TrackIDs = slices.DeleteFunc(slices.Clone(pl.TrackIDs), func(t struct {
				ID int64 `json:"id"`
			}) bool {
				return t.ID == id
			})
		}
		pl.TrackCount = max(0, oldCount+delta)
		if a.playlistAt == nil {
			a.playlistAt = map[int64]time.Time{}
		}
		a.playlistAt[pid] = time.Now()
	}
}
