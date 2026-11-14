package app

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// moreState holds what 1.2.13's additions keep between frames.
type moreState struct {
	// queue is the queue's popover over the player bar.
	queue struct {
		open bool
		list ui.ListState
		at   int // as app.queueAt
	}
	// draft is the comment being written in Now Playing.
	draft commentDraft
	// numbers are the album places of NetEase songs whose copies lacked
	// them, as songs kept before 1.2.13 do (album_numbers.go).
	numbers numberCache
	// switchedAt is when the songs sorted by album last switched between
	// the list and the grid, and gridCols how many cards a line of the
	// grid held (ui_album_grid.go).
	switchedAt time.Time
	gridCols   int
	// volumeAt is when Now Playing's volume last moved, for its percent
	// to show a moment (npVolume).
	volumeAt time.Time
}
