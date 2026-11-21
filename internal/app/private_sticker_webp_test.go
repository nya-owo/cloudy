package app

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

type webpFixtureFrame struct {
	x, y, w, h, ms int
	flags          byte
	chunks         []byte
}

func webpFixtureRIFF(chunks []byte) []byte {
	data := binary.LittleEndian.AppendUint32([]byte("RIFF"), uint32(4+len(chunks)))
	return append(append(data, "WEBP"...), chunks...)
}

func webpFixtureLE24(out []byte, v int) {
	out[0], out[1], out[2] = byte(v), byte(v>>8), byte(v>>16)
}

func webpFixtureAnimation(w, h int, bg color.NRGBA, plays uint16, profile []byte, frames ...webpFixtureFrame) []byte {
	ext := [10]byte{0x12}
	if profile != nil {
		ext[0] |= 0x20
	}
	webpFixtureLE24(ext[4:], w-1)
	webpFixtureLE24(ext[7:], h-1)
	chunks := appendWebPStickerChunk(nil, "VP8X", ext[:])
	if profile != nil {
		chunks = appendWebPStickerChunk(chunks, "ICCP", profile)
	}
	header := binary.LittleEndian.AppendUint16([]byte{bg.B, bg.G, bg.R, bg.A}, plays)
	chunks = appendWebPStickerChunk(chunks, "ANIM", header)
	for _, f := range frames {
		var meta [16]byte
		for i, v := range []int{f.x / 2, f.y / 2, f.w - 1, f.h - 1, f.ms} {
			webpFixtureLE24(meta[i*3:], v)
		}
		meta[15] = f.flags
		chunks = appendWebPStickerChunk(chunks, "ANMF", append(meta[:], f.chunks...))
	}
	return webpFixtureRIFF(chunks)
}

func webpFixturePayload(t *testing.T, data []byte) []byte {
	t.Helper()
	var payload []byte
	for rest := data[12:]; len(rest) > 0; {
		id, body, tail := splitWebPStickerChunk(rest)
		switch id {
		case "":
			t.Fatal("malformed fixture")
		case "VP8 ", "VP8L", "ALPH":
			payload = appendWebPStickerChunk(payload, id, body)
		}
		rest = tail
	}
	return payload
}

