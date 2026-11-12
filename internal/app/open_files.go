package app

import (
	"path/filepath"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

// openedFiles are the songs opened with Cloudy from the file manager, as
// they come: one at a time, a launch each, which the single instance hands
// on. Those that come together play together.
type openedFiles struct {
	mu    sync.Mutex
	paths []string
	timer *time.Timer
}

// openedTogether is how long after one opened song the next still comes
// with it: the file manager opens a selection one file at a time.
const openedTogether = 400 * time.Millisecond

// listenForOpenedFiles plays the songs opened with Cloudy. Registered before
// the app runs, it hears those it was launched with too.
func (a *app) listenForOpenedFiles() {
	a.opened = &openedFiles{}
	mygo.App.OnOpenFile(func(path string) {
		o := a.opened
		o.mu.Lock()
		defer o.mu.Unlock()
		o.paths = append(o.paths, path)
		if o.timer == nil {
			o.timer = time.AfterFunc(openedTogether, a.playOpenedFiles)
		} else {
			o.timer.Reset(openedTogether)
		}
	})
}

// playOpenedFiles plays the songs opened, once the window is there to.
func (a *app) playOpenedFiles() {
	o := a.opened
	o.mu.Lock()
	if a.win == nil {
		o.timer.Reset(openedTogether)
		o.mu.Unlock()
		return
	}
	paths := o.paths
	o.paths, o.timer = nil, nil
	o.mu.Unlock()
	art := ""
	if cache, err := mygo.App.Path(mygo.PathCache); err == nil {
		art = filepath.Join(cache, "local-covers")
	}
	tracks, other := droppedTracks(paths, art)
	a.post(func() {
		a.showMain()
		a.playDropped(tracks, other)
	})
}

// appIdentifier is Cloudy's identifier, as mygo.json says: kept from when it
// was Cloud-nya, which its data and registrations are under.
const appIdentifier = "io.github.nya-owo.cloud-nya"
