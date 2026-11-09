package app

import (
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// savedAlbums are three albums the listener saved, at different times.
func savedAlbums() []netease.AlbumProfile {
	return []netease.AlbumProfile{
		{Album: netease.Album{ID: 1, Name: "Zebra"}, Artists: []netease.Artist{{Name: "Beta"}}, SubTime: 300},
		{Album: netease.Album{ID: 2, Name: "Apple"}, Artists: []netease.Artist{{Name: "Gamma"}}, SubTime: 100},
		{Album: netease.Album{ID: 3, Name: "Mango"}, Artists: []netease.Artist{{Name: "Alpha"}}, SubTime: 200},
	}
}

// The albums saved list in the order chosen: those saved last first, by
// name, or by artist; the time for settings kept before there was a choice.
func TestAlbumSorts(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.myAlbums = savedAlbums()
	ids := func() []int64 {
		var out []int64
		for _, al := range a.sortedAlbums() {
			out = append(out, al.ID)
		}
		return out
	}
	if got := ids(); !slices.Equal(got, []int64{1, 3, 2}) {
		t.Errorf("by time: %v", got)
	}
	a.settings.AlbumSort = "name"
	if got := ids(); !slices.Equal(got, []int64{2, 3, 1}) {
		t.Errorf("by name: %v", got)
	}
	a.settings.AlbumSort = "artist"
	if got := ids(); !slices.Equal(got, []int64{3, 1, 2}) {
		t.Errorf("by artist: %v", got)
	}
}

// Saving an album from its page or the sidebar's menu adds it to the list,
// unsaving takes it out, a failure keeps it as it was, and a reply to an
// earlier login changes nothing.
func TestAlbumToggled(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.profile = &netease.Profile{UserID: 1}
	a.myAlbums = savedAlbums()
	al := netease.AlbumProfile{Album: netease.Album{ID: 9, Name: "New"}}

	a.albumToggled(a.accountGen, al, false, nil)
	if !a.albumSaved(9) {
		t.Fatal("saving the album did not add it")
	}
	if a.myAlbums[0].ID != 9 || a.myAlbums[0].SubTime == 0 {
		t.Errorf("the album saved came in as %+v", a.myAlbums[0])
	}
	a.albumToggled(a.accountGen, al, true, nil)
	if a.albumSaved(9) {
		t.Error("unsaving the album did not take it out")
	}
	a.albumToggled(a.accountGen, al, false, errors.New("offline"))
	if a.albumSaved(9) {
		t.Error("a failure added the album anyway")
	}
	gen := a.accountGen
	a.accountGen++
	a.albumToggled(gen, al, false, nil)
	if a.albumSaved(9) {
		t.Error("a reply to an earlier login added the album")
	}
}

// With NM_FRAMES set, saves the sidebar's albums and their menu, in the
// light and the dark, for looking at them.
func TestAlbumFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip("set NM_FRAMES to a directory to save frames")
	}
	save := func(name string, tt *ui.Tester) {
		tt.Advance(time.Second)
		tt.Frame()
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
	for _, dark := range []bool{false, true} {
		appearance := "light"
		if dark {
			appearance = "dark"
		}
		a := newTestApp()
		a.settings = defaultSettings()
		a.profile = &netease.Profile{UserID: 1, Nickname: "nya_owo"}
		a.myAlbums = savedAlbums()
		tt := ui.NewTester(a.view, 1280, 820)
		tt.SetDark(dark)
		save("albums-"+appearance, tt)

		// The album's page, with the button that unsaves it.
		al := netease.AlbumProfile{
			Album:   netease.Album{ID: 1, Name: "Zebra"},
			Artists: []netease.Artist{{Name: "Beta"}},
			Songs:   []netease.Track{{ID: 7, Name: "First", Duration: 180000}},
		}
		b := newTestApp()
		b.settings = defaultSettings()
		b.profile = &netease.Profile{UserID: 1, Nickname: "nya_owo"}
		b.myAlbums = []netease.AlbumProfile{al}
		b.albums[1] = &albumData{album: &al}
		b.route = route{page: pageAlbum, id: 1}
		bt := ui.NewTester(b.view, 1280, 820)
		bt.SetDark(dark)
		save("albums-detail-"+appearance, bt)
	}
}
