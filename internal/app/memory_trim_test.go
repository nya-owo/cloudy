package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

func TestArtCacheTrimAndBackdropDedupe(t *testing.T) {
	now := time.Unix(1000, 0)
	c := newArtCache(func(fn func()) { fn() })
	c.now = func() time.Time { return now }

	if c.limit != 16<<20 {
		t.Fatalf("artCache limit = %d, want 16 MiB", c.limit)
	}

	// Loading a "#backdrop" entry lets non-backdrop get() reuse it directly.
	bdArt := &artwork{used: now, size: 1 << 20}
	c.add("https://example.com/cover.jpg#backdrop", bdArt)
	if got := c.get("https://example.com/cover.jpg", false); got != bdArt {
		t.Fatal("non-backdrop artCache.get did not reuse loaded #backdrop entry")
	}

	// Add older and newer covers, then trim down to 2 MiB: newest must survive.
	for i := range 6 {
		now = now.Add(time.Second)
		c.add(fmt.Sprintf("https://example.com/%d.jpg", i), &artwork{used: now, size: 1 << 20})
	}
	c.trim(2 << 20)
	if c.size > 2<<20 {
		t.Fatalf("after trim(2MiB), size = %d", c.size)
	}
	if _, ok := c.art["https://example.com/5.jpg"]; !ok {
		t.Fatal("trim evicted the most recently used cover")
	}
	if _, ok := c.art["https://example.com/0.jpg"]; ok {
		t.Fatal("trim kept the oldest cover")
	}
}

func TestTrimPageCachesPreservesActiveAndLiked(t *testing.T) {
	a := newTestApp()
	a.profile = &netease.Profile{UserID: 777}
	a.mine = []netease.Playlist{{ID: 100, Name: "我喜欢的音乐", SpecialType: 5}}
	a.route = route{page: pagePlaylist, id: 200}
	a.searched = route{page: pageSearch, query: "keep"}

	for i := int64(1); i <= 40; i++ {
		a.playlists[i] = &netease.Playlist{ID: i}
		a.albums[i] = &albumData{}
		a.artists[i] = &artistData{}
		a.users[i] = &userData{}
		a.podcasts[i] = &podcastData{}
		a.searches[route{page: pageSearch, query: fmt.Sprint(i)}] = &searchData{}
	}
	a.playlists[100] = &netease.Playlist{ID: 100}
	a.playlists[200] = &netease.Playlist{ID: 200}
	a.users[777] = &userData{}
	a.searches[a.searched] = &searchData{}

	a.trimPageCaches()

	if len(a.playlists) > maxCachedPlaylists {
		t.Fatalf("playlists = %d, want <= %d", len(a.playlists), maxCachedPlaylists)
	}
	if a.playlists[100] == nil || a.playlists[200] == nil {
		t.Fatal("trimPageCaches evicted liked or active playlist")
	}
	if len(a.albums) > maxCachedAlbums || len(a.artists) > maxCachedArtists ||
		len(a.users) > maxCachedUsers || len(a.podcasts) > maxCachedPodcasts ||
		len(a.searches) > maxCachedSearches {
		t.Fatalf("caches exceeded limits: albums=%d artists=%d users=%d podcasts=%d searches=%d",
			len(a.albums), len(a.artists), len(a.users), len(a.podcasts), len(a.searches))
	}
	if a.users[777] == nil {
		t.Fatal("trimPageCaches evicted logged-in user")
	}
	if a.searches[a.searched] == nil {
		t.Fatal("trimPageCaches evicted last searched route")
	}
}

func TestTrimAutoCacheBoundsEntries(t *testing.T) {
	a := newTestApp()
	a.auto.song = 42
	a.auto.cache = map[int64][2][]string{}
	for i := int64(0); i < maxAutoTagCache+50; i++ {
		a.auto.cache[i] = [2][]string{{"流行"}, nil}
	}
	a.trimAutoCache()
	if len(a.auto.cache) > maxAutoTagCache {
		t.Fatalf("auto.cache = %d, want <= %d", len(a.auto.cache), maxAutoTagCache)
	}
	if _, ok := a.auto.cache[42]; !ok {
		t.Fatal("trimAutoCache evicted current song")
	}
}

func TestReleaseHiddenMemoryDropsBackdropAndTrimsArt(t *testing.T) {
	a := newTestApp()
	a.backdropCache.canvas = ui.NewCanvas(64, 64)
	a.backdropCache.before = ui.NewCanvas(64, 64)
	a.backdropCache.art = &artwork{size: 1 << 20}
	for i := range 8 {
		a.art.add(fmt.Sprintf("https://example.com/%d.jpg", i), &artwork{used: time.Now().Add(time.Duration(i) * time.Second), size: 1 << 20})
	}
	a.releaseHiddenMemory(true)
	if a.backdropCache.canvas != nil || a.backdropCache.before != nil || a.backdropCache.art != nil {
		t.Fatal("releaseHiddenMemory(true) retained backdrop canvas or art")
	}
	if a.art.size > hiddenArtBudget {
		t.Fatalf("releaseHiddenMemory(true) left artCache at %d bytes, want <= %d", a.art.size, hiddenArtBudget)
	}
}
