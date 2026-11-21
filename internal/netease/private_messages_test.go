package netease

import (
	"context"
	"crypto/aes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

// WEAPI wrapping matches an independent Node crypto/OpenSSL fixture, including reversed-key RSA.
func TestPrivateWEAPIVector(t *testing.T) {
	form, err := privateWEAPIKey([]byte(`{}`), []byte("abcdefghijklmnop"))
	if err != nil {
		t.Fatal(err)
	}
	if form.Get("params") != "qFnMWVHk2FofqgdXwao9gVJSweI5iRQHowAHHOvBiiA=" || form.Get("encSecKey") != "d15a1683c992095d0c234c19966605c5c5964911268bbeda8cb8d08d834913e59d53b32358903a121b5fca784c1f5ae44951fd02524df58ecc98e52cc7cf8689b42c2e93ddf05b0592512d87f5960467e2f086c018849d76014d323500e30f13ef4cafbb0cf5a66731a3f1776c75ca35d0062dac70a3e33245afabcf47938487" {
		t.Fatal("WEAPI interoperability vector changed")
	}
}

// Private endpoints use encrypted POSTs, decode text/pagination, and never echo server errors.
func TestPrivateMessagesTransport(t *testing.T) {
	c := NewPrivateMessageClient(90001)
	c.device = func() string { return strings.Repeat("A", 52) }
	responses := []struct{ path, body string }{
		{"/weapi/msg/private/users", `{"code":200,"more":true,"msgs":[{"fromUser":{"userId":90001},"toUser":{"userId":90002,"nickname":"虚构用户"},"lastMsg":"{\"type\":1,\"msg\":\"离线演示\"}","lastMsgTime":200}]}`},
		{"/weapi/msg/private/history", `{"code":200,"more":true,"msgs":[{"id":2,"time":200,"msg":"{\"type\":1,\"msg\":\"演示文本\"}"},{"id":1,"time":100,"msg":"{\"type\":2,\"msg\":\"未知类型\"}"}]}`},
		{"/eapi/msg/private/send", `{"code":200}`},
		{"/eapi/msg/private/send", `{"code":500,"message":"do-not-echo-this-body"}`},
		{"/weapi/msg/private/history", `{"code":301}`},
		{"/weapi/msg/private/history", `{"code":200}`},
		{"/weapi/msg/private/history", `{"code":200,"msgs":[]}`},
		{"/weapi/msg/private/history", `{"code":200,"more":true,"msgs":[]}`},
		{"/weapi/msg/private/history", `{"code":200,"more":false,"msgs":[]}`},
		{"/weapi/msg/private/history", `{"code":200,"more":false,"msgs":[{"id":3},{"id":2,"time":200}]}`},
	}
	index := 0
	c.client.Transport = browseTransport(func(r *http.Request) (*http.Response, error) {
		want := responses[index]
		index++
		if r.Method != "POST" || r.URL.Path != want.path || r.URL.Scheme != "https" {
			t.Fatalf("incorrect endpoint %s %s", r.Method, r.URL)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("params") == "" || r.URL.RawQuery != "" {
			t.Fatal("missing encryption or private data in URL")
		}
		if strings.HasPrefix(want.path, "/weapi/") && len(r.Form.Get("encSecKey")) != 256 {
			t.Fatal("missing WEAPI RSA key")
		}
		if strings.HasPrefix(want.path, "/eapi/") {
			if r.URL.Host != "interfacepc.music.163.com" {
				t.Fatal("incorrect EAPI host")
			}
			data, err := hex.DecodeString(r.Form.Get("params"))
			if err != nil {
				t.Fatal(err)
			}
			block, _ := aes.NewCipher([]byte(eapiKey))
			for i := 0; i < len(data); i += aes.BlockSize {
				block.Decrypt(data[i:i+aes.BlockSize], data[i:i+aes.BlockSize])
			}
			parts := strings.Split(string(data), "-36cd479b6b5-")
			if len(parts) != 3 || parts[0] != "/api/msg/private/send" {
				t.Fatal("incorrect EAPI path binding")
			}
			var body map[string]any
			if json.Unmarshal([]byte(parts[1]), &body) != nil || body["userIds"] != "[90002]" || body["msg"] != "虚构草稿" || body["type"] != "text" {
				t.Fatal("wrong EAPI send payload")
			}
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(want.body)), Header: make(http.Header)}, nil
	})
	ctx := context.Background()
	contacts, err := c.Conversations(ctx, 30)
	if err != nil || len(contacts.Items) != 1 || contacts.Items[0].User.UserID != 90002 || contacts.Items[0].Summary != "离线演示" || contacts.Next != 31 || !contacts.More {
		t.Fatalf("bad contacts: %+v %v", contacts, err)
	}
	history, err := c.History(ctx, 90002, 0)
	if err != nil || len(history.Items) != 2 || history.Before != 100 || !history.More || !history.Authoritative || history.Items[0].Text() != "演示文本" || history.Items[1].Text() != "未知类型" {
		t.Fatalf("bad history: %+v %v", history, err)
	}
	if err = c.Send(ctx, 90002, "虚构草稿"); err != nil {
		t.Fatal(err)
	}
	if err = c.Send(ctx, 90002, "虚构草稿"); err == nil || strings.Contains(err.Error(), "do-not-echo") {
		t.Fatal("server failure was accepted or leaked content")
	}
	if _, err = c.History(ctx, 90002, 0); err == nil {
		t.Fatal("expired login accepted")
	}
	if _, err = c.History(ctx, 90002, 0); err == nil {
		t.Fatal("unknown response schema treated as empty history")
	}
	for i, authoritative := range []bool{false, false, true, false} {
		page, err := c.History(ctx, 90002, 0)
		if err != nil || page.Items == nil || page.Authoritative != authoritative {
			t.Fatalf("unexpected coverage for response %d: %+v %v", i, page, err)
		}
	}
	if err = c.Send(ctx, 90002, " "); err == nil || index != len(responses) {
		t.Fatal("empty send reached transport")
	}
	for _, raw := range []string{`not-json`, `{"type":999,"msg":{}}`, `{"msg":{"unexpected":"object"}}`} {
		if (PrivateMessage{Raw: raw}).Text() != "暂不支持的消息类型" {
			t.Fatal("unknown payload rendered as text")
		}
	}
}

// Older History pages distinguish an explicit server end with cursor progress from empty,
// non-advancing or rate-limited responses without leaking remote error bodies.
func TestPrivateHistoryExhaustedAndRateLimit(t *testing.T) {
	c := NewPrivateMessageClient(90001)
	responses := []struct {
		status int
		body   string
	}{
		{200, `{"code":200,"more":true,"msgs":[]}`},
		{200, `{"code":200,"more":false,"msgs":[]}`},
		{200, `{"code":200,"msgs":[]}`},
		{200, `{"code":200,"more":false,"msgs":[{"id":3,"time":160}]}`},
		{200, `{"code":200,"more":false,"msgs":[{"id":2,"time":120}]}`},
		{http.StatusTooManyRequests, `secret-private-body-429`},
	}
	index := 0
	c.client.Transport = browseTransport(func(r *http.Request) (*http.Response, error) {
		resp := responses[index]
		index++
		return &http.Response{StatusCode: resp.status, Body: io.NopCloser(strings.NewReader(resp.body)), Header: make(http.Header)}, nil
	})
	ctx := context.Background()
	for i, wantExhausted := range []bool{false, false, false, false, true} {
		page, err := c.History(ctx, 90002, 160)
		if err != nil || page.More || page.Exhausted != wantExhausted {
			t.Fatalf("case %d: got more=%t exhausted=%t err=%v, want exhausted=%t", i, page.More, page.Exhausted, err, wantExhausted)
		}
	}
	_, err := c.Conversations(ctx, 0)
	var httpErr *PrivateMessageHTTPError
	if !errors.As(err, &httpErr) || httpErr.StatusCode != http.StatusTooManyRequests || strings.Contains(err.Error(), "secret-") {
		t.Fatalf("HTTP 429 misclassified or leaked body: %v", err)
	}
}

// Preview compatibility shares the same decoder while keeping malformed envelopes bounded.
func TestPrivateMessagePreviews(t *testing.T) {
	const unsupported = "暂不支持的消息类型"
	for _, tc := range []struct{ raw, want string }{
		{`{"msg":"文字","type":null}`, "文字"},
		{`{"song":"bad-resource","msg":"可读附言"}`, "可读附言"},
		{`{"song":null,"msg":"可读附言"}`, "可读附言"},
		{`{"song":{},"msg":{}}`, "[分享歌曲]"},
		{`{"song":{"name":"优先歌曲"},"album":{"name":"另一资源"}}`, "[分享歌曲] 优先歌曲"},
		{`not-json`, unsupported}, {`"not-an-envelope"`, unsupported}, {`[{"msg":"not-an-envelope"}]`, unsupported},
		{`{"msg":42}`, unsupported}, {`{"msg":"broken`, unsupported},
		{`{"msg":"` + strings.Repeat("x", 64<<10) + `"}`, unsupported},
	} {
		if (PrivateMessage{Raw: tc.raw}).Text() != tc.want {
			t.Fatal("preview or envelope boundary changed")
		}
	}
}

// lastMsg must use the same conservative decoder as history, including typeless text and shares.
func TestPrivateConversationPreviews(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{{`{"msg":"虚构摘要"}`, "虚构摘要"}, {`{"song":{"name":"虚构歌曲"}}`, "[分享歌曲] 虚构歌曲"}, {`{"type":2,"msg":"hidden"}`, "hidden"}} {
		c := NewPrivateMessageClient(90001)
		c.client.Transport = browseTransport(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/weapi/msg/private/users" {
				t.Fatal("unexpected history request")
			}
			body, _ := json.Marshal(map[string]any{"code": 200, "msgs": []any{map[string]any{"fromUser": MessageUser{UserID: 90002}, "lastMsg": tc.raw}}})
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
		})
		page, err := c.Conversations(context.Background(), 0)
		if err != nil || len(page.Items) != 1 || page.Items[0].Summary != tc.want {
			t.Fatal("conversation preview not decoded safely")
		}
	}
}

