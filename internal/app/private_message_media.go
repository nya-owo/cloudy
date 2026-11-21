package app

import (
	"time"

	"netease-music-mygo/internal/netease"
)

var loadPrivateMediaDetail = netease.PrivateMessageMediaDetail

type privateMediaKey struct {
	kind string
	id   int64
}

type privateMediaEntry struct {
	media   netease.PrivateMessageMedia
	loading bool
	retryAt time.Time
}

// Metadata is memory-only, shared by resource, and discarded with the account.
// Image bytes still belong to artCache, including its existing failure retry.
type privateMediaCache struct {
	items map[privateMediaKey]*privateMediaEntry
}

func (a *app) privateMedia(media netease.PrivateMessageMedia) netease.PrivateMessageMedia {
	if a.profile == nil {
		return media
	}
	if !a.dm.open {
		return a.dm.media.cached(media) // keep resolved content throughout the exit; never start work
	}
	if a.dm.media == nil {
		a.dm.media = &privateMediaCache{}
	}
	cache, account := a.dm.media, a.accountGen
	return cache.resolve(media, time.Now(), loadPrivateMediaDetail, func(fn func()) {
		a.post(func() {
			if a.accountGen == account && a.dm.media == cache {
				fn()
			}
		})
	})
}

func (c *privateMediaCache) resolve(media netease.PrivateMessageMedia, now time.Time,
	load func(string, int64) (netease.PrivateMessageMedia, error), post func(func())) netease.PrivateMessageMedia {
	if media.CoverURL != "" || media.ID <= 0 {
		return media
	}
	switch media.Kind {
	case "song", "album", "playlist":
	default:
		return media
	}
	if c.items == nil {
		c.items = map[privateMediaKey]*privateMediaEntry{}
	}
	key := privateMediaKey{media.Kind, media.ID}
	e := c.items[key]
	if e == nil {
		e = &privateMediaEntry{}
		c.items[key] = e
	}
	if !e.loading && e.media.CoverURL == "" && !now.Before(e.retryAt) {
		e.loading = true
		go func() {
			detail, err := load(key.kind, key.id)
			post(func() {
				e.loading = false
				e.retryAt = now.Add(time.Minute)
				if err == nil && detail.Kind == key.kind && detail.ID == key.id {
					e.media = detail
				}
			})
		}()
	}
	return c.cached(media)
}

func (c *privateMediaCache) cached(media netease.PrivateMessageMedia) netease.PrivateMessageMedia {
	if c == nil || media.CoverURL != "" || media.ID <= 0 {
		return media
	}
	if e := c.items[privateMediaKey{media.Kind, media.ID}]; e != nil {
		if media.Title == "" {
			media.Title = e.media.Title
		}
		if media.Subtitle == "" {
			media.Subtitle = e.media.Subtitle
		}
		media.CoverURL = e.media.CoverURL
	}
	return media
}
