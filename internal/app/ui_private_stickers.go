package app

import (
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

var iconPrivateSmile = strokeIcon(`<circle cx="12" cy="12" r="9"/><path d="M8 14a4 4 0 0 0 8 0M9 10h.01M15 10h.01"/>`)

const (
	privateStickerPanelWidth   = 350
	privateStickerPanelPadding = 12
)

type privateStickerPicker struct {
	open, loading               bool
	groupsLoading, groupsLoaded bool
	groupsErr                   string
	groups                      []netease.PrivateStickerGroup
	pages                       map[string]netease.PrivateStickerPage
	pageRequests                map[string]privateStickerPageRequest
	group, err                  string
	page                        netease.PrivateStickerPage
	pageGroup                   string // The group on screen can lag behind the requested group.
	scroll                      ui.ScrollState
	emoji, smallEmoji           bool
	emojiCaret                  *int
}

func privateFavoriteStickerGroup(id string) bool {
	return id == "1" || id == "-2"
}

func privateStickerGroupName(group netease.PrivateStickerGroup) string {
	if group.Name == "" {
		return "表情"
	}
	return group.Name
}

func (p *privateStickerPicker) title() string {
	if p.smallEmoji {
		return privateSmallEmojiTitle
	}
	if p.emoji {
		return "Emoji"
	}
	for _, group := range p.groups {
		if group.ID.String() == p.group {
			return privateStickerGroupName(group)
		}
	}
	return "表情"
}

func (a *app) privateStickerButton(c *ui.Context, chat *privateChat, ink paneInk) {
	p := &a.dm.stickers
	button := iconButton(c.Key("dm-stickers"), iconPrivateSmile, 18, ink.muted, "网易云表情").Size(32, 32).Disabled(chat.sending)
	button.OnClick(func() {
		p.open = !p.open
		if !p.open || p.loading {
			return
		}
		if p.group == "" && len(p.groups) != 0 {
			a.selectPrivateStickerGroup(p.groups[0].ID.String())
		}
	})
	const key = "dm-sticker-picker"
	k := a.paneProgress(c, key, p.open)
	if k == 0 && !p.open {
		return
	}
	content := func(panel ui.Element) {
		a.pane(panel, 20, false)
		panel.AttachTo(button, ui.AnchorTopStart, ui.AnchorBottomStart).Size(privateStickerPanelWidth, 310).
			Padding(privateStickerPanelPadding).Gap(8).
			TextColor(ink.text).TransformOrigin(0, 1)
		if p.open {
			panel.Label("选择网易云表情")
		}
		a.stylePaneProgress(panel, k)
		title := p.title()
		ui.Text(c.Key("sticker-title"), title).FontSize(16).FontWeight(600).Shrink(0).
			SingleLine().Label("表情分组：" + title)
		if p.emoji || p.smallEmoji {
			a.privateEmojiGrid(c, chat, ink)
			a.privateStickerTabs(c, ink)
			return
		}
		ui.Scroll(c.Key("sticker-grid")).Grow(1).MinHeight(0).Gap(5).TrackScroll(&p.scroll).Children(func() {
			passive := !p.open || p.loading && p.pageGroup != p.group
			ui.Row(c.Key("sticker-tiles")).Wrap().Gap(5).AlignItems(ui.Center).Children(func() {
				for _, sticker := range p.page.Items {
					a.privateStickerTile(c, chat, sticker, passive, ink)
				}
			})
			err := p.err
			if p.group == "" {
				err = p.groupsErr
			}
			switch {
			case p.loading || p.group == "" && p.groupsLoading:
				ui.Text(c, "正在加载表情…").FontSize(12).TextColor(ink.muted)
			case err != "":
				ui.Text(c, err).FontSize(12).TextColor(ink.muted)
				textLink(c.Key("sticker-retry"), "重试", ink.text).Label("重试加载表情").OnClick(func() {
					if !p.open {
						return
					}
					if p.group == "" {
						a.loadPrivateStickerGroups()
					} else {
						a.loadPrivateStickerPage()
					}
				})
			case p.page.Cursor != "":
				// A short page can have a cursor without providing anything to scroll.
				textLink(c.Key("sticker-more"), "加载更多表情", ink.text).OnClick(func() {
					if p.open {
						a.loadPrivateStickerPage()
					}
				})
				if p.open && p.scroll.Y > 0 && p.scroll.MaxY-p.scroll.Y <= 20 {
					a.loadPrivateStickerPage()
				}
			case len(p.page.Items) == 0:
				ui.Text(c, "暂无可用表情").FontSize(12).TextColor(ink.muted)
			}
		})
		a.privateStickerTabs(c, ink)
	}
	if p.open {
		ui.PopoverBase(c.Key(key), button, &p.open, content)
	} else {
		// Keep the anchored surface in front of the dialog without the popover's dismissal scope.
		ui.Overlay(c, func() {
			panel := ui.Box(c.Key(key)).PassThrough()
			panel.Children(func() { content(panel) })
		})
	}
}

func (a *app) privateStickerTile(c *ui.Context, chat *privateChat, sticker netease.PrivateSticker, passive bool, ink paneInk) {
	size, imageSize := float32(52), float32(42)
	if sticker.Width > 0 && sticker.Height > 0 && max(sticker.Width, sticker.Height) <= 64 {
		size, imageSize = 40, 30
	}
	key := c.Key(sticker.ID.String())
	var cell ui.Element
	if passive {
		// Keep the artwork at full opacity without a send action.
		cell = ui.Row(key).Shrink(0)
	} else {
		label := "发送表情"
		if sticker.Name != "" {
			label += "：" + sticker.Name
		}
		cell = ui.ButtonBase(key).Label(label).Tooltip(label).Disabled(chat.sending)
	}
	cell.Size(size, size).Center().Radius(8)
	if !passive && cell.Hovered() {
		cell.Background(ink.hover)
	}
	cell.Children(func() { sticker.URL = a.privateStickerImage(c, sticker, imageSize, false) })
	if passive {
		return
	}
	cell.Disabled(chat.sending || sticker.URL == "")
	cell.OnClick(func() { a.sendPrivateSticker(chat, sticker, c.Now()) })
}

func (a *app) privateStickerTabs(c *ui.Context, ink paneInk) {
	ui.Box(c.Key("sticker-tabs-divider")).Height(1).Shrink(0).AlignSelf(ui.Stretch).
		MarginX(-privateStickerPanelPadding).Background(ink.line)
	p := &a.dm.stickers
	const size, gap = 40, 6
	const width = privateStickerPanelWidth - 2*privateStickerPanelPadding
	tabButton := func(key, name, hint string) ui.Element {
		if !p.open {
			return ui.Row(c.Key(key)).Size(size, size).Shrink(0).Center().TextColor(ink.muted)
		}
		return ui.ButtonBase(c.Key(key)).Size(size, size).Radius(8).
			Label(name).Tooltip(hint).TextColor(ink.muted)
	}
	tabContent := func(tab ui.Element, selected bool, content func()) {
		if selected {
			tab.TextColor(ink.text)
		}
		tab.Children(func() {
			if selected {
				pill := ui.Box(c.Key("dm-sticker-tab-pill")).Absolute().Top(0).Left(0).Right(0).Bottom(0).
					Radius(8).PassThrough().Background(ink.hover)
				if a.motion == motionFull {
					pill.Transition(ui.ElementTransition{Animation: &motionPane, Match: dmContactPillMatch{stickers: true}})
				}
			}
			// Keep the glyph above the absolute selection background.
			ui.Row(c.Key("dm-sticker-tab-icon")).Absolute().Top(0).Left(0).Right(0).Bottom(0).
				Center().PassThrough().Children(content)
		})
	}
	remoteTab := func(group netease.PrivateStickerGroup, favorites bool) {
		id, name := group.ID.String(), privateStickerGroupName(group)
		var bmp *ui.Bitmap
		hint := name
		if !favorites {
			var detail string
			bmp, detail = a.privateStickerTabBitmap(id)
			if detail != "" {
				hint += " · " + detail
			}
		}
		tab := tabButton(id, name, hint)
		tabContent(tab, !p.emoji && !p.smallEmoji && id == p.group, func() {
			if favorites {
				ui.Icon(c, iconHeart).Size(22, 22).TextColor(ink.text)
			} else if bmp != nil {
				ui.Image(c, bmp).Size(26, 26).Fit(ui.Contain)
			} else {
				ui.Box(c).Size(16, 16).Radius(4).Background(ink.hover).Border(1, ink.muted)
			}
		})
		if p.open {
			tab.OnClick(func() {
				p.emoji, p.smallEmoji = false, false
				if p.group != id {
					a.selectPrivateStickerGroup(id)
				}
			})
		}
	}
	tabs := func() {
		emoji := tabButton("local-emoji", "Emoji 分类", "Emoji")
		tabContent(emoji, p.emoji, func() { ui.Icon(c, iconPrivateSmile).Size(22, 22).TextColor(ink.text) })
		if p.open {
			emoji.OnClick(func() { p.emoji, p.smallEmoji = true, false })
		}
		for _, group := range p.groups {
			if privateFavoriteStickerGroup(group.ID.String()) {
				remoteTab(group, true)
			}
		}
		small := tabButton("local-small-emoji", privateSmallEmojiTitle, privateSmallEmojiTitle)
		tabContent(small, p.smallEmoji, func() {
			marker := privateSmallEmojiMarkers[0]
			var bmp *ui.Bitmap
			if p.open {
				bmp = a.privateSmallEmojiBitmap(marker)
			} else {
				bmp = bitmap(a.art.peek(privateSmallEmojiURLs[marker]))
			}
			if bmp != nil {
				ui.Image(c, bmp).Size(26, 26).Fit(ui.Contain)
			} else {
				ui.Text(c, "多").FontSize(16).FontWeight(600).SingleLine()
			}
		})
		if p.open {
			small.OnClick(func() { p.emoji, p.smallEmoji = false, true })
		}
		for _, group := range p.groups {
			if !privateFavoriteStickerGroup(group.ID.String()) {
				remoteTab(group, false)
			}
		}
	}
	if (len(p.groups)+2)*size+(len(p.groups)+1)*gap <= width {
		ui.Row(c.Key("sticker-groups")).Height(size).Shrink(0).Gap(gap).Justify(ui.Start).Children(tabs)
	} else {
		ui.ScrollHorizontal(c.Key("sticker-groups")).Height(size).Shrink(0).Gap(gap).Justify(ui.Start).Children(tabs)
	}
}

func (a *app) privateStickerTabBitmap(id string) (*ui.Bitmap, string) {
	page, visited := a.dm.stickers.pages[id]
	if !visited {
		return nil, "封面等待贴图加载"
	}
	for _, sticker := range page.Items {
		// The page may outlive its artwork in the shared cache.
		art, _ := a.privateStickerArtwork(sticker, false)
		if bmp := bitmap(art); bmp != nil {
			return bmp, ""
		}
		if art == nil {
			return nil, "封面等待贴图加载"
		}
	}
	return nil, "封面暂不可用"
}

func (a *app) privateStickerArtwork(sticker netease.PrivateSticker, animate bool) (*artwork, string) {
	src := sticker.URL
	art := a.art.getSticker(src, animate)
	if art != nil && art.failed && sticker.AlternateURL != "" {
		src = sticker.AlternateURL
		art = a.art.getSticker(src, animate)
	}
	return art, src
}

// Return the same official image URL the picker is showing.
func (a *app) privateStickerImage(c *ui.Context, sticker netease.PrivateSticker, size float32, animate bool) string {
	art, src := a.privateStickerArtwork(sticker, animate)
	bmp := bitmap(art)
	label := "表情"
	if sticker.Name != "" {
		label += "：" + sticker.Name
	}
	if !animate && bmp != nil {
		ui.Image(c.Key("sticker-image"), bmp).Size(size, size).Fit(ui.Contain).Label(label)
		return src
	}
	box := ui.Box(c.Key("sticker-image")).Size(size, size).Center().Role(ui.RoleImage).Label(label)
	if bmp == nil {
		if animate {
			box.Jump("art", 0)
		}
		box.Radius(8).Background(a.pal.hover)
		if art != nil && art.failed {
			box.Role(ui.RoleGroup).Label("图片加载失败").Children(func() {
				ui.Text(c, "加载失败").FontSize(10).TextColor(a.pal.muted)
			})
			return ""
		}
		return src
	}
	in := float32(1)
	if a.motion != motionOff {
		in = artIn(box, bmp)
	} else {
		box.Jump("art", 1)
	}
	if in < 1 {
		box.Radius(8).Background(a.pal.hover.Alpha(1 - in))
	}
	if in == 0 {
		return src
	}
	box.Children(func() {
		if animate && art.sticker != nil && a.motion != motionOff {
			ui.Box(c).Size(size, size).Opacity(in).Draw(func(p *ui.Painter, r ui.Rect) {
				// Keep the cache entry alive for as long as its frames are drawn.
				art.sticker.paint(p, r)
			})
		} else {
			ui.Image(c, bmp).Size(size, size).Fit(ui.Contain).Opacity(in)
		}
	})
	return src
}

// Use the server history row for a sent sticker, so it only enters once.
func (a *app) sendPrivateSticker(chat *privateChat, sticker netease.PrivateSticker, at time.Time) {
	d := &a.dm
	id := chat.user.UserID
	if !d.open || d.selected != id || chat.sending {
		return
	}
	d.stickers.open, chat.sending = false, true
	account, epoch, ctx, client := a.accountGen, d.epoch, d.ctx, d.client
	go func() {
		err := client.SendSticker(ctx, id, sticker)
		a.post(func() { a.privateStickerAnswered(account, epoch, id, err) })
	}()
}

func (a *app) privateStickerAnswered(account, epoch int, id int64, err error) {
	if !a.privateRequestCurrent(account, epoch) {
		return
	}
	chat := a.dm.chats[id]
	chat.sending = false
	if err != nil {
		a.toast = "表情发送未确认，请先刷新会话"
		return
	}
	a.refreshPrivateAfterSend(chat)
}
