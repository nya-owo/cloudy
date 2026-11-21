package player

import (
	"cmp"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"math/cmplx"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/egoist/mygo/audio"
	gomp3 "github.com/hajimehoshi/go-mp3"

	"netease-music-mygo/internal/netease"
)

// A download fetches a song as it plays: readers wait for the bytes they
// need, so playback starts with the first ones. The song comes into a file
// of the cache, which keeps it once it has all come in, and is read from
// there: the system holds what of it was read lately, rather than the
// app's memory, as much again as the song. Uncached songs use temporary
// files; only a failure to create or write one falls back to bounded memory.
type download struct {
	// A shared decoder has its own cancellable view of the same bytes.
	// Cancelling it wakes only that reader, leaving playback's download alive.
	parent   *download
	ctx      context.Context
	dropView func()
	mu       sync.Mutex
	cond     sync.Cond
	file     *os.File // where the song is, or nil: then data
	data     []byte
	n        int64 // the bytes in so far
	size     int64 // expected, or 0 if unknown
	done     bool
	err      error
	cancel   context.CancelFunc
	// part is the file's path until the cache keeps it, which keep does
	// once the song has all come in: the path it is at then.
	part   string
	keep   func(part string, n int64) (string, bool)
	settle func(string)
	// users counts what reads or writes the song: the stream decoding it,
	// until it closes, and the goroutine fetching it. The last to go
	// closes the file, and removes it unless the cache kept it.
	users  int
	closed bool
	// base is what stops every connection of the download, and url
	// where the song is: a reader that wants bytes far past what is in
	// has a connection of its own fetch them from there (jump), into
	// the file where they belong: ahead, which has brought in the bytes
	// from a0 up to a1. The head, on its way, takes them as it meets
	// them. noJump is set once the server would send no part of the
	// song, and jumpFails counts the connections ahead that failed.
	base      context.Context
	url       string
	ahead     *fetcher
	a0, a1    int64
	noJump    bool
	jumpFails int
}

// startDownload fetches url, of size bytes if known, into part, a file
// that keep keeps once it has all come in: or into memory, without one.
func startDownload(url string, size int64, part *os.File,
	keep func(string, int64) (string, bool), settle func(string)) *download {
	ctx, cancel := context.WithCancel(context.Background())
	d := &download{size: size, cancel: cancel, users: 2, base: ctx, url: url}
	d.cond.L = &d.mu
	switch {
	case part != nil:
		d.file, d.part, d.keep, d.settle = part, part.Name(), keep, settle
	case size > 0:
		d.data = make([]byte, 0, prealloc(size))
	}
	head := &fetcher{head: true}
	hctx, stop := context.WithCancelCause(ctx)
	head.cancel = stop
	go d.fetch(hctx, head, 0)
	return d
}

// maxPrealloc is the most memory set aside for a song before its bytes
// come in: the size it is said to be is the API's word, which a song that
// turns out longer only outgrows.
const maxPrealloc = 32 << 20

// maxMemoryDownload bounds playback's fallback when no file can be written.
// Failing the download is preferable to retaining an unbounded long episode.
const maxMemoryDownload = 32 << 20

var errDownloadMemory = errors.New("player: audio buffer is full; no writable temporary file")

func prealloc(size int64) int64 { return min(size, maxPrealloc) }

// downloaded returns a download that has all of b in already.
func downloaded(b []byte) *download {
	d := &download{data: b, n: int64(len(b)), size: int64(len(b)), done: true, cancel: func() {}, users: 1}
	d.cond.L = &d.mu
	return d
}

// opened returns a download that has all of the file f in already, of
// size bytes, as a song from the cache.
func opened(f *os.File, size int64) *download {
	d := &download{file: f, n: size, size: size, done: true, cancel: func() {}, users: 1}
	d.cond.L = &d.mu
	return d
}

type streamHTTPError struct {
	code   int
	status string
}

func (e *streamHTTPError) Error() string { return e.status }

