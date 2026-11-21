package netease

import (
	"encoding/json"
	"net/url"
	"strings"
	"unicode/utf8"
)

// PrivateMessageContent contains only display fields, never an arbitrary wire object.
type PrivateMessageContent struct {
	Text    string
	Media   *PrivateMessageMedia
	Sticker *PrivateSticker
}

type PrivateMessageMedia struct {
	Kind                      string
	ID                        int64
	Title, Subtitle, CoverURL string
}

type privateMessageContentCache struct {
	raw   string
	value PrivateMessageContent
}

// WithContentCache prepares immutable display data on this message value. It is
// owned by the in-memory history, never a global cache or part of its wire JSON.
func (m PrivateMessage) WithContentCache() PrivateMessage {
	if m.content == nil || m.content.raw != m.Raw {
		m.content = &privateMessageContentCache{raw: m.Raw, value: decodePrivateMessage(m.Raw)}
	}
	return m
}

func (m PrivateMessage) Content() PrivateMessageContent {
	if m.content == nil || m.content.raw != m.Raw {
		return decodePrivateMessage(m.Raw)
	}
	value := m.content.value
	if value.Media != nil {
		media := *value.Media // callers may enrich a copy, never the cached value
		value.Media = &media
	}
	if value.Sticker != nil {
		sticker := *value.Sticker
		value.Sticker = &sticker
	}
	return value
}

func (m PrivateMessageMedia) Label() string {
	switch m.Kind {
	case "song":
		return "分享歌曲"
	case "playlist":
		return "分享歌单"
	case "album":
		return "分享专辑"
	}
	return "分享资源"
}

func (c PrivateMessageContent) Supported() bool {
	return c.Text != "" || c.Media != nil || c.Sticker != nil
}

func (c PrivateMessageContent) Summary() string {
	if c.Sticker != nil {
		return "[表情]"
	}
	if c.Media != nil {
		text := "[" + c.Media.Label() + "]"
		if c.Media.Title != "" {
			text += " " + c.Media.Title
		}
		if c.Text != "" {
			text += "\n" + c.Text
		}
		return text
	}
	if c.Text != "" {
		return c.Text
	}
	return "暂不支持的消息类型"
}

func decodePrivateMessage(raw string) PrivateMessageContent {
	unknown := PrivateMessageContent{}
	if len(raw) > 64<<10 || !utf8.ValidString(raw) {
		return unknown
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &payload) != nil || payload == nil {
		return unknown
	}
	var text string
	// msg is display text, not executable content. Type and unrelated
	// metadata must not veto a string, including a literal JSON message.
	_ = json.Unmarshal(payload["msg"], &text)
	text = privateDisplayText(text, 4000, false)
	content := PrivateMessageContent{Text: text}
	for _, kind := range []string{"song", "playlist", "album"} {
		value, ok := payload[kind]
		if !ok {
			continue
		}
		if content.Media != nil {
			break
		}
		var object map[string]json.RawMessage
		if json.Unmarshal(value, &object) != nil || object == nil {
			continue
		}
		var name string
		_ = json.Unmarshal(object["name"], &name)
		media := &PrivateMessageMedia{Kind: kind, Title: privateDisplayText(name, 120, true)}
		_ = json.Unmarshal(object["id"], &media.ID)
		if media.ID < 0 {
			media.ID = 0
		}
		media.Subtitle = privateMediaArtists(object)
		if kind == "playlist" && media.Subtitle == "" {
			var creator struct {
				Nickname string `json:"nickname"`
			}
			if json.Unmarshal(object["creator"], &creator) == nil {
				media.Subtitle = privateDisplayText(creator.Nickname, 160, true)
			}
		}
		var cover string
		if kind == "playlist" {
			_ = json.Unmarshal(object["coverImgUrl"], &cover)
			if privateMediaCover(cover) == "" {
				_ = json.Unmarshal(object["picUrl"], &cover)
			}
		} else {
			_ = json.Unmarshal(object["picUrl"], &cover)
		}
		if kind == "song" {
			for _, key := range []string{"al", "album"} {
				var album struct {
					Name   string `json:"name"`
					PicURL string `json:"picUrl"`
				}
				if json.Unmarshal(object[key], &album) == nil {
					if privateMediaCover(cover) == "" {
						cover = album.PicURL
					}
					if media.Subtitle == "" {
						media.Subtitle = privateDisplayText(album.Name, 160, true)
					}
				}
			}
		}
		media.CoverURL = privateMediaCover(cover)
		content.Media = media
	}
	if content.Media == nil && content.Text == "" {
		// title is optional display text, never a prerequisite for msg.
		var title string
		_ = json.Unmarshal(payload["title"], &title)
		content.Text = privateDisplayText(title, 4000, false)
	}
	if content.Media == nil {
		envelope := privateStickerEnvelope{
			Type: payload["msgType"], Body: payload["body"], Message: payload["msgBody"],
			Picture: payload["picInfo"], Emoji: payload["emoji"],
		}
		if sticker := envelope.sticker(); sticker != nil {
			return PrivateMessageContent{Sticker: sticker}
		}
	}
	return content
}

func privateMediaArtists(object map[string]json.RawMessage) string {
	var artists []struct {
		Name string `json:"name"`
	}
	for _, key := range []string{"artists", "ar"} {
		if json.Unmarshal(object[key], &artists) == nil && len(artists) > 0 {
			break
		}
	}
	var names []string
	for _, artist := range artists {
		if name := privateDisplayText(artist.Name, 80, true); name != "" {
			names = append(names, name)
		}
		if len(names) == 3 {
			break
		}
	}
	if len(names) == 0 {
		var artist struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(object["artist"], &artist) == nil && artist.Name != "" {
			names = append(names, artist.Name)
		}
	}
	return privateDisplayText(strings.Join(names, " / "), 160, true)
}

// Like Sized, upgrade NetEase's HTTP artwork to HTTPS before loading it.
// Cover requests can only reuse known NetEase image hosts; credentials,
// ports, fragments and caller-supplied query parameters never reach the cache.
func privateMediaCover(raw string) string {
	if len(raw) > 2048 || strings.ContainsAny(raw, "\r\n\t") {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Port() != "" || u.Fragment != "" || u.Opaque != "" || u.Path == "" || u.Path == "/" {
		return ""
	}
	switch strings.ToLower(u.Host) {
	case "p1.music.126.net", "p2.music.126.net", "p3.music.126.net", "p4.music.126.net":
	default:
		return ""
	}
	u.RawQuery = ""
	u.ForceQuery = false
	u.Scheme = "https"
	return u.String()
}