// The optional server count distinguishes a reliable zero from unusable values without rejecting the conversation.
func TestPrivateConversationUnreadCount(t *testing.T) {
	fields := []string{"0", "1", "5", "", "null", "-1", "1.5", "true", `"5"`, "{}", "9223372036854775808"}
	items := make([]json.RawMessage, 0, len(fields))
	for _, field := range fields {
		item := `{"fromUser":{"userId":90002},"lastMsgTime":100`
		if field != "" {
			item += `,"newMsgCount":` + field
		}
		items = append(items, json.RawMessage(item+"}"))
	}
	body, err := json.Marshal(map[string]any{"code": 200, "msgs": items})
	if err != nil {
		t.Fatal(err)
	}
	c := NewPrivateMessageClient(90001)
	c.client.Transport = browseTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/weapi/msg/private/users" {
			t.Fatal("unread count used a different endpoint")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body))), Header: make(http.Header)}, nil
	})
	page, err := c.Conversations(context.Background(), 0)
	if err != nil || len(page.Items) != len(fields) {
		t.Fatalf("unusable count broke the conversation page: %v", err)
	}
	values := []int{0, 1, 5}
	for i, item := range page.Items {
		if known := i < len(values); item.UnreadKnown != known || known && item.Unread != values[i] {
			t.Errorf("field %q became (count %d, known %t)", fields[i], item.Unread, item.UnreadKnown)
		}
	}
}

