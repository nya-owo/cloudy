package app

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/netease"
)

// With NM_FRAMES set, saves the downloads page in both appearances and at
// two widths, for looking at it.
func TestDownloadFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip("set NM_FRAMES to a directory to save frames")
	}
	cover := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			cover.SetRGBA(x, y, color.RGBA{
				R: uint8(70 + 80*x/64),
				G: uint8(125 + 55*y/64),
				B: uint8(145 + 80*x/64),
				A: 255,
			})
		}
	}
	coverPath := filepath.Join(t.TempDir(), "cover.png")
	f, err := os.Create(coverPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, cover); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	for _, dark := range []bool{false, true} {
		appearance := "light"
		if dark {
			appearance = "dark"
		}
		for _, width := range []int{1180, 880} {
			a := newTestApp()
			a.settings = defaultSettings()
			a.settings.DownloadFolder = filepath.Join("C:", "Users", "26065", "Music", "Cloudy Downloads")
			a.downloadsLoaded = true
			a.art.add(coverPath, &artwork{bmp: ui.NewBitmap(cover), tint: ui.RGB(80, 140, 160), used: time.Now(), size: 1})
			a.art.add(coverPath+"#backdrop", &artwork{bmp: ui.NewBitmap(cover), backdrop: ui.NewBitmap(cover), tint: ui.RGB(80, 140, 160), used: time.Now(), size: 1})
			a.downloads = []download.Item{
				{Track: netease.Track{ID: 1, Name: "晴天", Artists: []netease.Artist{{Name: "测试歌手"}}, Album: netease.Album{Name: "叶惠美", PicURL: coverPath}, Duration: 269000}, Path: filepath.Join("C:", "Music", "晴天.mp3"), Size: 8_400_000, At: time.Now()},
				{Track: netease.Track{ID: 2, Name: "夜曲", Artists: []netease.Artist{{Name: "测试歌手"}}, Album: netease.Album{Name: "十一月的萧邦"}, Duration: 226000}, Path: filepath.Join("C:", "Music", "夜曲.flac"), Size: 24_600_000, At: time.Now().Add(-time.Hour)},
				{Track: netease.Track{ID: 3, Name: "江南", Artists: []netease.Artist{{Name: "另一个歌手"}}, Duration: 258000}, Path: filepath.Join("C:", "Music", "江南.mp3"), Size: 7_900_000, At: time.Now().Add(-2 * time.Hour)},
			}
			a.downloadQ.running = &netease.Track{ID: 4, Name: "起风了", Artists: []netease.Artist{{Name: "第三位歌手"}}, Album: netease.Album{PicURL: coverPath}}
			a.downloadQ.done, a.downloadQ.size = 3_100_000, 8_900_000
			a.downloadQ.waiting = []netease.Track{{ID: 5, Name: "演员"}, {ID: 6, Name: "好久不见"}}
			a.open(route{page: pageDownloads})
			tt := ui.NewTester(a.view, width, 780)
			tt.SetDark(dark)
			tt.Advance(time.Second)
			tt.Frame()
			f, err := os.Create(filepath.Join(dir, "downloads-"+appearance+"-"+strconv.Itoa(width)+".png"))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(f, tt.Image()); err != nil {
				f.Close()
				t.Fatal(err)
			}
			f.Close()
		}
	}
	// And the page before anything is downloaded.
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.DownloadFolder = filepath.Join("C:", "Users", "26065", "Music", "Cloudy Downloads")
	a.downloadsLoaded = true
	a.open(route{page: pageDownloads})
	tt := ui.NewTester(a.view, 1180, 780)
	tt.Frame()
	f, err = os.Create(filepath.Join(dir, "downloads-empty-1180.png"))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, tt.Image()); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
}
