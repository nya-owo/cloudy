package app

import (
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

func privateCardApp(t *testing.T) *app {
	a, _ := privateTestApp(t)
	a.openPrivateMessages(nil)
	a.selectPrivateChat(a.dm.contacts[0].User)
	a.dm.loaded, a.dm.loading = true, false
	return a
}

// Cards stay within their bubble at compact/light/dark sizes and keep an accessible empty-cover fallback.
func TestPrivateMediaCardBounds(t *testing.T) {
	for _, dark := range []bool{false, true} {
		for _, width := range []int{700, 1180} {
			a := privateCardApp(t)
			name := strings.Repeat("合成标题", 8)
			raw, _ := json.Marshal(map[string]any{"album": map[string]any{"name": name, "artists": []any{map[string]any{"name": "合成艺术家"}}}})
			a.dm.chats[90002].messages = []netease.PrivateMessage{{ID: 1, From: netease.MessageUser{UserID: 90001}, Raw: string(raw)}}
			tt := ui.NewTester(a.view, width, 620)
			tt.SetDark(dark)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true})
			tt.Frame()
			card, ok := tt.Find("分享专辑：" + name)
			if !ok || card.W <= 56 || card.X < 0 || card.X+card.W > float32(width) {
				t.Fatal("media card escaped the viewport")
			}
			cover, ok := tt.Find("分享封面")
			if !ok || cover.W != 56 || cover.H != 56 || cover.X+cover.W > card.X+card.W {
				t.Fatal("cover fallback escaped the card")
			}
			if len(a.art.busy) != 0 {
				t.Fatal("empty cover started a network request")
			}
		}
	}
}

// ID-only cards use enriched artwork through the existing image cache without changing message text.
func TestPrivateMediaResolvedCover(t *testing.T) {
	a := privateCardApp(t)
	cover := "https://p1.music.126.net/synthetic-resolved.jpg"
	now := time.Unix(200, 0)
	a.art.now = func() time.Time { return now }
	art := &artwork{bmp: ui.NewBitmap(coverTestImage()), used: time.Unix(100, 0), size: 1}
	a.art.add(netease.Sized(cover, 120), art)
	a.dm.media = &privateMediaCache{items: map[privateMediaKey]*privateMediaEntry{}}
	chat := a.dm.chats[90002]
	for i, kind := range []string{"song", "album", "playlist"} {
		id := int64(71 + i)
		a.dm.media.items[privateMediaKey{kind, id}] = &privateMediaEntry{media: netease.PrivateMessageMedia{
			Kind: kind, ID: id, Title: "合成详情标题", Subtitle: "合成艺术家 / 作者", CoverURL: cover,
		}}
		from := chat.user
		if kind == "album" {
			from.UserID = 90001
		}
		chat.messages = append(chat.messages, netease.PrivateMessage{ID: id, From: from,
			Raw: fmt.Sprintf(`{"%s":{"id":%d},"msg":"保留分享附言"}`, kind, id)})
	}
	tt := ui.NewTester(a.view, 1180, 780)
	tt.SetDark(true)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	for range 3 {
		tt.Frame()
	}
	if art.used != now || len(a.art.busy) != 0 {
		t.Fatal("enriched cover did not reuse the existing artwork cache")
	}
	if dir := os.Getenv("NM_FRAMES"); dir != "" {
		f, err := os.Create(filepath.Join(dir, "media-r5-resolved.png"))
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

// With NM_FRAMES set, exports synthetic cards once, using an in-memory cover and empty-image fallbacks.
func TestPrivateMediaFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	for _, dark := range []bool{false, true} {
		for _, width := range []int{880, 1180} {
			a := privateCardApp(t)
			cover := "https://p1.music.126.net/synthetic-card.png"
			a.art.add(netease.Sized(cover, 120), &artwork{bmp: ui.NewBitmap(coverTestImage()), used: time.Now(), size: 1})
			chat := a.dm.chats[90002]
			chat.messages = []netease.PrivateMessage{
				{ID: 1, From: chat.user, Raw: `{"song":{"name":"合成歌曲","artists":[{"name":"合成歌手"}],"album":{"picUrl":"` + cover + `"}},"msg":"这是一张离线渲染的歌曲卡片 🎵"}`},
				{ID: 2, From: netease.MessageUser{UserID: 90001}, Raw: `{"album":{"name":"合成专辑","artists":[{"name":"合成艺术家"}]}}`},
				{ID: 3, From: chat.user, Raw: `{"playlist":{"name":"合成歌单","creator":{"nickname":"合成作者"}}}`},
			}
			tt := ui.NewTester(a.view, width, 780)
			tt.SetDark(dark)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true})
			tt.Frame()
			if len(a.art.busy) != 0 {
				t.Fatal("fixture cover escaped the memory cache")
			}
			f, err := os.Create(filepath.Join(dir, fmt.Sprintf("cards-%d-dark-%v.png", width, dark)))
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
