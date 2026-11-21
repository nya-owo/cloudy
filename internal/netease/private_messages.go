package netease

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
	"unicode"
)

// The paths and WEAPI protocol are sourced in docs/private-messages.md.
// Private responses never pass through general API error logging or disk caches.
type MessageUser struct {
	UserID    int64  `json:"userId"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatarUrl"`
}

type PrivateMessage struct {
	ID      int64       `json:"id"`
	Time    int64       `json:"time"`
	From    MessageUser `json:"fromUser"`
	To      MessageUser `json:"toUser"`
	Raw     string      `json:"msg"`
	content *privateMessageContentCache
}

func (m *PrivateMessage) UnmarshalJSON(raw []byte) error {
	type fields PrivateMessage
	wire := struct {
		*fields
		Message json.RawMessage `json:"msg"`
		privateStickerEnvelope
	}{fields: (*fields)(m)}
	if err := json.Unmarshal(raw, &wire); err != nil {
		return err
	}
	if len(wire.Message) > 0 && wire.Message[0] == '"' {
		if err := json.Unmarshal(wire.Message, &m.Raw); err != nil {
			return err
		}
	} else {
		m.Raw = string(wire.Message)
	}
	// Message-center metadata can sit beside msg or be split across the two.
	e := wire.privateStickerEnvelope
	if len(e.Type)+len(e.Body)+len(e.Message)+len(e.Picture)+len(e.Emoji) != 0 {
		_ = json.Unmarshal([]byte(m.Raw), &e)
		if sticker := e.sticker(); sticker != nil {
			data, _ := json.Marshal(struct {
				Type int             `json:"msgType"`
				Body *PrivateSticker `json:"body"`
			}{1, sticker})
			m.Raw = string(data)
		}
	}
	return nil
}

// Text returns a bounded preview of known text/share payloads, never the raw envelope.
func (m PrivateMessage) Text() string { return m.Content().Summary() }

func privateMessageText(raw string) string { return decodePrivateMessage(raw).Summary() }

func privateDisplayText(text string, limit int, singleLine bool) string {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' ||
			r == '\u061c' || r == '\u200e' || r == '\u200f' ||
			r >= '\u202a' && r <= '\u202e' || r >= '\u2066' && r <= '\u2069' {
			return -1
		}
		return r
	}, text)
	if singleLine {
		text = strings.Join(strings.Fields(text), " ")
	}
	runes := []rune(strings.TrimSpace(text))
	if len(runes) > limit {
		return string(runes[:limit]) + "…"
	}
	return string(runes)
}

type PrivateConversation struct {
	User        MessageUser
	Summary     string
	Time        int64
	Unread      int
	UnreadKnown bool
}
type PrivateConversationPage struct {
	Items []PrivateConversation
	More  bool
	Next  int
}
type PrivateHistoryPage struct {
	Items  []PrivateMessage
	More   bool
	Before int64
	// Authoritative marks a latest page with explicit, usable coverage.
	Authoritative bool
	// Exhausted marks an explicit server more=false on a valid page without
	// client-side truncation from an empty or non-advancing cursor.
	Exhausted bool
}

// PrivateMessageClient snapshots the active in-memory session when the user
// opens the panel. A delayed worker cannot accidentally use a later login.
type PrivateMessageClient struct {
	client  *http.Client
	self    int64
	csrf    string
	cookies []*http.Cookie
	device  func() string // resolved on the sending worker; tests supply a synthetic ID
}

