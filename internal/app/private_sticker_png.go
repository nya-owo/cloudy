package app

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/draw"
	"image/png"
	"slices"
	"time"
)

const pngSignature = "\x89PNG\r\n\x1a\n"

// A chunk includes the length, four-byte type, payload, and checksum.
func splitPNGChunk(data []byte) (chunk, rest []byte) {
	if len(data) < 12 {
		return nil, nil
	}
	size := binary.BigEndian.Uint32(data)
	if uint64(size) > uint64(len(data)-12) {
		return nil, nil
	}
	end := int(size) + 12
	return data[:end], data[end:]
}

func appendPNGChunk(dst []byte, kind string, payload []byte) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(payload)))
	start := len(dst)
	dst = append(dst, kind...)
	dst = append(dst, payload...)
	return binary.BigEndian.AppendUint32(dst, crc32.ChecksumIEEE(dst[start:]))
}

func validPNGChunkCRC(chunk []byte) bool {
	return crc32.ChecksumIEEE(chunk[4:len(chunk)-4]) == binary.BigEndian.Uint32(chunk[len(chunk)-4:])
}

// Some CDN PNGs carry palette transparency alongside full per-pixel alpha.
// Remove the redundant chunk only for alpha color types and a valid checksum.
func stripPNGAlphaTransparency(data []byte) []byte {
	if !bytes.HasPrefix(data, []byte(pngSignature)) {
		return data
	}
	const signatureLen = len(pngSignature)
	header, rest := splitPNGChunk(data[signatureLen:])
	if len(header) != 25 || string(header[4:8]) != "IHDR" || (header[17] != 4 && header[17] != 6) {
		return data
	}
	var clean []byte
	for len(rest) > 0 {
		chunk, tail := splitPNGChunk(rest)
		if chunk == nil || string(chunk[4:8]) == "IDAT" {
			break
		}
		if string(chunk[4:8]) == "tRNS" && validPNGChunkCRC(chunk) {
			if clean == nil {
				clean = append([]byte(nil), data[:len(data)-len(rest)]...)
			}
		} else if clean != nil {
			clean = append(clean, chunk...)
		}
		rest = tail
	}
	if clean != nil {
		return append(clean, rest...)
	}
	return data
}

func pngAnimationControl(data []byte) []byte {
	if !bytes.HasPrefix(data, []byte(pngSignature)) {
		return nil
	}
	for rest := data[len(pngSignature):]; len(rest) > 0; {
		chunk, tail := splitPNGChunk(rest)
		if chunk == nil || string(chunk[4:8]) == "IDAT" {
			break
		}
		if string(chunk[4:8]) == "acTL" && len(chunk) == 20 && validPNGChunkCRC(chunk) {
			return chunk[8:16]
		}
		rest = tail
	}
	return nil
}

type apngFrame struct {
	rect           image.Rectangle
	delay          time.Duration
	dispose, blend byte
}

func readAPNGFrame(p []byte, bounds image.Rectangle) (apngFrame, bool) {
	if len(p) != 26 || p[24] > 2 || p[25] > 1 {
		return apngFrame{}, false
	}
	w, h := uint64(binary.BigEndian.Uint32(p[4:])), uint64(binary.BigEndian.Uint32(p[8:]))
	x, y := uint64(binary.BigEndian.Uint32(p[12:])), uint64(binary.BigEndian.Uint32(p[16:]))
	if w == 0 || h == 0 || x+w > uint64(bounds.Dx()) || y+h > uint64(bounds.Dy()) {
		return apngFrame{}, false
	}
	denom := binary.BigEndian.Uint16(p[22:])
	if denom == 0 {
		denom = 100
	}
	delay := time.Duration(binary.BigEndian.Uint16(p[20:])) * time.Second / time.Duration(denom)
	return apngFrame{image.Rect(int(x), int(y), int(x+w), int(y+h)), delay, p[24], p[25]}, true
}

