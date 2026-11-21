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

// Private stickers retain exact IDs from both group types and send one native image with a fresh check token.
func TestPrivateStickerEAPI(t *testing.T) {
	const groupID = "100000000000000003"
	const emojiID = "109951166199016469"
	const picture = "https://p1.music.126.net/5rgzlgx3yofdin1Rnf8iQw==/109951166199016466.jpg"
	c := &PrivateMessageClient{
		self:   90001,
		device: func() string { return strings.Repeat("A", 52) },
		client: &http.Client{},
	}
	steps := []string{privateStickerGroupsPath, privateStickerPagePath, privateStickerTokenURL, privateStickerSendPath}
	c.client.Transport = browseTransport(func(r *http.Request) (*http.Response, error) {
		if len(steps) == 0 {
			t.Fatal("unexpected extra request")
		}
		path := steps[0]
		steps = steps[1:]
		reply := func(s string) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(s))}, nil
		}
		if path == privateStickerTokenURL {
			if r.Method != "GET" || r.URL.String() != path || r.Header.Get("Cookie") != "" {
				t.Fatal("wrong unauthenticated check token request")
			}
			return reply(`null([200,0,"fixture-token"]);`)
		}
		if r.Method != "POST" || r.URL.String() != eapiDomain+"/eapi/"+strings.TrimPrefix(path, "/api/") || r.ParseForm() != nil {
			t.Fatal("wrong EAPI request")
		}
		data, err := hex.DecodeString(r.Form.Get("params"))
		if err != nil || len(data) == 0 || len(data)%aes.BlockSize != 0 {
			t.Fatal("missing EAPI envelope")
		}
		block, _ := aes.NewCipher([]byte(eapiKey))
		for i := 0; i < len(data); i += aes.BlockSize {
			block.Decrypt(data[i:i+aes.BlockSize], data[i:i+aes.BlockSize])
		}
		parts := strings.Split(string(data), "-36cd479b6b5-")
		var body map[string]any
		if len(parts) != 3 || parts[0] != path || json.Unmarshal([]byte(parts[1]), &body) != nil {
			t.Fatal("invalid EAPI payload")
		}
		switch path {
		case privateStickerGroupsPath:
			if body["resourceType"] != float64(2) {
				t.Fatal("groups must use private resource type")
			}
			return reply(`{"code":200,"data":{"emojiGroups":[{"id":100000000000000003,"name":"内置","edit":false},{"id":"-2","name":"收藏","edit":true},{"id":3,"name":"不可用","enable":false}]}}`)
		case privateStickerPagePath:
			if body["emojiGroupId"] != groupID || body["cursor"] != "next-old" || body["size"] != float64(10) {
				t.Fatal("wrong group or page cursor")
			}
			return reply(`{"code":200,"data":{"emojis":[{"emojiId":109951166199016469,"picId":109951166199016466,"emojiImgUrl":"https://unsupported.example/sea.png","emojiName":"海豹","width":160,"height":120,"format":"png"},{"emojiId":11,"picId":109951166199016466,"emojiImgUrl":"https://p4.music.126.net/sea.png"},{"emojiId":12,"url":"https://p3.music.126.net/native.webp","emojiImgUrl":"https://unsupported.example/sea.png"},{"emojiId":10,"emojiImgUrl":"https://p1.music.126.net/unused.png","disabled":true}],"page":{"more":true,"cursor":"next-new"}}}`)
		case privateStickerSendPath:
			var send struct {
				Scene     int
				Receiver  string `json:"receiverUserIds"`
				ChannelID string
				MsgBody   struct {
					MsgType      int
					Body, Unikey string
				}
			}
			if body["checkToken"] != "fixture-token" || json.Unmarshal([]byte(body["sendMsgBody"].(string)), &send) != nil ||
				send.Scene != 1 || send.Receiver != "90002" || send.ChannelID != "90002" || send.MsgBody.MsgType != 1 || len(send.MsgBody.Unikey) != 36 {
				t.Fatal("send must target exactly one peer as a native image")
			}
			var image map[string]any
			if json.Unmarshal([]byte(send.MsgBody.Body), &image) != nil || image["emojiId"] != emojiID ||
				image["emojiGroupId"] != groupID || image["url"] != picture || image["format"] != "png" ||
				image["width"] != float64(160) || image["height"] != float64(120) {
				t.Fatal("sticker identity or metadata changed in transit")
			}
			return reply(`{"code":200,"data":{"msgBody":{"msgId":109951166199016495,"status":1}}}`)
		}
		t.Fatalf("unexpected path %s", path)
		return nil, nil
	})
	ctx := context.Background()
	groups, err := c.StickerGroups(ctx)
	if err != nil || len(groups) != 2 || groups[0].ID.String() != groupID || groups[0].Name != "内置" || groups[1].Name != "收藏" {
		t.Fatalf("groups lost official built-in or saved entries: %v, %v", groups, err)
	}
	page, err := c.StickerPage(ctx, groupID, "next-old")
	if err != nil || len(page.Items) != 3 || page.Cursor != "next-new" || page.Items[0].URL != picture ||
		page.Items[0].Name != "海豹" || page.Items[1].URL != "https://p4.music.126.net/sea.png" ||
		page.Items[1].AlternateURL != picture || page.Items[2].URL != "https://p3.music.126.net/native.webp" {
		t.Fatalf("page retained restricted entries or lost image data: %v, %v", page, err)
	}
	if err := c.SendSticker(ctx, 90002, page.Items[0]); err != nil || len(steps) != 0 {
		t.Fatalf("sticker send did not complete: %v", err)
	}
}

