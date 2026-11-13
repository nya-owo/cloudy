package app

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Landing used to stack two gray tracks at full strength, then flash darker.
func TestFlyingScrubTrackHandover(t *testing.T) {
	track := ui.RGBA(255, 255, 255, .18)
	alpha := float32(track.A) / 255
	for _, k := range []float32{.90, .995, .996, .997, .998, .999, .9995, 1} {
		land := easeRange(k, 1-flightEnd, 1-flightTouch)
		flight := float32(flyScrubTrack(track, k).A) / 255
		shown := 1 - easeRange(k, 1-flightTouch, 1)
		composite := alpha*land + flight*shown*(1-alpha*land)
		if math.Abs(float64(composite-alpha)) > 1.0/255 {
			t.Errorf("k=%v: combined track alpha %v, want %v", k, composite, alpha)
		}
	}
	if flyScrubTrack(track, .9) != track || flyScrubTrack(track, .999).A != 0 {
		t.Fatal("handover changed the earlier track or retained the departing track")
	}
}
