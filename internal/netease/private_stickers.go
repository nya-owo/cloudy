package netease

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	privateStickerGroupsPath = "/api/social/emoji/groups"
	privateStickerPagePath   = "/api/social/emoji/groups/detail/page"
	privateStickerSendPath   = "/api/communication/send/msg"
	privateStickerTokenURL   = "https://ac.dun.163yun.com/v3/b?pn=YD00000558929251"
)

type PrivateStickerGroup struct {
	ID       json.Number `json:"id"`
	Name     string      `json:"name"`
	Enabled  *bool       `json:"enable"`
	Disabled bool        `json:"disabled"`
}

type PrivateSticker struct {
	ID           json.Number `json:"emojiId"`
	Group        json.Number `json:"emojiGroupId"`
	URL          string      `json:"url"`
	Name         string      `json:"name"`
	Width        int         `json:"width"`
	Height       int         `json:"height"`
	Format       string      `json:"format"`
	AlternateURL string      `json:"-"`
}

type PrivateStickerPage struct {
	Items  []PrivateSticker
	Cursor string
}

func (c *PrivateMessageClient) StickerGroups(ctx context.Context) ([]PrivateStickerGroup, error) {
	var res struct {
		Data struct {
			Groups []PrivateStickerGroup `json:"emojiGroups"`
		}
	}
	if err := c.post(ctx, privateStickerGroupsPath, map[string]any{"resourceType": 2}, &res); err != nil {
		return nil, err
	}
	if res.Data.Groups == nil {
		return nil, errors.New("表情分组响应缺少列表")
	}
	var groups []PrivateStickerGroup
	for _, group := range res.Data.Groups {
		if group.ID == "" || group.Disabled || group.Enabled != nil && !*group.Enabled {
			continue
		}
		group.Name = privateDisplayText(group.Name, 80, true)
		groups = append(groups, group)
	}
	return groups, nil
}

func (c *PrivateMessageClient) StickerPage(ctx context.Context, group, cursor string) (PrivateStickerPage, error) {
	var res struct {
		Data struct {
			Emojis []struct {
				PrivateSticker
				PicID     json.Number `json:"picId"`
				ImageURL  string      `json:"emojiImgUrl"`
				EmojiName string      `json:"emojiName"`
				Enabled   *bool       `json:"enable"`
				Disabled  bool        `json:"disabled"`
			}
			Page struct {
				More   bool
				Cursor string
			}
		}
	}
	page := PrivateStickerPage{}
	if err := c.post(ctx, privateStickerPagePath, map[string]any{"emojiGroupId": group, "cursor": cursor, "size": 10}, &res); err != nil {
		return page, err
	}
	if res.Data.Emojis == nil {
		return page, errors.New("表情详情响应缺少列表")
	}
	for _, item := range res.Data.Emojis {
		sticker := item.PrivateSticker
		if item.Disabled || item.Enabled != nil && !*item.Enabled || sticker.ID == "" || sticker.ID == "0" {
			continue
		}
		if sticker.Group == "" {
			sticker.Group = json.Number(group)
		}
		// Preserve url when emojiImgUrl is absent or unusable.
		sticker.URL = privateStickerURL(sticker.URL)
		if src := privateStickerURL(item.ImageURL); src != "" {
			sticker.URL = src
		}
		picture := privateStickerPicURL(item.PicID.String())
		if sticker.URL == "" {
			sticker.URL = picture
		} else if sticker.URL != picture {
			sticker.AlternateURL = picture
		}
		if sticker.URL == "" {
			continue
		}
		if item.EmojiName != "" {
			sticker.Name = item.EmojiName
		}
		sticker.Name = privateDisplayText(sticker.Name, 80, true)
		page.Items = append(page.Items, sticker)
	}
	if res.Data.Page.More && res.Data.Page.Cursor != cursor {
		page.Cursor = res.Data.Page.Cursor
	}
	return page, nil
}

func privateStickerPicURL(id string) string {
	if id == "" || strings.Trim(id, "0123456789") != "" || id == "0" {
		return ""
	}
	// The official CDN address can be derived from the exact decimal picId.
	const key = "3go8&$8*3*3h0k(2)2"
	b := []byte(id)
	for i := range b {
		b[i] ^= key[i%len(key)]
	}
	hash := md5.Sum(b)
	return "https://p1.music.126.net/" + base64.URLEncoding.EncodeToString(hash[:]) + "/" + id + ".jpg"
}

func privateStickerURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Port() != "" || u.Path == "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	for _, domain := range []string{"music.126.net", "nos.netease.com", "nosdn.127.net", "nos-hz.163yun.com", "nos-jd.163yun.com", "nos-hz.netease.com"} {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			u.Scheme, u.Fragment = "https", ""
			return u.String()
		}
	}
	return ""
}

