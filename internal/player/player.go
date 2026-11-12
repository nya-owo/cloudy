package player

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/audio"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

type RepeatMode int

const (
	RepeatOff RepeatMode = iota
	RepeatAll
	RepeatOne
)

// Player plays a queue of tracks. Its methods run on the main thread;
// loading happens on goroutines, whose results post brings back there.
type Player struct {
	post func(func())
	// Quality returns the stream level to load, as the settings say.
	Quality func() string
	// Normalize reports whether to even out the loudness of tracks, as
	// the settings say.
	Normalize func() bool
	// Cache keeps the songs played, if set.
	Cache *Cache
	// Downloaded, if set, returns the file a song was downloaded to, or "":
	// a downloaded song plays from it wherever it was started from, not
	// from the network. It is called off the window's goroutine.
	LocalFile func(id int64) string
	// Changed, if set, hears that what plays, where it is or whether it
	// plays changed.
	Changed func()
	// StopAfterSong pauses as the song playing ends, once, rather than
	// going on; SongStopped, if set, hears that it did.
	StopAfterSong bool
	SongStopped   func()
	// PodcastSpeed is how fast episodes play, their pitch kept: 0 or 1 as
	// recorded. Songs play as they are.
	PodcastSpeed float64

	// Listened, if set, hears how long a track was listened to, once the
	// listener moves on from it or it plays to its end.
	Listened func(t netease.Track, played time.Duration, ended bool)

	stretcher *stretch                // between the stream and what plays, for a speed
	curSpeed  float64                 // the speed the track playing plays at
	heardGen  int                     // the load whose listening was told, so it is told once
	fx        atomic.Pointer[Effects] // the effects, which every track's sound goes through
	fxGlide   atomic.Bool             // the effects asked for glide to what they are

	queue   []netease.Track
	order   []int // the queue's indices in play order: shuffled or not
	at      int   // where in order the current track is, -1 for none
	shuffle bool
	repeat  RepeatMode
	volume  float64

	// A radio goes on with the tracks its feed gives, as the queue nears
	// its end, for as long as nothing else is played.
	radio    string
	feed     func(got func([]netease.Track))
	radioGen int  // counts radios, to drop what an old one gives
	feeding  bool // the feed is asked
	awaiting bool // at the queue's end, for what the feed gives

	// cover is the track's cover image as fetched, once it is, and coverOf
	// the track it is of.
	cover   []byte
	coverOf int64

	track      *netease.Track
	out        *audio.Player
	stream     stream
	info       *netease.Stream // what the current track streams
	fade       fader
	gain       float64 // the current track's normalizing gain
	loading    bool
	playing    bool // what the listener wants: false while paused
	err        string
	gen        int                // counts loads, to drop the results of stale ones
	cancelLoad context.CancelFunc // stops a load that is no longer selected
	retryTimer *time.Timer
	retries    int // reconnections for the current selection

	// Crossfade, if set, is how long songs fade into each other as one
	// ends: 0 for not at all.
	Crossfade func() time.Duration

	pre        *preload      // the next track, loading as this one nears its end
	watchDone  chan struct{} // closed to stop watching the track's end
	endFade    int           // the load whose end fades, once
	fadeInNext time.Duration // how long the next track loaded fades in
	tail       *tail         // the track before, fading out under this one

	lyrics       []lyrics.Line
	lyricsLoaded bool

	// pending holds a seek asked for while the track loads.
	pending time.Duration
	// A seek playing fades out, then lands, then fades in: meanwhile the
	// position is where it goes.
	seeking bool
	seekTo  time.Duration
	seekGen int
}

func New(post func(func())) *Player {
	p := &Player{post: post, at: -1, volume: 0.8, gain: 1}
	// Music, mixed far enough ahead to play on while macOS keeps the app
	// waiting, as it does while its appearance changes.
	audio.SetLatency(audio.LatencyPlayback)
	// The media keys, and the controls of Control Center.
	mygo.NowPlaying.OnCommand(func(cmd mygo.MediaCommand, pos time.Duration) {
		switch cmd {
		case mygo.MediaPlay, mygo.MediaPause:
			if (cmd == mygo.MediaPlay) != p.playing {
				p.TogglePlay()
			}
		case mygo.MediaNext:
			p.Next()
		case mygo.MediaPrevious:
			p.Prev()
		case mygo.MediaSeek:
			p.Seek(pos)
		}
		p.post(func() {})
	})
	return p
}

