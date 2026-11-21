package app

import (
	"regexp"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

const privateSmallEmojiTitle = "多多西西"

var privateSmallEmojiMarkers, privateSmallEmojiURLs = func() ([]string, map[string]string) {
	markers := make([]string, 0, len(netease.DuoduoXixi))
	urls := make(map[string]string, len(netease.DuoduoXixi))
	for _, emoji := range netease.DuoduoXixi {
		markers = append(markers, emoji.Marker)
		urls[emoji.Marker] = emoji.URL
	}
	return markers, urls
}()

var privateSmallEmojiPattern = regexp.MustCompile(`\[(?:多多|西西)\p{Han}{2}\]`)

func (a *app) privateSmallEmojiBitmap(marker string) *ui.Bitmap {
	return bitmap(a.art.get(privateSmallEmojiURLs[marker], false))
}

func (a *app) privateMessageText(c *ui.Context, text string) {
	var spans []ui.Span
	var images map[int]*ui.Bitmap
	end, offset := 0, 0
	for _, at := range privateSmallEmojiPattern.FindAllStringIndex(text, -1) {
		bmp := a.privateSmallEmojiBitmap(text[at[0]:at[1]])
		if bmp == nil {
			continue // Keep the marker readable while its image loads.
		}
		if images == nil {
			images = make(map[int]*ui.Bitmap)
		}
		if plain := text[end:at[0]]; plain != "" {
			spans = append(spans, ui.Span{Text: plain})
			offset += utf8.RuneCountInString(plain)
		}
		images[offset] = bmp
		spans = append(spans, ui.Span{Text: "\uFFFC", ObjectWidth: 20, ObjectHeight: 16, ObjectDescent: 4})
		end, offset = at[1], offset+1
	}
	if len(spans) == 0 {
		ui.Text(c, text).FontSize(13).LineHeight(1.5)
		return
	}
	if end < len(text) {
		spans = append(spans, ui.Span{Text: text[end:]})
	}
	ui.RichText(c, spans...).FontSize(13).LineHeight(1.5).Label(text).
		DrawObjects(func(p *ui.Painter, at int, bounds ui.Rect) {
			p.Image(images[at], bounds, ui.Contain)
		})
}
