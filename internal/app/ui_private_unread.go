package app

import (
	"strconv"

	"github.com/egoist/mygo/ui"
)

// One quiet NetEase-style badge for conversations.
func privateUnreadBadge(c *ui.Context, count int, label string) {
	if count <= 0 {
		return
	}
	text := strconv.Itoa(count)
	if count >= 100 {
		text = "99+"
	}
	ui.Box(c).MinWidth(18).Height(18).PaddingX(4).Shrink(0).Radius(999).
		Background(dangerColor).Center().Label(label).PassThrough().Children(func() {
		// Digits have no descenders: optically center their ink rather than
		// the font's line box, which reserves space below the baseline.
		ui.Text(c, text).FontSize(10.5).FontWeight(600).LineHeight(1).TextAlign(ui.Center).
			TextColor(ui.RGB(255, 255, 255)).SingleLine().Translate(0, -0.5)
	})
}