// A fetcher is a connection fetching a download: its head, from the
// start, which brings in all of the song; or the one ahead
// (download.ahead), from where a reader jumped to, past what the head
// had brought in.
type fetcher struct {
	cancel context.CancelCauseFunc
	head   bool // it brings in the head: only the head ends the download
	ended  bool // ahead, it reached the song's end
}

// jumpGap is how far past what is in a reader must want bytes for a
// connection of its own to fetch them: nearer, the one that is coming
// gets there sooner than a new one would.
const jumpGap = 256 << 10

// jumpTries is how many connections ahead may fail before a download
// gives them up, and its readers wait for the head as they did.
const jumpTries = 3

// What put says of the bytes it took: more are wanted, the fetcher is
// to stop, or the song is whole.
const (
	putOn = iota
	putStop
	putWhole
)

// fetch fetches the song from byte from on, for f.
func (d *download) fetch(ctx context.Context, f *fetcher, from int64) {
	defer d.release()
	defer f.cancel(nil)
	// A stalled connection gets reopened by the player. Resetting on bytes
	// leaves long songs unlimited, while a dead socket cannot wait forever.
	idle := time.AfterFunc(20*time.Second, func() { f.cancel(context.DeadlineExceeded) })
	defer idle.Stop()
	// finish ends the download, if f is its head. One ahead that failed
	// only goes, with what it brought in: its readers wait for the head.
	finish := func(err error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		if f.head {
			d.done, d.err = true, err
		} else if d.ahead == f {
			d.ahead, d.a0, d.a1 = nil, 0, 0
			d.jumpFails++
		}
		d.cond.Broadcast()
	}
	// whole ends the download with all of the song in: the cache's, if
	// it is as long as promised.
	whole := func() {
		if d.keep != nil && d.part != "" && (d.size <= 0 || d.n == d.size) {
			d.hand()
		}
		finish(nil)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", d.url, nil)
	if err != nil {
		finish(err)
		return
	}
	req.Header.Set("User-Agent", netease.UserAgent)
	want := http.StatusOK
	if from > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(from, 10)+"-")
		want = http.StatusPartialContent
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		finish(cmp.Or(context.Cause(ctx), err))
		return
	}
	defer res.Body.Close()
	if res.StatusCode != want {
		if from > 0 && res.StatusCode == http.StatusOK {
			// The server sends the song only whole: no part of it is
			// asked for again.
			d.mu.Lock()
			d.noJump = true
			d.mu.Unlock()
		}
		finish(&streamHTTPError{code: res.StatusCode, status: res.Status})
		return
	}
	buf := make([]byte, 64<<10)
	at := from
	for {
		n, err := res.Body.Read(buf)
		if n > 0 {
			idle.Reset(20 * time.Second)
			state, werr := d.put(f, buf[:n], at)
			at += int64(n)
			switch {
			case werr != nil:
				finish(werr)
				return
			case state == putStop:
				return
			case state == putWhole:
				whole()
				return
			}
		}
		if err == io.EOF {
			d.mu.Lock()
			if !f.head {
				// Ahead, at the song's end: the head, when it meets
				// what this brought in, has it all.
				if d.ahead == f {
					f.ended = true
				}
				d.cond.Broadcast()
				d.mu.Unlock()
				return
			}
			short := d.size > 0 && d.n < d.size
			d.mu.Unlock()
			if short {
				finish(io.ErrUnexpectedEOF)
				return
			}
			whole()
			return
		}
		if err != nil {
			finish(cmp.Or(context.Cause(ctx), err))
			return
		}
	}
}

