package app

import (
	"bytes"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

func privateViewPixels(tt *ui.Tester, r ui.Rect) []byte {
	im := tt.Image()
	var pixels []byte
	for y := int(r.Y); y < int(r.Y+r.H); y++ {
		for x := int(r.X); x < int(r.X+r.W); x++ {
			p := im.RGBAAt(x, y)
			pixels = append(pixels, p.R, p.G, p.B, p.A)
		}
	}
	return pixels
}

// Only cover hover reveals the centered play control; one click starts one lookup and never sends a message.
func TestPrivateCoverPlayInteraction(t *testing.T) {
	old := albumDetail
	defer func() { albumDetail = old }()
	called := make(chan int64, 4)
	var calls atomic.Int32
	albumDetail = func(id int64) (*netease.AlbumProfile, error) {
		calls.Add(1)
		called <- id
		return nil, errors.New("synthetic offline")
	}
	a := privateCardApp(t)
	coverURL := "https://p1.music.126.net/synthetic-r6.jpg"
	a.art.add(netease.Sized(coverURL, 120), &artwork{bmp: ui.NewBitmap(coverTestImage()), used: time.Now(), size: 1})
	a.dm.chats[90002].messages = []netease.PrivateMessage{{ID: 1, From: netease.MessageUser{UserID: 90002},
		Raw: `{"album":{"id":88,"name":"合成分享专辑","picUrl":"` + coverURL + `"},"msg":"合成分享附言"}`}}
	tt := ui.NewTester(a.view, 1180, 780)
	tt.SetDark(true)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	tt.Advance(time.Second)
	cover, ok := tt.Find("分享封面")
	if !ok {
		t.Fatal("missing cover")
	}
	before := privateViewPixels(tt, cover)
	body, _ := tt.Find("合成分享附言")
	tt.Move(body.X+body.W/2, body.Y+body.H/2)
	tt.Advance(time.Second)
	if !bytes.Equal(before, privateViewPixels(tt, cover)) || calls.Load() != 0 {
		t.Fatal("body hover showed the play overlay or loaded playback data")
	}
	tt.Move(cover.X+3, cover.Y+3)
	tt.Advance(time.Second)
	if bytes.Equal(before, privateViewPixels(tt, cover)) {
		t.Fatal("cover hover did not reveal the play overlay")
	}
	button, ok := tt.Find("播放分享专辑")
	if !ok || abs32(button.X+button.W/2-cover.X-cover.W/2) > 0.1 || abs32(button.Y+button.H/2-cover.Y-cover.H/2) > 0.1 || button.W > cover.W {
		t.Fatal("play button is not centered within the small cover")
	}
	if dir := os.Getenv("NM_FRAMES"); dir != "" {
		f, err := os.Create(filepath.Join(dir, "hover-play-r6.png"))
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(f, tt.Image())
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	tt.Move(button.X+button.W/2, button.Y+button.H/2)
	tt.Frame()
	hover := privateViewPixels(tt, cover)
	if err := tt.Click("播放分享专辑"); err != nil {
		t.Fatal(err)
	}
	select {
	case id := <-called:
		if id != 88 {
			t.Fatal("played wrong resource")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("click did not start metadata worker")
	}
	req := a.dm.play
	if req == nil {
		t.Fatal("click did not record its play intent")
	}
	// A pending lookup used to dim the button under a stationary pointer.
	tt.Advance(50 * time.Millisecond)
	if !bytes.Equal(hover, privateViewPixels(tt, cover)) {
		t.Fatal("the pending play request changed the hovered button")
	}
	_ = tt.Click("播放分享专辑")
	a.playPrivateMedia(netease.PrivateMessageMedia{Kind: "album", ID: 88})
	if calls.Load() != 1 || a.dm.play != req || a.dm.selected != 90002 || a.dm.chats[90002].sending || a.route.page == pageUser {
		t.Fatal("play click repeated or triggered a chat/profile action")
	}
	a.privatePlayAnswered(req, nil, errors.New("offline"))
	tt.Frame()
	tt.Advance(50 * time.Millisecond)
	if !bytes.Equal(hover, privateViewPixels(tt, cover)) {
		t.Fatal("finishing the play request changed the hovered button")
	}
	tt.Move(0, 0)
	tt.Advance(time.Second)
	if !bytes.Equal(before, privateViewPixels(tt, cover)) {
		t.Fatal("play overlay did not hide on pointer exit")
	}
	for _, raw := range []string{`{"album":{"name":"缺少资源编号"}}`, `{"msg":"纯文字没有播放操作"}`} {
		a.dm.chats[90002].messages[0].Raw = raw
		tt.Frame()
		if tt.Click("播放分享专辑") == nil || a.dm.play != nil || calls.Load() != 1 {
			t.Fatal("plain text or ID-less share exposed a play action")
		}
	}
}

// A small cover is keyboard playable, and the same animation settles after hover and exit.
func TestPrivateCoverPlayKeyboardAndMotion(t *testing.T) {
	a, _ := privateTestApp(t)
	a.dm.open = true
	a.pl.Cue([]netease.Track{{Name: "原曲"}})
	a.albums[89] = &albumData{album: &netease.AlbumProfile{Album: netease.Album{ID: 89}}}
	old := albumDetail
	defer func() { albumDetail = old }()
	called := make(chan int64, 2)
	albumDetail = func(id int64) (*netease.AlbumProfile, error) { called <- id; return nil, errors.New("offline") }
	media := netease.PrivateMessageMedia{Kind: "album", ID: 89}
	tt := ui.NewTester(func(c *ui.Context) {
		a.pal = paletteFor(c.Theme())
		ui.Box(c).Padding(24).Children(func() { a.privateMediaCover(c, media) })
	}, 160, 140)
	tt.Frame()
	tt.Move(25, 25)
	tt.Advance(2 * time.Second)
	tt.Move(150, 130)
	tt.Advance(2 * time.Second)
	tt.Frame()
	if tt.FramePending() {
		t.Fatal("cover kept requesting frames after hover ended")
	}
	tt.Key(0, ui.KeyTab)
	if !tt.Focused("播放分享专辑") {
		t.Fatal("play button cannot receive keyboard focus")
	}
	for _, key := range []ui.Key{ui.KeySpace, ui.KeyEnter} {
		tt.Key(0, key)
		select {
		case <-called:
		case <-time.After(3 * time.Second):
			t.Fatalf("%v did not start playback lookup", key)
		}
		a.privatePlayAnswered(a.dm.play, nil, errors.New("offline"))
		tt.Frame()
	}
}

// Each share kind uses its real catalog detail route and keeps album/playlist order, never a cover summary.
func TestPrivatePlayResourceRoutes(t *testing.T) {
	oldSongs, oldAlbum, oldPlaylist := trackDetails, albumDetail, fetchPlaylist
	defer func() { trackDetails, albumDetail, fetchPlaylist = oldSongs, oldAlbum, oldPlaylist }()
	trackDetails = func(ids []int64) ([]netease.Track, error) {
		if len(ids) != 1 || ids[0] != 91 {
			t.Fatal("incorrect single-song lookup")
		}
		return []netease.Track{{ID: 999}, {ID: 91}}, nil
	}
	albumDetail = func(id int64) (*netease.AlbumProfile, error) {
		if id != 92 {
			t.Fatal("incorrect album lookup")
		}
		return &netease.AlbumProfile{Album: netease.Album{ID: id}, Songs: []netease.Track{{ID: 6}, {ID: 3}}}, nil
	}
	fetchPlaylist = func(id int64) (*netease.Playlist, error) {
		if id != 93 {
			t.Fatal("incorrect full playlist lookup")
		}
		return &netease.Playlist{ID: id, Tracks: []netease.Track{{ID: 7}, {ID: 2}}}, nil
	}
	for _, tc := range []struct {
		kind string
		id   int64
		want []int64
	}{{"song", 91, []int64{91}}, {"album", 92, []int64{6, 3}}, {"playlist", 93, []int64{7, 2}}} {
		got, err := privateMediaTracks(tc.kind, tc.id)
		if err != nil || len(got) != len(tc.want) {
			t.Fatal("share did not load playable catalog tracks")
		}
		for i, id := range tc.want {
			if got[i].ID != id {
				t.Fatal("share changed resource or song order")
			}
		}
	}
}

func privatePlayFixture(t *testing.T) (*app, *privatePlayRequest, []netease.Track) {
	t.Helper()
	a, _ := privateTestApp(t)
	a.dm.open = true
	a.pl.Cue([]netease.Track{{Name: "原曲"}})
	req := &privatePlayRequest{key: privateMediaKey{"album", 92}, account: a.accountGen, epoch: a.dm.epoch,
		queue: a.pl.RadioGeneration(), radio: a.radioGen, track: a.pl.Current()}
	a.dm.play = req
	// Non-NetEase, missing local files exercise the real queue synchronously;
	// asynchronous audio/lyrics workers have no network or personal data to read.
	path := filepath.Join(t.TempDir(), "synthetic-missing.mp3")
	tracks := []netease.Track{{Name: "不可播", Privilege: &netease.Privilege{St: -1}},
		{Name: "第一首可播", LocalPath: path}, {Name: "下一首", LocalPath: path}}
	return a, req, tracks
}

// An accepted reply feeds the real ordered player queue, starting at the first playable song.
func TestPrivatePlayQueue(t *testing.T) {
	for _, kind := range []string{"album", "playlist"} {
		a, _, tracks := privatePlayFixture(t)
		a.dm.play = nil
		a.pl.ToggleShuffle()
		if kind == "album" {
			a.albums[92] = &albumData{album: &netease.AlbumProfile{Album: netease.Album{ID: 92}, Songs: tracks}}
		} else {
			a.playlists[92] = &netease.Playlist{ID: 92, Tracks: tracks}
		}
		a.playPrivateMedia(netease.PrivateMessageMedia{Kind: kind, ID: 92})
		if a.dm.play != nil || a.pl.Shuffle() || len(a.pl.Queue()) != 3 || a.pl.Current().Name != "第一首可播" || a.pl.Queue()[2].Name != "下一首" {
			t.Fatal("share did not reuse the full list and replace the real queue in original order")
		}
	}
}

// Errors, empty rights, newer selections and account/pane changes cannot replace what the listener chose.
func TestPrivatePlayRejectedReplies(t *testing.T) {
	for _, change := range []string{"network", "empty", "rights", "new-share", "switch-song", "pause", "radio-intent", "account", "close"} {
		t.Run(change, func(t *testing.T) {
			a, req, tracks := privatePlayFixture(t)
			var err error
			switch change {
			case "network":
				err = errors.New("private server error must not be shown")
			case "empty":
				tracks = nil
			case "rights":
				tracks = tracks[:1]
			case "new-share":
				a.dm.play = &privatePlayRequest{}
			case "switch-song":
				a.pl.Cue([]netease.Track{{Name: "后来选择"}})
			case "pause":
				a.pl.Hold(0)
			case "radio-intent":
				a.requestRadio("synthetic-station")
			case "account":
				a.accountGen++
			case "close":
				a.closePrivateMessages()
			}
			current := a.pl.Current()
			a.privatePlayAnswered(req, tracks, err)
			if a.pl.Current() != current || len(a.pl.Queue()) != 1 {
				t.Fatal("rejected result took over playback")
			}
			if (change == "network" || change == "empty" || change == "rights") && (a.toast == "" || a.toast == errString(err)) {
				t.Fatal("failure was silent or exposed the raw error")
			}
		})
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
