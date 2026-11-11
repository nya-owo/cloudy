// Package local reads the songs in a folder of the listener's: which files
// are songs, and what their tags say they are.
package local

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf16"
)

// Song is a file of the listener's folder, as its tags describe it, or as
// its name does when it has none.
type Song struct {
	Path        string
	Title       string
	Artist      string
	Album       string
	Duration    time.Duration
	Picture     []byte
	PictureMIME string
	// NeteaseID is the NetEase song the file was downloaded as, which Cloudy
	// writes into what it downloads, or 0.
	NeteaseID int64

	pictureType byte
}

// Scan returns the songs in dir, and those below it, in the order of their
// paths. Files it cannot read are left out rather than stopping it.
func Scan(dir string) ([]Song, error) {
	paths, err := Paths(dir)
	if err != nil {
		return nil, err
	}
	songs := make([]Song, 0, len(paths))
	for _, path := range paths {
		s, err := Read(path)
		if err != nil {
			continue
		}
		songs = append(songs, *s)
	}
	return songs, nil
}

// Paths returns the paths of the songs in dir, and those below it, in
// order, without reading them: Read reads each, one at a time, for a large
// folder's covers not to be all in memory at once.
func Paths(dir string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == dir {
				return err
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".mp3", ".flac":
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(paths, func(i, j int) bool {
		return strings.ToLower(paths[i]) < strings.ToLower(paths[j])
	})
	return paths, nil
}

// Read returns the song at path, MP3 or FLAC, with the fallbacks its name
// gives where its tags say nothing.
func Read(path string) (*Song, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	s := &Song{Path: path}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		readMP3(f, info.Size(), s)
	case ".flac":
		readFLAC(f, info.Size(), s)
	default:
		return nil, fmt.Errorf("local: %s is not an MP3 or a FLAC", path)
	}
	infer(s)
	return s, nil
}

// infer fills what the tags left empty from the file's name.
func infer(s *Song) {
	s.Title = strings.TrimSpace(s.Title)
	s.Artist = strings.TrimSpace(s.Artist)
	s.Album = strings.TrimSpace(s.Album)
	base := strings.TrimSpace(strings.TrimSuffix(filepath.Base(s.Path), filepath.Ext(s.Path)))
	if s.Title == "" {
		name := trimTrackNumber(base)
		if s.Artist == "" {
			if artist, title, ok := strings.Cut(name, " - "); ok {
				s.Artist = strings.TrimSpace(artist)
				name = strings.TrimSpace(title)
			}
		}
		s.Title = name
	}
	if s.Title == "" {
		s.Title = base
	}
}

// trimTrackNumber drops a number at the front of a file's name, as
// "01 - " or "01. ", which says where it sits in an album rather than what
// it is.
func trimTrackNumber(name string) string {
	i := 0
	for i < len(name) && i < 3 && name[i] >= '0' && name[i] <= '9' {
		i++
	}
	if i == 0 {
		return name
	}
	rest := name[i:]
	if !strings.HasPrefix(rest, ".") && !strings.HasPrefix(rest, "-") &&
		!strings.HasPrefix(rest, "_") && !strings.HasPrefix(rest, " - ") {
		return name
	}
	rest = strings.TrimLeft(rest, " ._-")
	if rest == "" {
		return name
	}
	return strings.TrimSpace(rest)
}

// readMP3 reads what an ID3v2 tag, or else an ID3v1 one, says of the song,
// and how long it plays.
func readMP3(r io.ReaderAt, size int64, s *Song) {
	start := id3TagSize(r)
	if start > 10 && start < size && start < 64<<20 {
		b := make([]byte, start)
		if got, _ := r.ReadAt(b, 0); int64(got) == int64(len(b)) {
			readID3v2(b, s)
		}
	}
	if s.Title == "" || s.Artist == "" || s.Album == "" {
		readID3v1(r, size, s)
	}
	if s.Duration <= 0 {
		s.Duration = mp3Duration(r, size, start)
	}
}

// id3TagSize returns how many bytes the ID3v2 tag at the start takes, with
// its header, or 0 without one.
func id3TagSize(r io.ReaderAt) int64 {
	h := make([]byte, 10)
	if n, _ := r.ReadAt(h, 0); n < 10 || string(h[:3]) != "ID3" {
		return 0
	}
	n := int64(syncsafe(h[6:10]))
	if h[3] == 4 && h[5]&0x10 != 0 {
		n += 10 // the footer stands after the tag's size, before the audio.
	}
	return 10 + n
}

