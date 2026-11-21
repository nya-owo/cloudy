package netease

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

// Display memoization must survive copies without leaking mutations or stale Raw values into another message.
func TestPrivateContentCache(t *testing.T) {
	original := PrivateMessage{Raw: `{"album":{"name":"合成专辑"},"msg":"合成附言"}`}
	cached := original.WithContentCache()
	if cached.WithContentCache().content != cached.content {
		t.Fatal("unchanged content was parsed again")
	}
	value := cached.Content()
	value.Media.Title = "调用者的临时标题"
	if cached.Content().Media.Title != "合成专辑" || original.Content().Media.Title != "合成专辑" {
		t.Fatal("caller changed cached display data")
	}
	before, _ := json.Marshal(original)
	after, _ := json.Marshal(cached)
	if string(before) != string(after) {
		t.Fatal("display cache changed wire JSON")
	}
	other := cached
	other.Raw = `{"msg":"另一条无ID消息"}`
	if other.Content().Media != nil || other.Content().Text != "另一条无ID消息" ||
		other.WithContentCache().Text() != "另一条无ID消息" || cached.Content().Text != "合成附言" {
		t.Fatal("changed Raw or ID-less message reused another value's display data")
	}
}

// String msg must survive type/title differences, metadata and literal JSON without being executed.
func TestPrivateTextCompatibility(t *testing.T) {
	for _, tc := range []struct{ name, raw, want string }{
		{"short", `{"type":6,"msg":"1"}`, "1"},
		{"symbol", `{"type":6,"title":"","msg":"."}`, "."},
		{"platform-hint", `{"msg":"不支持展示该内容，请前往移动端查看","resource":{"id":123}}`, "不支持展示该内容，请前往移动端查看"},
		{"notice", `{"type":6,"title":"合成标题","msg":"合成通知 👋🎵"}`, "合成通知 👋🎵"},
		{"missing-title", `{"type":6,"msg":"2"}`, "2"},
		{"bad-title", `{"type":6,"title":{},"msg":"文字"}`, "文字"},
		{"string-type", `{"type":"6","msg":"文字"}`, "文字"},
		{"unknown-type", `{"type":99,"msg":"文字"}`, "文字"},
		{"metadata", `{"type":6,"msg":"文字","data":{"nested":[1,2]},"extra":[{}]}`, "文字"},
		{"json-literal", `{"type":6,"msg":"{\"example\":1}"}`, `{"example":1}`},
		{"html-literal", `{"msg":"<script>synthetic()</script>"}`, "<script>synthetic()</script>"},
		{"title-fallback", `{"type":6,"title":"可读标题","msg":""}`, "可读标题"},
		{"title-with-object-msg", `{"title":"可读标题","msg":{}}`, "可读标题"},
		{"duplicate-title", `{"title":"重复","msg":"重复"}`, "重复"},
		{"controls", `{"msg":"前\u0000\u202e后\r\n下一行"}`, "前后\n下一行"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := PrivateMessage{Raw: tc.raw}
			if m.Content().Text != tc.want || m.Text() != tc.want || privateMessageText(tc.raw) != tc.want {
				t.Fatal("readable string was blocked or modified unexpectedly")
			}
		})
	}
	long := strings.Repeat("文", 4100)
	raw, _ := json.Marshal(map[string]any{"msg": long})
	if got := (PrivateMessage{Raw: string(raw)}).Text(); utf8.RuneCountInString(got) > 4001 || !strings.HasSuffix(got, "…") {
		t.Fatal("text bound lost")
	}
	share := (PrivateMessage{Raw: `{"type":6,"title":{},"msg":"附言","album":{"name":"合成专辑"},"metadata":[{}]}`}).Content()
	if share.Media == nil || share.Media.Kind != "album" || share.Text != "附言" {
		t.Fatal("known media priority changed")
	}
	for _, raw := range []string{`{"msg":null,"unknown":{}}`, `{"msg":{"text":"not-string"}}`, `{"type":6,"msg":" \n "}`} {
		if (PrivateMessage{Raw: raw}).Content().Supported() {
			t.Fatal("unreadable envelope was serialized as text")
		}
	}
}

