package netease

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

// The public red-count request uses a song ID and preserves NetEase's abbreviated count.
func TestSongRedCount(t *testing.T) {
	old := httpClient
	defer func() { httpClient = old }()
	for _, body := range []string{`{"code":200,"data":{"count":6822849,"countDesc":"100w+"}}`, `{"code":503}`, `{"code":200,"data":{"count":-1}}`} {
		httpClient = &http.Client{Transport: browseTransport(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/api/song/red/count" || r.URL.Query().Get("songId") != "186016" {
				t.Fatalf("wrong request: %s", r.URL)
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
		got, err := SongRedCount(186016)
		if strings.Contains(body, "6822849") {
			if err != nil || got.Count != 6822849 || got.Description != "100w+" {
				t.Fatalf("count=%+v err=%v", got, err)
			}
		} else if err == nil {
			t.Fatal("invalid count accepted")
		}
	}
}

// The public endpoint returns a positive count without restoring a personal login.
func TestSongRedCountLive(t *testing.T) {
	if os.Getenv("LIVE") == "" {
		t.Skip("set LIVE to query the public endpoint")
	}
	got, err := SongRedCount(186016)
	if err != nil || got.Count <= 0 {
		t.Fatalf("count=%+v err=%v", got, err)
	}
	t.Logf("count=%d description=%q", got.Count, got.Description)
}