func readID3v2(b []byte, s *Song) {
	if len(b) < 10 || string(b[:3]) != "ID3" {
		return
	}
	version, flags := b[3], b[5]
	b = b[10 : 10+syncsafe(b[6:10])]
	if flags&0x80 != 0 {
		b = deunsynchronize(b)
	}
	off := 0
	if flags&0x40 != 0 && len(b) >= 4 {
		if version >= 4 {
			off = int(syncsafe(b[:4]))
			if off < 6 {
				off = 6
			}
		} else {
			off = 4 + int(binary.BigEndian.Uint32(b[:4]))
		}
	}
	for off >= 0 && off < len(b) {
		if b[off] == 0 {
			break
		}
		idLen, head := 4, 10
		if version == 2 {
			idLen, head = 3, 6
		}
		if off+head > len(b) {
			break
		}
		id := string(b[off : off+idLen])
		var size int
		switch {
		case version == 2:
			size = int(b[off+3])<<16 | int(b[off+4])<<8 | int(b[off+5])
		case version >= 4:
			size = int(syncsafe(b[off+4 : off+8]))
		default:
			size = int(binary.BigEndian.Uint32(b[off+4 : off+8]))
		}
		if size <= 0 || off+head+size > len(b) {
			break
		}
		body := b[off+head : off+head+size]
		switch id {
		case "TIT2", "TT2":
			s.Title = first(s.Title, id3Text(body))
		case "TPE1", "TP1":
			s.Artist = first(s.Artist, id3Text(body))
		case "TALB", "TAL":
			s.Album = first(s.Album, id3Text(body))
		case "TPE2", "TP2":
			s.Artist = first(s.Artist, id3Text(body))
		case "TXXX":
			readTXXX(body, s)
		case "APIC":
			readAPIC(body, s)
		case "PIC":
			readPIC(body, s)
		}
		off += head + size
	}
}

func readID3v1(r io.ReaderAt, size int64, s *Song) {
	if size < 128 {
		return
	}
	b := make([]byte, 128)
	if n, _ := r.ReadAt(b, size-128); n < 128 || string(b[:3]) != "TAG" {
		return
	}
	clean := func(b []byte) string {
		return strings.TrimSpace(strings.TrimRight(string(b), "\x00"))
	}
	s.Title = first(s.Title, clean(b[3:33]))
	s.Artist = first(s.Artist, clean(b[33:63]))
	s.Album = first(s.Album, clean(b[63:93]))
}

// id3Text reads a text frame's body, its encoding first.
func id3Text(b []byte) string {
	if len(b) < 1 {
		return ""
	}
	enc, b := b[0], b[1:]
	switch enc {
	case 1:
		return decodeUTF16(b, true)
	case 2:
		return decodeUTF16(b, false)
	case 3:
		return strings.TrimRight(string(b), "\x00")
	default:
		return strings.TrimRight(string(b), "\x00")
	}
}

func decodeUTF16(b []byte, bom bool) string {
	var order binary.ByteOrder = binary.LittleEndian
	if len(b) >= 2 && bom {
		switch {
		case b[0] == 0xfe && b[1] == 0xff:
			order, b = binary.BigEndian, b[2:]
		case b[0] == 0xff && b[1] == 0xfe:
			b = b[2:]
		}
	}
	if len(b)%2 != 0 {
		b = b[:len(b)-1]
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = order.Uint16(b[2*i:])
	}
	return strings.TrimRight(string(utf16.Decode(u)), "\x00")
}

// readAPIC reads an ID3v2.3 or 2.4 attached picture: its MIME type, the
// kind of picture, a description, and the picture itself.
func readAPIC(b []byte, s *Song) {
	if len(b) < 4 {
		return
	}
	enc, b := b[0], b[1:]
	i := bytes.IndexByte(b, 0)
	if i < 0 || i+2 > len(b) {
		return
	}
	mime, b := string(b[:i]), b[i+1:]
	typ, b := b[0], b[1:]
	rest, ok := cutDescription(b, enc)
	if !ok {
		return
	}
	setPicture(s, typ, mime, rest)
}

// readPIC reads the same as readAPIC, as ID3v2.2 spells it: a three-letter
// format rather than a MIME type.
func readPIC(b []byte, s *Song) {
	if len(b) < 5 {
		return
	}
	enc, b := b[0], b[1:]
	format, b := string(b[:3]), b[3:]
	typ, b := b[0], b[1:]
	mime := "image/" + strings.ToLower(strings.TrimSpace(format))
	if mime == "image/jpg" {
		mime = "image/jpeg"
	}
	rest, ok := cutDescription(b, enc)
	if !ok {
		return
	}
	setPicture(s, typ, mime, rest)
}