// These 3x3 lossless samples are synthetic solid pixels encoded by libwebp.
func webpSolidFixture(t *testing.T, transparent bool) []byte {
	t.Helper()
	encoded := "UklGRhwAAABXRUJQVlA4TA8AAAAvAoAAEAfQ/4gCBiKi/wEA" // green, alpha 128
	if transparent {
		encoded = "UklGRhoAAABXRUJQVlA4TA0AAAAvAoAAEAcQERGIiP4HAA=="
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return webpFixturePayload(t, data)
}

func webpFixtureBitmap(bmp *ui.Bitmap) *image.RGBA {
	w, h := bmp.Size()
	theme := ui.LightTheme()
	theme.Background = ui.Transparent
	tt := ui.NewTester(func(c *ui.Context) {
		c.SetTheme(theme)
		ui.Image(c, bmp).Size(float32(w), float32(h))
	}, w, h)
	return tt.Image()
}

// Offset frames follow WebP's source/over, disposal and nonzero background rules.
func TestPrivateStickerWebPCanvasAndTiming(t *testing.T) {
	green, transparent := webpSolidFixture(t, false), webpSolidFixture(t, true)
	bg := color.NRGBA{R: 20, G: 40, B: 90, A: 255}
	data := webpFixtureAnimation(9, 9, bg, 3, []byte{1, 2, 3},
		webpFixtureFrame{x: 2, y: 4, w: 3, h: 3, flags: 1, chunks: green},
		webpFixtureFrame{x: 4, y: 4, w: 3, h: 3, ms: 40, flags: 2, chunks: green},
		webpFixtureFrame{x: 4, y: 4, w: 3, h: 3, ms: 50, chunks: green},
		webpFixtureFrame{x: 4, y: 4, w: 3, h: 3, ms: 60, chunks: transparent},
		webpFixtureFrame{x: 4, y: 4, w: 3, h: 3, ms: 70, flags: 2, chunks: transparent},
	)
	art, err := decodeStickerArt(data)
	if err != nil || art.sticker == nil || len(art.sticker.frames) != 5 || art.sticker.plays != 3 || art.size != 9*9*4*5 {
		t.Fatalf("wrong animation or cache size: %v", err)
	}
	background := color.RGBA{R: 20, G: 40, B: 90, A: 255}
	wantAtFirstRect := []color.RGBA{{R: 10, G: 148, B: 45, A: 255}, background, background, background, background}
	wantAtSecondRect := []color.RGBA{background, {G: 128, A: 128}, {G: 192, A: 192}, {G: 192, A: 192}, {}}
	ends := []int{10, 50, 100, 160, 230}
	for i, f := range art.sticker.frames {
		img := webpFixtureBitmap(f.bitmap)
		if (img.RGBAAt(0, 0) != background) || img.RGBAAt(2, 4) != wantAtFirstRect[i] ||
			img.RGBAAt(6, 6) != wantAtSecondRect[i] || f.end != time.Duration(ends[i])*time.Millisecond {
			t.Fatalf("frame %d: base=%v disposed=%v blended=%v end=%s", i, img.RGBAAt(0, 0), img.RGBAAt(2, 4), img.RGBAAt(6, 6), f.end)
		}
	}
}

// Synthetic lossy fixtures catch studio-range and chroma errors against libwebp RGBA.
func TestPrivateStickerWebPLossyColors(t *testing.T) {
	for _, name := range []string{"vp8", "vp8-alpha"} {
		t.Run(name, func(t *testing.T) {
			const w, h = 13, 9
			prefix := "testdata/sticker-" + name
			raw, err := os.ReadFile(prefix + ".webp")
			if err != nil {
				t.Fatal(err)
			}
			ref, err := os.ReadFile(prefix + ".rgba")
			if err != nil || len(ref) != w*h*4 {
				t.Fatalf("invalid libwebp reference: %v", err)
			}
			still, err := decodeStickerArt(raw)
			if err != nil || still.bmp == nil || still.sticker != nil {
				t.Fatalf("still WebP did not use the still path: %v", err)
			}

			payload := webpFixturePayload(t, raw)
			frame := webpFixtureFrame{w: w, h: h, ms: 30, flags: 2, chunks: payload}
			// Compare pixels from the second frame at its offset in the canvas.
			offset := frame
			offset.x, offset.y = 2, 2
			data := webpFixtureAnimation(w+2, h+2, color.NRGBA{}, 0, nil, frame, offset)
			art, err := decodeStickerArt(data)
			if err != nil || art.sticker == nil || len(art.sticker.frames) != 2 {
				t.Fatalf("lossy WebP animation did not decode: %v", err)
			}
			got := webpFixtureBitmap(art.sticker.frames[1].bitmap)
			bounds := image.Rect(0, 0, w, h)
			want := image.NewRGBA(bounds)
			draw.Draw(want, bounds, &image.NRGBA{Pix: ref, Stride: w * 4, Rect: bounds}, image.Point{}, draw.Src)
			maxDiff := 0
			for y := range h {
				for x := range w {
					p, q := got.RGBAAt(x+2, y+2), want.RGBAAt(x, y)
					if p.A != q.A {
						t.Fatalf("alpha at %d,%d: got %d, want %d", x, y, p.A, q.A)
					}
					for _, pair := range [][2]byte{{p.R, q.R}, {p.G, q.G}, {p.B, q.B}} {
						d := int(pair[0]) - int(pair[1])
						maxDiff = max(maxDiff, d, -d)
					}
				}
			}
			if maxDiff > 2 {
				t.Fatalf("largest RGB difference from libwebp = %d, want at most 2", maxDiff)
			}
			t.Logf("max RGB difference from libwebp = %d, alpha difference = 0", maxDiff)
		})
	}
}

// Over-budget input and canvas are rejected; extra frames leave only frame zero.
func TestPrivateStickerWebPBudgets(t *testing.T) {
	frame := webpFixtureFrame{w: 3, h: 3, flags: 2, chunks: webpSolidFixture(t, false)}
	for _, dim := range []int{4096, 1 << 24} {
		t.Run(fmt.Sprint(dim), func(t *testing.T) {
			data := webpFixtureAnimation(dim, dim, color.NRGBA{}, 0, nil, frame)
			if art, err := decodeWebPSticker(data); art != nil || err != errWebPCanvasBudget {
				t.Fatalf("oversized canvas was not rejected: %v", err)
			}
		})
	}
	if art, err := decodeWebPSticker(make([]byte, stickerInputLimit+1)); art != nil || err == nil || !strings.Contains(err.Error(), "byte limit") {
		t.Fatalf("oversized input reached the parser: %v", err)
	}
	for _, test := range []struct {
		name       string
		dim, count int
	}{
		{"decoded bytes", 200, 210},
		{"frame count", 3, webpFrameLimit + 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			frames := make([]webpFixtureFrame, test.count)
			frames[0] = frame
			for i := 1; i < len(frames); i++ {
				frames[i] = frame
				frames[i].chunks = appendWebPStickerChunk(nil, "VP8L", []byte{0})
			}
			data := webpFixtureAnimation(test.dim, test.dim, color.NRGBA{}, 0, nil, frames...)
			art, err := decodeStickerArt(data)
			if err != nil || art.bmp == nil || art.sticker != nil || art.size != test.dim*test.dim*4 {
				t.Fatalf("over-budget animation did not decode only frame zero: %v", err)
			}
			if pixel := webpFixtureBitmap(art.bmp).RGBAAt(1, 1); pixel != (color.RGBA{G: 128, A: 128}) {
				t.Fatalf("fallback did not retain frame zero: %v", pixel)
			}
		})
	}
}

