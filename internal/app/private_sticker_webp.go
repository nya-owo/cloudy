package app

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"time"

	"golang.org/x/image/webp"
)

const (
	stickerInputLimit  = 16 << 20
	webpCanvasLimit    = 4096
	webpFrameLimit     = 512
	webpAnimationBytes = artLimit / 2
)

var (
	errInvalidWebP      = errors.New("webp: invalid sticker container")
	errWebPCanvasBudget = errors.New("webp: sticker canvas exceeds budget")
)

type webpStickerFrame struct {
	rect             image.Rectangle
	delay            time.Duration
	replace, dispose bool
	codec            string
	alpha, pixels    []byte
}

func isWebPSticker(data []byte) bool {
	return len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP"
}

func webpCanvasFits(w, h int) bool {
	// Cap both axes before multiplying, including on 32-bit builds.
	return w > 0 && h > 0 && w <= webpCanvasLimit && h <= webpCanvasLimit && w*h*4 <= webpAnimationBytes
}

func decodeWebPSticker(data []byte) (*artwork, error) {
	return decodeWebPStickerFrames(data, webpFrameLimit)
}

// Parse and budget the complete animation before any bitmap allocation.
// A picker only needs frame zero. Over-budget animations also keep just
// that frame, never a shorter animation with different playback.
func decodeWebPStickerFrames(data []byte, limit int) (*artwork, error) {
	if len(data) > stickerInputLimit {
		return nil, errors.New("webp: sticker input exceeds byte limit")
	}
	if !isWebPSticker(data) {
		return nil, errInvalidWebP
	}
	riffSize := uint64(binary.LittleEndian.Uint32(data[4:8]))
	if riffSize < 4 || riffSize+8 > uint64(len(data)) {
		return nil, errInvalidWebP
	}
	data = data[:8+riffSize]
	id, header, rest := splitWebPStickerChunk(data[12:])
	switch id {
	case "VP8 ", "VP8L":
		return decodeStillWebPSticker(data, rest)
	case "VP8X":
		if len(header) != 10 {
			return nil, errInvalidWebP
		}
		if header[0]&2 == 0 {
			return decodeStillWebPSticker(data, rest)
		}
	default:
		return nil, errInvalidWebP
	}
	w, h := webpUint24(header[4:])+1, webpUint24(header[7:])+1
	if !webpCanvasFits(w, h) {
		return nil, errWebPCanvasBudget
	}
	bounds := image.Rect(0, 0, w, h)
	var background color.NRGBA
	var plays, count int
	var seenAnimation bool
	var frames []webpStickerFrame
	// Division avoids multiplying an attacker-controlled frame count.
	limit = min(limit, webpAnimationBytes/(w*h*4))
	for len(rest) > 0 {
		id, body, tail := splitWebPStickerChunk(rest)
		if id == "" {
			return nil, errInvalidWebP
		}
		rest = tail
		switch id {
		case "VP8X", "VP8 ", "VP8L", "ALPH":
			return nil, errInvalidWebP
		case "ANIM":
			if seenAnimation || len(body) != 6 {
				return nil, errInvalidWebP
			}
			seenAnimation = true
			background = color.NRGBA{R: body[2], G: body[1], B: body[0], A: body[3]}
			plays = int(binary.LittleEndian.Uint16(body[4:]))
		case "ANMF":
			if !seenAnimation {
				return nil, errInvalidWebP
			}
			frame, err := parseWebPStickerFrame(body, bounds)
			if err != nil {
				return nil, err
			}
			count++
			if count <= limit {
				frames = append(frames, frame)
			}
		}
		// ICCP, EXIF, XMP and unknown chunks do not affect frame assembly.
	}
	if count == 0 {
		return nil, errInvalidWebP
	}
	if count > limit {
		frames = frames[:1]
	}

	canvas := image.NewNRGBA(bounds)
	fillWebPSticker(canvas, bounds, background)
	anim := stickerAnimation{plays: plays}
	for _, frame := range frames {
		img, err := frame.decode()
		if err != nil {
			return nil, err
		}
		compositeWebPSticker(canvas, frame, img)
		// MyGo converts NRGBA to an independent premultiplied bitmap.
		anim.add(canvas, frame.delay)
		if frame.dispose {
			fillWebPSticker(canvas, frame.rect, background)
		}
	}
	return anim.artwork(bounds), nil
}