// Cover returns the cover image of the track playing, as fetched (JPEG or
// PNG), or nil until it is.
func (p *Player) Cover() []byte {
	if p.track == nil || p.coverOf != p.track.ID {
		return nil
	}
	return p.cover
}

// sync shows the system, and Changed, where playback is.
func (p *Player) sync() {
	if p.Changed != nil {
		defer p.Changed()
	}
	if p.track == nil {
		mygo.NowPlaying.Clear()
		return
	}
	state := mygo.PlaybackPaused
	if p.playing {
		state = mygo.PlaybackPlaying
	}
	mygo.NowPlaying.SetPlayback(state, p.Position())
}

// announce shows the system the track, and its cover once loaded.
func (p *Player) announce(t *netease.Track, artwork []byte) {
	p.cover, p.coverOf = artwork, t.ID
	state := mygo.PlaybackPaused
	if p.playing {
		state = mygo.PlaybackPlaying
	}
	mygo.NowPlaying.Set(mygo.NowPlayingInfo{
		Title:    t.Name,
		Artist:   t.ArtistNames(),
		Album:    t.Album.Name,
		Artwork:  artwork,
		Duration: t.Length(),
		Position: p.Position(),
		State:    state,
		Commands: mygo.MediaPlay | mygo.MediaPause | mygo.MediaNext | mygo.MediaPrevious | mygo.MediaSeek,
	})
}

func (p *Player) ended() {
	p.tellListened(true)
	if p.StopAfterSong {
		p.StopAfterSong = false
		if p.SongStopped != nil {
			p.SongStopped()
		}
		// The next song waits, paused, for play to start it.
		if at := p.nextPosition(); at >= 0 && p.repeat != RepeatOne {
			p.loadSkipping(at, 0, false, 0, 1)
			return
		}
		p.playing = false
		p.sync()
		return
	}
	if p.repeat == RepeatOne {
		p.out.Seek(0)
		p.out.Play()
		return
	}
	if p.at+1 >= len(p.order) && p.feed != nil {
		p.awaitFeed()
		return
	}
	if p.at+1 >= len(p.order) && p.repeat == RepeatOff {
		p.playing = false
		p.sync()
		return
	}
	p.Next()
}

// PlayQueue replaces the queue with tracks and plays the one at start.
func (p *Player) PlayQueue(tracks []netease.Track, start int) {
	if len(tracks) == 0 {
		return
	}
	p.endRadio()
	start = min(max(start, 0), len(tracks)-1)
	p.queue = append([]netease.Track(nil), tracks...)
	p.order = make([]int, len(p.queue))
	for i := range p.order {
		p.order[i] = i
	}
	if p.shuffle {
		p.shuffleOrder(start)
		p.load(0)
		return
	}
	p.load(start)
}

// AddToQueue adds tracks to the end of the queue without changing what
// plays, and starts the first when nothing does. It returns how many were
// added, those already in the queue left out.
func (p *Player) AddToQueue(tracks []netease.Track) int {
	before := len(p.queue)
	p.appendTracks(tracks)
	added := len(p.queue) - before
	if added > 0 {
		if p.track == nil {
			p.load(len(p.order) - added)
		} else {
			p.sync()
		}
	}
	return added
}

// PlayRadio plays tracks in their order, then goes on with those feed
// gives as the queue nears its end, until something else is played. The
// radio's name says which it is. feed calls got on the main thread.
func (p *Player) PlayRadio(name string, tracks []netease.Track, feed func(got func([]netease.Track))) {
	if len(tracks) == 0 {
		return
	}
	shuffle := p.shuffle
	p.shuffle = false
	p.PlayQueue(tracks, 0)
	p.shuffle = shuffle
	p.AttachRadio(name, feed)
}