// put adds b, the song's bytes from at on, which f fetched: to what is
// in, for the head, or to what is ahead. The head that meets what was
// fetched ahead takes it: and stops, leaving the connection ahead, if
// it still fetches, to bring in the rest as the head.
func (d *download) put(f *fetcher, b []byte, at int64) (int, error) {
	d.mu.Lock()
	file, head := d.file, f.head
	d.mu.Unlock()
	if file != nil {
		if _, err := file.WriteAt(b, at); err != nil {
			if !head {
				return putStop, err
			}
			if err := d.toMemory(); err != nil {
				return putStop, err
			}
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if !f.head {
		if d.ahead != f {
			// Another jump took its place.
			return putStop, nil
		}
		d.a1 = at + int64(len(b))
		d.cond.Broadcast()
		return putOn, nil
	}
	if d.file == nil {
		if d.n+int64(len(b)) > maxMemoryDownload {
			return putStop, errDownloadMemory
		}
		if need := len(d.data) + len(b); need > cap(d.data) {
			next := make([]byte, len(d.data), min(maxMemoryDownload, max(need, 2*cap(d.data))))
			copy(next, d.data)
			d.data = next
		}
		d.data = append(d.data, b...)
	}
	d.n += int64(len(b))
	state := putOn
	if a := d.ahead; a != nil && d.n >= d.a0 {
		d.n = max(d.n, d.a1)
		d.ahead, d.a0, d.a1 = nil, 0, 0
		if a.ended {
			state = putWhole
		} else {
			a.head, f.head = true, false
			state = putStop
		}
	}
	d.cond.Broadcast()
	return state, nil
}

// avail returns how many bytes are in from off on, without a gap. Its
// caller holds the lock.
func (d *download) avail(off int64) int64 {
	switch {
	case off < 0:
		return 0
	case off < d.n:
		return d.n - off
	case d.ahead != nil && off >= d.a0 && off < d.a1:
		return d.a1 - off
	}
	return 0
}

// jump has a connection of its own fetch the song from off on, if that
// is far past what is in and no connection ahead is about to get there:
// the listener jumped ahead in a song still coming in, and hears it from
// there without waiting for all that lies before. Its caller holds the
// lock.
func (d *download) jump(off int64) {
	if d.file == nil || d.size <= 0 || d.base == nil || d.done || d.noJump || d.jumpFails >= jumpTries || d.users <= 0 {
		return
	}
	if off < d.n+jumpGap || off >= d.size {
		return
	}
	if d.ahead != nil {
		if off >= d.a0 && off <= d.a1+jumpGap {
			return
		}
		d.ahead.cancel(context.Canceled)
	}
	f := &fetcher{}
	ctx, stop := context.WithCancelCause(d.base)
	f.cancel = stop
	d.ahead, d.a0, d.a1 = f, off, off
	d.users++
	go d.fetch(ctx, f, off)
}

// waitAt waits until the n bytes from off on are in, or the download
// ended, and reports whether they are. The stream that plays, waiting
// far past what is in, has them fetched from there (jump).
func (d *download) waitAt(off, n int64) bool {
	view := d
	d = d.root()
	d.mu.Lock()
	defer d.mu.Unlock()
	for d.avail(off) < n && !d.done && view.errContext() == nil {
		if view.parent == nil {
			d.jump(off)
		}
		d.cond.Wait()
	}
	return view.errContext() == nil && d.avail(off) >= n
}

// toMemory moves the song into memory from a file that takes no more, as
// on a full disk: it plays on, but is not kept.
func (d *download) toMemory() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.n > maxMemoryDownload {
		return errDownloadMemory
	}
	d.data = make([]byte, d.n, max(prealloc(d.size), d.n))
	got, _ := d.file.ReadAt(d.data, 0)
	d.data = d.data[:got]
	d.n = int64(got)
	d.file.Close()
	os.Remove(d.part)
	d.file, d.part = nil, ""
	// What was fetched ahead was in the file only.
	if d.ahead != nil {
		d.ahead.cancel(context.Canceled)
		d.ahead, d.a0, d.a1 = nil, 0, 0
	}
	return nil
}

// hand gives the song, all in, to the cache, and reads it from where it
// is kept. Windows moves no file that is open: it closes the file first,
// and opens it again where it went, while no one reads it.
func (d *download) hand() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.file.Close()
	path, ok := d.keep(d.part, d.n)
	f, err := os.Open(path)
	if ok && d.settle != nil {
		d.settle(path)
	}
	if err != nil {
		d.file, d.n, d.done, d.err = nil, 0, true, err
		return
	}
	d.file = f
	if ok {
		d.part = ""
	}
}

