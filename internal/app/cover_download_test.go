package app

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

func coverTestImage() *image.RGBA {
	im := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			im.SetRGBA(x, y, color.RGBA{uint8(50 + x*2), uint8(90 + y), uint8(150 + x), 255})
		}
	}
	return im
}

// Saving uses the original URL and bytes, and the actual image format in
// the dialog, even when the CDN URL has no file extension.
func TestSaveTrackCover(t *testing.T) {
	old := chooseCoverFile
	t.Cleanup(func() { chooseCoverFile = old })
	for _, format := range []string{"png", "jpg"} {
		t.Run(format, func(t *testing.T) {
			var b bytes.Buffer
			if format == "png" {
				png.Encode(&b, coverTestImage())
			} else {
				jpeg.Encode(&b, coverTestImage(), nil)
			}
			requested := make(chan string, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requested <- r.URL.RequestURI()
				w.Write(b.Bytes())
			}))
			defer srv.Close()
			path := filepath.Join(t.TempDir(), "cover."+format)
			chooseCoverFile = func(opts mygo.SaveDialogOptions) (string, error) {
				if filepath.Ext(opts.DefaultPath) != "."+format || opts.Filters[0].Extensions[0] != format {
					t.Errorf("wrong image format: %+v", opts)
				}
				if strings.ContainsAny(filepath.Base(opts.DefaultPath), `<>:"/\|?*`) {
					t.Errorf("unsafe filename: %s", opts.DefaultPath)
				}
				return path, nil
			}
			song := netease.Track{Name: `雨/夜:*?`, Album: netease.Album{PicURL: srv.URL + "/original"}}
			ok, err := saveTrackCover(song, nil)
			if err != nil || !ok {
				t.Fatalf("saved %v: %v", ok, err)
			}
			if got := <-requested; got != "/original" {
				t.Fatalf("requested thumbnail instead: %s", got)
			}
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, b.Bytes()) {
				t.Fatalf("original image changed: %v", err)
			}
		})
	}
}

// Canceling, failed fetches, invalid images and write errors never report
// success; a mismatched extension never overwrites the user's file.
func TestSaveTrackCoverFailures(t *testing.T) {
	old := chooseCoverFile
	t.Cleanup(func() { chooseCoverFile = old })
	dir := t.TempDir()
	source := filepath.Join(dir, "source.png")
	var b bytes.Buffer
	png.Encode(&b, coverTestImage())
	os.WriteFile(source, b.Bytes(), 0o600)
	song := netease.Track{Album: netease.Album{PicURL: source}}
	chooseCoverFile = func(mygo.SaveDialogOptions) (string, error) { return "", nil }
	if ok, err := saveTrackCover(song, nil); ok || err != nil {
		t.Fatalf("cancel: %v, %v", ok, err)
	}
	target := filepath.Join(dir, "existing.jpg")
	os.WriteFile(target, []byte("keep me"), 0o600)
	chooseCoverFile = func(mygo.SaveDialogOptions) (string, error) { return target, nil }
	if ok, err := saveTrackCover(song, nil); ok || err == nil {
		t.Fatalf("mismatched extension: %v, %v", ok, err)
	}
	if got, _ := os.ReadFile(target); string(got) != "keep me" {
		t.Fatal("overwrote the file with another image format")
	}
	blocked := filepath.Join(dir, "folder.png")
	os.Mkdir(blocked, 0o700)
	chooseCoverFile = func(mygo.SaveDialogOptions) (string, error) { return blocked, nil }
	if ok, err := saveTrackCover(song, nil); ok || err == nil {
		t.Fatalf("write failure: %v, %v", ok, err)
	}
	chooseCoverFile = func(mygo.SaveDialogOptions) (string, error) {
		t.Fatal("opened dialog for a missing or invalid image")
		return "", nil
	}
	for _, path := range []string{"", filepath.Join(dir, "missing.png"), target} {
		song.Album.PicURL = path
		if ok, err := saveTrackCover(song, nil); ok || err == nil {
			t.Errorf("invalid source %q: %v, %v", path, ok, err)
		}
	}
}