// RadioFrom makes the track playing the first of a radio's queue, which
// goes on with tracks, then with those feed gives: without starting it
// over.
func (p *Player) RadioFrom(name string, tracks []netease.Track, feed func(got func([]netease.Track))) {
	if p.track == nil {
		return
	}
	p.queue, p.order, p.at = []netease.Track{*p.track}, []int{0}, 0
	p.appendTracks(tracks)
	p.AttachRadio(name, feed)
}

// AttachRadio makes the queue the radio name's, which feed goes on with,
// as after restoring a session that played it.
func (p *Player) AttachRadio(name string, feed func(got func([]netease.Track))) {
	p.endRadio()
	p.radio, p.feed = name, feed
	p.refill()
}

// EndRadio stops the radio playing: the song playing plays on, but the
// songs the radio queued after it go, and it adds no more.
func (p *Player) EndRadio() {
	if p.radio == "" && p.feed == nil {
		return
	}
	awaiting := p.awaiting
	p.endRadio()
	if p.at >= 0 && p.at+1 < len(p.order) {
		kept := make([]netease.Track, p.at+1)
		for i, k := range p.order[:p.at+1] {
			kept[i] = p.queue[k]
		}
		p.queue, p.order = kept, make([]int, len(kept))
		for i := range p.order {
			p.order[i] = i
		}
	}
	if awaiting {
		// At its end, waiting for more: nothing plays on.
		p.playing = false
		p.sync()
	}
}

func (p *Player) endRadio() {
	p.radioGen++
	p.radio, p.feed, p.feeding, p.awaiting = "", nil, false, false
}

// Radio returns the name of the radio playing, or "".
func (p *Player) Radio() string { return p.radio }

// RadioGeneration changes when a queue or a radio replaces the current one.
// A pending first radio response must not replace that newer selection.
func (p *Player) RadioGeneration() int { return p.radioGen }

// refill asks the radio for more once the queue is near its end.
func (p *Player) refill() {
	if p.feed == nil || p.feeding || p.at < len(p.order)-2 {
		return
	}
	p.feeding = true
	gen := p.radioGen
	p.feed(func(more []netease.Track) {
		if gen != p.radioGen {
			return
		}
		p.feeding = false
		p.appendTracks(more)
		switch {
		case !p.awaiting:
		case p.at+1 < len(p.order):
			p.awaiting = false
			p.load(p.at + 1)
		default:
			// The radio has nothing more: it stops at its end.
			p.awaiting, p.playing = false, false
			p.err = "电台暂时没有更多歌曲"
			p.sync()
		}
	})
}

// awaitFeed waits at the end of a radio's queue for what its feed gives.
func (p *Player) awaitFeed() {
	p.awaiting = true
	p.refill()
}

// appendTracks adds tracks to the end of the queue, but those in it.
func (p *Player) appendTracks(tracks []netease.Track) {
	in := make(map[int64]bool, len(p.queue))
	for _, t := range p.queue {
		in[t.ID] = true
	}
	for _, t := range tracks {
		if !in[t.ID] {
			in[t.ID] = true
			p.order = append(p.order, len(p.queue))
			p.queue = append(p.queue, t)
		}
	}
}

// shuffleOrder shuffles the order, with first first.
func (p *Player) shuffleOrder(first int) {
	rand.Shuffle(len(p.order), func(i, j int) { p.order[i], p.order[j] = p.order[j], p.order[i] })
	for i, v := range p.order {
		if v == first {
			p.order[0], p.order[i] = p.order[i], p.order[0]
		}
	}
}

func (p *Player) ToggleShuffle() {
	p.shuffle = !p.shuffle
	if len(p.order) == 0 {
		return
	}
	cur := p.order[max(p.at, 0)]
	if p.shuffle {
		p.shuffleOrder(cur)
		p.at = 0
	} else {
		for i := range p.order {
			p.order[i] = i
		}
		p.at = cur
	}
}

func (p *Player) CycleRepeat() { p.repeat = (p.repeat + 1) % 3 }

// Current returns the track playing or loading, or nil.
func (p *Player) Current() *netease.Track { return p.track }

// Queue returns the tracks queued, in the order they were given.
func (p *Player) Queue() []netease.Track { return p.queue }

