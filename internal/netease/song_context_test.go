package netease

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type songContextTransport func(*http.Request) (*http.Response, error)

func (f songContextTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Cancelling a superseded song must cancel the playback URL request itself.
func TestSongURLContextCancellation(t *testing.T) {
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	http.DefaultTransport = songContextTransport(func(r *http.Request) (*http.Response, error) {
		return nil, r.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := SongURLContext(ctx, 1, "exhigh"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled request returned %v", err)
	}
}

// An API outage or expired login must not make every song look unavailable.
func TestSongURLServiceErrorIsNotUnavailable(t *testing.T) {
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	for _, body := range []string{`{"code":301}`, `{"code":429}`, `{"code":500}`} {
		http.DefaultTransport = songContextTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})
		if _, err := SongURL(1, "exhigh"); err == nil || errors.Is(err, ErrUnavailable) {
			t.Fatalf("service response %s returned %v", body, err)
		}
	}
}

// Empty or malformed responses do not establish that a song cannot play.
func TestSongURLIncompleteResponseIsRequestFailure(t *testing.T) {
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	for _, body := range []string{`{}`, `{"code":200}`, `{"code":200,"data":[]}`, `{"code":200,"data":[{"url":null}]}`, `<html>busy</html>`} {
		http.DefaultTransport = songContextTransport(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})
		var requestError *StreamError
		if _, err := SongURL(1, "exhigh"); !errors.As(err, &requestError) {
			t.Errorf("incomplete response %s classified as %T: %v", body, err, err)
		}
	}
}

// A usable URL remains usable when the response has no outer status code.
func TestSongURLAcceptsUsableDataWithoutOuterCode(t *testing.T) {
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	http.DefaultTransport = songContextTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[{"code":200,"url":"https://audio.test/song","type":"mp3"}]}`)), Request: r}, nil
	})
	st, err := SongURL(1, "exhigh")
	if err != nil || st.URL != "https://audio.test/song" {
		t.Fatalf("usable response failed: stream=%v error=%v", st, err)
	}
}

// An explicit preview still reports unavailability rather than reconnecting it forever.
func TestSongURLPreviewRemainsUnavailable(t *testing.T) {
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	for _, address := range []string{`null`, `"https://audio.test/preview"`} {
		http.DefaultTransport = songContextTransport(func(r *http.Request) (*http.Response, error) {
			body := `{"code":200,"data":[{"url":` + address + `,"freeTrialInfo":{"start":0,"end":30}}]}`
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})
		if _, err := SongURL(1, "exhigh"); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("preview was classified as %v", err)
		}
	}
}

// When the desktop API answers -460, the web player's stream is used.
func TestSongURLFallsBackToWeb(t *testing.T) {
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	http.DefaultTransport = songContextTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"code":-460}`
		if r.URL.Host == "music.163.com" {
			body = `{"code":200,"data":[{"url":"http://audio.test/web","type":"mp3","size":123,"level":"exhigh","peak":0.5}]}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	st, err := SongURL(1, "exhigh")
	if err != nil {
		t.Fatal(err)
	}
	if st.URL != "https://audio.test/web" || st.Size != 123 || st.Type != "mp3" || st.Peak != 0.5 {
		t.Fatalf("stream %+v", st)
	}
}

// A song's own 404 means no rights to it, and -110 no right to play it:
// both skip, rather than retry.
func TestSongURLItemCodesAreUnavailable(t *testing.T) {
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	for code, want := range map[string]error{"404": ErrNoCopyright, "-110": ErrUnavailable, "-10": ErrUnavailable, "-100": ErrUnavailable} {
		http.DefaultTransport = songContextTransport(func(r *http.Request) (*http.Response, error) {
			body := `{"code":200,"data":[{"id":1,"url":null,"code":` + code + `}]}`
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
		})
		if _, err := SongURL(1, "exhigh"); !errors.Is(err, want) {
			t.Errorf("item code %s returned %v", code, err)
		}
	}
	if !errors.Is(ErrNoCopyright, ErrUnavailable) {
		t.Fatal("no copyright is not unavailable")
	}
}

// IP-literal CDN addresses stay on HTTP so TLS hostname checks do not break
// playback, while domain addresses still upgrade to HTTPS.
func TestSongURLKeepsIPLiteralHTTP(t *testing.T) {
	if got := upgradeStreamURL("http://112.25.60.15/m801.music.126.net/song.mp3"); got != "http://112.25.60.15/m801.music.126.net/song.mp3" {
		t.Fatalf("IP URL was rewritten to %q", got)
	}
	if got := upgradeStreamURL("http://m701.music.126.net/song.mp3"); got != "https://m701.music.126.net/song.mp3" {
		t.Fatalf("domain URL was %q", got)
	}
}

// When a lossless request returns an unsupported format such as ape, SongURL
// falls back to exhigh MP3 so playback still succeeds.
func TestSongURLFallbackFromUnsupportedLosslessFormat(t *testing.T) {
	old := http.DefaultTransport
	defer func() { http.DefaultTransport = old }()
	calls := 0
	http.DefaultTransport = songContextTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		body := `{"code":200,"data":[{"url":"http://audio.test/song.ape","type":"ape","size":1000,"level":"lossless"}]}`
		if calls > 1 {
			body = `{"code":200,"data":[{"url":"http://audio.test/song.mp3","type":"mp3","size":500,"level":"exhigh"}]}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	st, err := SongURL(1, "lossless")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || st.Type != "mp3" || st.Level != "exhigh" {
		t.Fatalf("fallback failed: calls=%d stream=%+v", calls, st)
	}
}

// A WAV of the cloud drive streams as it is: its type was refused as one
// the player cannot play, and the song never played (#54).
func TestCloudWAVStreams(t *testing.T) {
	st, err := streamFromSongURLData(200, songURLData{URL: "http://audio.test/song.wav", Type: "WAV", Size: 1000, Level: "lossless"}, "lossless")
	if err != nil {
		t.Fatal(err)
	}
	if st.Type != "wav" {
		t.Fatalf("type %q", st.Type)
	}
	if _, err := streamFromSongURLData(200, songURLData{URL: "http://audio.test/song.ape", Type: "ape"}, "lossless"); !errors.Is(err, errUnsupportedFormat) {
		t.Fatalf("ape: %v", err)
	}
}

// Cloud-drive uploads in formats the player can decode stream as they are
// instead of being rejected as unsupported (#88).
func TestCloudFormatsStream(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want string
	}{
		{"M4A", "m4a"},
		{"m4b", "m4b"},
		{"AAC", "aac"},
		{"wma", "wma"},
		{"aiff", "aiff"},
		{"dsf", "dsf"},
	} {
		st, err := streamFromSongURLData(200, songURLData{URL: "http://audio.test/song." + tc.want, Type: tc.raw, Size: 1000, Level: "lossless"}, "lossless")
		if err != nil {
			t.Fatalf("%s: %v", tc.raw, err)
		}
		if st.Type != tc.want {
			t.Fatalf("%s: type %q, want %q", tc.raw, st.Type, tc.want)
		}
	}
	for _, bad := range []string{"ape", "ogg"} {
		if _, err := streamFromSongURLData(200, songURLData{URL: "http://audio.test/song." + bad, Type: bad}, "lossless"); !errors.Is(err, errUnsupportedFormat) {
			t.Fatalf("%s: %v", bad, err)
		}
	}
}
