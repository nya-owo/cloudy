package player

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/audio"

	"netease-music-mygo/internal/local"
	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

// preloadLead is how long before a track's end the next one starts
// loading: its address, the first of its audio, its lyrics and cover, so
// that it plays the moment this one ends.
const preloadLead = 20 * time.Second

// A preload is the next track, loading while this one plays. Each of its
// parts closes its channel once in; whoever takes it from Player.pre owns
// its stream.
type preload struct {
	at     int
	id     int64
	level  string
	cancel context.CancelFunc

	audio chan struct{}
	info  *netease.Stream
	s     stream
	err   error

	words chan struct{}
	lines []lyrics.Line

	art      chan struct{}
	cover    []byte
	coverErr error
}

// ready reports whether the preload's audio is in.
func (n *preload) ready() bool {
	select {
	case <-n.audio:
		return true
	default:
		return false
	}
}

// discard stops the preload, and closes its stream once it opened.
func (n *preload) discard() {
	n.cancel()
	go func() {
		<-n.audio
		if n.s != nil {
			n.s.Close()
		}
	}()
}

// startPreload loads the track at order[at] for what follows.
func (p *Player) startPreload(at int) {
	t := p.queue[p.order[at]]
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	n := &preload{at: at, id: t.ID, level: p.level(), cancel: cancel,
		audio: make(chan struct{}), words: make(chan struct{}), art: make(chan struct{})}
	p.pre = n
	go func() {
		defer close(n.audio)
		defer cancel()
		n.info, n.s, n.err = p.openContext(ctx, t, n.level)
	}()
	go func() {
		defer close(n.words)
		n.lines = fetchLyrics(t)
	}()
	go func() {
		defer close(n.art)
		n.cover, n.coverErr = fetchCover(t)
	}()
}

// takePreload returns the preload of the track at order[at], as level,
// played from its start, and drops any other.
func (p *Player) takePreload(at int, level string, from time.Duration, retries int) *preload {
	n := p.pre
	p.pre = nil
	if n == nil {
		return nil
	}
	if n.at == at && at >= 0 && at < len(p.order) && p.queue[p.order[at]].ID == n.id &&
		n.level == level && from == 0 && retries == 0 {
		return n
	}
	n.discard()
	return nil
}

func (p *Player) level() string {
	if p.Quality != nil {
		return p.Quality()
	}
	return "exhigh"
}

// fetchLyrics fetches a track's lyrics. A downloaded song is still
// NetEase's, whose lyrics it fetches; a file put in the folder by hand has
// only the .lrc beside it, if any, which is also what a downloaded song
// falls back to offline.
func fetchLyrics(t netease.Track) []lyrics.Line {
	var lines []lyrics.Line
	var err error = errNoLyrics
	if t.ID > 0 {
		var lrc, trans, yrc string
		if lrc, trans, yrc, err = lyricsOf(t.ID); err == nil {
			lines = lyrics.Build(lrc, trans, yrc)
		}
	}
	if err != nil && t.LocalPath != "" {
		if b, e := os.ReadFile(strings.TrimSuffix(t.LocalPath, filepath.Ext(t.LocalPath)) + ".lrc"); e == nil {
			lines = lyrics.FromText(string(b))
		}
		if len(lines) == 0 {
			// Lyrics a tagging app wrote into the file itself.
			lines = lyrics.FromText(local.LyricsOf(t.LocalPath))
		}
	}
	return lines
}

// fetchCover fetches a track's cover; tests keep it off the network.
var fetchCover = func(t netease.Track) ([]byte, error) {
	return netease.Fetch(netease.Sized(t.Album.PicURL, 600))
}

// watchEnd tells nearEnd, while out plays its last stretch, how much of it
// is left, until stop closes done.
func (p *Player) watchEnd(gen int, out *audio.Player, length time.Duration, done chan struct{}) {
	lead := preloadLead + p.crossfade()
	go func() {
		tick := time.NewTicker(250 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
			}
			if !out.Playing() {
				continue
			}
			total := out.Duration()
			if total <= 0 {
				total = length
			}
			if total <= 0 {
				continue
			}
			left := total - out.Position()
			if left > lead {
				continue
			}
			p.post(func() {
				if gen == p.gen {
					p.nearEnd(left)
				}
			})
		}
	}()
}

// crossfade is how long songs fade into each other, as the settings say:
// 0 for not at all.
func (p *Player) crossfade() time.Duration {
	if p.Crossfade == nil {
		return 0
	}
	return p.Crossfade()
}

// nearEnd readies what plays next, left before the track playing ends:
// loads it, and as the end comes, fades one into the other.
func (p *Player) nearEnd(left time.Duration) {
	if !p.playing || p.out == nil || p.seeking || p.track == nil {
		return
	}
	at := p.nextPosition()
	if at < 0 || p.repeat == RepeatOne || p.StopAfterSong {
		return
	}
	next := p.queue[p.order[at]]
	if n := p.pre; n == nil || n.at != at || n.id != next.ID || n.level != p.level() {
		if n != nil {
			n.discard()
		}
		p.startPreload(at)
	}
	d := p.crossfade()
	// Episodes play on as they are, and a track shorter than the fades
	// does not fade.
	if d <= 0 || p.track.ProgramID != 0 || next.ProgramID != 0 ||
		p.Duration() < 3*d || next.Length() < 3*d {
		return
	}
	if left > d || p.endFade == p.gen {
		return
	}
	p.endFade = p.gen
	if n := p.pre; n.ready() && n.err == nil {
		p.crossTo(at, d)
		return
	}
	// The next is not in yet: this one fades out as it ends, the next in
	// as it starts.
	p.fade.fade(0, left, nil)
	p.fadeInNext = d
}

// A tail is a track faded out on its own, under the next fading in.
type tail struct {
	fade fader
	out  *audio.Player
	s    stream
}

// close stops the tail's player and its download.
func (t *tail) close() {
	t.out.Pause()
	s, out := t.s, t.out
	time.AfterFunc(100*time.Millisecond, func() {
		if s != nil {
			dl, _ := s.source()
			dl.Close()
		}
		out.Close()
	})
}

// crossTo plays the track at order[at], loaded, fading in over d, while
// the one playing fades out under it.
func (p *Player) crossTo(at int, d time.Duration) {
	p.tellListened(true)
	out, s := p.out, p.stream
	level, gain := p.fade.detach()
	p.out, p.stream = nil, nil
	p.fadeInNext = d
	p.loadAttempt(at, 0, true, 0, 1, 0)
	t := &tail{out: out, s: s}
	t.fade.gain = gain
	t.fade.attach(out, level)
	p.tail = t
	t.fade.fade(0, d, func() {
		p.post(func() {
			if p.tail == t {
				p.tail = nil
			}
			t.close()
		})
	})
}

// cutTail fades out quickly what still fades out under the track playing.
func (p *Player) cutTail() {
	t := p.tail
	if t == nil {
		return
	}
	p.tail = nil
	t.fade.fade(0, fadeOut, func() { p.post(t.close) })
}
