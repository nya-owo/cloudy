package app

import (
	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

func (a *app) privateMessageBubble(c *ui.Context, msg netease.PrivateMessage, own bool) {
	content := msg.Content()
	if content.Media != nil {
		media := a.privateMedia(*content.Media)
		content.Media = &media
	}
	ink := a.inkOn(false)
	bg := ink.hover
	if own {
		bg = a.pal.accent.Alpha(0.16)
	}
	bubble := ui.Column(c).MaxWidth(420).Shrink(1).MinWidth(0).Padding(10, 14).Radius(14).Gap(10).Background(bg)
	if content.Media != nil {
		bubble.Width(320)
	}
	bubble.Children(func() {
		if media := content.Media; media != nil {
			title := media.Title
			if title == "" {
				title = media.Label()
			}
			ui.Row(c.Key("media")).Gap(12).AlignItems(ui.Center).Role(ui.RoleGroup).Label(media.Label() + "：" + title).Children(func() {
				a.privateMediaCover(c, *media)
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
					ui.Text(c, media.Label()).FontSize(10.5).TextColor(ink.faint)
					ui.Text(c, title).FontSize(13).FontWeight(600).MaxLines(2).LineHeight(1.3)
					if media.Subtitle != "" {
						ui.Text(c, media.Subtitle).FontSize(11.5).TextColor(ink.muted).MaxLines(2).LineHeight(1.3)
					}
				})
			})
		}
		if content.Text != "" {
			a.privateMessageText(c, content.Text)
		} else if !content.Supported() {
			ui.Text(c, content.Summary()).FontSize(13).LineHeight(1.5)
		}
	})
}

func (a *app) privateMediaCover(c *ui.Context, media netease.PrivateMessageMedia) {
	cover := a.cover(c, media.CoverURL, 120, 56, 8).Shrink(0).Label("分享封面")
	if !privateMediaPlayable(media) {
		cover.Tooltip("此分享缺少可播放的资源编号")
		return
	}
	hovered := cover.Hovered()
	label := "播放" + media.Label()
	cover.Children(func() {
		a.coverPlayControl(c.Key("play"), hovered, 30, label).
			Absolute().Top(13).Left(13).Tooltip(label).
			OnClick(func() { a.playPrivateMedia(media) })
	})
}
