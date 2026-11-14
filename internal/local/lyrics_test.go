package local

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func id3Tag(frames ...[]byte) []byte {
	body := bytes.Join(frames, nil)
	header := []byte{'I', 'D', '3', 3, 0, 0, 0, 0, 0, 0}
	size := len(body)
	header[6], header[7], header[8], header[9] = byte(size>>21)&0x7f, byte(size>>14)&0x7f, byte(size>>7)&0x7f, byte(size)&0x7f
	return append(append(header, body...), mp3Frames()...)
}

func TestMP3LyricsAndNumbers(t *testing.T) {
	uslt := append([]byte{3}, []byte("zho\x00故事的小黄花\n从出生那年就飘着")...)
	path := t.TempDir() + "/a.mp3"
	writeFile(t, path, id3Tag(
		id3Frame("TIT2", append([]byte{3}, []byte("晴天")...)),
		id3Frame("TRCK", append([]byte{3}, []byte("3/10")...)),
		id3Frame("TPOS", utf16Text("2/2")),
		id3Frame("USLT", uslt),
	))
	s, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Track != 3 || s.Disc != 2 {
		t.Errorf("track %d disc %d, want 3 and 2", s.Track, s.Disc)
	}
	if s.Lyrics != "故事的小黄花\n从出生那年就飘着" {
		t.Errorf("lyrics %q", s.Lyrics)
	}
}

func TestMP3SyncedLyrics(t *testing.T) {
	var b bytes.Buffer
	b.Write([]byte{3, 'z', 'h', 'o', 2, 1, 0}) // UTF-8, ms, lyrics, no description
	for _, p := range []struct {
		text string
		ms   uint32
	}{{"故事的", 1500}, {"小黄花", 2000}, {"\n从出生那年", 65250}, {"就飘着", 66000}} {
		b.WriteString(p.text)
		b.WriteByte(0)
		binary.Write(&b, binary.BigEndian, p.ms)
	}
	path := t.TempDir() + "/b.mp3"
	writeFile(t, path, id3Tag(
		id3Frame("USLT", append([]byte{3}, []byte("eng\x00plain")...)),
		id3Frame("SYLT", b.Bytes()),
	))
	got := LyricsOf(path)
	want := "[00:01.50]故事的小黄花\n[01:05.25]从出生那年就飘着"
	if got != want {
		t.Errorf("lyrics %q, want %q", got, want)
	}
}

func TestFLACLyricsAndNumbers(t *testing.T) {
	var comment bytes.Buffer
	comments := []string{"TITLE=夜曲", "TRACKNUMBER=07", "DISCNUMBER=1/2", "UNSYNCEDLYRICS=[00:12.00]一群嗜血的蚂蚁"}
	writeLE(&comment, 4)
	comment.WriteString("test")
	writeLE(&comment, uint32(len(comments)))
	for _, c := range comments {
		writeLE(&comment, uint32(len(c)))
		comment.WriteString(c)
	}
	info := make([]byte, 34)
	b := []byte("fLaC")
	b = append(b, flacBlock(0, false, info)...)
	b = append(b, flacBlock(4, true, comment.Bytes())...)
	path := t.TempDir() + "/c.flac"
	writeFile(t, path, b)
	s, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if s.Track != 7 || s.Disc != 1 {
		t.Errorf("track %d disc %d, want 7 and 1", s.Track, s.Disc)
	}
	if !strings.Contains(s.Lyrics, "一群嗜血的蚂蚁") || !timed(s.Lyrics) {
		t.Errorf("lyrics %q", s.Lyrics)
	}
}