// Playing reports what the listener wants: false while paused.
func (p *Player) Playing() bool { return p.playing }

// Loading reports whether the current track is still loading.
func (p *Player) Loading() bool { return p.loading || p.awaiting }

// Ready reports whether the current track has loaded, to play.
func (p *Player) Ready() bool { return p.out != nil }

// Err returns why the current track did not play, or "".
func (p *Player) Err() string { return p.err }

// Stream returns what the current track streams, once loaded, or nil.
func (p *Player) Stream() *netease.Stream { return p.info }

func (p *Player) Shuffle() bool      { return p.shuffle }
func (p *Player) Repeat() RepeatMode { return p.repeat }

// Lyrics returns the current track's lyrics, and whether they have loaded.
func (p *Player) Lyrics() ([]lyrics.Line, bool) { return p.lyrics, p.lyricsLoaded }

// Downloaded returns the share of the current track downloaded so far, or
// false before it has started to.
func (p *Player) Downloaded() (float64, bool) {
	if p.stream == nil {
		return 0, false
	}
	dl, _ := p.stream.source()
	return dl.Progress(), true
}

// Audio is the current track's sound decoded apart from what plays, as
// OpenAudio opens it: interleaved stereo at its rate, read as it
// downloads.
type Audio interface {
	SampleRate() int
	Channels() int
	Read(dst []float32) (int, error)
	// SetPosition goes to a frame, of SampleRate a second.
	SetPosition(frame int64) error
	Close() error
}

// OpenAudio opens the current track's sound again, for decoding apart
// from what plays, as for transcribing an episode. It reads the same
// download, waiting for what has not come in, and fails once the track
// changes. It works on any goroutine once opened; close it when done.
func (p *Player) OpenAudio() (Audio, error) {
	return p.AudioOpener()(context.Background())
}

// AudioOpener captures the current download on the main thread. The returned
// function opens it on a worker and can cancel waiting for downloaded bytes
// without stopping playback. It never reads Player from that worker.
func (p *Player) AudioOpener() func(context.Context) (Audio, error) {
	if p.stream == nil || p.track == nil {
		return func(context.Context) (Audio, error) {
			return nil, errors.New("player: nothing loaded")
		}
	}
	dl, _ := p.stream.source()
	duration := p.track.Duration
	return func(ctx context.Context) (Audio, error) {
		return openSharedContext(ctx, dl, duration)
	}
}

// PlayOrder returns the queue's indices in play order, and where the
// current track is among them, -1 for none.
func (p *Player) PlayOrder() ([]int, int) { return p.order, p.at }

// nextPosition is where Next will go in the play order, or -1 while a
// radio needs to wait for its feed.
func (p *Player) nextPosition() int {
	if len(p.order) == 0 {
		return -1
	}
	at := p.at + 1
	if at >= len(p.order) {
		if p.feed != nil {
			return -1
		}
		at = 0
	}
	return at
}

// NextIndex returns the queue index Next will play, or -1 when it must wait.
func (p *Player) NextIndex() int {
	if at := p.nextPosition(); at >= 0 {
		return p.order[at]
	}
	return -1
}

func (p *Player) Next() {
	if len(p.order) == 0 {
		return
	}
	at := p.nextPosition()
	if at < 0 {
		p.stop()
		p.awaitFeed()
		return
	}
	p.load(at)
}

// Prev plays the track before this one, however far this one has played:
// once a few seconds in, going back to its start only kept the listener
// pressing twice.
func (p *Player) Prev() {
	if len(p.order) == 0 {
		return
	}
	at := p.at - 1
	if at < 0 {
		at = len(p.order) - 1
	}
	p.loadSkipping(at, 0, true, 0, -1)
}

// Jump plays the queue's track i.
func (p *Player) Jump(i int) {
	for k, v := range p.order {
		if v == i {
			p.load(k)
			return
		}
	}
}