func coverMenuApp(nowPlaying bool) (*app, *ui.Tester) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.dl.loaded = true
	tr := netease.Track{ID: 7, Name: "雨后的岛屿", Artists: []netease.Artist{{Name: "测试歌手"}}, Album: netease.Album{Name: "海边来信", PicURL: "cover-fixture"}, Duration: 240000}
	for _, key := range []string{"cover-fixture", "cover-fixture#backdrop"} {
		a.art.add(key, &artwork{bmp: ui.NewBitmap(coverTestImage()), tint: ui.RGB(70, 120, 180), used: time.Now(), size: 1})
	}
	a.pl.Cue([]netease.Track{tr})
	a.pl.SetLyrics(nil)
	a.nowPlaying = nowPlaying
	tt := ui.NewTester(a.view, 1180, 780)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Advance(time.Second)
	tt.Frame()
	return a, tt
}

// Both covers and both three-dot buttons save the song the menu opened
// for, even if playback changes before the action is chosen.
func TestCoverDownloadMenus(t *testing.T) {
	old := exportCover
	t.Cleanup(func() { exportCover = old })
	for _, tc := range []struct {
		label      string
		now, right bool
	}{
		{"播放栏封面", false, true}, {"专辑封面", true, true},
		{"歌曲详情", false, false}, {"更多", true, false},
	} {
		t.Run(tc.label, func(t *testing.T) {
			saved := make(chan netease.Track, 1)
			exportCover = func(tr netease.Track, _ *mygo.Window) (bool, error) { saved <- tr; return false, nil }
			a, tt := coverMenuApp(tc.now)
			var err error
			if tc.right {
				err = tt.RightClick(tc.label)
			} else {
				err = tt.Click(tc.label)
			}
			if err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			if a.nowPlaying != tc.now || a.pl.Current().ID != 7 {
				t.Fatal("opening menu changed playback or view")
			}
			a.pl.Cue([]netease.Track{{ID: 8, Name: "另一首"}})
			if err := tt.Click("下载封面"); err != nil {
				t.Fatal(err)
			}
			tt.Frame()
			select {
			case tr := <-saved:
				if tr.ID != 7 || tr.Album.PicURL != "cover-fixture" {
					t.Fatalf("saved wrong cover: %+v", tr)
				}
			case <-time.After(time.Second):
				t.Fatal("cover action did not run")
			}
			if a.menu.open {
				t.Fatal("save left menu open")
			}
		})
	}
}

// Local songs and podcasts expose their covers too; absent artwork does
// not offer a save action for the placeholder.
func TestCoverDownloadSources(t *testing.T) {
	a := newTestApp()
	a.dl.loaded = true
	for _, tr := range []netease.Track{{ID: -1, LocalPath: "song.mp3"}, {ID: 7, ProgramID: 42}, {ID: 8}} {
		for _, has := range []bool{false, true} {
			tr.Album.PicURL = ""
			if has {
				tr.Album.PicURL = "cover.png"
			}
			found := false
			for _, group := range a.songActions(&tr, 0) {
				for _, action := range group {
					if action.label == "下载封面" {
						found = true
					}
				}
			}
			if found != has {
				t.Fatalf("cover availability %v, action %v: %+v", has, found, tr)
			}
		}
	}
}

// With NM_FRAMES set, render the new menus in light and dark surroundings.
func TestCoverDownloadFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip("set NM_FRAMES to save menu frames")
	}
	for _, now := range []bool{false, true} {
		for _, right := range []bool{false, true} {
			_, tt := coverMenuApp(now)
			tt.SetDark(now)
			tt.Frame()
			var err error
			label := "歌曲详情"
			if now {
				label = "更多"
			}
			if right {
				label = "播放栏封面"
				if now {
					label = "专辑封面"
				}
				err = tt.RightClick(label)
			} else {
				err = tt.Click(label)
			}
			if err != nil {
				t.Fatal(err)
			}
			tt.Advance(time.Second)
			tt.Frame()
			f, err := os.Create(filepath.Join(dir, "cover-menu-"+label+".png"))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}
