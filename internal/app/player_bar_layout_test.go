package app

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

func playerBarLayoutApp() *app {
	a := newTestApp()
	a.settings = defaultSettings()
	a.dl.loaded = true
	a.route = route{page: pageRecent}
	song := netease.Track{ID: 7, Name: "Only Love", Artists: []netease.Artist{{Name: "Ben Howard"}},
		Album: netease.Album{PicURL: "bar-cover"}, Duration: 249000}
	a.pl.Cue([]netease.Track{song})
	a.pl.Seek(84 * time.Second)
	a.pl.SetLyrics(nil)
	a.redCounts = map[int64]*redCountState{7: {known: true, value: netease.RedCount{Count: 230000, Description: "23w+"}}}
	a.comments = commentState{track: 7, loaded: true, total: 9999}
	a.art.add("bar-cover", &artwork{bmp: ui.NewBitmap(coverTestImage()), used: time.Now(), size: 1})
	return a
}

func playerBarTester(a *app, width int) *ui.Tester {
	tt := ui.NewTester(a.view, width, 780)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Advance(time.Second)
	tt.Frame()
	return tt
}

func barElement(t *testing.T, tt *ui.Tester, name string) ui.Rect {
	t.Helper()
	r, ok := tt.Find(name)
	if !ok || r.W <= 0 || r.H <= 0 {
		t.Fatalf("%q has no usable bounds: %+v", name, r)
	}
	return r
}

// The old equal-sided layout squeezed song text to zero width. Covers,
// credits and controls now fit independently of sidebar size or long text.
func TestPlayerBarKeepsReadableCredits(t *testing.T) {
	for _, tc := range []struct {
		width, sidebar int
		zoom           float64
		plain          bool
	}{
		{880, 232, 1, false}, {1000, 232, 1, false}, {1180, 232, 1, false},
		{1440, 232, 1, false}, {880, 380, 1, false}, {1180, 380, 1, false},
		{1180, 232, 1.25, false}, {880, 232, 1, true},
	} {
		t.Run(fmt.Sprintf("%d-side%d-zoom%g-plain%v", tc.width, tc.sidebar, tc.zoom, tc.plain), func(t *testing.T) {
			a := playerBarLayoutApp()
			a.settings.SidebarWidth, a.settings.UIZoom = tc.sidebar, tc.zoom
			a.settings.LiquidGlass = !tc.plain
			// Use a title and credits that cannot fit without truncation.
			tr := *a.pl.Current()
			tr.Name = strings.Repeat("那些你很冒险的梦 ", 5)
			tr.Artists = []netease.Artist{{Name: strings.Repeat("林俊杰 JJ Lin ", 4)}}
			a.pl.Cue([]netease.Track{tr})
			tt := playerBarTester(a, tc.width)
			info := barElement(t, tt, "打开正在播放")
			cover := barElement(t, tt, "播放栏封面")
			title := barElement(t, tt, "播放栏歌名")
			artist := barElement(t, tt, "播放栏歌手")
			heart := barElement(t, tt, "喜欢")
			comments := barElement(t, tt, "评论")
			prev := barElement(t, tt, "上一首")
			play := barElement(t, tt, "播放/暂停")
			next := barElement(t, tt, "下一首")
			progress := barElement(t, tt, "播放进度")
			more := barElement(t, tt, "歌曲详情")
			z := float32(tc.zoom)
			if title.W < 96*z || artist.W < 96*z {
				t.Fatalf("credits squeezed: title=%+v artist=%+v", title, artist)
			}
			if title.X < cover.X+cover.W+10*z || title.X+title.W > info.X+info.W+0.5 ||
				artist.Y < title.Y+title.H || info.X+info.W > heart.X || heart.X+heart.W > comments.X ||
				comments.X+comments.W > prev.X || prev.X+prev.W > play.X || play.X+play.W > next.X ||
				next.X+next.W > more.X || more.X+more.W > float32(tc.width)-12*z || progress.W < 48*z {
				t.Fatalf("overlapping or clipped bar: info=%+v title=%+v heart=%+v comments=%+v prev=%+v play=%+v next=%+v progress=%+v more=%+v", info, title, heart, comments, prev, play, next, progress, more)
			}
			// The credits remain a useful target, not just painted text.
			tt.ClickAt(title.X+title.W/2, title.Y+title.H/2)
			if !a.nowPlaying {
				t.Fatal("clicking the title did not open Now Playing")
			}
		})
	}
}