// Close stops the download and wakes its readers: the stream lets the
// song go.
func (d *download) Close() {
	if d.dropView != nil {
		d.dropView()
		return
	}
	d.cancel()
	d.mu.Lock()
	if !d.done {
		d.done, d.err = true, context.Canceled
	}
	d.cond.Broadcast()
	closed := d.closed
	d.closed = true
	d.mu.Unlock()
	if !closed {
		d.release()
	}
}

// share counts one more user of the song, as a second stream decoding
// it, unless it is gone already.
func (d *download) share() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.users <= 0 {
		return false
	}
	d.users++
	return true
}

// release lets the song go for one of its users. With none left, nothing
// reads it any more: its file closes, and goes unless the cache kept it.
func (d *download) release() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.users--; d.users > 0 {
		return
	}
	if d.file != nil {
		d.file.Close()
	}
	if d.part != "" {
		os.Remove(d.part)
	}
	d.file, d.data, d.n, d.part = nil, nil, 0, ""
	d.ahead, d.a0, d.a1 = nil, 0, 0
}

// Progress returns the share of the song downloaded so far.
func (d *download) Progress() float64 {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.done && d.err == nil {
		return 1
	}
	if d.size <= 0 {
		return 0
	}
	return float64(d.n) / float64(d.size)
}

// waitFor waits until n bytes are in, or the download ended, and returns
// how many there are.
func (d *download) waitFor(n int64) int64 {
	view := d
	d = d.root()
	d.mu.Lock()
	defer d.mu.Unlock()
	for d.n < n && !d.done && view.errContext() == nil {
		d.cond.Wait()
	}
	if view.errContext() != nil {
		return 0
	}
	return d.n
}

func (d *download) root() *download {
	if d.parent != nil {
		return d.parent
	}
	return d
}

func (d *download) errContext() error {
	if d.ctx != nil {
		return d.ctx.Err()
	}
	return nil
}

// readAt reads into b what is in from off, as much as fits. Its caller
// holds the lock.
func (d *download) readAt(b []byte, off int64) (int, error) {
	n := min(int64(len(b)), d.avail(off))
	if n <= 0 {
		return 0, nil
	}
	if d.file == nil {
		return copy(b, d.data[off:off+n]), nil
	}
	return d.file.ReadAt(b[:n], off)
}

// peek returns up to n bytes from off, as far as they are in.
func (d *download) peek(off int64, n int) []byte {
	d = d.root()
	d.mu.Lock()
	defer d.mu.Unlock()
	b := make([]byte, n)
	got, _ := d.readAt(b, off)
	return b[:got]
}

// A window reads a download a piece at a time, for scanning it byte by
// byte without reading the file for each.
type window struct {
	dl  *download
	off int64
	b   []byte
}

// at returns up to n bytes from off, as far as they are in.
func (w *window) at(off int64, n int) []byte {
	if off < w.off || off+int64(n) > w.off+int64(len(w.b)) {
		w.off, w.b = off, w.dl.peek(off, max(n, 32<<10))
	}
	return w.b[off-w.off : min(int64(len(w.b)), off-w.off+int64(n))]
}

// A downloadReader reads a download from an offset, waiting for its bytes.
// It is no io.Seeker, so go-mp3 does not read the whole file up front to
// find where each frame starts.
type downloadReader struct {
	d   *download
	pos int64
}

