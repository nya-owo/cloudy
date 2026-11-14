package local

import (
	"encoding/binary"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// lrcStamp is the time an LRC line begins with, as [01:23.45].
var lrcStamp = regexp.MustCompile(`\[\d+:\d+(?:[.:]\d+)?\]`)

// timed reports whether lyrics carry LRC times.
func timed(lyrics string) bool {
	return lrcStamp.MatchString(lyrics)
}

// readUSLT reads an ID3 unsynchronised lyrics frame: its encoding, its
// language, a description, and the lyrics.
func readUSLT(b []byte) string {
	if len(b) < 5 {
		return ""
	}
	enc := b[0]
	rest, ok := cutDescription(b[4:], enc)
	if !ok {
		return ""
	}
	return strings.TrimSpace(textOf(rest, enc))
}

// readSYLT reads an ID3 synchronised lyrics frame into LRC: each piece of
// text with the time it is sung. Its times must be in milliseconds; MPEG
// frames, the other kind, are left out. Pieces may be whole lines or the
// syllables of one, a new line beginning with a line break.
func readSYLT(b []byte) string {
	if len(b) < 7 {
		return ""
	}
	enc, format := b[0], b[4]
	if format != 2 {
		return ""
	}
	rest, ok := cutDescription(b[6:], enc)
	if !ok {
		return ""
	}
	type piece struct {
		at   time.Duration
		text string
	}
	var pieces []piece
	breaks := false
	for len(rest) > 0 {
		text, after, ok := cutText(rest, enc)
		if !ok || len(after) < 4 {
			break
		}
		at := time.Duration(binary.BigEndian.Uint32(after[:4])) * time.Millisecond
		rest = after[4:]
		if strings.ContainsAny(text, "\r\n") {
			breaks = true
		}
		pieces = append(pieces, piece{at, text})
	}
	var lines []piece
	for _, p := range pieces {
		starts := !breaks || len(lines) == 0 || strings.HasPrefix(p.text, "\n") || strings.HasPrefix(p.text, "\r")
		if starts {
			lines = append(lines, piece{p.at, strings.TrimLeft(p.text, "\r\n")})
		} else {
			lines[len(lines)-1].text += p.text
		}
	}
	var sb strings.Builder
	for _, l := range lines {
		ms := l.at.Milliseconds()
		fmt.Fprintf(&sb, "[%02d:%02d.%02d]%s\n", ms/60000, ms/1000%60, ms%1000/10, strings.TrimSpace(strings.ReplaceAll(l.text, "\n", " ")))
	}
	return sb.String()
}

// cutText reads a string ended by a nul in encoding enc, and what follows.
func cutText(b []byte, enc byte) (string, []byte, bool) {
	if enc == 1 || enc == 2 {
		for i := 0; i+1 < len(b); i += 2 {
			if b[i] == 0 && b[i+1] == 0 {
				return textOf(b[:i], enc), b[i+2:], true
			}
		}
		return "", nil, false
	}
	for i, c := range b {
		if c == 0 {
			return textOf(b[:i], enc), b[i+1:], true
		}
	}
	return "", nil, false
}

// textOf decodes b, a string of encoding enc without its nul.
func textOf(b []byte, enc byte) string {
	switch enc {
	case 1:
		return decodeUTF16(b, true)
	case 2:
		return decodeUTF16(b, false)
	case 0:
		// Latin-1.
		r := make([]rune, len(b))
		for i, c := range b {
			r[i] = rune(c)
		}
		return strings.TrimRight(string(r), "\x00")
	default:
		return strings.TrimRight(string(b), "\x00")
	}
}
