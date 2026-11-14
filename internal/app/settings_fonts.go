package app

import "github.com/egoist/mygo/ui"

// followUIFont is the lyrics' font choice that takes the app's.
const followUIFont = "跟随界面字体"

// fontRows are the settings of the app's font and the lyrics'.
func (a *app) fontRows(c *ui.Context, k *settingsKit) {
	s := &a.settings
	k.row("界面字体", "界面与评论使用的字体", func() {
		a.fontSelect(c, "font", "界面字体", &s.Font, false)
	})
	k.row("歌词字体", "播放页歌词与桌面歌词使用的字体，默认跟随界面字体", func() {
		a.fontSelect(c, "lyric-font", "歌词字体", &s.LyricFont, true)
	})
}

// fontSelect chooses the font of id among those on offer, each shown in
// itself, the last choice opening the picker of the system's. For the
// lyrics the first choice follows the app's font.
func (a *app) fontSelect(c *ui.Context, key, label string, id *string, lyric bool) {
	fonts := a.fontChoices()
	a.loadFontFiles()
	// The font chosen in the picker shows among those on offer.
	if cur := fontOf(*id); cur.custom() {
		fonts = append(fonts[:len(fonts):len(fonts)], cur)
	}
	labels := make([]string, 0, len(fonts)+2)
	chosen := fontOf(*id).label
	if lyric {
		labels = append(labels, followUIFont)
		if *id == "" {
			chosen = followUIFont
		}
	}
	for _, f := range fonts {
		labels = append(labels, f.label)
	}
	labels = append(labels, moreFonts)
	own := func(label string) string {
		if label == followUIFont {
			return a.fontFamily()
		}
		for _, f := range fonts {
			if f.label == label && f.id != "" {
				return f.family
			}
		}
		return defaultFontFamily()
	}
	if !a.paneSelect(c.Key(key), label, &chosen, labels, own) {
		return
	}
	switch chosen {
	case moreFonts:
		a.openFontPicker(lyric)
		return
	case followUIFont:
		*id = ""
	default:
		for _, f := range fonts {
			if f.label == chosen {
				*id = f.id
				if lyric && f.id == "" {
					// 默认 for the lyrics: the default font, not the app's.
					*id = "default"
				}
			}
		}
	}
	a.loadFont()
	a.changed()
}