func (r *downloadReader) Read(p []byte) (int, error) {
	d := r.d.root()
	d.mu.Lock()
	defer d.mu.Unlock()
	for d.avail(r.pos) <= 0 && !d.done && r.d.errContext() == nil {
		if r.d.parent == nil {
			d.jump(r.pos)
		}
		d.cond.Wait()
	}
	if err := r.d.errContext(); err != nil {
		return 0, err
	}
	if d.avail(r.pos) <= 0 {
		if d.err != nil {
			return 0, d.err
		}
		return 0, io.EOF
	}
	n, err := d.readAt(p, r.pos)
	if n == 0 {
		return 0, cmp.Or(err, io.ErrUnexpectedEOF)
	}
	r.pos += int64(n)
	return n, nil
}

// A stream decodes a download for package audio, and measures the
// spectrum of what it decodes, for the visualizers: an MP3's or a FLAC's.
type stream interface {
	audio.Decoder
	Close() error
	// source returns the download decoded, and its spectrum.
	source() (*download, *spectrum)
}

// checkedStream preserves decode errors which audio.OnEnd otherwise reports
// in the same way as the natural end of a song.
type checkedStream struct {
	stream
	mu  sync.Mutex
	err error
}

func (s *checkedStream) Read(dst []float32) (int, error) {
	n, err := s.stream.Read(dst)
	if err != nil && err != io.EOF {
		s.mu.Lock()
		s.err = err
		s.mu.Unlock()
	}
	return n, err
}

// whole reports whether all of the song's bytes came in, without error.
func (s *checkedStream) whole() bool {
	dl, _ := s.source()
	if dl == nil {
		return false
	}
	d := dl.root()
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.done && d.err == nil && (d.size <= 0 || d.n >= d.size)
}

func (s *checkedStream) Err() error {
	s.mu.Lock()
	err := s.err
	s.mu.Unlock()
	if err != nil {
		return err
	}
	dl, _ := s.source()
	dl.mu.Lock()
	defer dl.mu.Unlock()
	return dl.err
}

// openStream opens the song dl fetches, MP3 or FLAC, as its first bytes
// say. An MP3 says nothing of its length: durationMs is it.
func openStream(dl *download, durationMs int64) (stream, error) {
	off := id3Size(dl)
	if dl.waitFor(off+4) >= off+4 && string(dl.peek(off, 4)) == "fLaC" {
		return openFLACStream(dl)
	}
	dl.waitFor(off + 12)
	if format := otherFormat(dl.peek(off, 12)); format == "DSD" {
		if s, err := openDSDStream(dl); err == nil {
			return s, nil
		}
		return nil, &UnsupportedError{Format: format}
	} else if format != "" {
		if s, err := openOther(dl, durationMs, format); err == nil {
			return s, nil
		}
		return nil, &UnsupportedError{Format: format}
	}
	return openMP3Stream(dl, durationMs)
}

// UnsupportedError is audio in a format the player cannot decode. The MP3
// decoder, given it, read the whole file looking for a frame and failed,
// and the song was fetched again and again: it stayed "loading" (AAC from
// a viewer's cloud drive, 1.3.2). Such a song is skipped.
type UnsupportedError struct {
	Format string
}

func (e *UnsupportedError) Error() string {
	return "暂不支持播放 " + e.Format + " 格式的音频"
}

// otherFormat names the format of audio starting with head when it is one
// the player knows it cannot decode, or is "".
func otherFormat(head []byte) string {
	switch {
	case len(head) >= 8 && string(head[4:8]) == "ftyp":
		return "AAC (M4A)"
	case len(head) >= 2 && head[0] == 0xff && head[1]&0xf6 == 0xf0:
		// ADTS: the sync word with its layer bits zero, which MP3's never are.
		return "AAC"
	case len(head) >= 4 && string(head[:4]) == "OggS":
		return "OGG"
	case len(head) >= 4 && string(head[:4]) == "RIFF":
		return "WAV"
	case len(head) >= 4 && (string(head[:4]) == "wvpk" || string(head[:4]) == "MAC "):
		return "无损压缩（APE/WV）"
	case len(head) >= 4 && string(head[:4]) == "DSD ":
		return "DSD"
	case len(head) >= 4 && string(head[:4]) == "FRM8":
		return "DSD"
	case len(head) >= 4 && head[0] == 0x30 && head[1] == 0x26 && head[2] == 0xb2 && head[3] == 0x75:
		return "WMA"
	}
	return ""
}