// TogglePlay pauses, fading out, or plays, fading in.
func (p *Player) TogglePlay() {
	if p.out == nil {
		switch {
		case p.loading:
			// It plays once loaded, or not.
			p.playing = !p.playing
			if !p.playing && p.retryTimer != nil {
				p.stop()
			}
			p.sync()
		case len(p.order) > 0:
			p.loadSkipping(max(p.at, 0), 0, true, p.pending, 1)
		}
		return
	}
	out := p.out
	if p.playing {
		p.playing = false
		p.cutTail()
		p.fade.fade(0, fadeOut, func() {
			p.post(func() {
				if p.out == out && !p.playing {
					out.Pause()
					p.sync()
				}
			})
		})
		p.sync()
		return
	}
	p.playing = true
	if !p.seeking && !out.Playing() {
		if out.Position() >= out.Duration()-time.Second {
			out.Seek(0)
		}
		out.Play()
	}
	p.fade.fade(1, fadeIn, nil)
	p.sync()
}

// seekGap is how long a seek waits for what plays to fade out, which the
// audio player does over 5 ms, as it pauses: within the next buffer or
// two it sends the system.
const seekGap = 30 * time.Millisecond

// Seek moves to t. Playing, it pauses and plays again around the jump,
// which the audio player fades over a few milliseconds, rather than
// cutting from one sound to the other with a click.
func (p *Player) Seek(t time.Duration) {
	if p.out == nil {
		p.pending = t
		return
	}
	// Back from where this track was fading out at its end: it plays on.
	if p.endFade == p.gen {
		p.endFade, p.fadeInNext = 0, 0
		if p.playing {
			p.fade.fade(1, fadeIn, nil)
		}
	}
	out := p.out
	// Paused, it jumps at once; but a seek on its way paused it only for
	// the jump, and this one takes its place, playing on from there.
	if !out.Playing() && !p.seeking {
		out.Seek(p.heard(t))
		p.sync()
		return
	}
	p.seekGen++
	gen := p.seekGen
	p.seeking, p.seekTo = true, t
	out.Pause()
	time.AfterFunc(seekGap, func() {
		p.post(func() {
			if gen != p.seekGen || p.out != out {
				return
			}
			p.seeking = false
			out.Seek(p.heard(t))
			if p.playing || !p.fade.silent() {
				out.Play()
			}
			p.sync()
		})
	})
	p.sync()
}

func (p *Player) SetVolume(v float64) {
	p.volume = v
	p.applyGain()
}

// applyGain sets the volume of what plays: the listener's, which the ear
// hears in powers, and the track's gain, if normalizing.
func (p *Player) applyGain() {
	g := p.volume * p.volume
	if p.Normalize != nil && p.Normalize() {
		g *= p.gain
	}
	p.fade.setGain(g)
}

// normPreamp lifts normalized tracks, which NetEase brings to the quiet
// loudness of ReplayGain, nearer to how loud music plays elsewhere.
const normPreamp = 4

// normGain is the gain that brings a track as loud as others, as NetEase
// measured it, but never so loud that its peaks would clip.
func normGain(st *netease.Stream) float64 {
	if st == nil || st.Gain == 0 && st.Peak == 0 {
		return 1
	}
	g := math.Pow(10, (st.Gain+normPreamp)/20)
	if st.Peak > 0 {
		g = min(g, 1/st.Peak)
	}
	return g
}

func (p *Player) Position() time.Duration {
	switch {
	case p.out == nil:
		return p.pending
	case p.seeking:
		return p.seekTo
	}
	return time.Duration(float64(p.out.Position()) * p.speed())
}

func (p *Player) Duration() time.Duration {
	if p.track == nil {
		return 0
	}
	return p.track.Length()
}

// Levels returns the spectrum of what plays now.
func (p *Player) Levels() ([Bands]float32, bool) {
	if p.stream == nil || p.out == nil || !p.playing {
		return [Bands]float32{}, false
	}
	_, spec := p.stream.source()
	frame := int64(p.Position().Seconds() * float64(p.stream.SampleRate()))
	return spec.at(frame)
}

