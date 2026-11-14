package app

import (
	"netease-music-mygo/internal/netease"
)

// Songs kept on disk before 1.2.13, as those played lately, the queue and
// the downloads' index, lack where they sit in their albums (Track.No and
// Cd), which sorting by album goes by: such a song went last in its album.
// Sorted by album, the songs of NetEase without them have them asked for
// once, and the list sorts again as they come.

// trackDetails asks NetEase for songs' details; tests replace it.
var trackDetails = netease.SongDetails

// numberCache is what is known of songs' places in their albums, and which
// are being asked for.
type numberCache struct {
	known  map[int64][2]netease.Num // no, cd
	asking map[int64]bool
}

// numberTracks gives the songs of tracks that lack their place in their
// album the one known, and asks for those not known yet.
func (a *app) numberTracks(tracks []netease.Track) {
	n := &a.more.numbers
	var ask []int64
	for i := range tracks {
		t := &tracks[i]
		if t.No != 0 || t.ID <= 0 || t.ProgramID != 0 {
			continue
		}
		if k, ok := n.known[t.ID]; ok {
			t.No, t.Cd = k[0], k[1]
			continue
		}
		if !n.asking[t.ID] && len(ask) < 1000 {
			ask = append(ask, t.ID)
		}
	}
	if len(ask) == 0 || a.win == nil {
		return
	}
	if n.asking == nil {
		n.asking = map[int64]bool{}
	}
	for _, id := range ask {
		n.asking[id] = true
	}
	go func() {
		got, _ := trackDetails(ask)
		a.post(func() {
			if n.known == nil {
				n.known = map[int64][2]netease.Num{}
			}
			for _, t := range got {
				n.known[t.ID] = [2]netease.Num{t.No, t.Cd}
			}
			// Asked for, known or not, they are not asked for again.
			for _, id := range ask {
				if _, ok := n.known[id]; !ok {
					n.known[id] = [2]netease.Num{}
				}
			}
			// The list sorts again with them.
			a.plView.matched = false
			a.redraw()
		})
	}()
}