// openShared opens a second stream of what dl fetches, for decoding apart
// from what plays: it measures no spectrum, and closing it lets the song
// go for it alone, leaving the download to the stream that plays.
func openShared(dl *download, durationMs int64) (stream, error) {
	return openSharedContext(context.Background(), dl, durationMs)
}

func openSharedContext(ctx context.Context, dl *download, durationMs int64) (stream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !dl.share() {
		return nil, errors.New("player: the song is gone")
	}
	view := &download{parent: dl, ctx: ctx, size: dl.size}
	stop := context.AfterFunc(ctx, func() {
		dl.mu.Lock()
		dl.cond.Broadcast()
		dl.mu.Unlock()
	})
	view.dropView = sync.OnceFunc(func() { stop(); dl.release() })
	s, err := openStream(view, durationMs)
	if err != nil {
		view.Close()
		return nil, err
	}
	return s, nil
}

// spectrum belongs to the stream playing, not another decoder of its bytes.
func (d *download) spectrum(rate int, frames int64) *spectrum {
	if d.parent != nil {
		return nil
	}
	return newSpectrum(rate, frames)
}

// mp3Stream decodes a download for package audio, seeking by bytes: songs
// are encoded at a constant bit rate, so time and bytes are in proportion.
// It also measures the spectrum of what it decodes, for the visualizers.
type mp3Stream struct {
	dl      *download
	dec     *gomp3.Decoder
	rate    int
	frames  int64 // the song's length in frames, from its duration
	start   int64 // the first audio frame's byte, after the ID3 tag
	pos     int64 // the frame decoded next
	skip    int   // bytes of decoded audio to drop after a seek
	buf     []byte
	spec    *spectrum
	drop    func() // lets the download go, as the stream closes
	closeMu sync.Once
}

func openMP3Stream(dl *download, durationMs int64) (*mp3Stream, error) {
	s := &mp3Stream{dl: dl, drop: dl.Close}
	dec, err := gomp3.NewDecoder(&downloadReader{d: dl})
	if err != nil {
		return nil, err
	}
	s.dec, s.rate = dec, dec.SampleRate()
	s.frames = durationMs * int64(s.rate) / 1000
	s.start = id3Size(dl)
	s.spec = dl.spectrum(s.rate, s.frames)
	return s, nil
}

// id3Size returns the length of the ID3v2 tag at the start of the file.
func id3Size(dl *download) int64 {
	dl.waitFor(10)
	b := dl.peek(0, 10)
	if len(b) < 10 || string(b[:3]) != "ID3" {
		return 0
	}
	size := int64(b[6]&0x7f)<<21 | int64(b[7]&0x7f)<<14 | int64(b[8]&0x7f)<<7 | int64(b[9]&0x7f)
	return 10 + size
}

func (s *mp3Stream) SampleRate() int { return s.rate }
func (s *mp3Stream) Channels() int   { return 2 }
func (s *mp3Stream) Len() int64      { return s.frames }

func (s *mp3Stream) Read(dst []float32) (int, error) {
	n := len(dst) / 2 * 4
	if cap(s.buf) < n {
		s.buf = make([]byte, n)
	}
	b := s.buf[:n]
	for s.skip > 0 {
		got, err := io.ReadFull(s.dec, b[:min(n, s.skip)])
		s.skip -= got
		if err != nil {
			s.skip = 0
		}
	}
	got, err := io.ReadFull(s.dec, b)
	got -= got % 4
	samples := got / 2
	for i := range samples {
		dst[i] = float32(int16(binary.LittleEndian.Uint16(b[2*i:]))) / (1 << 15)
	}
	frames := int64(samples / 2)
	s.spec.feed(s.pos, dst[:samples])
	s.pos += frames
	if samples > 0 {
		return samples, nil
	}
	if err == io.ErrUnexpectedEOF || err == nil {
		err = io.EOF
	}
	return 0, err
}