func (p *Player) stop() {
	p.gen++
	if p.retryTimer != nil {
		p.retryTimer.Stop()
		p.retryTimer = nil
	}
	if p.cancelLoad != nil {
		p.cancelLoad()
		p.cancelLoad = nil
	}
	if p.watchDone != nil {
		close(p.watchDone)
		p.watchDone = nil
	}
	p.cutTail()
	if out := p.out; out != nil {
		// Paused first, it fades out over a few milliseconds, rather than
		// stopping with a click.
		out.Pause()
		s := p.stream
		time.AfterFunc(100*time.Millisecond, func() {
			// Closing the audio player alone cannot wake a decoder waiting
			// for network bytes, so cancel its download after fading out.
			if s != nil {
				dl, _ := s.source()
				dl.Close()
			}
			out.Close()
		})
	}
	p.out, p.stream, p.info, p.stretcher, p.curSpeed = nil, nil, nil, nil, 1
	p.loading = false
	p.seeking = false
	p.fade.attach(nil, 1)
}

// load plays the track at order[at], skipping those that cannot play.
func (p *Player) load(at int) {
	p.loadSkipping(at, 0, true, 0, 1)
}

// loadSkipping loads the track at order[at], to play or not, from from,
// skipping those that cannot play: skipped of them so far.
func (p *Player) loadSkipping(at, skipped int, play bool, from time.Duration, direction int) {
	p.loadAttempt(at, skipped, play, from, direction, 0)
}

func (p *Player) loadAttempt(at, skipped int, play bool, from time.Duration, direction, retries int) {
	// Moving on to another track: how long this one was listened to. A
	// retry of the same one is no move.
	if at >= 0 && at < len(p.order) && p.track != nil && p.queue[p.order[at]].ID != p.track.ID {
		p.tellListened(false)
	}
	level := p.level()
	pre := p.takePreload(at, level, from, retries)
	crossIn := p.fadeInNext
	p.fadeInNext = 0
	p.stop()
	p.retries = retries
	gen := p.gen
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	if pre != nil {
		stopLoad := cancel
		cancel = func() {
			pre.cancel()
			stopLoad()
		}
	}
	p.cancelLoad = cancel
	p.at = at
	t := p.queue[p.order[at]]
	p.track = &t
	p.loading, p.playing, p.err = true, play, ""
	p.awaiting = false
	p.lyrics, p.lyricsLoaded = nil, false
	p.pending = from
	p.announce(&t, nil)
	p.refill()

	// What the preload has in already shows at once.
	if pre != nil {
		select {
		case <-pre.words:
			p.lyrics, p.lyricsLoaded = pre.lines, true
		default:
		}
		select {
		case <-pre.art:
			if pre.coverErr == nil {
				p.announce(&t, pre.cover)
			}
		default:
		}
	}
	if p.coverOf != t.ID || p.cover == nil {
		go func() {
			var cover []byte
			var err error
			if pre != nil {
				<-pre.art
				cover, err = pre.cover, pre.coverErr
			} else {
				cover, err = fetchCover(t)
			}
			if err == nil {
				p.post(func() {
					if gen == p.gen {
						p.announce(&t, cover)
						if p.Changed != nil {
							p.Changed()
						}
					}
				})
			}
		}()
	}
	if !p.lyricsLoaded {
		go func() {
			var lines []lyrics.Line
			if pre != nil {
				<-pre.words
				lines = pre.lines
			} else {
				lines = fetchLyrics(t)
			}
			p.post(func() {
				if gen == p.gen {
					p.lyrics = lines
					p.lyricsLoaded = true
				}
			})
		}()
	}

	// A preload with its audio in plays now, with no wait for a post:
	// the end of one track runs into the start of the next.
	if pre != nil && pre.ready() {
		p.opened(gen, t, pre.info, pre.s, pre.err, skipped, direction, crossIn)
		return
	}
	go func() {
		var info *netease.Stream
		var s stream
		var err error
		if pre != nil {
			select {
			case <-pre.audio:
				info, s, err = pre.info, pre.s, pre.err
			case <-ctx.Done():
				pre.discard()
				err = ctx.Err()
			}
		} else {
			info, s, err = p.openContext(ctx, t, level)
		}
		p.post(func() {
			if gen != p.gen {
				if s != nil {
					s.Close()
				}
				return
			}
			p.opened(gen, t, info, s, err, skipped, direction, crossIn)
		})
	}()
}

