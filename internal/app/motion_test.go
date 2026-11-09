package app

import (
	"image"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

const motionFrame = 16 * time.Millisecond

// markArt is a cover of a color nothing else has, to find it in a frame.
func markArt(a *app, url string, px int) {
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = 255, 0, 255, 255
	}
	a.art.add(netease.Sized(url, px), &artwork{bmp: ui.NewBitmap(img), tint: ui.RGB(120, 60, 120), used: time.Now(), size: 1})
}

// A mark is where that color shows in a frame, in DIPs, and how much of it.
type mark struct {
	x, y float32
	area int
}

func findMark(img *image.RGBA, s int) mark {
	var sx, sy, n int
	b := img.Bounds()
	for y := 0; y < b.Dy(); y += 2 {
		for x := 0; x < b.Dx(); x += 2 {
			c := img.RGBAAt(x, y)
			if c.R > 170 && c.G < 90 && c.B > 170 {
				sx, sy, n = sx+x, sy+y, n+1
			}
		}
	}
	if n == 0 {
		return mark{}
	}
	return mark{float32(sx) / float32(n*s), float32(sy) / float32(n*s), n * 4 / (s * s)}
}

// frameDiff is how much two frames differ.
func frameDiff(p, q *image.RGBA) int {
	if p == nil || q == nil {
		return 0
	}
	d, b := 0, p.Bounds()
	for y := 0; y < b.Dy(); y += 2 {
		for x := 0; x < b.Dx(); x += 2 {
			u, v := p.RGBAAt(x, y), q.RGBAAt(x, y)
			d += absInt(int(u.R)-int(v.R)) + absInt(int(u.G)-int(v.G)) + absInt(int(u.B)-int(v.B))
		}
	}
	return d / 3000
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func grab(tt *ui.Tester) *image.RGBA {
	img := tt.Image()
	own := image.NewRGBA(img.Bounds())
	copy(own.Pix, img.Pix)
	return own
}

// watchCover logs where the cover shows, frame by frame, as it flies.
func watchCover(t *testing.T, a *app, tt *ui.Tester, frames int, at func(i int)) []mark {
	var marks []mark
	s := tt.Image().Bounds().Dx() / 1100
	for i := 0; i < frames; i++ {
		if at != nil {
			at(i)
		}
		tt.Frame()
		m := findMark(tt.Image(), s)
		marks = append(marks, m)
		f := " "
		if a.flight.url != "" {
			f = "F"
		}
		t.Logf("%3d ms %s  x %7.1f  y %7.1f  area %6d", i*16, f, m.x, m.y, m.area)
		tt.Advance(motionFrame)
	}
	return marks
}

func flightApp() (*app, *ui.Tester, string) {
	a := newTestApp()
	a.settings = defaultSettings()
	url := "https://example.com/cover.jpg"
	markArt(a, url, 400)
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	tt.Advance(100 * time.Millisecond)
	tt.Frame()
	return a, tt, url
}

// With NM_MOTION set, logs a cover flying to a page already here.
func TestCoverFlightFrames(t *testing.T) {
	if os.Getenv("NM_MOTION") == "" {
		t.Skip("set NM_MOTION to log the frames")
	}
	a, tt, url := flightApp()
	a.playlists[7] = &netease.Playlist{ID: 7, Name: "钢琴曲", CoverURL: url}
	a.flight = coverFlight{url: url, from: ui.Rect{X: 300, Y: 420, W: 180, H: 180}, asked: tt.Now()}
	a.open(route{page: pagePlaylist, id: 7})
	watchCover(t, a, tt, 40, nil)
}

// With NM_MOTION set, logs a cover flying to a page that loads on the way,
// its name and what it says taking more room than its skeleton did.
func TestCoverFlightLoadingFrames(t *testing.T) {
	if os.Getenv("NM_MOTION") == "" {
		t.Skip("set NM_MOTION to log the frames")
	}
	for _, loadsAt := range []int{10, 34} {
		t.Logf("loads at frame %d", loadsAt)
		a, tt, url := flightApp()
		a.loadingPl[7] = true
		a.flight = coverFlight{url: url, from: ui.Rect{X: 300, Y: 420, W: 180, H: 180}, asked: tt.Now()}
		a.open(route{page: pagePlaylist, id: 7})
		long := ""
		for range 12 {
			long += "一个很长很长的歌单名字"
		}
		watchCover(t, a, tt, 42, func(i int) {
			if i == loadsAt {
				a.playlists[7] = &netease.Playlist{ID: 7, Name: long, CoverURL: url, Description: long + long + long}
				a.loadingPl[7] = false
			}
		})
	}
}

// With NM_MOTION set, logs how much each of the other animations changes the
// window from frame to frame: a jump on its way shows as a spike.
func TestOtherMotionFrames(t *testing.T) {
	if os.Getenv("NM_MOTION") == "" {
		t.Skip("set NM_MOTION to log the frames")
	}
	lists := []netease.Playlist{{ID: 1, Name: "一"}, {ID: 2, Name: "二"}, {ID: 3, Name: "三"}, {ID: 4, Name: "四"}}
	menu := [][]menuAction{{{label: "下一首播放", icon: iconPlay, do: func() {}}, {label: "收藏", icon: iconPlus, do: func() {}}}}
	for _, s := range []struct {
		name string
		do   func(a *app)
	}{
		{"page across", func(a *app) { a.open(route{page: pageCharts}) }},
		{"page deeper", func(a *app) { a.open(route{page: pagePlaylist, id: 9}) }},
		{"menu open", func(a *app) { a.openMenu(300, 200, 200, false, menu, nil) }},
		{"menu close", func(a *app) { a.menu.open = false }},
		{"settings open", func(a *app) { a.openSettings() }},
		{"settings close", func(a *app) { a.settingsView.open = false }},
		{"volume open", func(a *app) { a.volumeOpen = true }},
		{"volume close", func(a *app) { a.volumeOpen = false }},
		{"collapse", func(a *app) { a.mineCollapsed = true }},
		{"expand", func(a *app) { a.mineCollapsed = false }},
		{"next song", func(a *app) { a.pl.Next() }},
	} {
		a := newTestApp()
		a.settings = defaultSettings()
		a.settings.Smoke = false
		a.mine, a.saved = lists, lists
		a.pl.Cue([]netease.Track{{ID: 1, Name: "第一首歌", Duration: 180000}, {ID: 2, Name: "第二首歌", Duration: 180000}})
		tt := ui.NewTester(a.view, 1100, 760)
		tt.Frame()
		tt.Advance(time.Second)
		tt.Frame()
		switch s.name {
		case "menu close":
			a.openMenu(300, 200, 200, false, menu, nil)
		case "settings close":
			a.openSettings()
		case "volume close":
			a.volumeOpen = true
		case "expand":
			a.mineCollapsed = true
		}
		tt.Frame()
		tt.Advance(time.Second)
		tt.Frame()
		tt.Advance(time.Second)
		tt.Frame()
		last := grab(tt)
		s.do(a)
		line := ""
		for i := 0; i < 36; i++ {
			tt.Advance(motionFrame)
			tt.Frame()
			now := grab(tt)
			line += " " + itoa(frameDiff(now, last))
			last = now
		}
		t.Logf("%-16s%s", s.name, line)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// A cover shows all the way from its card to its page, where it stays, though
// the page's larger cover hasn't loaded, and never jumps.
func TestCoverShowsAllTheWay(t *testing.T) {
	a, tt, url := flightApp()
	a.playlists[7] = &netease.Playlist{ID: 7, Name: "钢琴曲", CoverURL: url}
	a.flight = coverFlight{url: url, from: ui.Rect{X: 300, Y: 420, W: 180, H: 180}, asked: tt.Now()}
	a.open(route{page: pagePlaylist, id: 7})
	marks := watchCover(t, a, tt, 44, nil)
	for i, m := range marks {
		if m.area < 30000 {
			t.Fatalf("frame %d shows %d of the cover", i, m.area)
		}
		if i > 0 && marks[i-1].y-m.y > 40 {
			t.Errorf("frame %d jumps from %v to %v", i, marks[i-1], m)
		}
	}
	if a.flight.url != "" {
		t.Error("the cover did not land")
	}
}

// A page loading under a cover on its way, or just landed, leaves it where it
// is, whatever its name and what it says take.
func TestCoverStaysAsPageLoads(t *testing.T) {
	for _, loadsAt := range []int{10, 36} {
		a, tt, url := flightApp()
		a.loadingPl[7] = true
		a.flight = coverFlight{url: url, from: ui.Rect{X: 300, Y: 420, W: 180, H: 180}, asked: tt.Now()}
		a.open(route{page: pagePlaylist, id: 7})
		long := ""
		for range 40 {
			long += "一个很长的歌单名字"
		}
		marks := watchCover(t, a, tt, 44, func(i int) {
			if i == loadsAt {
				a.playlists[7] = &netease.Playlist{ID: 7, Name: long, CoverURL: url, Description: long}
				a.loadingPl[7] = false
			}
		})
		for i := 32; i < len(marks); i++ {
			if d := marks[i].y - marks[31].y; d > 2.5 || d < -2.5 {
				t.Errorf("loaded at %d, the cover moved %v by frame %d", loadsAt, d, i)
			}
		}
	}
}

// Opening and closing, the cover shows all the way, once, and it is back in
// the bar exactly where it left it.
func TestNowPlayingCoverGoesAndComesBack(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.Smoke = false
	url := "https://example.com/album.jpg"
	markArt(a, url, 120)
	a.art.add(netease.Sized(url, 600)+"#backdrop", a.art.peek(netease.Sized(url, 120)))
	tr := netease.Track{ID: 1, Name: "被神明写的歌", Duration: 180000}
	tr.Album.PicURL = url
	a.pl.Cue([]netease.Track{tr})
	tt := ui.NewTester(a.view, 1100, 760)
	settle := func() {
		for range 3 {
			tt.Frame()
			tt.Advance(time.Second)
		}
		tt.Frame()
	}
	settle()
	// Once, for where it all lands.
	a.nowPlaying = true
	settle()
	a.nowPlaying = false
	settle()
	s := tt.Image().Bounds().Dx() / 1100
	rest := findMark(tt.Image(), s)
	if rest.area == 0 {
		t.Fatal("no cover in the bar")
	}
	a.nowPlaying = true
	for i := range 90 {
		if i == 45 {
			a.nowPlaying = false
		}
		tt.Advance(motionFrame)
		tt.Frame()
		m := findMark(tt.Image(), s)
		if m.area < rest.area*8/10 {
			t.Fatalf("frame %d shows %d of the cover, at rest %d", i, m.area, rest.area)
		}
		// Two covers apart would pull the middle of it off the way it goes.
		if a.npK > 0.02 && a.npK < 0.98 && m.area > 2*rest.area && m.y > 690 {
			t.Errorf("frame %d, %v of the way, still shows the bar's cover: %v", i, a.npK, m)
		}
	}
	settle()
	back := findMark(tt.Image(), s)
	if d := back.x - rest.x + back.y - rest.y; d > 1 || d < -1 || back.area != rest.area {
		t.Errorf("the cover left at %v and came back at %v", rest, back)
	}
}

// A dialog's panel is as new as its backdrop, so a Transition wouldn't move
// it: it comes in over some frames, and stays until it has gone.
func TestDialogComesAndGoes(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	moves := func() int {
		n, last := 0, grab(tt)
		for range 30 {
			tt.Advance(motionFrame)
			tt.Frame()
			now := grab(tt)
			if frameDiff(now, last) > 30 {
				n++
			}
			last = now
		}
		return n
	}
	a.settingsView.open = true
	if n := moves(); n < 6 {
		t.Errorf("it came in over %d frames", n)
	}
	a.settingsView.open = false
	if n := moves(); n < 6 {
		t.Errorf("it went over %d frames", n)
	}
}

// The corners of the app keep to four sizes: 8 for what is small, 14 for cards,
// rows and covers, 20 for panes, 26 for the largest. Circles, capsules and the
// smallest badges are shapes, not corners, and the notch is a Mac's.
func TestCornersKeepToFour(t *testing.T) {
	shapes := map[string]bool{"3": true, "4": true, "13": true, "17": true, "36": true, "999": true}
	corners := map[string]bool{"8": true, "14": true, "20": true, "26": true}
	files, _ := filepath.Glob("*.go")
	radius := regexp.MustCompile(`\.Radius\((\d+)\)|a\.pane\([\w.]+, (\d+),`)
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "ui_notch.go" {
			continue
		}
		b, _ := os.ReadFile(f)
		for _, m := range radius.FindAllStringSubmatch(string(b), -1) {
			v := m[1] + m[2]
			if !corners[v] && !shapes[v] {
				t.Errorf("%s: %s", f, m[0])
			}
		}
	}
}

// The colors that follow the cover follow it without Now Playing ever having
// opened, and from one song to the next.
func TestColorsFollowTheCoverPlaying(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	one, two := "https://example.com/one.jpg", "https://example.com/two.jpg"
	for _, url := range []string{one, two} {
		markArt(a, url, 120)
		a.art.add(netease.Sized(url, 600)+"#backdrop", &artwork{bmp: a.art.peek(netease.Sized(url, 120)).bmp, used: time.Now(), size: 1})
	}
	first := netease.Track{ID: 1, Name: "一"}
	first.Album.PicURL = one
	second := netease.Track{ID: 2, Name: "二"}
	second.Album.PicURL = two
	a.pl.Cue([]netease.Track{first, second})
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	if a.npArt == nil {
		t.Fatal("the colors wait for Now Playing to open")
	}
	was := a.npArt
	a.pl.Next()
	tt.Frame()
	if a.npArt == was {
		t.Error("the colors stayed the last song's")
	}
}
