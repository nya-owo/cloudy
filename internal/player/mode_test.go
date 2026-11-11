package player

import (
	"testing"

	"github.com/egoist/mygo/audio"

	"netease-music-mygo/internal/netease"
)

// One button goes through the modes in order: in order, round, the one
// song, shuffled, and back; shuffled always goes round.
func TestPlayModeCycles(t *testing.T) {
	p, _ := navigationPlayer(t, []netease.Track{{ID: 1}, {ID: 2}, {ID: 3}})
	want := []PlayMode{ModeLoop, ModeOne, ModeShuffle, ModeOrder}
	for _, m := range want {
		if got := p.CycleMode(); got != m || p.Mode() != m {
			t.Fatalf("cycled to %v (mode %v), want %v", got, p.Mode(), m)
		}
		if m == ModeShuffle && (!p.shuffle || p.repeat != RepeatAll) {
			t.Fatalf("shuffle is shuffle %v, repeat %v", p.shuffle, p.repeat)
		}
		if m == ModeOrder && (p.shuffle || p.repeat != RepeatOff) {
			t.Fatalf("in order is shuffle %v, repeat %v", p.shuffle, p.repeat)
		}
	}
	// A session from before the one button, shuffled without repeat.
	p.shuffle, p.repeat = true, RepeatOff
	p.normalizeMode()
	if p.Mode() != ModeShuffle || p.repeat != RepeatAll {
		t.Fatalf("old shuffle came back as %v, repeat %v", p.Mode(), p.repeat)
	}
}

// A cloud drive's song, all come in, ends where its file does, even short
// of the length NetEase gives the song it matched: one song over plays it
// again, rather than calling it cut off.
func TestCloudSongShorterThanItsDetailsRepeats(t *testing.T) {
	cloud := &netease.Privilege{ID: 1, Pl: 320000, Cs: true}
	p, _ := navigationPlayer(t, []netease.Track{{ID: 1, Duration: 600000, Privilege: cloud}, {ID: 2}})
	p.SetMode(ModeOne)
	dl := downloaded(nil)
	s := &checkedStream{stream: &mp3Stream{dl: dl, drop: dl.Close, rate: 48000, frames: 48000 * 600}}
	p.stream, p.out, p.playing = s, audio.NewPlayer(silence{}), true
	p.finish(s)
	if p.Current().ID != 1 || p.Err() != "" || p.out == nil || !p.Playing() {
		t.Fatalf("the cloud song did not play again: id=%d error=%q", p.Current().ID, p.Err())
	}
}
