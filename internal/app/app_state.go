package app

import (
	"github.com/egoist/mygo"
	"sync/atomic"
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
	// The folder being read: scanning is the scan's number while it runs,
	// scanGen the last begun; scanned says how far it has read.
	scanning, scanGen int
	scanned           *scanCount
}

// scanCount is how many of how many songs a scan has read, as it reads.
type scanCount struct{ done, all atomic.Int64 }

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

// settingsView is the state of the settings panel, apart from the settings.
type settingsView struct {
	open   bool
	tab    settingsCategory
	scroll [settingsCategoryCount]ui.ScrollState
	// query is what is searched in the settings, searchBox its field, and
	// searchScroll how far what it found is scrolled.
	query        string
	searchBox    ui.Handle
	searchScroll ui.ScrollState
	dirty        bool       // a slider moved, and the settings are not kept yet
	accentDraft  accentPick // the color well open, and the color it shows
	fontPick     fontPicker // the dialog of the system's fonts
	confirmGlass bool       // asking before enabling global liquid glass
}