// DM send previously omitted the EAPI device/client envelope and mixed in WEAPI fields.
func TestPrivateMessagesEAPISnapshotAnd250(t *testing.T) {
	old := httpClient
	defer func() { httpClient = old }()
	jar, _ := cookiejar.New(nil)
	jar.SetCookies(siteURL, []*http.Cookie{
		{Name: "MUSIC_U", Value: "synthetic-A"}, {Name: "__csrf", Value: "synthetic-csrf-A"},
		{Name: "NMTID", Value: "synthetic-nmtid-A"}, {Name: "unrelated", Value: "ignored"},
	})
	httpClient = &http.Client{Jar: jar}
	c := NewPrivateMessageClient(90001)
	c.device = func() string { return strings.Repeat("A", 52) }
	jar.SetCookies(siteURL, []*http.Cookie{{Name: "MUSIC_U", Value: "synthetic-B"}, {Name: "__csrf", Value: "synthetic-csrf-B"}})
	if c.client.Timeout != 20*time.Second {
		t.Fatal("private request timeout changed")
	}
	calls := 0
	c.client.Transport = browseTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Context().Err() != nil {
			return nil, r.Context().Err()
		}
		if r.Method != "POST" || r.URL.String() != eapiDomain+"/eapi/msg/private/send" || r.Header.Get("User-Agent") != eapiUA || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			t.Fatal("send changed endpoint or HTTP protocol")
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal("cannot parse encrypted send form")
		}
		if len(r.Form) != 1 || r.Form.Get("encSecKey") != "" {
			t.Fatal("WEAPI fields leaked into EAPI form")
		}
		data, err := hex.DecodeString(r.Form.Get("params"))
		if err != nil || len(data) == 0 || len(data)%aes.BlockSize != 0 {
			t.Fatal("bad EAPI envelope")
		}
		block, _ := aes.NewCipher([]byte(eapiKey))
		for i := 0; i < len(data); i += aes.BlockSize {
			block.Decrypt(data[i:i+aes.BlockSize], data[i:i+aes.BlockSize])
		}
		parts := strings.Split(string(data), "-36cd479b6b5-")
		if len(parts) != 3 || parts[0] != "/api/msg/private/send" {
			t.Fatal("wrong encrypted URI")
		}
		var body struct {
			Header map[string]string `json:"header"`
			Type   string            `json:"type"`
			Msg    string            `json:"msg"`
			Users  string            `json:"userIds"`
			ER     bool              `json:"e_r"`
			CSRF   *string           `json:"csrf_token"`
		}
		if json.Unmarshal([]byte(parts[1]), &body) != nil || body.Type != "text" || body.Msg != "synthetic draft" || body.Users != "[90002]" || body.ER || body.CSRF != nil {
			t.Fatal("unexpected EAPI body fields")
		}
		header := body.Header
		want := map[string]string{"osver": eapiClient["osver"], "os": eapiClient["os"], "appver": eapiClient["appver"], "channel": eapiClient["channel"], "deviceId": strings.Repeat("A", 52), "versioncode": "140", "mobilename": "", "resolution": "1920x1080", "MUSIC_U": "synthetic-A", "__csrf": "synthetic-csrf-A", "NMTID": "synthetic-nmtid-A"}
		if len(header) != len(want)+2 {
			t.Fatal("unexpected EAPI header fields")
		}
		for key, value := range want {
			got, ok := header[key]
			if !ok || got != value {
				t.Fatalf("EAPI header field mismatch: %s", key)
			}
		}
		if _, err := strconv.ParseInt(header["buildver"], 10, 64); err != nil || !strings.Contains(header["requestId"], "_") {
			t.Fatal("missing EAPI request metadata")
		}
		cookies := map[string]string{}
		for _, part := range strings.Split(r.Header.Get("Cookie"), "; ") {
			key, value, ok := strings.Cut(part, "=")
			if !ok {
				t.Fatal("bad EAPI cookie encoding")
			}
			key, err = url.QueryUnescape(key)
			if err != nil {
				t.Fatal("bad cookie key encoding")
			}
			value, err = url.QueryUnescape(value)
			if err != nil {
				t.Fatal("bad cookie value encoding")
			}
			cookies[key] = value
		}
		if len(cookies) != len(header) {
			t.Fatal("HTTP/encrypted EAPI headers diverged")
		}
		for key, value := range header {
			if got, ok := cookies[key]; !ok || got != value {
				t.Fatalf("HTTP/encrypted EAPI field mismatch: %s", key)
			}
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":250,"message":"synthetic-private-echo"}`)), Header: make(http.Header)}, nil
	})
	err := c.Send(context.Background(), 90002, "synthetic draft")
	var status *PrivateMessageStatusError
	if !errors.As(err, &status) || status.Code != 250 || !strings.Contains(err.Error(), "服务器拒绝") || strings.Contains(err.Error(), "synthetic-") || strings.Contains(err.Error(), "网络") {
		t.Fatal("250 misclassified or leaked remote content")
	}
	if calls != 1 {
		t.Fatal("send retried automatically")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if c.Send(ctx, 90002, "synthetic draft") == nil {
		t.Fatal("cancelled request succeeded")
	}
}

