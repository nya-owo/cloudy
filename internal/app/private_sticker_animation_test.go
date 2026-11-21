package app

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

// The sticker decoder retains timed GIF and WebP frames, while static artwork stays on its first frame.
func TestPrivateStickerDecoding(t *testing.T) {
	webp, err := os.ReadFile("testdata/sticker-motion.webp")
	if err != nil {
		t.Fatal(err)
	}
	bounds := image.Rect(0, 0, 16, 16)
	red, blue := color.RGBA{R: 255, A: 255}, color.RGBA{B: 255, A: 255}
	first := image.NewPaletted(bounds, color.Palette{red})
	second := image.NewPaletted(bounds, color.Palette{blue})
	var animatedGIF, staticPNG bytes.Buffer
	if err := gif.EncodeAll(&animatedGIF, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{8, 15}}); err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(&staticPNG, first); err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{webp, animatedGIF.Bytes()} {
		art, err := decodeStickerArt(data)
		if err != nil || art.sticker == nil || len(art.sticker.frames) != 2 {
			t.Fatalf("animated image did not decode to two frames: %v", err)
		}
		_, cover, err := decodeArt(data)
		if err != nil || color.RGBAModel.Convert(cover.At(8, 8)) != red {
			t.Fatal("ordinary artwork did not retain first-frame decoding")
		}
		a := art.sticker
		tt := ui.NewTester(func(c *ui.Context) { ui.Box(c).Size(16, 16).Draw(a.paint) }, 20, 20)
		check := func(want color.RGBA) {
			t.Helper()
			if got := tt.Image().RGBAAt(8, 8); got != want {
				t.Fatalf("frame pixel = %v, want %v", got, want)
			}
		}
		check(red)
		tt.Advance(70 * time.Millisecond)
		check(red)
		tt.Advance(25 * time.Millisecond)
		check(blue)
		tt.Advance(120 * time.Millisecond)
		check(blue)
		tt.Advance(30 * time.Millisecond)
		check(red)
	}
	static, err := decodeStickerArt(staticPNG.Bytes())
	if err != nil || static.bmp == nil || static.sticker != nil {
		t.Fatal("static PNG entered animation path")
	}
}

// Grid thumbnails replace the placeholder immediately while history keeps its existing fade.
func TestPrivateStickerArtworkOnLoad(t *testing.T) {
	path, err := filepath.Abs("testdata/sticker-motion.webp")
	if err != nil {
		t.Fatal(err)
	}
	for _, animate := range []bool{false, true} {
		name := "thumbnail"
		if animate {
			name = "animation"
		}
		t.Run(name, func(t *testing.T) {
			a := newTestApp()
			a.motion = motionFull
			posted := make(chan func(), 1)
			a.art = newArtCache(func(fn func()) { posted <- fn })
			a.pal.hover = ui.RGB(200, 200, 200)
			sticker := netease.PrivateSticker{ID: "42", Name: "样本", URL: path}
			show := true
			tt := ui.NewTester(func(c *ui.Context) {
				if show {
					a.privateStickerImage(c, sticker, 42, animate)
				}
			}, 80, 80)
			tt.Frame()
			before, hadPlaceholder := tt.Find("表情：样本")
			select {
			case fn := <-posted:
				fn()
			case <-time.After(2 * time.Second):
				t.Fatal("artwork did not finish loading")
			}
			art, _ := a.privateStickerArtwork(sticker, animate)
			if bitmap(art) == nil {
				t.Fatal("artwork did not decode")
			}
			tt.Frame()
			if tt.FramePending() != animate {
				t.Fatalf("unexpected fade after load, history = %t", animate)
			}
			room, ok := tt.Find("表情：样本")
			if !hadPlaceholder || !ok || before != room || room.W != 42 || room.H != 42 {
				t.Fatalf("artwork changed bounds from %v to %v", before, room)
			}
			if animate {
				tt.Advance(artFade.Duration / 2)
				if art.sticker == nil || art.sticker.started.IsZero() {
					t.Fatal("the animated sticker did not draw during the fade")
				}
			} else if got := tt.Image().RGBAAt(int(room.X+room.W/2), int(room.Y+room.H/2)); got != (color.RGBA{R: 255, A: 255}) {
				t.Fatalf("the first ready frame did not paint the thumbnail: %v", got)
			}
			tt.Advance(artFade.Duration)
			tt.Frame()
			if tt.FramePending() {
				t.Fatal("settled artwork restarted its fade")
			}
			show = false
			tt.Frame()
			tt.Advance(time.Second)
			show = true
			tt.Frame()
			got, ok := tt.Find("表情：样本")
			if !ok || got != room {
				t.Fatal("cached artwork did not return at the same bounds")
			}
			if tt.FramePending() {
				t.Fatal("cached artwork asked for another fade")
			}
		})
	}
}