func decodeStillWebPSticker(data, rest []byte) (*artwork, error) {
	for len(rest) > 0 {
		id, _, tail := splitWebPStickerChunk(rest)
		if id == "" || id == "ANIM" || id == "ANMF" {
			return nil, errInvalidWebP
		}
		rest = tail
	}
	cfg, err := webp.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if !webpCanvasFits(cfg.Width, cfg.Height) {
		return nil, errWebPCanvasBudget
	}
	bmp, _, err := decodeArt(data)
	return &artwork{bmp: bmp, size: len(data)}, err
}

func splitWebPStickerChunk(data []byte) (id string, body, tail []byte) {
	if len(data) < 8 {
		return "", nil, nil
	}
	size := uint64(binary.LittleEndian.Uint32(data[4:8]))
	padded := size + size%2
	if padded > uint64(len(data)-8) {
		return "", nil, nil
	}
	return string(data[:4]), data[8 : 8+size], data[8+padded:]
}

func webpUint24(b []byte) int {
	return int(b[0]) | int(b[1])<<8 | int(b[2])<<16
}

func parseWebPStickerFrame(data []byte, bounds image.Rectangle) (webpStickerFrame, error) {
	if len(data) < 16 || data[15]&^3 != 0 {
		return webpStickerFrame{}, errInvalidWebP
	}
	// RFC 9649 stores offsets in two-pixel units and sizes minus one.
	x, y := webpUint24(data)*2, webpUint24(data[3:])*2
	w, h := webpUint24(data[6:])+1, webpUint24(data[9:])+1
	rect := image.Rect(x, y, x+w, y+h)
	if !rect.In(bounds) {
		return webpStickerFrame{}, errInvalidWebP
	}
	frame := webpStickerFrame{
		rect: rect, delay: time.Duration(webpUint24(data[12:])) * time.Millisecond,
		replace: data[15]&2 != 0, dispose: data[15]&1 != 0,
	}
	for rest := data[16:]; len(rest) > 0; {
		id, body, tail := splitWebPStickerChunk(rest)
		rest = tail
		switch id {
		case "":
			return webpStickerFrame{}, errInvalidWebP
		case "ALPH":
			if frame.alpha != nil || frame.codec != "" || len(body) == 0 {
				return webpStickerFrame{}, errInvalidWebP
			}
			frame.alpha = body
		case "VP8 ", "VP8L":
			if frame.codec != "" || id == "VP8L" && frame.alpha != nil {
				return webpStickerFrame{}, errInvalidWebP
			}
			frame.codec, frame.pixels = id, body
		}
	}
	if frame.codec == "" {
		return webpStickerFrame{}, errInvalidWebP
	}
	return frame, nil
}

func appendWebPStickerChunk(data []byte, id string, body []byte) []byte {
	data = append(data, id...)
	data = binary.LittleEndian.AppendUint32(data, uint32(len(body)))
	data = append(data, body...)
	if len(body)%2 != 0 {
		data = append(data, 0)
	}
	return data
}