func cutDescription(b []byte, enc byte) ([]byte, bool) {
	if enc == 1 || enc == 2 {
		for i := 0; i+1 < len(b); i += 2 {
			if b[i] == 0 && b[i+1] == 0 {
				return b[i+2:], true
			}
		}
		return nil, false
	}
	i := bytes.IndexByte(b, 0)
	if i < 0 {
		return nil, false
	}
	return b[i+1:], true
}

func setPicture(s *Song, typ byte, mime string, data []byte) {
	if len(data) == 0 {
		return
	}
	// The front cover is the one to show, wherever the tag puts it.
	if s.Picture != nil && (s.pictureType == 3 || typ != 3) {
		return
	}
	s.Picture, s.PictureMIME, s.pictureType = data, mime, typ
}

func syncsafe(b []byte) int64 {
	if len(b) < 4 {
		return 0
	}
	return int64(b[0]&0x7f)<<21 | int64(b[1]&0x7f)<<14 | int64(b[2]&0x7f)<<7 | int64(b[3]&0x7f)
}

func deunsynchronize(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		out = append(out, b[i])
		if b[i] == 0xff && i+1 < len(b) && b[i+1] == 0 {
			i++
		}
	}
	return out
}

func first(old, next string) string {
	if old != "" {
		return old
	}
	return next
}

// mp3Duration returns how long the MP3 plays: as its Xing or VBRI header
// counts frames, or, without one, as its first frame's bit rate and the
// file's size say.
func mp3Duration(r io.ReaderAt, size, start int64) time.Duration {
	if size <= start {
		return 0
	}
	n := min(int64(128<<10), size-start)
	b := make([]byte, n)
	if got, _ := r.ReadAt(b, start); got < 4 {
		return 0
	}
	for i := 0; i+4 <= len(b); i++ {
		h, ok := parseMP3Header(b[i : i+4])
		if !ok {
			continue
		}
		if i+h.frameLen+4 <= len(b) {
			if _, next := parseMP3Header(b[i+h.frameLen : i+h.frameLen+4]); !next {
				continue
			}
		}
		if d := mp3HeaderDuration(h, b[i:], size-start); d > 0 {
			return d
		}
	}
	return 0
}

type mp3Header struct {
	bitrate    int
	sampleRate int
	samples    int
	frameLen   int
	mono       bool
	mpeg1      bool
}

// parseMP3Header reads an MPEG Layer III frame header.
func parseMP3Header(b []byte) (mp3Header, bool) {
	if len(b) < 4 || b[0] != 0xff || b[1]&0xe0 != 0xe0 {
		return mp3Header{}, false
	}
	version := (b[1] >> 3) & 3
	layer := (b[1] >> 1) & 3
	if version == 1 || layer != 1 {
		return mp3Header{}, false
	}
	br, sr := int(b[2]>>4), int((b[2]>>2)&3)
	if br == 0 || br == 15 || sr == 3 {
		return mp3Header{}, false
	}
	h := mp3Header{mono: b[3]>>6 == 3, mpeg1: version == 3}
	if h.mpeg1 {
		h.bitrate = [...]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}[br]
		h.samples = 1152
	} else {
		h.bitrate = [...]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}[br]
		h.samples = 576
	}
	h.sampleRate = [...]int{44100, 48000, 32000}[sr]
	if version == 2 {
		h.sampleRate /= 2
	} else if version == 0 {
		h.sampleRate /= 4
	}
	h.frameLen = h.samples/8*h.bitrate*1000/h.sampleRate + int((b[2]>>1)&1)
	return h, true
}

// mp3HeaderDuration reads a Xing or VBRI count of frames from the first
// frame, or falls back to its constant bit rate.
func mp3HeaderDuration(h mp3Header, frame []byte, bytes int64) time.Duration {
	side := 32
	if !h.mpeg1 {
		side = 17
	}
	if h.mono {
		side /= 2
	}
	if len(frame) >= 4+side+12 {
		tag := frame[4+side:]
		if string(tag[:4]) == "Xing" || string(tag[:4]) == "Info" {
			if binary.BigEndian.Uint32(tag[4:8])&1 != 0 {
				if frames := int64(binary.BigEndian.Uint32(tag[8:12])); frames > 0 {
					return framesDuration(frames, h)
				}
			}
		}
	}
	if len(frame) >= 4+32+18 && string(frame[4+32:4+32+4]) == "VBRI" {
		if frames := int64(binary.BigEndian.Uint32(frame[4+32+14:])); frames > 0 {
			return framesDuration(frames, h)
		}
	}
	if h.bitrate > 0 && h.sampleRate > 0 {
		return time.Duration(float64(bytes) * 8 * float64(time.Second) / float64(h.bitrate*1000))
	}
	return 0
}