func NewPrivateMessageClient(self int64) *PrivateMessageClient {
	jar, _ := cookiejar.New(nil)
	var csrf string
	if httpClient.Jar != nil {
		cookies := httpClient.Jar.Cookies(siteURL)
		jar.SetCookies(siteURL, cookies)
		for _, c := range cookies {
			if c.Name == "__csrf" {
				csrf = c.Value
			}
		}
	}
	return &PrivateMessageClient{self: self, csrf: csrf, cookies: jar.Cookies(siteURL), device: device, client: &http.Client{
		Timeout: 20 * time.Second, Jar: jar, Transport: httpClient.Transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (c *PrivateMessageClient) doWithoutJar(req *http.Request) (*http.Response, error) {
	client := *c.client
	client.Jar = nil
	return client.Do(req)
}

func (c *PrivateMessageClient) Conversations(ctx context.Context, offset int) (PrivateConversationPage, error) {
	var res struct {
		Msgs []struct {
			From   MessageUser     `json:"fromUser"`
			To     MessageUser     `json:"toUser"`
			Last   string          `json:"lastMsg"`
			Time   int64           `json:"lastMsgTime"`
			Unread json.RawMessage `json:"newMsgCount"`
		} `json:"msgs"`
		More bool `json:"more"`
	}
	err := c.post(ctx, "/api/msg/private/users", map[string]any{"offset": offset, "limit": 30, "total": "true"}, &res)
	if err == nil && res.Msgs == nil {
		err = errors.New("私信会话响应缺少列表字段")
	}
	p := PrivateConversationPage{More: res.More, Next: offset + len(res.Msgs)}
	for _, m := range res.Msgs {
		u := m.From
		if u.UserID == c.self {
			u = m.To
		}
		if u.UserID > 0 && u.UserID != c.self {
			item := PrivateConversation{User: u, Summary: privateMessageText(m.Last), Time: m.Time}
			var count *int
			if json.Unmarshal(m.Unread, &count) == nil && count != nil && *count >= 0 {
				item.Unread, item.UnreadKnown = *count, true
			}
			p.Items = append(p.Items, item)
		}
	}
	if len(res.Msgs) == 0 {
		p.More = false
	}
	return p, err
}

func (c *PrivateMessageClient) History(ctx context.Context, user, before int64) (PrivateHistoryPage, error) {
	var res struct {
		Msgs []PrivateMessage `json:"msgs"`
		More *bool            `json:"more"`
	}
	err := c.post(ctx, "/api/msg/private/history", map[string]any{"userId": user, "time": before, "limit": 30, "total": "true"}, &res)
	if err == nil && res.Msgs == nil {
		err = errors.New("私信历史响应缺少列表字段")
	}
	p := PrivateHistoryPage{Items: res.Msgs, More: res.More != nil && *res.More}
	timed := true
	for _, m := range res.Msgs {
		if m.Time <= 0 {
			timed = false
		} else if p.Before == 0 || m.Time < p.Before {
			p.Before = m.Time
		}
	}
	progressed := p.Before > 0 && (before == 0 || p.Before < before)
	p.Authoritative = err == nil && before == 0 && res.More != nil && timed && (!p.More || p.Before > 0)
	p.Exhausted = err == nil && res.More != nil && !*res.More && timed && (before == 0 || progressed)
	if !progressed {
		p.More = false
	}
	return p, err
}

func (c *PrivateMessageClient) Send(ctx context.Context, user int64, text string) error {
	if user <= 0 || user == c.self || strings.TrimSpace(text) == "" || len([]rune(text)) > 1000 {
		return errors.New("私信内容或收件人无效")
	}
	return c.post(ctx, "/api/msg/private/send", map[string]any{"type": "text", "msg": text, "userIds": fmt.Sprintf("[%d]", user)}, &struct{}{})
}

const privateReadPath = "/api/communication/msg/unread/count/clean"

// Read acknowledges one opened conversation, never the account's entire inbox.
func (c *PrivateMessageClient) Read(ctx context.Context, user int64) error {
	if user <= 0 || user == c.self {
		return errors.New("私信会话无效")
	}
	return c.post(ctx, privateReadPath, map[string]any{"userId": user}, &struct{}{})
}

// post uses the same site's HTTP transport/session as the rest of NetEase,
// using WEAPI for history and the existing EAPI cipher for send/read mutations.
func (c *PrivateMessageClient) post(ctx context.Context, path string, data map[string]any, out any) error {
	if ctx.Err() != nil {
		return errors.New("私信请求已取消")
	}
	var form url.Values
	var err error
	domain := apiBase + "/weapi/"
	ua := UserAgent
	var cookieHeader string
	switch path {
	case "/api/msg/private/send", privateReadPath, privateStickerGroupsPath, privateStickerPagePath, privateStickerSendPath:
		// Match the existing EAPI envelope, using only this account snapshot.
		header := eapiHeader(eapiClient, c.device(), c.cookies, time.Now())
		cookieHeader = eapiCookie(header)
		data["header"], data["e_r"] = header, false
		var params string
		params, err = eapiEncrypt(path, data)
		form = url.Values{"params": {params}}
		domain = eapiDomain + "/eapi/"
		ua = eapiUA
	default:
		data["csrf_token"] = c.csrf
		form, err = privateWEAPI(data)
	}
	if err != nil {
		return errors.New("私信请求加密失败")
	}
	req, err := http.NewRequestWithContext(ctx, "POST", domain+strings.TrimPrefix(path, "/api/"), strings.NewReader(form.Encode()))
	if err != nil {
		return errors.New("私信请求失败")
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Referer", apiBase+"/")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var res *http.Response
	if cookieHeader != "" {
		req.Header.Set("Cookie", cookieHeader)
		res, err = c.doWithoutJar(req)
	} else {
		res, err = c.client.Do(req)
	}
	if err != nil {
		return errors.New("私信网络请求失败，请重试")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return &PrivateMessageHTTPError{StatusCode: res.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return errors.New("私信响应读取失败")
	}
	var status struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &status) != nil {
		return errors.New("私信响应格式不受支持")
	}
	if status.Code != 200 {
		if status.Code == 301 || status.Code == 302 {
			return errors.New("登录已失效，请重新登录")
		}
		// Do not surface server text that could echo message bodies or cookies.
		return &PrivateMessageStatusError{Code: status.Code}
	}
	if path == privateReadPath {
		var data struct{ Success *bool }
		rejected := string(status.Data) == "false"
		if len(status.Data) > 0 && status.Data[0] == '{' && json.Unmarshal(status.Data, &data) == nil && data.Success != nil && !*data.Success {
			rejected = true
		}
		if rejected {
			return errors.New("服务器未确认该会话已读")
		}
	}
	if json.Unmarshal(body, out) != nil {
		return errors.New("私信响应格式不受支持")
	}
	return nil
}

// PrivateMessageHTTPError reports a non-200 HTTP status without remote body text.
type PrivateMessageHTTPError struct{ StatusCode int }

func (e *PrivateMessageHTTPError) Error() string {
	return fmt.Sprintf("私信服务 HTTP %d", e.StatusCode)
}

// PrivateMessageStatusError carries no remote text, identity or message body.
type PrivateMessageStatusError struct{ Code int }

func (e *PrivateMessageStatusError) Error() string {
	if e.Code == 250 {
		return "服务器拒绝（250）。可能涉及请求兼容性、客户端会话或设备安全验证、接收方限制。请先刷新历史确认，避免重复发送；若仍失败，需与官方客户端的账号及该联系人权限对照排查。"
	}
	return fmt.Sprintf("私信请求未成功（%d）", e.Code)
}

func privateWEAPI(data map[string]any) (url.Values, error) {
	plain, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	key := make([]byte, 16)
	for i := range key {
		n, err := rand.Int(rand.Reader, big.NewInt(62))
		if err != nil {
			return nil, err
		}
		key[i] = alphabet[n.Int64()]
	}
	return privateWEAPIKey(plain, key)
}

func privateWEAPIKey(plain, key []byte) (url.Values, error) {
	encrypt := func(text, key []byte) (string, error) {
		block, err := aes.NewCipher(key)
		if err != nil {
			return "", err
		}
		pad := aes.BlockSize - len(text)%aes.BlockSize
		padded := append(append([]byte(nil), text...), make([]byte, pad)...)
		for i := len(text); i < len(padded); i++ {
			padded[i] = byte(pad)
		}
		cipher.NewCBCEncrypter(block, []byte("0102030405060708")).CryptBlocks(padded, padded)
		return base64.StdEncoding.EncodeToString(padded), nil
	}
	first, err := encrypt(plain, []byte("0CoJUm6Qyw8W8jud"))
	if err != nil {
		return nil, err
	}
	params, err := encrypt([]byte(first), key)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode([]byte(privateWEAPIPublicKey))
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaKey := pub.(*rsa.PublicKey)
	reversed := append([]byte(nil), key...)
	for i, j := 0, len(reversed)-1; i < j; i, j = i+1, j-1 {
		reversed[i], reversed[j] = reversed[j], reversed[i]
	}
	n := new(big.Int).Exp(new(big.Int).SetBytes(reversed), big.NewInt(int64(rsaKey.E)), rsaKey.N)
	return url.Values{"params": {params}, "encSecKey": {fmt.Sprintf("%0256x", n)}}, nil
}

const privateWEAPIPublicKey = `-----BEGIN PUBLIC KEY-----
MIGfMA0GCSqGSIb3DQEBAQUAA4GNADCBiQKBgQDgtQn2JZ34ZC28NWYpAUd98iZ37BUrX/aKzmFbt7clFSs6sXqHauqKWqdtLkF2KexO40H1YTX8z2lSgBBOAxLsvaklV8k4cBFK9snQXE9/DDaFt6Rr7iVZMldczhC0JNgTz+SHXT6CBHuX3e9SdB1Ua44oncaTWz7OBGLbCiK45wIDAQAB
-----END PUBLIC KEY-----`
