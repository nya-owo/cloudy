package app

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// CDN APNGs can include palette transparency despite already storing alpha per pixel.
func TestPrivateStickerAPNGWithExtraTransparency(t *testing.T) {
	red := color.NRGBA{R: 255, A: 128}
	green := color.NRGBA{G: 255, A: 255}
	blue := color.NRGBA{B: 255, A: 255}
	first := image.NewNRGBA(image.Rect(0, 0, 3, 1))
	first.SetNRGBA(0, 0, red)
	first.SetNRGBA(1, 0, green)
	second := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	second.SetNRGBA(0, 0, blue)
	third := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	third.SetNRGBA(0, 0, color.NRGBA{B: 255, A: 128})

	header, idat := apngTestFrame(t, first)
	_, secondIDAT := apngTestFrame(t, second)
	_, thirdIDAT := apngTestFrame(t, third)
	data := appendPNGChunk([]byte(pngSignature), "IHDR", header)
	data = appendPNGChunk(data, "acTL", []byte{0, 0, 0, 3, 0, 0, 0, 1})
	data = appendPNGChunk(data, "PLTE", []byte{255, 0, 0})
	transparencyOffset := len(data)
	data = appendPNGChunk(data, "tRNS", []byte{128})
	data = appendPNGChunk(data, "fcTL", apngTestControl(0, 3, 0, 0, 0))
	data = appendPNGChunk(data, "IDAT", idat)
	data = appendPNGChunk(data, "fcTL", apngTestControl(1, 2, 1, 2, 0))
	data = appendPNGChunk(data, "fdAT", append([]byte{0, 0, 0, 2}, secondIDAT...))
	data = appendPNGChunk(data, "fcTL", apngTestControl(3, 1, 0, 0, 1))
	data = appendPNGChunk(data, "fdAT", append([]byte{0, 0, 0, 4}, thirdIDAT...))
	data = appendPNGChunk(data, "IEND", nil)

	if _, err := png.Decode(bytes.NewReader(data)); err == nil {
		t.Fatal("fixture did not reproduce the PNG error")
	}
	bmp, thumbnail, err := decodeArt(data)
	if err != nil || bmp == nil || color.NRGBAModel.Convert(thumbnail.At(0, 0)) != red {
		t.Fatalf("thumbnail lost the first frame or its alpha: %v", err)
	}
	art, err := decodeStickerArt(data)
	if err != nil || art.sticker == nil || len(art.sticker.frames) != 3 || art.sticker.plays != 1 {
		t.Fatalf("history did not retain the APNG sequence and loop count: %v", err)
	}
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Size(3, 1).Draw(art.sticker.paint)
	}, 3, 1)
	for _, want := range []color.NRGBA{green, blue, green, green} {
		got := tt.Image().RGBAAt(1, 0)
		if got != (color.RGBA{R: want.R, G: want.G, B: want.B, A: want.A}) {
			t.Fatalf("APNG pixel = %v, want %v", got, want)
		}
		tt.Advance(90 * time.Millisecond)
	}

	corrupt := bytes.Clone(data)
	corrupt[transparencyOffset+12] ^= 1
	if _, _, err := decodeArt(corrupt); err == nil {
		t.Fatal("the compatibility path accepted a damaged chunk")
	}
}

// Palette PNGs still need their valid tRNS chunk for transparency.
func TestPrivateStickerPNGPaletteTransparency(t *testing.T) {
	var encoded bytes.Buffer
	img := image.NewPaletted(image.Rect(0, 0, 1, 1), color.Palette{color.Transparent, color.White})
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	original := encoded.Bytes()
	if !bytes.Contains(original, []byte("tRNS")) || !bytes.Equal(stripPNGAlphaTransparency(original), original) {
		t.Fatal("valid palette transparency was removed")
	}
}

func apngTestFrame(t *testing.T, img image.Image) (header, idat []byte) {
	t.Helper()
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	for data := encoded.Bytes()[len(pngSignature):]; len(data) != 0; {
		chunk, tail := splitPNGChunk(data)
		if chunk == nil {
			t.Fatal("incomplete PNG fixture")
		}
		switch string(chunk[4:8]) {
		case "IHDR":
			header = chunk[8:21]
		case "IDAT":
			idat = append(idat, chunk[8:len(chunk)-4]...)
		}
		data = tail
	}
	return header, idat
}

func apngTestControl(sequence, width, x uint32, dispose, blend byte) []byte {
	var frame [26]byte
	binary.BigEndian.PutUint32(frame[:], sequence)
	binary.BigEndian.PutUint32(frame[4:], width)
	binary.BigEndian.PutUint32(frame[8:], 1)
	binary.BigEndian.PutUint32(frame[12:], x)
	binary.BigEndian.PutUint16(frame[20:], 8)
	binary.BigEndian.PutUint16(frame[22:], 100)
	frame[24], frame[25] = dispose, blend
	return frame[:]
}
