package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/netease"
)

// The app's state, grouped by what it is for.

// downloadState is the songs saved to disk, the ones on their way there,
// and where both are kept.
type downloadState struct {
	items     []download.Item
	loaded    bool
	err       string
	queue     jobQueue[netease.Track, songKey]
	fixed     bool   // the downloads without tags were looked at
	artDir    string // where covers found in the folder are kept
	index     string // where the index of downloaded songs is kept
	box       ui.Handle
	pathDraft string // the folder path being typed in the settings
	view      queueWindow
	deletion  downloadDelete
}

// uploadState is the songs going up to the cloud drive, as downloads come
// down.
type uploadState struct {
	queue    jobQueue[cloudUpload, fileKey]
	user     int64 // the account the queue goes up for, 0 for none
	sentSome bool  // a song went up since the drive was last read
	view     queueWindow
}

// sleepState is the timer that pauses the music once the time set is up.
type sleepState struct {
	timer   *time.Timer
	at      time.Time // when
	minutes int       // what the listener chose
}
