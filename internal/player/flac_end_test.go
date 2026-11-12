package player

import (
	"errors"
	"io"
	"os"
	"testing"

	"github.com/egoist/mygo/audio"
	"netease-music-mygo/internal/netease"
)

// A FLAC's trailing tag used to look like a broken frame even after all music was decoded.
func TestFLACTrailingTagEndsNaturally(t *testing.T) {
	b, err := os.ReadFile("testdata/seek.flac")
	if err != nil {
		t.Fatal(err)
	}
	tag := make([]byte, 128)
	copy(tag, "TAGCloudy regression")
	s, err := openFLACStream(downloaded(append(b, tag...)))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	checked := &checkedStream{stream: s}
	buf := make([]float32, 4096)
	var frames int64
	for {
		n, err := checked.Read(buf)
		frames += int64(n / 2)
		if err != nil {
			if !errors.Is(err, io.EOF) || checked.Err() != nil || uint64(frames) != s.info.NSamples {
				t.Fatalf("frames=%d declared=%d err=%v checked=%v", frames, s.info.NSamples, err, checked.Err())
			}
			break
		}
	}
	quietNavigation(t)
	cloud := &netease.Privilege{ID: 1, Pl: 320000, Cs: true}
	p, ch := navigationPlayer(t, []netease.Track{{ID: 1, Duration: 600000, Privilege: cloud}, {ID: 2, Duration: 5000}})
	cacheNavigationTrack(t, p, 2)
	p.stream, p.out, p.playing = checked, audio.NewPlayer(silence{}), true
	p.finish(checked)
	finishNavigation(t, p, ch)
	if p.Current().ID != 2 || !p.Playing() {
		t.Fatalf("cloud ending did not advance: id=%d error=%q", p.Current().ID, p.Err())
	}
}

// A damaged frame before the declared end must still report an error.
func TestFLACEndDoesNotHideDamagedAudio(t *testing.T) {
	b, err := os.ReadFile("testdata/seek.flac")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := openFLACStream(downloaded(b))
	if err != nil {
		t.Fatal(err)
	}
	start := ref.start
	ref.Close()
	b[start+12] ^= 0x55
	s, err := openFLACStream(downloaded(b))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for {
		_, err := s.Read(make([]float32, 4096))
		if err != nil {
			if errors.Is(err, io.EOF) {
				t.Fatal("damaged audio was accepted as a natural end")
			}
			return
		}
	}
}
