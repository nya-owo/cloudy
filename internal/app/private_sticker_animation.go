package app

import (
	"bytes"
	"image"
	"image/draw"
	"image/gif"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"
)

type stickerFrame struct {
	bitmap *ui.Bitmap
	end    time.Duration
}

type stickerAnimation struct {
	frames  []stickerFrame
	started time.Time
	plays   int // zero loops indefinitely
}

func (a *stickerAnimation) paint(p *ui.Painter, r ui.Rect) {
	if a.started.IsZero() {
		a.started = p.Now()
	}
	last := a.frames[len(a.frames)-1]
	elapsed := p.Now().Sub(a.started)
	if a.plays > 0 && elapsed/last.end >= time.Duration(a.plays) {
		p.Image(last.bitmap, r, ui.Contain)
		return
	}
	phase := elapsed % last.end
	for _, frame := range a.frames {
		if phase < frame.end {
			p.Image(frame.bitmap, r, ui.Contain)
			p.After(frame.end - phase)
			return
		}
	}
}

func (a *stickerAnimation) add(img image.Image, delay time.Duration) {
	if delay <= 0 {
		delay = 10 * time.Millisecond
	}
	if n := len(a.frames); n > 0 {
		delay += a.frames[n-1].end
	}
	a.frames = append(a.frames, stickerFrame{ui.NewBitmap(img), delay})
}

func (a *stickerAnimation) artwork(bounds image.Rectangle) *artwork {
	size := bounds.Dx() * bounds.Dy() * 4
	art := &artwork{bmp: a.frames[0].bitmap, size: size}
	if len(a.frames) > 1 {
		art.sticker = a
		art.size *= len(a.frames)
	}
	return art
}

func stickerFrameCount(bounds image.Rectangle, frames int) int {
	// Share the existing artwork budget. A single oversized animation
	// keeps its first frame instead of evicting the entire cache.
	if int64(bounds.Dx())*int64(bounds.Dy())*4*int64(frames) > artLimit/2 {
		return 1
	}
	return frames
}

func decodeStickerArt(data []byte) (*artwork, error) {
	if bytes.HasPrefix(data, []byte("GIF8")) {
		return decodeGIFSticker(data)
	}
	if control := pngAnimationControl(data); control != nil {
		return decodeAPNGSticker(stripPNGAlphaTransparency(data), control)
	}
	if isWebPSticker(data) {
		return decodeWebPSticker(data)
	}
	bmp, _, err := decodeArt(data)
	return &artwork{bmp: bmp, size: len(data)}, err
}

func decodeGIFSticker(data []byte) (*artwork, error) {
	decoded, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	bounds := image.Rect(0, 0, decoded.Config.Width, decoded.Config.Height)
	canvas := image.NewRGBA(bounds)
	a := &stickerAnimation{}
	if decoded.LoopCount != 0 {
		a.plays = max(1, decoded.LoopCount+1)
	}
	for i, frame := range decoded.Image[:stickerFrameCount(bounds, len(decoded.Image))] {
		disposal := decoded.Disposal[i]
		var previous []byte
		if disposal == gif.DisposalPrevious {
			previous = slices.Clone(canvas.Pix)
		}
		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)
		// MyGo can retain packed RGBA pixels, so keep each canvas independent.
		snapshot := *canvas
		snapshot.Pix = slices.Clone(canvas.Pix)
		a.add(&snapshot, time.Duration(decoded.Delay[i])*10*time.Millisecond)
		switch disposal {
		case gif.DisposalBackground:
			draw.Draw(canvas, frame.Bounds(), image.Transparent, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			copy(canvas.Pix, previous)
		}
	}
	return a.artwork(bounds), nil
}
