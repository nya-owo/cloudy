package app

import (
	"runtime"
	"slices"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
)

var privateEmojiGlyphs = []string{
	"😀", "😂", "😊", "😍", "❤️", "👍", "🎵",
	"😁", "😆", "😅", "🤣", "😉", "😎", "🎧",
	"🥰", "😘", "🤔", "😮", "😴", "😭", "😤",
	"😢", "🙃", "😋", "😜", "😇", "🙄", "😬",
	"👋", "🙌", "👏", "🙏", "💪", "✌️", "🤝",
	"💖", "💔", "💕", "🔥", "✨", "⭐", "🎉",
	"🎶", "🎤", "🎸", "🎹", "🥁", "🎼", "💿",
	"🌙", "☀️", "☕", "🍕", "🎂", "🐱", "🐶",
}

func privateEmojiGlyph(c *ui.Context, glyph string, size float32) {
	text := ui.Text(c, glyph).FontSize(size).SingleLine()
	if runtime.GOOS == "windows" {
		text.Font("Segoe UI Emoji")
	}
}

func (a *app) privateEmojiGrid(c *ui.Context, chat *privateChat, ink paneInk) {
	glyphs, key := privateEmojiGlyphs, "emoji-list"
	small := a.dm.stickers.smallEmoji
	if small {
		glyphs, key = privateSmallEmojiMarkers, "small-emoji-list"
	}
	ui.Scroll(c.Key(key)).Grow(1).MinHeight(0).Gap(3).Children(func() {
		for row := range slices.Chunk(glyphs, 7) {
			ui.Row(c).Height(35).Gap(4).Children(func() {
				for _, glyph := range row {
					open := a.dm.stickers.open
					var tile ui.Element
					if open {
						label := "插入 Emoji：" + glyph
						if small {
							label = "插入表情：" + glyph
						}
						tile = ui.ButtonBase(c.Key(glyph)).Label(label)
					} else {
						tile = ui.Row(c.Key(glyph)).Center().Shrink(0)
					}
					tile.Size(40, 35).Radius(8).Clip()
					if open && tile.Hovered() {
						tile.Background(ink.hover)
					}
					tile.Children(func() {
						if !small {
							privateEmojiGlyph(c, glyph, 24)
						} else if bmp := a.privateSmallEmojiBitmap(glyph); bmp != nil {
							ui.Image(c, bmp).Size(28, 28).Fit(ui.Contain)
						}
					})
					if open {
						tile.OnClick(func() { a.insertPrivateEmoji(c, chat, glyph) })
					}
				}
			})
		}
	})
}

func (a *app) insertPrivateEmoji(c *ui.Context, chat *privateChat, glyph string) {
	d := &a.dm
	draft := []rune(chat.draft)
	start, end := len(draft), len(draft)
	if composer := c.Resolve(d.composer); composer.Valid() {
		if composer.Composing() {
			return
		}
		start, end = composer.TextSelection()
		start = max(0, min(start, len(draft)))
		end = max(start, min(end, len(draft)))
	}
	glyphSize := utf8.RuneCountInString(glyph)
	if len(draft)-(end-start)+glyphSize > 1000 {
		return
	}
	chat.draft = string(draft[:start]) + glyph + string(draft[end:])
	caret := start + glyphSize
	d.stickers.emojiCaret = &caret
	d.stickers.open = false
	d.composer.Focus(c)
}
