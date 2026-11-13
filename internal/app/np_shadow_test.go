package app

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// shadowCover runs the real cover and flight on a plain backdrop, so the
// shadow can be measured without lyrics, fog or a changing song position.
func shadowCover(playing bool) (*app, *ui.Tester, *float32) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.BarSpin = false
	a.motion = motionFull
	url := "https://fixture.invalid/shadow-cover"
	markArt(a, url, 120)
	art := a.art.peek(netease.Sized(url, 120))
	a.art.add(netease.Sized(url, 600)+"#backdrop", art)
	tr := netease.Track{ID: 1, Name: "Shadow", Duration: 180000, Album: netease.Album{PicURL: url}}
	a.pl.Cue([]netease.Track{tr})
	if playing {
		a.pl.Hold(30 * time.Second)
	}
	a.barSpot = flightSpot{cover: ui.Rect{X: 100, Y: 610, W: 48, H: 48}, radius: 24}
	k := float32(1)
	tt := ui.NewTester(func(c *ui.Context) {
		a.pal = paletteFor(c.Theme())
		a.npK = k
		ui.Box(c).Fill().Background(ui.RGB(192, 192, 192)).Children(func() {
			ui.Box(c.Key("target")).Absolute().Left(250).Top(90).Size(360, 360).Children(func() {
				a.npCover(c, a.pl.Current(), art, 360)
			})
			a.flights(c, a.pl.Current(), k)
		})
	}, 900, 700)
	tt.Frame()
	tt.Advance(2 * time.Second)
	tt.Frame()
	return a, tt, &k
}

// shadowDarkness samples below the cover, outside both cover images.
func shadowDarkness(img *image.RGBA) float64 {
	var sum, count int
	density := float64(img.Bounds().Dx()) / 900
	for y := 460; y < 520; y++ {
		for x := 240; x < 630; x++ {
			sum += 192 - int(img.RGBAAt(int(float64(x)*density), int(float64(y)*density)).R)
			count++
		}
	}
	return float64(sum) / float64(count)
}

// Two independently fading cover shadows used to overlap, then drop at landing.
// Check their actual near and far field through the handover, in both directions.
func TestCoverShadowHandover(t *testing.T) {
	for _, playing := range []bool{false, true} {
		for _, density := range []float32{1, 1.25} {
			for _, hover := range []bool{false, true} {
				t.Run(fmt.Sprintf("playing=%v/scale=%.2f/hover=%v", playing, density, hover), func(t *testing.T) {
					_, tt, k := shadowCover(playing)
					tt.SetScale(density)
					if hover {
						tt.Move(430, 270)
						tt.Advance(2 * time.Second)
						tt.Frame()
					}
					end := tt.Image()
					settled := shadowDarkness(end)
					frames := make(map[float32][]byte)
					for _, progress := range []float32{.90, .95, .98, .99, .995, .996, .997, .998, .999, .99925, .9995, .99975, 1} {
						*k = progress
						tt.Frame()
						img := tt.Image()
						frames[progress] = append([]byte(nil), img.Pix...)
						dark := shadowDarkness(img)
						t.Logf("k=%.6f darkness=%.6f settled=%.6f", progress, dark, settled)
						if progress >= .999 {
							// All samples are outside the pictures, across the near edge
							// and the full feather. Do not hide an edge flash in a mean.
							maxDelta := 0
							for y := int(460 * density); y < int(550*density); y++ {
								for x := int(210 * density); x < int(660*density); x++ {
									d := int(math.Abs(float64(int(img.RGBAAt(x, y).R) - int(end.RGBAAt(x, y).R))))
									maxDelta = max(maxDelta, d)
								}
							}
							if maxDelta > 2 {
								t.Errorf("edge/feather jump at k=%v: max channel delta %d", progress, maxDelta)
							}
						}
						if dir := os.Getenv("NM_FRAMES"); dir != "" {
							if err := os.MkdirAll(dir, 0755); err != nil {
								t.Fatal(err)
							}
							f, err := os.Create(filepath.Join(dir, fmt.Sprintf("playing-%v-scale-%.2f-hover-%v-k-%.6f.png", playing, density, hover, progress)))
							if err != nil {
								t.Fatal(err)
							}
							err = png.Encode(f, img)
							f.Close()
							if err != nil {
								t.Fatal(err)
							}
						}
					}
					for _, progress := range []float32{.99975, .9995, .99925, .999, .998, .997, .996, .995, .99, .98, .95, .90} {
						*k = progress
						tt.Frame()
						if !bytes.Equal(tt.Image().Pix, frames[progress]) {
							t.Errorf("reverse shadow differs at the same progress k=%v", progress)
						}
					}
				})
			}
		}
	}
}

// Compare a co-located caster to its original resting shadow: movement must
// not turn a fixed screen sampling rectangle into a false strength reading.
func TestCoverShadowConstantPixels(t *testing.T) {
	for _, playing := range []bool{false, true} {
		a, tt, k := shadowCover(playing)
		s, r := a.npCoverScale, a.npSpot.cover
		a.barSpot.cover = ui.Rect{X: r.X + r.W*(1-s)/2, Y: r.Y + r.H*(1-s)/2, W: r.W * s, H: r.H * s}
		a.barSpot.radius = 14 * s
		end := tt.Image()
		for _, progress := range []float32{.001, .1, .25, .5, .75, .90, .95, .98, .99, .995, .997, .999, .9995, 1} {
			*k = progress
			tt.Frame()
			img := tt.Image()
			dark, final := shadowDarkness(img), shadowDarkness(end)
			t.Logf("aligned playing=%v k=%.6f darkness=%.6f final=%.6f", playing, progress, dark, final)
			if math.Abs(dark-final) > .2 {
				t.Errorf("flight shadow is not constant: %.3f, final %.3f", dark, final)
			}
			for y := 460; y < 550; y++ {
				for x := 210; x < 660; x++ {
					v := img.RGBAAt(x, y).R
					if d := int(v) - int(end.RGBAAt(x, y).R); d < -1 || d > 1 {
						t.Fatalf("playing=%v k=%v shadow peak/dip at (%d,%d)", playing, progress, x, y)
					}
				}
			}
		}
	}
}

// The spinning bar's cover must carry its shadow to the same original endpoint.
func TestCoverShadowSpinningHandover(t *testing.T) {
	a, tt, k := shadowCover(true)
	a.settings.BarSpin = true
	a.spin = discState{angle: 137, since: tt.Now()}
	end := tt.Image()
	for _, progress := range []float32{.999, .9995, .9999, 1} {
		*k = progress
		tt.Frame()
		img := tt.Image()
		for y := 460; y < 550; y++ {
			for x := 210; x < 660; x++ {
				if d := int(img.RGBAAt(x, y).R) - int(end.RGBAAt(x, y).R); d < -2 || d > 2 {
					t.Fatalf("spinning shadow jumped at k=%v (%d,%d): %d", progress, x, y, d)
				}
			}
		}
	}
}