// preroll is how many frames of sound a seek decodes before the one asked
// for, and drops. An MP3 frame borrows bits from the frames before it, up
// to 511 bytes back, which at 128 kbps is two frames, and its sound
// overlaps theirs: the first few decoded from the middle of a file are
// noise, as loud as the loudest sound.
const preroll = 4 * 1152

func (s *mp3Stream) SetPosition(frame int64) error {
	var off int64
	if from := frame - preroll; from > 0 && s.frames > 0 {
		size := s.dl.size
		if size <= 0 {
			size = s.dl.waitFor(math.MaxInt64)
		}
		off = s.start + int64(float64(size-s.start)*float64(from)/float64(s.frames))
		off = findFrame(s.dl, off)
	}
	dec, err := gomp3.NewDecoder(&downloadReader{d: s.dl, pos: off})
	if err != nil {
		return err
	}
	s.dec, s.pos = dec, frame
	// Drop what comes before frame: the preroll, or near the start, all
	// of it from there.
	s.skip = int(min(frame, preroll)) * 4
	return nil
}

func (s *mp3Stream) source() (*download, *spectrum) { return s.dl, s.spec }

func (s *mp3Stream) Close() error {
	s.closeMu.Do(s.drop)
	return nil
}

// findFrame returns the start of the first MP3 frame at or after off whose
// header the next frame's confirms, so that bytes that only look like one
// within the audio do not count.
func findFrame(dl *download, off int64) int64 {
	w := window{dl: dl}
	for tries := 0; tries < 64<<10; tries++ {
		if !dl.waitAt(off, 4) {
			return off
		}
		size := frameSize(w.at(off, 4))
		if size > 0 {
			dl.waitAt(off+int64(size), 4)
			next := w.at(off+int64(size), 4)
			if len(next) < 4 || frameSize(next) > 0 {
				return off
			}
		}
		off++
	}
	return off
}

var (
	bitratesV1 = [16]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
	bitratesV2 = [16]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}
	rates      = [4]int{44100, 48000, 32000, 0}
)

// frameSize returns the length of the MPEG layer III frame whose header b
// starts with, or 0 if it starts with none.
func frameSize(b []byte) int {
	if len(b) < 4 || b[0] != 0xff || b[1]&0xe0 != 0xe0 {
		return 0
	}
	version := (b[1] >> 3) & 3 // 3: MPEG 1, 2: MPEG 2, 0: MPEG 2.5
	layer := (b[1] >> 1) & 3   // 1: layer III
	if version == 1 || layer != 1 {
		return 0
	}
	bi, ri := b[2]>>4, (b[2]>>2)&3
	if bi == 0 || bi == 15 || ri == 3 {
		return 0
	}
	pad := int(b[2]>>1) & 1
	rate := rates[ri]
	switch version {
	case 3:
		return 144*bitratesV1[bi]*1000/rate + pad
	case 2:
		return 72*bitratesV2[bi]*1000/(rate/2) + pad
	default:
		return 72*bitratesV2[bi]*1000/(rate/4) + pad
	}
}

// The spectrum of a song, measured in chunks of fftSize frames as it
// decodes, a second ahead of what plays: the visualizers look up the
// chunk playing.

const (
	fftSize       = 1024
	Bands         = 24
	spectrumSlots = 512 // ~5.5 s at 96 kHz, ~11.9 s at 44.1 kHz (~56 KB fixed)
)

type spectrumSlot struct {
	chunk int64 // chunk index stored in this slot, or -1 if empty
	bands [Bands]float32
}

type spectrum struct {
	mu     sync.RWMutex
	rate   int
	slots  [spectrumSlots]spectrumSlot
	window [fftSize]float32
	acc    []float32 // mono samples of the chunk being filled
	accAt  int64     // its first frame
	edges  [Bands + 1]int
	fft    []complex128
}

