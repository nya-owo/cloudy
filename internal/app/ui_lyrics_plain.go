package app

import (
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/lyrics"
)

// plainLyrics shows lyrics without times, as a file's tags may carry them
// (lyrics.IsPlain): all of them alike, to scroll through, none lit and
// none followed, at size, centered when center.
func (a *app) plainLyrics(c *ui.Context, key string, lines []lyrics.Line, size float32, center bool) {
	scroll := ui.Scroll(c.Key(key)).Grow(1).Padding(24, 12, 48, 0)
	col := ui.Column(c).Gap(size * 0.55)
	if center {
		col.AlignItems(ui.Center)
	} else {
		col.AlignItems(ui.Start)
	}
	scroll.Children(func() {
		col.Children(func() {
			ui.Text(c.Key("note"), "歌词无时间轴，不随播放滚动").FontSize(13).TextColor(white.Alpha(0.4))
			for i, l := range lines {
				if l.Text == "" {
					ui.Box(c.Key(i)).Height(size * 0.6)
					continue
				}
				t := ui.Text(c.Key(i), l.Text).FontSize(size).FontWeight(700).LineHeight(1.3).LetterSpacing(-0.3).TextColor(white.Alpha(0.78))
				a.lyricFace(t)
				if center {
					t.TextAlign(ui.Center)
				}
			}
		})
	})
}
