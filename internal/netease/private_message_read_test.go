package netease

import (
	"context"
	"crypto/aes"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// Read uses the existing EAPI session envelope for exactly one peer and rejects invalid peers or non-200 business responses.
func TestPrivateReadEAPI(t *testing.T) {
	c := NewPrivateMessageClient(90001)
	c.device = func() string { return strings.Repeat("A", 52) }
	calls := 0
	c.client.Transport = browseTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "POST" || r.URL.String() != eapiDomain+"/eapi/communication/msg/unread/count/clean" || r.Header.Get("User-Agent") != eapiUA {
			t.Fatal("wrong fixed read endpoint or client")
		}
		if r.ParseForm() != nil {
			t.Fatal("invalid read form")
		}
		data, err := hex.DecodeString(r.Form.Get("params"))
		if err != nil || len(data) == 0 || len(data)%aes.BlockSize != 0 || r.Form.Get("encSecKey") != "" {
			t.Fatal("read did not use EAPI")
		}
		block, _ := aes.NewCipher([]byte(eapiKey))
		for i := 0; i < len(data); i += aes.BlockSize {
			block.Decrypt(data[i:i+aes.BlockSize], data[i:i+aes.BlockSize])
		}
		parts := strings.Split(string(data), "-36cd479b6b5-")
		var body map[string]any
		if len(parts) != 3 || parts[0] != "/api/communication/msg/unread/count/clean" || json.Unmarshal([]byte(parts[1]), &body) != nil || body["userId"] != float64(90002) || len(body) != 3 || body["header"] == nil || body["e_r"] != false {
			t.Fatal("read payload did not target exactly one peer in the current envelope")
		}
		response := `{"code":200}`
		if calls == 2 {
			response = `{"code":200,"data":false}`
		} else if calls == 3 {
			response = `{"code":200,"data":{"success":false}}`
		} else if calls == 4 {
			response = `{"code":500,"message":"do-not-echo-private-body"}`
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(response))}, nil
	})
	ctx := context.Background()
	if c.Read(ctx, 90002) != nil || calls != 1 {
		t.Fatal("read success was rejected")
	}
	for range 3 {
		if err := c.Read(ctx, 90002); err == nil || strings.Contains(err.Error(), "do-not-echo") {
			t.Fatal("business rejection/failure was accepted or leaked a remote body")
		}
	}
	if c.Read(ctx, 0) == nil || c.Read(ctx, 90001) == nil || calls != 4 {
		t.Fatal("invalid/self peer reached the network")
	}
}