// Resizing puts counts in tooltips without losing their values, and
// widening restores the visible badges without another network reply.
func TestPlayerBarCompactCounts(t *testing.T) {
	a := playerBarLayoutApp()
	tt := playerBarTester(a, 1180)
	if !tt.HasText("230000 人喜欢") || !tt.HasText("999+") {
		t.Fatalf("wide bar lost counts: %q", tt.Texts())
	}
	tt.SetSize(880, 780)
	tt.Frame()
	if tt.HasText("230000 人喜欢") || tt.HasText("999+") {
		t.Fatal("narrow bar still spends space on badges")
	}
	for _, tip := range []struct{ button, text string }{{"喜欢", "230000 人喜欢"}, {"评论", "9999 条评论"}} {
		r := barElement(t, tt, tip.button)
		tt.Move(r.X+r.W/2, r.Y+r.H/2)
		tt.Advance(time.Second)
		if !tt.HasText(tip.text) {
			t.Fatalf("hovering %s did not reveal its count: %q", tip.button, tt.Texts())
		}
	}
	tt.Move(800, 100)
	tt.SetSize(1180, 780)
	tt.Frame()
	if !tt.HasText("230000 人喜欢") || !tt.HasText("999+") {
		t.Fatal("widening did not restore counts")
	}
}

// The compact menu retains mode selection and playlist actions; its
// visible comment button still opens the current song's comments.
func TestPlayerBarCompactActions(t *testing.T) {
	a := playerBarLayoutApp()
	a.profile = &netease.Profile{UserID: 1}
	a.mine = []netease.Playlist{{ID: 42, Name: "收藏测试"}}
	tt := playerBarTester(a, 880)
	if err := tt.Click("歌曲详情"); err != nil {
		t.Fatal(err)
	}
	if err := chooseItem(tt, "播放模式", "单曲循环"); err != nil {
		t.Fatal(err)
	}
	if a.pl.Mode() != player.ModeOne {
		t.Fatal("compact menu did not set playback mode")
	}
	// Let the menu's close guard expire on the test clock by reopening via
	// a context-independent frame, rather than relying on a wall-clock wait.
	a.menu.closedAt = time.Time{}
	if err := tt.Click("歌曲详情"); err != nil {
		t.Fatal(err)
	}
	if err := chooseItem(tt, "添加到歌单", "新建歌单…"); err != nil {
		t.Fatal(err)
	}
	if !a.naming.open || a.naming.add == nil || a.naming.add.ID != 7 {
		t.Fatal("playlist action lost the current song")
	}
	a.naming.open = false
	tt.Frame()
	if err := tt.Click("评论"); err != nil {
		t.Fatal(err)
	}
	if !a.nowPlaying || a.npPane != paneComments {
		t.Fatal("comment button no longer opens comments")
	}
}

// With NM_FRAMES set, save the real wide and compact bars, including a
// both appearances, for checking spacing and truncation.
func TestPlayerBarResponsiveFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip("set NM_FRAMES to save bar previews")
	}
	for _, width := range []int{880, 1180} {
		for _, dark := range []bool{false, true} {
			a := playerBarLayoutApp()
			tt := playerBarTester(a, width)
			tt.SetDark(dark)
			tt.Frame()
			img := tt.Image()
			scale := float32(img.Bounds().Dx()) / float32(width)
			crop := img.SubImage(image.Rect(int(a.sideWidth(float32(width))*scale), int(670*scale), img.Bounds().Dx(), img.Bounds().Dy()))
			path := filepath.Join(dir, fmt.Sprintf("player-bar-%d-dark%v.png", width, dark))
			f, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, crop)
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}