func newSpectrum(rate int, _ int64) *spectrum {
	s := &spectrum{rate: rate, accAt: -1}
	for i := range s.slots {
		s.slots[i].chunk = -1
	}
	for i := range s.window {
		s.window[i] = float32(0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(fftSize-1)))
	}
	// Bands spaced evenly in pitch, from 40 Hz to 14 kHz.
	lo, hi := math.Log(40), math.Log(14000)
	for i := range s.edges {
		f := math.Exp(lo + (hi-lo)*float64(i)/Bands)
		s.edges[i] = max(1, min(fftSize/2-1, int(f*fftSize/float64(rate))))
	}
	for i := 1; i < len(s.edges); i++ {
		s.edges[i] = max(s.edges[i], s.edges[i-1]+1)
	}
	s.fft = make([]complex128, fftSize)
	return s
}

// feed takes stereo samples decoded from frame at, unless the stream
// measures no spectrum.
func (s *spectrum) feed(at int64, stereo []float32) {
	if s == nil {
		return
	}
	if s.accAt < 0 || at != s.accAt+int64(len(s.acc)) {
		// The start, or a seek: begin a chunk at a chunk's boundary.
		s.acc = s.acc[:0]
		skip := (fftSize - at%fftSize) % fftSize
		if int(skip)*2 >= len(stereo) {
			s.accAt = -1
			return
		}
		stereo = stereo[skip*2:]
		at += skip
		s.accAt = at
	}
	for i := 0; i+1 < len(stereo); i += 2 {
		s.acc = append(s.acc, (stereo[i]+stereo[i+1])/2)
		if len(s.acc) == fftSize {
			s.analyze(s.accAt / fftSize)
			s.accAt += fftSize
			s.acc = s.acc[:0]
		}
	}
}

func (s *spectrum) analyze(chunk int64) {
	if chunk < 0 {
		return
	}
	for i, v := range s.acc {
		s.fft[i] = complex(float64(v*s.window[i]), 0)
	}
	fft(s.fft)
	var bands [Bands]float32
	for b := range Bands {
		var sum float64
		for k := s.edges[b]; k < s.edges[b+1]; k++ {
			sum += cmplx.Abs(s.fft[k])
		}
		mag := sum / float64(s.edges[b+1]-s.edges[b]) / (fftSize / 4)
		// Higher bands carry less energy: tilt them up, as the ear hears.
		mag *= 1 + float64(b)/6
		db := 20 * math.Log10(mag+1e-9)
		bands[b] = float32(math.Max(0, math.Min(1, (db+60)/52)))
	}
	s.mu.Lock()
	slot := &s.slots[chunk%spectrumSlots]
	slot.chunk = chunk
	slot.bands = bands
	s.mu.Unlock()
}

// at returns the bands of frame, or false if not measured.
func (s *spectrum) at(frame int64) ([Bands]float32, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := frame / fftSize
	if c < 0 {
		return [Bands]float32{}, false
	}
	slot := &s.slots[c%spectrumSlots]
	if slot.chunk != c {
		return [Bands]float32{}, false
	}
	return slot.bands, true
}

// fft transforms x in place; len(x) is a power of two.
func fft(x []complex128) {
	n := len(x)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}
	for size := 2; size <= n; size <<= 1 {
		w := cmplx.Exp(complex(0, -2*math.Pi/float64(size)))
		for start := 0; start < n; start += size {
			wk := complex(1, 0)
			for k := range size / 2 {
				a, b := x[start+k], x[start+k+size/2]*wk
				x[start+k], x[start+k+size/2] = a+b, a-b
				wk *= w
			}
		}
	}
}

// write adds b after what is in, as the head does.
func (d *download) write(b []byte) error {
	d.mu.Lock()
	at := d.n
	d.mu.Unlock()
	_, err := d.put(&fetcher{head: true}, b, at)
	return err
}
