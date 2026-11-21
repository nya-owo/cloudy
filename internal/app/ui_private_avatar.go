package app

import (
	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

type privateAvatarKey struct {
	account int
	userID  int64
}

func (a *app) privateAvatar(c *ui.Context, user netease.MessageUser, size float32, empty ui.Color, visible bool) {
	key := privateAvatarKey{account: a.accountGen, userID: user.UserID}
	box := ui.Box(c.Key(key)).Size(size, size).Radius(size / 2).Shrink(0)
	held := ui.Local(box, "artwork", func() *artwork { return nil })
	if !visible || user.AvatarURL == "" {
		*held = nil
		box.Background(empty)
		return
	}
	// The chat header shares the 38px contact row's artwork.
	art := a.art.get(netease.Sized(user.AvatarURL, max(114, int(size*3))), false)
	bmp := bitmap(art)
	// Keep the last displayed image alive through cache eviction and while
	// a replacement for the same contact loads.
	if bmp != nil {
		*held = art
	} else {
		bmp = bitmap(*held)
	}
	in := artIn(box, bmp)
	if in < 1 {
		box.Background(empty)
	}
	box.Children(func() { artImage(c, bmp, size/2, in) })
}
