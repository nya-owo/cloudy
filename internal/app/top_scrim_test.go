package app

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// With NM_FRAMES set, saves an album page whose cover fills the top of the
// window, in the light and the dark, for the strip over it to be looked at:
// its scroll edge at rest, in each of the window's looks.
func TestTopScrimFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip("set NM_FRAMES to a directory to save frames")
	}
	looks := []struct {
		name string
		set  func(*settings)
	}{
		{"main", func(s *settings) { s.MainGlass, s.LiquidGlass = true, true }},
		{"liquid", func(s *settings) { s.MainGlass, s.LiquidGlass = false, true }},
		{"plain", func(s *settings) { s.MainGlass, s.LiquidGlass = false, false }},
	}
	cover := coverTestImage()
	url := "https://example.invalid/cover.jpg"
	for _, look := range looks {
		for _, dark := range []bool{false, true} {
			appearance := "light"
			if dark {
				appearance = "dark"
			}
			a := catalogApp()
			look.set(&a.settings)
			a.albums[8].album.Album.PicURL = url
			for i := range a.albums[8].album.Songs {
				a.albums[8].album.Songs[i].Album.PicURL = url
			}
			a.art.add(netease.Sized(url, 600)+"#backdrop", &artwork{
				backdrop: ui.NewBitmap(cover),
				tint:     dominantColor(cover),
				used:     time.Now(),
				size:     1,
			})
			a.open(route{page: pageAlbum, id: 8})
			tt := ui.NewTester(a.view, 1200, 800)
			tt.SetDark(dark)
			tt.Advance(2 * time.Second)
			tt.Frame()
			name := "top-scrim-" + look.name + "-" + appearance + ".png"
			out, err := os.Create(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(out, tt.Image()); err != nil {
				out.Close()
				t.Fatal(err)
			}
			out.Close()
		}
	}
}
