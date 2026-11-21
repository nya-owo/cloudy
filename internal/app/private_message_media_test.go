package app

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"netease-music-mygo/internal/netease"
)

func privateMediaPosted(t *testing.T, posted <-chan func()) {
	t.Helper()
	select {
	case fn := <-posted:
		fn()
	case <-time.After(3 * time.Second):
		t.Fatal("metadata worker did not post its result")
	}
}

// Missing artwork is fetched once per resource, published only on post, and reused across repeated frames.
func TestPrivateMediaCache(t *testing.T) {
	var cache privateMediaCache
	now := time.Unix(100, 0)
	posted := make(chan func(), 10)
	var calls atomic.Int32
	load := func(kind string, id int64) (netease.PrivateMessageMedia, error) {
		calls.Add(1)
		return netease.PrivateMessageMedia{Kind: kind, ID: id, Title: "详情标题", Subtitle: "合成作者", CoverURL: "https://p1.music.126.net/fixture.jpg"}, nil
	}
	post := func(fn func()) { posted <- fn }
	for _, kind := range []string{"song", "album", "playlist"} {
		m := netease.PrivateMessageMedia{Kind: kind, ID: 41, Title: "原消息标题"}
		for range 12 {
			got := cache.resolve(m, now, load, post)
			if got.CoverURL != "" || got.Title != m.Title {
				t.Fatal("worker changed display state before main-thread post")
			}
		}
		privateMediaPosted(t, posted)
		for range 12 {
			got := cache.resolve(m, now.Add(time.Hour), load, post)
			if got.CoverURL == "" || got.Subtitle != "合成作者" || got.Title != m.Title {
				t.Fatal("metadata was lost or replaced the message's title")
			}
		}
	}
	if calls.Load() != 3 || len(cache.items) != 3 {
		t.Fatal("resource kind/ID deduplication failed")
	}
	for _, m := range []netease.PrivateMessageMedia{{Kind: "song"}, {Kind: "unknown", ID: 55}, {Kind: "song", ID: 55, CoverURL: "https://p1.music.126.net/ready.jpg"}} {
		cache.resolve(m, now, load, post)
	}
	if len(cache.items) != 3 {
		t.Fatal("complete, unknown or ID-less media started lookup")
	}
}

// Failed lookups keep the readable card and wait a minute before a later frame may retry.
func TestPrivateMediaRetry(t *testing.T) {
	var cache privateMediaCache
	now := time.Unix(100, 0)
	posted := make(chan func(), 3)
	post := func(fn func()) { posted <- fn }
	var calls atomic.Int32
	load := func(kind string, id int64) (netease.PrivateMessageMedia, error) {
		if calls.Add(1) == 1 {
			return netease.PrivateMessageMedia{}, errors.New("synthetic offline")
		}
		return netease.PrivateMessageMedia{Kind: kind, ID: id, CoverURL: "https://p2.music.126.net/retry.jpg"}, nil
	}
	m := netease.PrivateMessageMedia{Kind: "album", ID: 42, Title: "离线也可读"}
	cache.resolve(m, now, load, post)
	privateMediaPosted(t, posted)
	for range 10 {
		if got := cache.resolve(m, now.Add(59*time.Second), load, post); got != m {
			t.Fatal("failed enrichment changed original display fields")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("failure retried every frame")
	}
	cache.resolve(m, now.Add(time.Minute), load, post)
	privateMediaPosted(t, posted)
	if got := cache.resolve(m, now.Add(time.Minute), load, post); got.CoverURL == "" || calls.Load() != 2 {
		t.Fatal("eligible retry did not fill the cover")
	}
}
