package app

import (
	"time"

	"netease-music-mygo/internal/netease"
)

const personalRefreshGap = 5 * time.Second

var (
	fetchPlaylist          = netease.PlaylistDetail
	fetchLiked             = netease.LikedIDs
	fetchPersonalPlaylists = netease.UserPlaylists
)

type librarySync struct {
	at     time.Time
	busy   bool
	pollAt time.Time
}

type libraryReply struct {
	liked              []int64
	made, saved        []netease.Playlist
	likesErr, listsErr error
}

// freshenPersonal checks changes made on another device without emptying
// any page. Focus and re-entry share a short throttle.
func (a *app) freshenPersonal() {
	a.freshenHome()
	a.refreshLibrary(time.Now())
	if a.route.page == pagePlaylist {
		a.loadPlaylist(a.route.id, false)
	}
}

func (a *app) pollPersonal(now time.Time) {
	if now.Sub(a.librarySync.pollAt) < 30*time.Second {
		return
	}
	a.librarySync.pollAt = now
	a.freshenPersonal()
}

func (a *app) refreshLibrary(now time.Time) {
	if a.profile == nil || a.win == nil || a.librarySync.busy || len(a.likeEdits) != 0 || a.likeBatches != 0 || now.Sub(a.librarySync.at) < personalRefreshGap {
		return
	}
	a.librarySync.busy, a.librarySync.at = true, now
	uid, gen, revision := a.profile.UserID, a.accountGen, a.likeRevision
	go func() {
		var r libraryReply
		r.liked, r.likesErr = fetchLiked(uid)
		r.made, r.saved, r.listsErr = fetchPersonalPlaylists(uid)
		a.post(func() { a.libraryAnswered(gen, revision, r) })
	}()
}

func (a *app) libraryAnswered(gen, revision int, r libraryReply) {
	if gen != a.accountGen || a.profile == nil {
		return
	}
	a.librarySync.busy = false
	// A heart changed after this request started. Keep that newer state,
	// including its playlist edits, until the next quiet refresh.
	if revision != a.likeRevision || len(a.likeEdits) != 0 || a.likeBatches != 0 {
		return
	}
	if r.likesErr == nil {
		a.liked = make(map[int64]bool, len(r.liked))
		for _, id := range r.liked {
			a.liked[id] = true
		}
	}
	if r.listsErr == nil {
		a.mine, a.saved = r.made, r.saved
	}
}

func (a *app) isLikedPlaylist(id int64) bool {
	if pl := a.playlists[id]; pl != nil && pl.SpecialType == 5 {
		return true
	}
	for _, pl := range a.mine {
		if pl.ID == id && pl.SpecialType == 5 {
			return true
		}
	}
	return false
}