func framesDuration(frames int64, h mp3Header) time.Duration {
	if h.sampleRate <= 0 {
		return 0
	}
	return time.Duration(float64(frames) * float64(h.samples) / float64(h.sampleRate) * float64(time.Second))
}

// readFLAC reads what a FLAC file's stream info, comments and pictures say.
func readFLAC(r io.ReaderAt, size int64, s *Song) {
	off := id3TagSize(r)
	h := make([]byte, 4)
	if n, _ := r.ReadAt(h, off); n < 4 || string(h) != "fLaC" {
		return
	}
	off += 4
	for off+4 <= size {
		if n, _ := r.ReadAt(h, off); n < 4 {
			return
		}
		last := h[0]&0x80 != 0
		typ := h[0] & 0x7f
		n := int64(h[1])<<16 | int64(h[2])<<8 | int64(h[3])
		if n <= 0 || n > 16<<20 || off+4+n > size {
			return
		}
		b := make([]byte, n)
		if got, _ := r.ReadAt(b, off+4); int64(got) != n {
			return
		}
		switch typ {
		case 0:
			readStreamInfo(b, s)
		case 4:
			readVorbisComment(b, s)
		case 6:
			readFLACPicture(b, s)
		}
		off += 4 + n
		if last {
			return
		}
	}
}

func readStreamInfo(b []byte, s *Song) {
	if len(b) < 18 {
		return
	}
	x := binary.BigEndian.Uint64(b[10:18])
	rate := int(x >> 44)
	total := int64(x & (1<<36 - 1))
	if rate > 0 && total > 0 {
		s.Duration = time.Duration(total) * time.Second / time.Duration(rate)
	}
}

func readVorbisComment(b []byte, s *Song) {
	read := func() (string, bool) {
		if len(b) < 4 {
			return "", false
		}
		n := int(binary.LittleEndian.Uint32(b[:4]))
		if n < 0 || len(b) < 4+n {
			return "", false
		}
		v := string(b[4 : 4+n])
		b = b[4+n:]
		return v, true
	}
	if _, ok := read(); !ok {
		return
	}
	if len(b) < 4 {
		return
	}
	count := int(binary.LittleEndian.Uint32(b[:4]))
	b = b[4:]
	for range count {
		v, ok := read()
		if !ok {
			return
		}
		key, value, found := strings.Cut(v, "=")
		if !found {
			continue
		}
		switch strings.ToUpper(key) {
		case "TITLE":
			s.Title = first(s.Title, value)
		case "ARTIST":
			s.Artist = first(s.Artist, value)
		case "ALBUM":
			s.Album = first(s.Album, value)
		case "ALBUMARTIST":
			s.Artist = first(s.Artist, value)
		case "NETEASE_ID":
			s.NeteaseID, _ = strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		}
	}
}

func readFLACPicture(b []byte, s *Song) {
	if len(b) < 8 {
		return
	}
	typ := b[0]
	mb, b := flacBytes(b[4:])
	mime := string(mb)
	if mime == "" {
		mime = "image/jpeg"
	}
	if _, b = flacBytes(b); b == nil {
		return
	}
	if len(b) < 16 {
		return
	}
	data, _ := flacBytes(b[16:])
	if len(data) == 0 {
		return
	}
	setPicture(s, typ, mime, data)
}

// flacBytes reads a 32-bit length and the bytes after it.
func flacBytes(b []byte) ([]byte, []byte) {
	if len(b) < 4 {
		return nil, nil
	}
	n := int(binary.BigEndian.Uint32(b[:4]))
	if n < 0 || len(b) < 4+n {
		return nil, nil
	}
	return b[4 : 4+n], b[4+n:]
}

// readTXXX reads a user text frame: Cloudy keeps the NetEase song a file
// was downloaded as in one, as NETEASE_ID, in Latin-1 or UTF-8.
func readTXXX(b []byte, s *Song) {
	if len(b) < 2 || b[0] != 0 && b[0] != 3 {
		return
	}
	desc, value, ok := bytes.Cut(b[1:], []byte{0})
	if !ok || string(desc) != "NETEASE_ID" {
		return
	}
	s.NeteaseID, _ = strconv.ParseInt(strings.TrimSpace(strings.TrimRight(string(value), "\x00")), 10, 64)
}
