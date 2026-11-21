package app

import (
	"runtime/debug"
	"time"

	"github.com/egoist/mygo"
)

// In-memory page cache limits prevent long browsing sessions from retaining
// unbounded playlist track slices, artist catalogs, albums, podcasts, and
// search results for the lifetime of the process.
const (
	maxCachedPlaylists = 24
	maxCachedArtists   = 16
	maxCachedAlbums    = 24
	maxCachedUsers     = 12
	maxCachedPodcasts  = 16
	maxCachedSearches  = 12
	maxNavHistory      = 64
	maxAutoTagCache    = 256
	hiddenArtBudget    = 4 << 20
	reclaimIdleDelay   = 2500 * time.Millisecond
)

// trimPageCaches bounds in-memory page maps while preserving the active route,
// liked playlist, logged-in user profile, and any in-flight loads.
func (a *app) trimPageCaches() {
	if len(a.playlists) > maxCachedPlaylists {
		for id := range a.playlists {
			if len(a.playlists) <= maxCachedPlaylists {
				break
			}
			if (a.route.page == pagePlaylist && a.route.id == id) || a.isLikedPlaylist(id) || a.loadingPl[id] {
				continue
			}
			delete(a.playlists, id)
			delete(a.playlistAt, id)
			delete(a.plErr, id)
		}
	}
	if len(a.artists) > maxCachedArtists {
		for id, d := range a.artists {
			if len(a.artists) <= maxCachedArtists {
				break
			}
			if (a.route.page == pageArtist && a.route.id == id) || (d != nil && d.loading) {
				continue
			}
			delete(a.artists, id)
		}
	}
	if len(a.albums) > maxCachedAlbums {
		for id, d := range a.albums {
			if len(a.albums) <= maxCachedAlbums {
				break
			}
			if (a.route.page == pageAlbum && a.route.id == id) || (d != nil && d.loading) {
				continue
			}
			delete(a.albums, id)
		}
	}
	if len(a.users) > maxCachedUsers {
		for id, d := range a.users {
			if len(a.users) <= maxCachedUsers {
				break
			}
			if (a.route.page == pageUser && a.route.id == id) || (a.profile != nil && a.profile.UserID == id) || (d != nil && (d.loading > 0 || d.subsLoading > 0)) {
				continue
			}
			delete(a.users, id)
		}
	}
	if len(a.podcasts) > maxCachedPodcasts {
		for id, d := range a.podcasts {
			if len(a.podcasts) <= maxCachedPodcasts {
				break
			}
			if (a.route.page == pagePodcast && a.route.id == id) || (d != nil && (d.loading || d.page.loading)) {
				continue
			}
			delete(a.podcasts, id)
		}
	}
	if len(a.searches) > maxCachedSearches {
		for r, d := range a.searches {
			if len(a.searches) <= maxCachedSearches {
				break
			}
			if r == a.route || r == a.searched || (d != nil && d.page.loading) {
				continue
			}
			delete(a.searches, r)
		}
	}
}

// trimAutoCache keeps the auto-effects song tag cache bounded.
func (a *app) trimAutoCache() {
	if len(a.auto.cache) <= maxAutoTagCache {
		return
	}
	for id := range a.auto.cache {
		if len(a.auto.cache) <= maxAutoTagCache*3/4 {
			break
		}
		if id == a.auto.song || id == a.auto.asked {
			continue
		}
		delete(a.auto.cache, id)
	}
}

// releaseHiddenMemory drops transient visual caches when the main window hides
// or minimizes and schedules a deferred OS memory scavenge after the UI host's
// idle timeout has released GPU textures and text atlases.
func (a *app) releaseHiddenMemory(hidden bool) {
	a.backdropCache.canvas = nil
	a.backdropCache.before = nil
	if hidden {
		a.backdropCache.art = nil
		a.art.trim(hiddenArtBudget)
	}
	a.trimPageCaches()
	a.trimAutoCache()
	if a.freeMemTimer != nil {
		a.freeMemTimer.Stop()
	}
	a.freeMemGen++
	gen := a.freeMemGen
	if a.win == nil {
		return
	}
	a.freeMemTimer = time.AfterFunc(reclaimIdleDelay, func() {
		mygo.RunOnMain(func() {
			if gen != a.freeMemGen || a.win == nil {
				return
			}
			if a.win.IsVisible() && !a.win.IsMinimized() {
				return
			}
			go debug.FreeOSMemory()
		})
	})
}

func (a *app) cancelMemoryReclaim() {
	a.freeMemGen++
	if a.freeMemTimer != nil {
		a.freeMemTimer.Stop()
		a.freeMemTimer = nil
	}
}