// The same WebP plays in history while its picker tile keeps the first frame.
func TestPrivateStickerPlayback(t *testing.T) {
	data, err := os.ReadFile("testdata/sticker-motion.webp")
	if err != nil {
		t.Fatal(err)
	}
	art, err := decodeStickerArt(data)
	if err != nil || art.sticker == nil {
		t.Fatalf("could not decode fixture: %v", err)
	}
	a, _ := privateTestApp(t)
	user := a.dm.contacts[0].User
	a.openPrivateMessages(&user)
	sticker := netease.PrivateSticker{ID: "42", Group: "10", Name: "动态样本", URL: "https://p1.music.126.net/fixture.webp"}
	art.used = time.Now()
	a.art.add(sticker.URL+"#sticker", art)
	thumbnail, _, err := decodeArt(data)
	if err != nil {
		t.Fatal(err)
	}
	a.art.add(sticker.URL, &artwork{bmp: thumbnail, used: art.used})
	a.dm.stickers.groups = []netease.PrivateStickerGroup{{ID: sticker.Group, Name: "测试表情"}}
	a.dm.stickers.group = sticker.Group.String()
	a.dm.stickers.page.Items = []netease.PrivateSticker{sticker}
	raw, _ := json.Marshal(map[string]any{"msgType": 1, "body": sticker})
	chat := a.dm.chats[user.UserID]
	chat.messages = []netease.PrivateMessage{{ID: 42, From: netease.MessageUser{UserID: a.profile.UserID}, Raw: string(raw)}}
	tt := ui.NewTester(a.view, 1180, 780)
	tt.Frame()
	privateShowContents(tt)
	tt.Advance(time.Second)
	history, ok := tt.Find("表情：动态样本")
	if !ok || history.W != 112 || art.sticker.started.IsZero() {
		t.Fatal("history animation is missing")
	}
	if err := tt.Click("网易云表情"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(motionPane.Duration)
	tt.Frame()
	tile, ok := tt.Find("发送表情：动态样本")
	if !ok || tile.W != 52 {
		t.Fatal("picker tile is missing")
	}
	cycle := 230 * time.Millisecond
	for _, phase := range []time.Duration{40 * time.Millisecond, 120 * time.Millisecond} {
		elapsed := tt.Now().Sub(art.sticker.started)
		tt.Advance((phase - elapsed%cycle + cycle) % cycle)
		img := tt.Image()
		first := color.RGBA{R: 255, A: 255}
		if got := img.RGBAAt(int(tile.X+tile.W/2), int(tile.Y+tile.H/2)); got != first {
			t.Fatalf("picker pixel = %v, want the first frame %v", got, first)
		}
		want := first
		if phase >= 80*time.Millisecond {
			want = color.RGBA{B: 255, A: 255}
		}
		if got := img.RGBAAt(int(history.X+history.W/2), int(history.Y+history.H/2)); got != want {
			t.Fatalf("history pixel = %v, want %v", got, want)
		}
	}
}
