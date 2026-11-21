package player

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"

	"netease-music-mygo/internal/netease"
)

// Uncached songs used to retain their entire download in memory.
func TestUncachedDownloadUsesTemporaryFile(t *testing.T) {
	body := bytes.Repeat([]byte("audio"), 1024)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(body) }))
	defer srv.Close()
	for _, cache := range []*Cache{nil, NewCache(t.TempDir(), 0), NewCache(t.TempDir(), 1)} {
		d := cache.download(7, &netease.Stream{URL: srv.URL, Size: int64(len(body)), Level: "exhigh", Type: "mp3"})
		defer d.Close()
		d.mu.Lock()
		path, file := d.part, d.file
		d.mu.Unlock()
		if file == nil || path == "" {
			t.Fatal("uncached audio was not spooled to a file")
		}
		got, err := io.ReadAll(&downloadReader{d: d})
		if err != nil || !bytes.Equal(got, body) {
			t.Fatalf("download: %v, %d bytes", err, len(got))
		}
		d.mu.Lock()
		memory := len(d.data)
		d.mu.Unlock()
		if memory != 0 {
			t.Fatal("the spool retained a copy in memory")
		}
		// Wait for the writer's release as well as EOF before the last reader closes.
		d.mu.Lock()
		for d.users > 1 {
			d.mu.Unlock()
			runtime.Gosched()
			d.mu.Lock()
		}
		d.mu.Unlock()
		d.Close()
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("the spool survived its last reader")
		}
	}
}

// A failed spool must not let a long download grow beyond its memory budget.
func TestDownloadMemoryBound(t *testing.T) {
	d := downloaded(nil)
	d.n = maxMemoryDownload
	if err := d.write([]byte{1}); !errors.Is(err, errDownloadMemory) {
		t.Fatalf("buffer overflow: %v", err)
	}
	if len(d.data) != 0 || d.n != maxMemoryDownload {
		t.Fatal("overflow changed the buffered data")
	}
}

// Spectrum analysis must use fixed memory regardless of track length (e.g.
// multi-hour podcasts or 96 kHz Hi-Res FLACs) while accurately serving chunks
// near the current playback position.
func TestSpectrumFixedMemoryRingBuffer(t *testing.T) {
	// Simulate a 2-hour 96 kHz track (~691M frames, previously ~65 MB upfront).
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	s := newSpectrum(96000, 96000*7200)
	runtime.ReadMemStats(&after)
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 256<<10 {
		t.Fatalf("newSpectrum allocated %d bytes for a 2h 96kHz track, want <= 256 KB", allocated)
	}

	// Feed chunks beyond spectrumSlots to exercise wraparound and tag matching.
	buf := make([]float32, fftSize*2)
	for i := range buf {
		buf[i] = 0.5
	}
	for c := int64(0); c < spectrumSlots+10; c++ {
		s.feed(c*fftSize, buf)
	}
	// Chunk 0 was overwritten by chunk spectrumSlots, so querying chunk 0 must miss.
	if _, ok := s.at(0); ok {
		t.Fatal("evicted chunk 0 falsely matched newer ring slot")
	}
	// Recent chunk must hit.
	if _, ok := s.at((spectrumSlots + 5) * fftSize); !ok {
		t.Fatal("recent chunk missed spectrum ring buffer")
	}
	runtime.KeepAlive(s)
}