// Message-center and legacy picture bodies render with or without send-only sticker IDs.
func TestPrivateStickerHistory(t *testing.T) {
	body := `{"emojiId":109951166199016469,"emojiGroupId":"-2","url":"http://p4.music.126.net/sea.png","name":"海豹"}`
	encoded, _ := json.Marshal(body)
	for _, raw := range []string{
		`{"id":1,"msgType":1,"body":` + string(encoded) + "}",
		`{"id":1,"msg":{"msgBody":{"msgType":1,"body":` + body + "}}}",
	} {
		var message PrivateMessage
		if err := json.Unmarshal([]byte(raw), &message); err != nil {
			t.Fatal(err)
		}
		sticker := message.WithContentCache().Content().Sticker
		if message.ID != 1 || sticker == nil || sticker.ID.String() != "109951166199016469" || sticker.Group.String() != "-2" || sticker.URL != "https://p4.music.126.net/sea.png" {
			t.Fatalf("official sticker did not decode: %+v", sticker)
		}
	}
	quote := func(raw string) string {
		encoded, _ := json.Marshal(raw)
		return string(encoded)
	}
	for _, raw := range []string{
		`{"id":2,"msgType":1,"msg":` + quote(`{"body":`+quote(`{"url":"https://p4.music.126.net/sea.png","width":"160"}`)+"}") + "}",
		`{"id":2,"msg":{"type":0,"picInfo":` + quote(`{"imgUrl":"https://p4.music.126.net/sea.png","emojiId":""}`) + "}}",
		`{"id":2,"msg":{"msgBody":{"msgType":1,"body":{"picInfo":{"picUrl":"https://p4.music.126.net/sea.png","emojiId":0}}}}}`,
	} {
		var message PrivateMessage
		if err := json.Unmarshal([]byte(raw), &message); err != nil {
			t.Fatal(err)
		}
		if sticker := message.WithContentCache().Content().Sticker; sticker == nil || sticker.URL != "https://p4.music.126.net/sea.png" {
			t.Fatalf("picture with no sendable identity did not decode: %+v", sticker)
		}
	}
	var card PrivateMessage
	if err := json.Unmarshal([]byte(`{"msg":{"msg":"分享给你","song":{"id":19,"name":"合成歌曲","picUrl":"https://p4.music.126.net/sea.png"}}}`), &card); err != nil {
		t.Fatal(err)
	}
	content := card.WithContentCache().Content()
	if content.Sticker != nil || content.Media == nil || content.Media.ID != 19 || content.Text != "分享给你" {
		t.Fatalf("ordinary text/share was mistaken for a picture: %+v", content)
	}
}