// opened plays track t, its audio opened, or tells why it cannot: the
// load of gen, which is the current one.
func (p *Player) opened(gen int, t netease.Track, info *netease.Stream, s stream, err error, skipped, direction int, crossIn time.Duration) {
	if p.cancelLoad != nil {
		p.cancelLoad()
		p.cancelLoad = nil
	}
	p.loading = false
	if err != nil {
		if p.retryLoad(skipped, direction, err) {
			return
		}
		p.err = err.Error()
		switch {
		case !errors.Is(err, netease.ErrUnavailable):
		case t.ProgramID != 0:
			p.err = "这期节目需要购买才能收听"
		case errors.Is(err, netease.ErrNoCopyright):
			p.err = "这首歌暂无版权"
		default:
			p.err = "这首歌需要 VIP 才能播放"
		}
		// Only an unavailable song is skipped. A failed connection or
		// cache must not send the listener through the whole queue.
		if errors.Is(err, netease.ErrUnavailable) && skipped+1 < len(p.order) {
			next := (p.at + direction + len(p.order)) % len(p.order)
			if direction > 0 && next == 0 && p.feed != nil {
				p.awaitFeed()
				return
			}
			p.loadSkipping(next, skipped+1, p.playing, 0, direction)
		} else {
			p.playing = false
			p.sync()
		}
		return
	}
	checked := &checkedStream{stream: s}
	p.stream, p.info = checked, info
	p.gain = normGain(info)
	p.stretcher = newStretch(checked, p.speedOf(&t))
	p.curSpeed = p.stretcher.speed
	p.out = audio.NewPlayer(newShaper(p.stretcher, &p.fx, &p.fxGlide))
	// From its middle, as a session picked up again, it fades in, as it
	// does fading into another.
	level := 1.0
	if p.pending > 0 || crossIn > 0 {
		level = 0
	}
	if crossIn <= 0 {
		crossIn = fadeIn
	}
	p.fade.attach(p.out, level)
	p.applyGain()
	p.out.OnEnd(func() {
		p.post(func() {
			if gen == p.gen {
				p.finish(checked)
			}
		})
	})
	if p.pending > 0 {
		p.out.Seek(p.heard(p.pending))
		p.pending = 0
	}
	if p.playing {
		p.out.Play()
		p.fade.fade(1, crossIn, nil)
	}
	p.watchDone = make(chan struct{})
	p.watchEnd(gen, p.out, time.Duration(float64(t.Length())/p.curSpeed), p.watchDone)
	p.sync()
}

// open opens track t's audio at level: kept in the cache, or else
// streamed into it, and kept once it has all come in. A song of the
// listener's folder opens where it is, and is not kept again.
func (p *Player) open(t netease.Track, level string) (*netease.Stream, stream, error) {
	return p.openContext(context.Background(), t, level)
}

var songURL = netease.SongURLContext

// lyricsOf fetches a song's lyrics; tests keep it off the network.
var lyricsOf = netease.Lyrics

var errNoLyrics = errors.New("player: no lyrics")

// openLocal opens the song in the file at path, an MP3 or a FLAC.
func openLocal(path string, d int64) (*netease.Stream, stream, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("本地文件无法读取：%s", filepath.Base(path))
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, fmt.Errorf("本地文件无法读取：%s", filepath.Base(path))
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	if ext != "mp3" && ext != "flac" {
		f.Close()
		return nil, nil, fmt.Errorf("不支持的本地音乐格式：%s", ext)
	}
	st := &netease.Stream{Size: info.Size(), Type: ext, Level: "exhigh"}
	if ext == "flac" {
		st.Level = "lossless"
	}
	s, err := openStream(opened(f, info.Size()), d)
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	return st, s, nil
}

