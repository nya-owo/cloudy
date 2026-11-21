package netease

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
)

// ID-only shares use catalog metadata, upgrade its covers, and fetch no playlist tracks or private endpoints.
func TestPrivateMediaDetail(t *testing.T) {
	deviceOnce.Do(func() { deviceID = strings.Repeat("A", 52) })
	oldClient, oldTransport := httpClient, http.DefaultTransport
	defer func() { httpClient, http.DefaultTransport = oldClient, oldTransport }()
	jar, _ := cookiejar.New(nil)
	calls := 0
	transport := browseTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		body := ""
		switch r.URL.Path {
		case "/eapi/v3/song/detail":
			body = `{"code":200,"songs":[{"id":81,"name":"合成歌曲","ar":[{"name":"合成歌手"}],"al":{"picUrl":"http://p1.music.126.net/song.jpg"}}]}`
		case "/api/v1/album/82":
			body = `{"code":200,"album":{"id":82,"name":"合成专辑","artists":[{"name":"合成艺术家"}],"picUrl":"http://p2.music.126.net/album.jpg"},"songs":[]}`
		case "/api/v6/playlist/detail":
			if r.URL.Query().Get("n") != "0" || r.URL.Query().Get("id") != "83" {
				t.Fatal("cover lookup fetched playlist tracks or wrong ID")
			}
			body = `{"code":200,"playlist":{"id":83,"name":"合成歌单","creator":{"nickname":"合成作者"},"picUrl":"http://p3.music.126.net/list.jpg"}}`
		default:
			t.Fatalf("unexpected metadata endpoint: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})
	http.DefaultTransport = transport
	httpClient = &http.Client{Jar: jar, Transport: transport}
	for _, tc := range []struct {
		kind, title, subtitle, cover string
		id                           int64
	}{
		{"song", "合成歌曲", "合成歌手", "https://p1.music.126.net/song.jpg", 81},
		{"album", "合成专辑", "合成艺术家", "https://p2.music.126.net/album.jpg", 82},
		{"playlist", "合成歌单", "合成作者", "https://p3.music.126.net/list.jpg", 83},
	} {
		got, err := PrivateMessageMediaDetail(tc.kind, tc.id)
		if err != nil || got.Kind != tc.kind || got.ID != tc.id || got.Title != tc.title || got.Subtitle != tc.subtitle || got.CoverURL != tc.cover {
			t.Fatalf("incorrect catalog projection: %+v, %v", got, err)
		}
	}
	if _, err := PrivateMessageMediaDetail("song", 0); err == nil {
		t.Fatal("ID-less share started a lookup")
	}
	if _, err := PrivateMessageMediaDetail("unknown", 84); err == nil || calls != 3 {
		t.Fatal("unknown kind started a lookup")
	}
}
