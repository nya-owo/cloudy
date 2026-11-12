package player

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"netease-music-mygo/internal/netease"
)

// A downloaded song plays from its file wherever it was started from, and
// a file gone plays from the network instead.
func TestDownloadedSongPlaysFromItsFile(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("testdata", "seek.mp3"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "song.mp3")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	asked := 0
	defer func(f func(context.Context, int64, string) (*netease.Stream, error)) { songURL = f }(songURL)
	songURL = func(context.Context, int64, string) (*netease.Stream, error) {
		asked++
		return nil, errors.New("offline")
	}
	file := path
	p := &Player{LocalFile: func(id int64) string {
		if id == 42 {
			return file
		}
		return ""
	}}
	st, s, err := p.open(netease.Track{ID: 42, Duration: 1000}, "exhigh")
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	if st.Type != "mp3" || asked != 0 {
		t.Fatalf("opened %q, asked the network %d times", st.Type, asked)
	}
	file = filepath.Join(t.TempDir(), "gone.mp3")
	if _, _, err := p.open(netease.Track{ID: 42, Duration: 1000}, "exhigh"); err == nil || asked != 1 {
		t.Fatalf("a missing file: err %v, asked %d", err, asked)
	}
}

// Moving the cache keeps songs in the new folder and drops those kept in
// the old one, leaving other files there alone.
func TestCacheMoves(t *testing.T) {
	old, to := t.TempDir(), t.TempDir()
	c := NewCache(old, 1<<30)
	for _, name := range []string{"1-exhigh.mp3", "1-exhigh.json"} {
		os.WriteFile(filepath.Join(old, name), []byte("x"), 0o600)
	}
	mine := filepath.Join(old, "notes.txt")
	os.WriteFile(mine, []byte("keep"), 0o600)
	c.SetDir(to)
	if c.Dir() != to {
		t.Fatalf("dir %q", c.Dir())
	}
	if _, err := os.Stat(filepath.Join(old, "1-exhigh.mp3")); err == nil {
		t.Error("the old cache's song stayed")
	}
	if _, err := os.Stat(mine); err != nil {
		t.Error("a file not the cache's was removed")
	}
	if f := c.part(&netease.Stream{Size: 10}); f == nil || filepath.Dir(f.Name()) != to {
		t.Fatalf("a song comes into %v", f)
	} else {
		f.Close()
	}
}