func (p *Player) openContext(ctx context.Context, t netease.Track, level string) (*netease.Stream, stream, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if t.LocalPath != "" {
		return openLocal(t.LocalPath, t.Duration)
	}
	if p.LocalFile != nil && t.ID > 0 && t.ProgramID == 0 {
		// Downloaded: from its file, whatever page it was started from. A
		// file that fails to open plays from the network instead.
		if path := p.LocalFile(t.ID); path != "" {
			if st, s, err := openLocal(path, t.Duration); err == nil {
				return st, s, nil
			}
		}
	}
	dl, info, ok := p.Cache.lookup(t.ID, level)
	if ok {
		s, err := openStream(dl, t.Duration)
		if err == nil {
			return info, s, nil
		}
		path := dl.file.Name()
		dl.Close()
		// A corrupt cached copy is retried from the network for this
		// song alone; it is never treated as a missing playback right.
		p.Cache.remove(path)
	}
	info, err := songURL(ctx, t.ID, level)
	if err != nil {
		return nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	dl = p.Cache.download(t.ID, info)
	stop := context.AfterFunc(ctx, dl.Close)
	s, err := openStream(dl, t.Duration)
	if !stop() || ctx.Err() != nil {
		if err == nil {
			s.Close()
		}
		dl.Close()
		return nil, nil, ctx.Err()
	}
	if err != nil {
		dl.mu.Lock()
		if dl.err != nil {
			err = dl.err
		}
		dl.mu.Unlock()
		dl.Close()
		return nil, nil, err
	}
	return info, s, nil
}

// Session is where playback was left, to pick up again.
type Session struct {
	Queue    []netease.Track `json:"queue"`
	Order    []int           `json:"order"`
	At       int             `json:"at"`
	Shuffle  bool            `json:"shuffle"`
	Repeat   RepeatMode      `json:"repeat"`
	Position int64           `json:"position"` // milliseconds
	Radio    string          `json:"radio,omitempty"`
}

// Session returns where playback is, or false with nothing queued.
func (p *Player) Session() (Session, bool) {
	if p.track == nil || p.at < 0 {
		return Session{}, false
	}
	return Session{
		Queue:    p.queue,
		Order:    p.order,
		At:       p.at,
		Shuffle:  p.shuffle,
		Repeat:   p.repeat,
		Position: p.Position().Milliseconds(),
		Radio:    p.radio,
	}, true
}

// Restore picks up a session where it was left, paused, and loads its
// track to play at once when asked.
func (p *Player) Restore(s Session) {
	n := len(s.Queue)
	if n == 0 || len(s.Order) != n || s.At < 0 || s.At >= n {
		return
	}
	seen := make([]bool, n)
	for _, i := range s.Order {
		if i < 0 || i >= n || seen[i] {
			return
		}
		seen[i] = true
	}
	p.queue, p.order = s.Queue, s.Order
	p.shuffle, p.repeat = s.Shuffle, s.Repeat%3
	p.normalizeMode()
	p.loadSkipping(s.At, 0, false, time.Duration(s.Position)*time.Millisecond, 1)
}

// finish keeps a broken stream from looking like a song played to its end.
func (p *Player) finish(s *checkedStream) {
	err := s.Err()
	if err == nil && p.out.Duration()-p.out.Position() > 2*time.Second && !(p.cloudSong() && s.whole()) {
		// Short of its length: cut off. But a cloud drive's song that all
		// came in is as long as its file, which is often shorter than the
		// song NetEase matched it to, whose length its details give.
		err = errors.New("歌曲音频不完整，请重新加载")
	}
	if err != nil {
		p.pending = p.Position()
		p.err = "播放中断：" + err.Error()
		p.stop()
		if p.Cache != nil {
			// A stream can open from its header and fail on its audio later.
			// Let a retry fetch a fresh copy instead of reopening that cache.
			dl, _ := s.source()
			dl.mu.Lock()
			path := ""
			if dl.file != nil && dl.part == "" {
				path = dl.file.Name()
			}
			dl.mu.Unlock()
			if filepath.Dir(path) == p.Cache.Dir() {
				s.Close()
				p.Cache.remove(path)
			}
		}
		if p.retryLoad(0, 1, err) {
			return
		}
		p.playing = false
		p.sync()
		return
	}
	p.ended()
}

// cloudSong reports whether the track playing is from the listener's cloud
// drive.
func (p *Player) cloudSong() bool {
	return p.track != nil && p.track.Privilege != nil && p.track.Privilege.Cs
}