// Structured content extracts only evidenced resource metadata and trusted artwork locations.
func TestPrivateMediaContent(t *testing.T) {
	for _, tc := range []struct {
		raw, kind, title, subtitle, cover string
		id                                int64
	}{
		{`{"song":{"id":12,"name":"合成歌曲","artists":[{"name":"合成歌手"}],"album":{"name":"合成专辑","picUrl":"https://p1.music.126.net/synthetic.png?param=200y200"}},"msg":"合成附言"}`, "song", "合成歌曲", "合成歌手", "https://p1.music.126.net/synthetic.png", 12},
		{`{"song":{"name":"新格式","ar":[{"name":"合成作者"}],"al":{"picUrl":"https://p2.music.126.net/a.png"}}}`, "song", "新格式", "合成作者", "https://p2.music.126.net/a.png", 0},
		{`{"album":{"id":23,"name":"合成专辑","artist":{"name":"合成艺术家"},"picUrl":"https://p3.music.126.net/b.png"}}`, "album", "合成专辑", "合成艺术家", "https://p3.music.126.net/b.png", 23},
		{`{"playlist":{"id":34,"name":"合成歌单","creator":{"nickname":"合成创建者"},"coverImgUrl":"https://p4.music.126.net/c.png"}}`, "playlist", "合成歌单", "合成创建者", "https://p4.music.126.net/c.png", 34},
		// HTTP artwork used to be discarded before Sized could upgrade it.
		{`{"album":{"id":25,"name":"HTTP专辑","picUrl":"http://p1.music.126.net/album.jpg?param=500y500"}}`, "album", "HTTP专辑", "", "https://p1.music.126.net/album.jpg", 25},
		{`{"song":{"id":13,"name":"HTTP歌曲","album":{"picUrl":"http://p2.music.126.net/song.jpg"}}}`, "song", "HTTP歌曲", "", "https://p2.music.126.net/song.jpg", 13},
		{`{"playlist":{"id":35,"name":"备用封面","picUrl":"http://p3.music.126.net/list.jpg"}}`, "playlist", "备用封面", "", "https://p3.music.126.net/list.jpg", 35},
		{`{"song":{"id":14,"name":"嵌套封面","picUrl":"https://example.invalid/x","al":{"picUrl":"http://p4.music.126.net/nested.jpg"}}}`, "song", "嵌套封面", "", "https://p4.music.126.net/nested.jpg", 14},
		{`{"album":{"id":-1,"name":"空图回退","picUrl":"https://example.invalid/body"}}`, "album", "空图回退", "", "", 0},
	} {
		got := (PrivateMessage{Raw: tc.raw}).Content()
		if !got.Supported() || got.Media == nil || got.Media.Kind != tc.kind || got.Media.Title != tc.title || got.Media.Subtitle != tc.subtitle || got.Media.CoverURL != tc.cover || got.Media.ID != tc.id {
			t.Fatal("incorrect bounded media content")
		}
		if got.Summary() != (PrivateMessage{Raw: tc.raw}).Text() {
			t.Fatal("legacy preview drifted from rich content")
		}
	}
	for _, raw := range []string{"http://example.invalid/a.png", "https://p1.music.126.net.evil.invalid/a.png", "https://p1.music.126.net@evil.invalid/a.png", "https://user@p1.music.126.net/a.png", "https://p1.music.126.net:443/a.png", "file:///a.png", "https://p1.music.126.net/a.png#secret", "https://p1.music.126.net/", "https://p1.music.126.net/a\n.png"} {
		if privateMediaCover(raw) != "" {
			t.Fatal("untrusted cover accepted")
		}
	}
}
