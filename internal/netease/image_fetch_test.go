package netease

import (
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type countedImageBody struct {
	io.Reader
	bytes  int
	closed bool
}

func (b *countedImageBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.bytes += n
	return n, err
}

func (b *countedImageBody) Close() error {
	b.closed = true
	return nil
}

// Oversized response bodies must be rejected even if the length is unknown.
func TestFetchLimitedHTTP(t *testing.T) {
	old := httpClient
	t.Cleanup(func() { httpClient = old })
	const source = "https://example.test/image.webp"
	for _, test := range []struct {
		name, data    string
		length, limit int64
		wantRead      int
		tooLarge      bool
	}{
		{"declared oversized", "123456789", 9, 4, 0, true},
		{"streamed oversized", "123456789", -1, 4, 5, true},
		{"exact length", "1234", -1, 4, 4, false},
		{"shorter than limit", "123", -1, 4, 3, false},
		{"ordinary Fetch", "123456789", 9, 0, 9, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := &countedImageBody{Reader: strings.NewReader(test.data)}
			httpClient = &http.Client{Transport: browseTransport(func(r *http.Request) (*http.Response, error) {
				if r.Method != "GET" || r.URL.String() != source || r.Header.Get("User-Agent") != UserAgent {
					t.Error("unexpected fetch request")
				}
				return &http.Response{StatusCode: http.StatusOK, ContentLength: test.length, Body: body}, nil
			})}
			var data []byte
			var err error
			if test.limit == 0 {
				data, err = Fetch(source)
			} else {
				data, err = FetchLimited(source, test.limit)
			}
			if test.tooLarge {
				if data != nil || !errors.Is(err, errFetchTooLarge) {
					t.Fatalf("oversized body was not rejected: %v", err)
				}
			} else if err != nil || string(data) != test.data {
				t.Fatalf("valid body did not survive fetch: %q, %v", data, err)
			}
			if body.bytes != test.wantRead || !body.closed {
				t.Fatalf("read %d bytes, want %d, closed=%t", body.bytes, test.wantRead, body.closed)
			}
		})
	}
}

// The byte limit also applies after stripping a requested size from a local path.
func TestFetchLimitedLocal(t *testing.T) {
	const data = "123456789"
	path := filepath.Join(t.TempDir(), "cover.webp")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	url := Sized(path, 114)
	if got, err := FetchLimited(url, 4); got != nil || !errors.Is(err, errFetchTooLarge) {
		t.Fatalf("oversized local file was not rejected: %v", err)
	}
	if got, err := FetchLimited(url, int64(len(data))); string(got) != data || err != nil {
		t.Fatalf("exact-length local file was not read: %v", err)
	}
	if got, err := Fetch(url); string(got) != data || err != nil {
		t.Fatalf("ordinary local fetch changed: %v", err)
	}
}