// Malformed chunk lengths and frame rectangles fail before the bitmap decoder gets them.
func TestPrivateStickerWebPMalformed(t *testing.T) {
	green := webpSolidFixture(t, false)
	valid := webpFixtureFrame{w: 3, h: 3, chunks: green}
	animation := func(frames ...webpFixtureFrame) []byte {
		return webpFixtureAnimation(5, 5, color.NRGBA{}, 0, nil, frames...)
	}
	badRIFF := animation(valid)
	binary.LittleEndian.PutUint32(badRIFF[4:8], 0xffffffff)
	noPad := valid
	noPad.chunks = green[:len(green)-1]
	outside := valid
	outside.x = (1 << 25) - 2
	reserved := valid
	reserved.flags = 4
	wrongSize := valid
	wrongSize.w = 1
	wrongAlpha := valid
	wrongAlpha.chunks = append(appendWebPStickerChunk(nil, "ALPH", []byte{0, 128}), green...)
	noHeader := animation(valid)
	noHeader = webpFixtureRIFF(append(append([]byte(nil), noHeader[12:30]...), noHeader[44:]...))
	for name, data := range map[string][]byte{
		"missing RIFF": []byte("RIFF"), "overflowed RIFF length": badRIFF,
		"no frames": animation(), "missing ANIM": noHeader, "missing padding": animation(noPad),
		"outside canvas": animation(outside), "reserved frame flag": animation(reserved),
		"bitstream size mismatch": animation(wrongSize), "ALPH plus VP8L": animation(wrongAlpha),
	} {
		t.Run(name, func(t *testing.T) {
			if art, err := decodeWebPSticker(data); err == nil || art != nil {
				t.Fatal("malformed WebP did not fail")
			}
		})
	}
}

// Both chat stickers and picker thumbnails reject large files before decoding.
func TestPrivateStickerWebPLoadLimit(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "large.webp"))
	if err != nil {
		t.Fatal(err)
	}
	err = file.Truncate(stickerInputLimit + 1)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("creating oversized fixture: %v / %v", err, closeErr)
	}
	for _, animate := range []bool{false, true} {
		if art, err := loadArt(file.Name(), false, true, animate); art != nil || err == nil || !strings.Contains(err.Error(), "byte limit") {
			t.Fatalf("sticker load (animate=%t) did not reject the input size: %v", animate, err)
		}
	}
}
