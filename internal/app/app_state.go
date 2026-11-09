package app

import (
	"github.com/egoist/mygo"
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

// shortcutState is the global keys and the menu keys.
type shortcutState struct {
	bound  map[string]string // the global key registered for each action
	held   []string          // the global keys held, to let them go
	taken  int               // how many global keys other apps hold
	setup  shortcutEditor
	paused bool             // the global keys and menu keys are let go, for a recording
	items  []*mygo.MenuItem // the menu items with keys of their own
}