func (f webpStickerFrame) decode() (image.Image, error) {
	// The extended header makes x/image/webp check the bitstream size against
	// the bounded ANMF rectangle before allocating VP8/VP8L pixels.
	var header [10]byte
	w, h := f.rect.Dx()-1, f.rect.Dy()-1
	header[4], header[5], header[7], header[8] = byte(w), byte(w>>8), byte(h), byte(h>>8)
	data := make([]byte, 0, 48+len(f.alpha)+len(f.pixels))
	data = append(data, "RIFF\x00\x00\x00\x00WEBP"...)
	if f.alpha != nil {
		header[0] = 0x10
	}
	data = appendWebPStickerChunk(data, "VP8X", header[:])
	if f.alpha != nil {
		data = appendWebPStickerChunk(data, "ALPH", f.alpha)
	}
	data = appendWebPStickerChunk(data, f.codec, f.pixels)
	binary.LittleEndian.PutUint32(data[4:8], uint32(len(data)-8))
	return webp.Decode(bytes.NewReader(data))
}

func fillWebPSticker(canvas *image.NRGBA, rect image.Rectangle, c color.NRGBA) {
	pixel := [4]byte{c.R, c.G, c.B, c.A}
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		off := canvas.PixOffset(rect.Min.X, y)
		for range rect.Dx() {
			copy(canvas.Pix[off:off+4], pixel[:])
			off += 4
		}
	}
}

func compositeWebPSticker(canvas *image.NRGBA, frame webpStickerFrame, img image.Image) {
	for y := range frame.rect.Dy() {
		for x := range frame.rect.Dx() {
			src := webpStickerColor(img, x, y)
			dx, dy := frame.rect.Min.X+x, frame.rect.Min.Y+y
			if frame.replace || src.A == 255 {
				canvas.SetNRGBA(dx, dy, src)
			} else if src.A != 0 {
				canvas.SetNRGBA(dx, dy, blendWebPSticker(src, canvas.NRGBAAt(dx, dy)))
			}
		}
	}
}

func blendWebPSticker(src, dst color.NRGBA) color.NRGBA {
	// Porter-Duff Over in straight alpha with round-to-nearest.
	srcWeight := uint32(src.A) * 255
	dstWeight := uint32(dst.A) * (255 - uint32(src.A))
	sum := srcWeight + dstWeight
	channel := func(s, d byte) byte {
		return byte((uint32(s)*srcWeight + uint32(d)*dstWeight + sum/2) / sum)
	}
	return color.NRGBA{R: channel(src.R, dst.R), G: channel(src.G, dst.G), B: channel(src.B, dst.B), A: byte((sum + 127) / 255)}
}

func webpStickerColor(img image.Image, x, y int) color.NRGBA {
	switch img := img.(type) {
	case *image.NRGBA:
		return img.NRGBAAt(x, y)
	case *image.NYCbCrA:
		return webpStickerVP8Color(&img.YCbCr, x, y, img.A[img.AOffset(x, y)])
	case *image.YCbCr:
		return webpStickerVP8Color(img, x, y, 255)
	default:
		return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
	}
}

func webpStickerVP8Color(img *image.YCbCr, x, y int, alpha byte) color.NRGBA {
	// x/image decodes VP8 to planar 4:2:0. Interpolate chroma using
	// the 9:3:3:1 weights and clamp neighbors at the image edges.
	cx, cy := x/2, y/2
	nx := min(max(cx+(x%2)*2-1, 0), (img.Rect.Dx()-1)/2)
	ny := min(max(cy+(y%2)*2-1, 0), (img.Rect.Dy()-1)/2)
	row, next := cy*img.CStride, ny*img.CStride
	chroma := func(ch []byte) int {
		return (9*int(ch[row+cx]) + 3*int(ch[row+nx]) + 3*int(ch[next+cx]) + int(ch[next+nx]) + 8) / 16
	}

	// VP8 uses BT.601 limited-range Y'CbCr (RFC 6386).
	yy := (int(img.Y[img.YOffset(x, y)]) - 16) * 298
	cb, cr := chroma(img.Cb)-128, chroma(img.Cr)-128
	channel := func(value int) byte { return byte(min(max((value+128)>>8, 0), 255)) }
	return color.NRGBA{R: channel(yy + 409*cr), G: channel(yy - 100*cb - 208*cr), B: channel(yy + 516*cb), A: alpha}
}