func (c *PrivateMessageClient) SendSticker(ctx context.Context, user int64, sticker PrivateSticker) error {
	if user <= 0 || user == c.self || sticker.ID == "" || sticker.ID == "0" || sticker.Group == "" || privateStickerURL(sticker.URL) == "" {
		return errors.New("表情或收件人无效")
	}
	token, err := c.stickerCheckToken(ctx)
	if err != nil {
		return err
	}
	to := strconv.FormatInt(user, 10)
	var key [16]byte
	rand.Read(key[:])
	key[6], key[8] = key[6]&0x0f|0x40, key[8]&0x3f|0x80
	unikey := fmt.Sprintf("%x-%x-%x-%x-%x", key[:4], key[4:6], key[6:8], key[8:10], key[10:])
	body, _ := json.Marshal(map[string]any{
		"url": sticker.URL, "name": sticker.Name, "emojiId": sticker.ID.String(), "emojiGroupId": sticker.Group.String(),
		"width": sticker.Width, "height": sticker.Height, "format": sticker.Format,
	})
	msg, _ := json.Marshal(map[string]any{
		"scene": 1, "receiverUserIds": to, "channelId": to, "symphonyId": "", "refMsgBody": struct{}{},
		"msgBody": map[string]any{
			"msgType": 1, "body": string(body), "unikey": unikey,
			"msgTime": time.Now().UnixMilli(), "status": 1, "sendStatus": 0,
			"sender": map[string]any{"user": map[string]any{"userId": c.self}},
			"text":   map[string]any{"textBody": "", "atBody": []any{}},
		},
	})
	var reply struct {
		Data struct {
			Success *bool
			MsgBody struct {
				MsgID  json.Number
				Status int
			}
		}
	}
	if err := c.post(ctx, privateStickerSendPath, map[string]any{"checkToken": token, "sendMsgBody": string(msg)}, &reply); err != nil {
		return err
	}
	if reply.Data.Success != nil && !*reply.Data.Success || reply.Data.MsgBody.MsgID == "" || reply.Data.MsgBody.MsgID == "0" || reply.Data.MsgBody.Status < 0 {
		return errors.New("服务器未确认表情发送，请先刷新会话")
	}
	return nil
}

// Message-center image bodies arrive as objects or encoded JSON strings.
type privateStickerEnvelope struct {
	Type    json.RawMessage `json:"msgType"`
	Body    json.RawMessage `json:"body"`
	Message json.RawMessage `json:"msgBody"`
	Picture json.RawMessage `json:"picInfo"`
	Emoji   json.RawMessage `json:"emoji"`
}

func (e privateStickerEnvelope) sticker() *PrivateSticker {
	if len(e.Type) == 0 && len(e.Message) != 0 {
		_ = decodeStickerObject(e.Message, &e)
	}
	if string(e.Type) == "1" {
		body := e.Body
		if len(body) == 0 {
			body = e.Message
		}
		if sticker := privateStickerPicture(body); sticker != nil {
			return sticker
		}
		_ = decodeStickerObject(body, &e)
	}
	if sticker := privateStickerPicture(e.Picture); sticker != nil {
		return sticker
	}
	return privateStickerPicture(e.Emoji)
}

// History can display a native image without the IDs required to send a sticker.
func privateStickerPicture(raw json.RawMessage) *PrivateSticker {
	var fields map[string]json.RawMessage
	if len(raw) == 0 || decodeStickerObject(raw, &fields) != nil {
		return nil
	}
	for _, key := range []string{"url", "picUrl", "imgUrl", "emojiImgUrl"} {
		var value string
		_ = json.Unmarshal(fields[key], &value)
		if src := privateStickerURL(value); src != "" {
			picture := &PrivateSticker{URL: src}
			_ = json.Unmarshal(fields["emojiId"], &picture.ID)
			_ = json.Unmarshal(fields["emojiGroupId"], &picture.Group)
			_ = json.Unmarshal(fields["name"], &picture.Name)
			if picture.Name == "" {
				_ = json.Unmarshal(fields["emojiName"], &picture.Name)
			}
			picture.Name = privateDisplayText(picture.Name, 80, true)
			return picture
		}
	}
	return nil
}

func decodeStickerObject(raw json.RawMessage, out any) error {
	if len(raw) > 0 && raw[0] == '"' {
		var encoded string
		if err := json.Unmarshal(raw, &encoded); err != nil {
			return err
		}
		raw = []byte(encoded)
	}
	return json.Unmarshal(raw, out)
}

func (c *PrivateMessageClient) stickerCheckToken(ctx context.Context) (string, error) {
	failure := errors.New("未取得表情发送校验令牌")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, privateStickerTokenURL, nil)
	if err != nil {
		return "", failure
	}
	res, err := c.doWithoutJar(req)
	if err != nil {
		return "", failure
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 16<<10))
	if err != nil || res.StatusCode != http.StatusOK {
		return "", failure
	}
	start := strings.TrimSuffix(strings.TrimSpace(string(body)), ";")
	payload, ok := strings.CutPrefix(start, "null(")
	var reply []json.RawMessage
	var token string
	if !ok || !strings.HasSuffix(payload, ")") ||
		json.Unmarshal([]byte(strings.TrimSuffix(payload, ")")), &reply) != nil ||
		len(reply) < 3 || string(reply[0]) != "200" || json.Unmarshal(reply[2], &token) != nil || token == "" {
		return "", failure
	}
	return token, nil
}
