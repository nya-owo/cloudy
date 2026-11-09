package player

import (
	"io"
	"math"
	"sync/atomic"

	"github.com/egoist/mygo/audio"
)

// A stretch plays a decoder faster or slower without changing its pitch,
// for episodes heard at 1.5 times: the audio player's own speed plays a
// record faster, a voice an octave higher at twice. It cuts the sound
// into overlapping pieces and lays them down closer together or further
// apart (WSOLA): each piece is taken where it best continues the one
// before, near where the speed says, so that no wave breaks between them.
//
// Its frames are of the time heard: frame f of it is frame f*speed of
// the decoder, from the start, at the speed set by its last SetPosition.
type stretch struct {
	audio.Decoder
	speed float64
	next  atomic.Uint64 // the speed asked for, as math.Float64bits; it applies at SetPosition

	in      []float32 // stereo frames of the decoder from inStart
	inStart int64
	eof     bool
	ana     float64 // where the next piece would start at the speed, in the decoder's frames
	prev    int64   // where the last piece did start, -1 before the first
	acc     []float32
	out     []float32 // ready to be read
	window  []float32
	scratch []float32
}

const (
	stretchN     = 1024         // frames a piece
	stretchHop   = stretchN / 2 // frames between pieces laid down
	stretchSeek  = 160          // how far from where the speed says a piece may start
	stretchMatch = stretchN / 2 // frames compared to find where it best continues
	stretchStep  = 2            // of which every second
)

func newStretch(d audio.Decoder, speed float64) *stretch {
	s := &stretch{Decoder: d, speed: speed, prev: -1}
	s.next.Store(math.Float64bits(speed))
	s.window = make([]float32, stretchN)
	for i := range s.window {
		// Periodic Hann: windows half a piece apart sum to one.
		s.window[i] = float32(0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/stretchN))
	}
	s.acc = make([]float32, 2*stretchN)
	s.scratch = make([]float32, 2*4096)
	return s
}

// setSpeed asks for another speed, heard from the next SetPosition.
func (s *stretch) setSpeed(speed float64) { s.next.Store(math.Float64bits(speed)) }

func (s *stretch) Channels() int { return 2 }

func (s *stretch) Len() int64 {
	n := s.Decoder.Len()
	if n < 0 || s.speed == 1 {
		return n
	}
	return int64(float64(n) / s.speed)
}

func (s *stretch) SetPosition(frame int64) error {
	s.speed = math.Float64frombits(s.next.Load())
	at := int64(float64(frame) * s.speed)
	s.in, s.inStart, s.eof = s.in[:0], at, false
	s.ana, s.prev = float64(at), -1
	clear(s.acc)
	s.out = s.out[:0]
	return s.Decoder.SetPosition(at)
}

func (s *stretch) Close() error {
	if c, ok := s.Decoder.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

func (s *stretch) Read(dst []float32) (int, error) {
	if s.speed == 1 {
		return s.Decoder.Read(dst)
	}
	for len(s.out) < len(dst) {
		if !s.piece() {
			break
		}
	}
	n := copy(dst[:len(dst)/2*2], s.out)
	s.out = s.out[n:]
	if n == 0 {
		return 0, io.EOF
	}
	return n, nil
}

// have reads the decoder until frame end is in, or it ends.
func (s *stretch) have(end int64) error {
	for !s.eof && s.inStart+int64(len(s.in)/2) < end {
		n, err := s.Decoder.Read(s.scratch)
		s.in = append(s.in, s.scratch[:n]...)
		if err == io.EOF {
			s.eof = true
		} else if err != nil {
			return err
		}
	}
	return nil
}

// at returns the stereo sample i frames into the decoder, zero past what
// it gave.
func (s *stretch) at(frame int64, ch int) float32 {
	i := (frame-s.inStart)*2 + int64(ch)
	if i < 0 || i >= int64(len(s.in)) {
		return 0
	}
	return s.in[i]
}

// piece lays down the next piece, and reports whether there was one.
func (s *stretch) piece() bool {
	nominal := int64(math.Round(s.ana))
	if err := s.have(nominal + stretchSeek + stretchN); err != nil {
		return false
	}
	if s.eof && nominal >= s.inStart+int64(len(s.in)/2) {
		// Nothing more: what is still in the overlap goes out.
		if s.prev >= 0 {
			s.out = append(s.out, s.acc[:2*stretchHop]...)
			s.prev = -1
			clear(s.acc)
			return true
		}
		return false
	}
	start := nominal
	if s.prev >= 0 {
		// Where the piece before would have gone on: the piece taken is
		// the one near nominal that sounds most like it.
		natural := s.prev + stretchHop
		s.have(natural + stretchN)
		best, bestScore := int64(0), math.Inf(-1)
		lo := max(-stretchSeek, s.inStart-nominal)
		for d := lo; d <= stretchSeek; d++ {
			var dot, energy float64
			for k := int64(0); k < stretchMatch; k += stretchStep {
				x := float64(s.at(nominal+d+k, 0) + s.at(nominal+d+k, 1))
				y := float64(s.at(natural+k, 0) + s.at(natural+k, 1))
				dot += x * y
				energy += x * x
			}
			score := dot / math.Sqrt(energy+1e-9)
			if score > bestScore {
				best, bestScore = d, score
			}
		}
		start = nominal + best
	}
	for i := range int64(stretchN) {
		w := s.window[i]
		s.acc[2*i] += w * s.at(start+i, 0)
		s.acc[2*i+1] += w * s.at(start+i, 1)
	}
	s.out = append(s.out, s.acc[:2*stretchHop]...)
	copy(s.acc, s.acc[2*stretchHop:])
	clear(s.acc[2*(stretchN-stretchHop):])
	s.prev = start
	s.ana += stretchHop * s.speed
	// What no later piece can reach goes.
	keep := min(s.prev, int64(math.Round(s.ana))-stretchSeek)
	if drop := keep - s.inStart; drop > 0 && int(drop)*2 <= len(s.in) {
		s.in = append(s.in[:0], s.in[drop*2:]...)
		s.inStart = keep
	}
	return true
}
