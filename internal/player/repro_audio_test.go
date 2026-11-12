package player

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// The delivered five-second tones reproduce the trailing-tag error on the old decoder.
func TestReproductionAudioEnd(t *testing.T) {
	dir := os.Getenv("CLOUDY_REPRO_DIR")
	if dir == "" {
		t.Skip("set CLOUDY_REPRO_DIR to inspect the delivered audio")
	}
	for _, name := range []string{"01-tail-tag.flac", "02-next-clean.flac"} {
		t.Run(name, func(t *testing.T) {
			b, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			s, err := openFLACStream(downloaded(b))
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			buf := make([]float32, 4096)
			var frames int64
			for {
				n, err := s.Read(buf)
				frames += int64(n / 2)
				if err != nil {
					t.Logf("frames=%d declared=%d ending=%v", frames, s.info.NSamples, err)
					if !errors.Is(err, io.EOF) {
						t.Errorf("music decoded fully but ending failed: %v", err)
					}
					return
				}
			}
		})
	}
}