// A request keeps its original synthetic session after the application's cookie jar changes.
func TestPrivateMessagesSessionSnapshot(t *testing.T) {
	old := httpClient
	defer func() { httpClient = old }()
	jar, _ := cookiejar.New(nil)
	jar.SetCookies(siteURL, []*http.Cookie{{Name: "MUSIC_U", Value: "synthetic-account-A"}, {Name: "__csrf", Value: "synthetic-csrf"}})
	httpClient = &http.Client{Jar: jar}
	c := NewPrivateMessageClient(90001)
	jar.SetCookies(siteURL, []*http.Cookie{{Name: "MUSIC_U", Value: "synthetic-account-B"}})
	c.client.Transport = browseTransport(func(r *http.Request) (*http.Response, error) {
		cookie, err := r.Cookie("MUSIC_U")
		if err != nil || cookie.Value != "synthetic-account-A" {
			t.Fatal("request adopted a different account")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"code":200,"msgs":[]}`)), Header: make(http.Header)}, nil
	})
	if _, err := c.Conversations(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	if err := c.client.CheckRedirect(&http.Request{URL: &url.URL{Scheme: "https", Host: "example.invalid"}}, nil); err != http.ErrUseLastResponse {
		t.Fatal("private transport follows redirects")
	}
}

// Simulated Set-Cookie responses must not duplicate or leak cookies into EAPI
// or third-party check-token requests, while WEAPI continues using the snapshotted Jar.
func TestPrivateMessagesCookieJarIsolation(t *testing.T) {
	old := httpClient
	defer func() { httpClient = old }()
	jar, _ := cookiejar.New(nil)
	jar.SetCookies(siteURL, []*http.Cookie{
		{Name: "MUSIC_U", Value: "synthetic-A"},
		{Name: "__csrf", Value: "synthetic-csrf-A"},
		{Name: "NMTID", Value: "synthetic-nmtid-A"},
	})
	httpClient = &http.Client{Jar: jar}
	c := NewPrivateMessageClient(90001)
	c.device = func() string { return strings.Repeat("A", 52) }
	// Mutating the global jar later must not affect this client.
	jar.SetCookies(siteURL, []*http.Cookie{{Name: "MUSIC_U", Value: "synthetic-B"}})

	checkManualCookieOnly := func(r *http.Request, label string) {
		t.Helper()
		rawHeaders := r.Header.Values("Cookie")
		if len(rawHeaders) != 1 {
			t.Fatalf("%s sent %d Cookie headers, want 1: %v", label, len(rawHeaders), rawHeaders)
		}
		seen := map[string]int{}
		for _, part := range strings.Split(rawHeaders[0], "; ") {
			k, v, ok := strings.Cut(part, "=")
			if !ok {
				t.Fatalf("%s malformed cookie part %q", label, part)
			}
			seen[k]++
			if seen[k] > 1 {
				t.Fatalf("%s duplicated cookie %q in %q", label, k, rawHeaders[0])
			}
			if k == "MUSIC_U" && v != "synthetic-A" {
				t.Fatalf("%s used wrong MUSIC_U %q", label, v)
			}
		}
		for _, forbidden := range []string{"jar_only", "eapi_extra", "yd_track"} {
			if seen[forbidden] > 0 {
				t.Fatalf("%s leaked %q into manual Cookie header: %q", label, forbidden, rawHeaders[0])
			}
		}
	}

	step := 0
	c.client.Transport = browseTransport(func(r *http.Request) (*http.Response, error) {
		step++
		hdr := make(http.Header)
		switch step {
		case 1: // WEAPI Conversations sets cookies on .music.163.com
			if r.URL.Path != "/weapi/msg/private/users" {
				t.Fatalf("step 1 unexpected path %s", r.URL.Path)
			}
			hdr.Add("Set-Cookie", "MUSIC_U=jar-mutated; Domain=.music.163.com; Path=/")
			hdr.Add("Set-Cookie", "jar_only=from-weapi; Domain=.music.163.com; Path=/")
			return &http.Response{StatusCode: 200, Header: hdr, Body: io.NopCloser(strings.NewReader(`{"code":200,"msgs":[]}`))}, nil
		case 2: // EAPI Send must not receive Jar cookies and its Set-Cookie must not pollute Jar
			if r.URL.Path != "/eapi/msg/private/send" {
				t.Fatalf("step 2 unexpected path %s", r.URL.Path)
			}
			checkManualCookieOnly(r, "send")
			hdr.Add("Set-Cookie", "eapi_extra=from-send; Domain=.music.163.com; Path=/")
			hdr.Add("Set-Cookie", "MUSIC_U=from-send; Domain=.music.163.com; Path=/")
			return &http.Response{StatusCode: 200, Header: hdr, Body: io.NopCloser(strings.NewReader(`{"code":200}`))}, nil
		case 3: // EAPI Read must still only send manual EAPI cookies
			if r.URL.Path != "/eapi/communication/msg/unread/count/clean" {
				t.Fatalf("step 3 unexpected path %s", r.URL.Path)
			}
			checkManualCookieOnly(r, "read")
			return &http.Response{StatusCode: 200, Header: hdr, Body: io.NopCloser(strings.NewReader(`{"code":200}`))}, nil
		case 4, 5: // stickerCheckToken third-party GET must never send or retain cookies
			if r.URL.String() != privateStickerTokenURL {
				t.Fatalf("step %d unexpected url %s", step, r.URL)
			}
			if got := r.Header.Get("Cookie"); got != "" {
				t.Fatalf("step %d stickerCheckToken leaked Cookie: %q", step, got)
			}
			hdr.Add("Set-Cookie", "yd_track=1; Path=/")
			return &http.Response{StatusCode: 200, Header: hdr, Body: io.NopCloser(strings.NewReader(`null([200,0,"tok"]);`))}, nil
		case 6: // WEAPI History still uses the snapshotted Jar (with WEAPI updates, never EAPI/third-party Set-Cookie)
			if r.URL.Path != "/weapi/msg/private/history" {
				t.Fatalf("step 6 unexpected path %s", r.URL.Path)
			}
			cookie, err := r.Cookie("MUSIC_U")
			if err != nil || cookie.Value != "jar-mutated" {
				t.Fatalf("WEAPI History did not use snapshotted Jar MUSIC_U: %v %v", cookie, err)
			}
			if extra, err := r.Cookie("jar_only"); err != nil || extra.Value != "from-weapi" {
				t.Fatalf("WEAPI History lost Jar cookie: %v %v", extra, err)
			}
			for _, leaked := range []string{"eapi_extra", "yd_track"} {
				if _, err := r.Cookie(leaked); err == nil {
					t.Fatalf("non-Jar request polluted WEAPI Jar with %q", leaked)
				}
			}
			return &http.Response{StatusCode: 200, Header: hdr, Body: io.NopCloser(strings.NewReader(`{"code":200,"more":false,"msgs":[]}`))}, nil
		case 7: // doWithoutJar must preserve CheckRedirect refusal
			hdr.Set("Location", "https://example.invalid/redirected")
			return &http.Response{StatusCode: http.StatusFound, Header: hdr, Body: io.NopCloser(strings.NewReader(""))}, nil
		default:
			t.Fatalf("unexpected request step %d: %s", step, r.URL)
			return nil, nil
		}
	})

	ctx := context.Background()
	if _, err := c.Conversations(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if err := c.Send(ctx, 90002, "测试消息"); err != nil {
		t.Fatal(err)
	}
	if err := c.Read(ctx, 90002); err != nil {
		t.Fatal(err)
	}
	if _, err := c.stickerCheckToken(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.stickerCheckToken(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.History(ctx, 90002, 0); err != nil {
		t.Fatal(err)
	}
	if err := c.Send(ctx, 90002, "测试重定向拒绝"); err == nil {
		t.Fatal("doWithoutJar followed or accepted a 302 redirect")
	}
	if step != 7 {
		t.Fatalf("completed %d steps, want 7", step)
	}
}