// Decode each APNG frame as a PNG using the standard library, then composite
// it onto the shared canvas with the frame's blend and disposal rules.
func decodeAPNGSticker(data, control []byte) (*artwork, error) {
	invalid := png.FormatError("invalid animation chunks")
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	bounds := image.Rect(0, 0, config.Width, config.Height)
	frameCount := int(binary.BigEndian.Uint32(control))
	if frameCount <= 0 {
		return nil, invalid
	}
	if uint64(config.Width)*uint64(config.Height)*4 > artLimit/2 {
		return nil, png.UnsupportedError("APNG canvas exceeds sticker budget")
	}
	if frameCount > 1 && stickerFrameCount(bounds, frameCount) == 1 {
		bmp, _, err := decodeArt(data)
		return &artwork{bmp: bmp, size: len(data)}, err
	}
	header, rest := splitPNGChunk(data[len(pngSignature):])
	if len(header) != 25 || string(header[4:8]) != "IHDR" {
		return nil, invalid
	}
	ihdr := slices.Clone(header[8:21])
	var palette, framePNG []byte
	var frame apngFrame
	var canvas *image.RGBA
	var sequence uint32
	var seenIDAT, frameUsesIDAT bool
	anim := &stickerAnimation{plays: int(binary.BigEndian.Uint32(control[4:]))}
	finishFrame := func() error {
		img, err := png.Decode(bytes.NewReader(appendPNGChunk(framePNG, "IEND", nil)))
		if err != nil {
			return err
		}
		if canvas == nil {
			canvas = image.NewRGBA(bounds)
		}
		var previous []byte
		if frame.dispose == 2 {
			previous = slices.Clone(canvas.Pix)
		}
		op := draw.Src
		if frame.blend == 1 {
			op = draw.Over
		}
		draw.Draw(canvas, frame.rect, img, img.Bounds().Min, op)
		snapshot := *canvas
		snapshot.Pix = slices.Clone(canvas.Pix)
		anim.add(&snapshot, frame.delay)
		switch frame.dispose {
		case 1:
			draw.Draw(canvas, frame.rect, image.Transparent, image.Point{}, draw.Src)
		case 2:
			copy(canvas.Pix, previous)
		}
		return nil
	}
	for len(rest) > 0 {
		chunk, tail := splitPNGChunk(rest)
		if chunk == nil {
			return nil, invalid
		}
		rest = tail
		kind, payload := string(chunk[4:8]), chunk[8:len(chunk)-4]
		switch kind {
		case "PLTE", "tRNS":
			if !seenIDAT {
				palette = append(palette, chunk...)
				if frameUsesIDAT {
					framePNG = append(framePNG, chunk...)
				}
			}
		case "fcTL", "fdAT":
			if len(payload) < 4 || !validPNGChunkCRC(chunk) || binary.BigEndian.Uint32(payload) != sequence {
				return nil, invalid
			}
			sequence++
			if kind == "fdAT" {
				if !seenIDAT || framePNG == nil || frameUsesIDAT {
					return nil, invalid
				}
				framePNG = appendPNGChunk(framePNG, "IDAT", payload[4:])
				continue
			}
			if framePNG != nil {
				if err := finishFrame(); err != nil {
					return nil, err
				}
			}
			var ok bool
			frame, ok = readAPNGFrame(payload, bounds)
			if !ok || len(anim.frames) >= frameCount || !seenIDAT && frame.rect != bounds {
				return nil, invalid
			}
			frameUsesIDAT = !seenIDAT
			binary.BigEndian.PutUint32(ihdr, uint32(frame.rect.Dx()))
			binary.BigEndian.PutUint32(ihdr[4:], uint32(frame.rect.Dy()))
			framePNG = append(framePNG[:0], pngSignature...)
			framePNG = appendPNGChunk(framePNG, "IHDR", ihdr)
			framePNG = append(framePNG, palette...)
		case "IDAT":
			seenIDAT = true
			if frameUsesIDAT {
				framePNG = append(framePNG, chunk...)
			} else if framePNG != nil {
				return nil, invalid
			}
		case "IEND":
			if framePNG == nil || !seenIDAT || len(anim.frames)+1 != frameCount || len(payload) != 0 || !validPNGChunkCRC(chunk) {
				return nil, invalid
			}
			if err := finishFrame(); err != nil {
				return nil, err
			}
			return anim.artwork(bounds), nil
		}
	}
	return nil, invalid
}
