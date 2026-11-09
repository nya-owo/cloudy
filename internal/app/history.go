package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/netease"
)

// What the listener played in Cloudy on this computer is kept in
// history.json in the user data directory, the latest first, each song
// once, as the 最近播放 page lists it. A song counts once it is heard for
// a while, or to its end: one skipped past is not played.

// playedSong is a song played, and when it was last.
type playedSong struct {
	Track netease.Track `json:"track"`
	At    int64         `json:"at"` // Unix milliseconds
}

// historyLimit is how many songs are kept, and historyAfter how long one
// is heard before it counts.
const (
	historyLimit = 500
	historyAfter = 10 * time.Second
)

// historyPath is where the songs played are kept; tests replace it.
var historyPath = func() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "history.json"), nil
}

// historyKey tells songs apart: by NetEase's id, an episode by its
// program's, and a song only on this computer by its file.
func historyKey(t *netease.Track) string {
	if t.ID == 0 && t.ProgramID == 0 {
		return "file:" + t.LocalPath
	}
	return strconv.FormatInt(t.ID, 10) + "/" + strconv.FormatInt(t.ProgramID, 10)
}

// addPlayed puts t at the head of played, taking it out from further down,
// and keeps the list to historyLimit.
func addPlayed(played []playedSong, t netease.Track, at time.Time) []playedSong {
	key := historyKey(&t)
	out := make([]playedSong, 0, min(len(played)+1, historyLimit))
	out = append(out, playedSong{Track: t, At: at.UnixMilli()})
	for _, p := range played {
		if len(out) == historyLimit {
			break
		}
		if historyKey(&p.Track) != key {
			out = append(out, p)
		}
	}
	return out
}

// loadHistory reads the songs played, once.
func (a *app) loadHistory() {
	r := &a.recent
	if r.loaded {
		return
	}
	r.loaded = true
	path, err := historyPath()
	if err != nil {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var played []playedSong
	if json.Unmarshal(b, &played) == nil {
		r.played = played
	}
}

// remember adds a song the listener moved on from, or heard to its end, to
// the songs played, if they heard enough of it.
func (a *app) remember(t netease.Track, played time.Duration, ended bool) {
	if !ended && played < historyAfter {
		return
	}
	a.loadHistory()
	a.recent.played = addPlayed(a.recent.played, t, time.Now())
	a.saveHistory()
	a.redraw()
}

// clearHistory forgets the songs played.
func (a *app) clearHistory() {
	a.recent.played, a.recent.loaded = nil, true
	a.saveHistory()
}

// historyWrites keeps the file to the latest list written: writes run off
// the main thread, and one begun later never loses to one begun before.
var (
	historyWrites struct {
		sync.Mutex
		last int64
	}
	historyGen atomic.Int64
)

// saveHistory writes the songs played, off the main thread.
func (a *app) saveHistory() {
	b, err := json.Marshal(a.recent.played)
	if err != nil {
		return
	}
	path, err := historyPath()
	if err != nil {
		return
	}
	gen := historyGen.Add(1)
	go func() {
		w := &historyWrites
		w.Lock()
		defer w.Unlock()
		if gen <= w.last {
			return
		}
		if writeFileAtomic(path, b, 0o600) == nil {
			w.last = gen
		}
	}()
}

// recentSongs are the songs the 最近播放 page lists: the one playing,
// though it has not counted yet, then those played.
func (a *app) recentSongs() []netease.Track {
	a.loadHistory()
	played := a.recent.played
	out := make([]netease.Track, 0, len(played)+1)
	cur := a.pl.Current()
	if cur != nil {
		out = append(out, *cur)
	}
	for i := range played {
		if cur != nil && historyKey(&played[i].Track) == historyKey(cur) {
			continue
		}
		out = append(out, played[i].Track)
	}
	return out
}
