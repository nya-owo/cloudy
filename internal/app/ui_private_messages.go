package app

import (
	"fmt"

	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

func (a *app) privateMessagesPanel(c *ui.Context) {
	d := &a.dm
	p := &d.presentation
	open := d.open
	w, h := c.Size()
	ink := a.inkOn(false)
	mounted := a.dialogProgress(c, "private-messages", 0.3, &open, func(panel ui.Element, k float32, dismissed bool) {
		if dismissed {
			a.requestPrivateClose()
		}
		p.admit(c, k, d.open, a.motion)
		panel.Width(min(940, w-48)).Height(max(h-72, 320)).TextColor(ink.text).Label("私信").TransformOrigin(0, 1)
		a.privateMessagePane(c, panel, func() {
			if !p.contents {
				return
			}
			ui.Column(c.Key("dm-visible-content")).Grow(1).MinHeight(0).Children(func() {
				ui.Row(c).Height(68).Padding(0, 20, 0, 34).Gap(10).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "私信").FontSize(20).FontWeight(700)
					ui.Box(c).Grow(1)
					if iconButton(c.Key("close-dm"), iconX, 16, ink.muted, "关闭私信").Clicked() {
						a.requestPrivateClose()
					}
				})
				if a.profile == nil {
					ui.Column(c).Grow(1).Center().Gap(16).Children(func() {
						ui.Text(c, "登录后查看私信").TextColor(ink.muted)
						if primaryButton(c, "登录", nil, a.pal, false).Clicked() {
							a.openLogin()
						}
					})
					return
				}
				ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
					a.privateContacts(c, min(264, (w-48)*0.34))
					ui.Box(c).Width(0.5).Background(ink.line)
					if p.chat || d.selected == 0 {
						a.privateChatBody(c)
					} else {
						ui.Box(c.Key("dm-chat-wait")).Grow(1).MinWidth(0)
					}
				})
			})
		})
	})
	if !mounted {
		*p = privatePresentation{}
	}
}

func (a *app) privateContacts(c *ui.Context, width float32) {
	d := &a.dm
	ink := a.inkOn(false)
	ui.Column(c.Key("dm-contacts")).Width(width).Shrink(0).Padding(2, 12, 16, 22).Gap(8).Children(func() {
		ui.Row(c).Height(36).PaddingX(12).Gap(8).Radius(999).AlignItems(ui.Center).Background(ink.hover).Border(0.5, ink.line).Children(func() {
			ui.Icon(c, iconSearch).Size(15, 15).TextColor(ink.muted)
			ui.TextInputBase(c.Key("dm-query"), &d.query).Bind(&d.searchBox).Grow(1).MinWidth(0).FontSize(13).Label("搜索会话").Placeholder("搜索会话")
		})
		ui.Scroll(c.Key("dm-contact-list")).TrackScroll(&d.listScroll).Grow(1).MinHeight(0).Gap(4).Children(func() {
			query := strings.ToLower(strings.TrimSpace(d.query))
			viewport := d.listScroll.Bounds(c)
			// Elements stay local to this build, never in persistent app state.
			var entering []ui.Element
			var contactKeys []privateMessageIdentity
			var keys []privateMessageIdentity
			laidOut := viewport.H > 0
			shown := 0
			for _, contact := range d.contacts {
				name := privateDisplayName(contact.User.Nickname)
				if !strings.Contains(strings.ToLower(name), query) {
					continue
				}
				shown++
				row := ui.ButtonBase(c.Key(contact.User.UserID)).Padding(10, 8).Radius(14).Gap(10).AlignItems(ui.Center).FocusRing(false).Label("会话："+name).Border(0.5, ui.Transparent)
				selected := d.selected == contact.User.UserID
				if !selected && row.Hovered() {
					row.Background(ink.hover)
				}
				key := privateMessageIdentity{id: contact.User.UserID}
				entering = append(entering, row)
				contactKeys = append(contactKeys, key)
				bounds := row.LayoutBounds()
				laidOut = laidOut && bounds.H > 0
				inViewport := viewport.H > 0 && bounds.H > 0 && bounds.Y < viewport.Y+viewport.H && bounds.Y+bounds.H > viewport.Y
				if inViewport {
					keys = append(keys, key)
				}
				row.Children(func() {
					if selected {
						pill := ui.Box(c.Key("dm-contact-pill")).Absolute().Top(0).Left(0).Right(0).Bottom(0).Radius(14).PassThrough().Background(ink.hover).Border(0.5, ink.line)
						if a.motion == motionFull {
							pill.Transition(ui.ElementTransition{Animation: &motionPane, Match: dmContactPillMatch{}})
						}
					}
					a.privateAvatar(c, contact.User, 38, ink.hover, inViewport)
					ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
						ui.Text(c, name).FontSize(13).FontWeight(600).SingleLine()
						ui.Text(c, contact.Summary).FontSize(11.5).TextColor(ink.muted).SingleLine()
						if contact.Time > 0 {
							ui.Text(c, privateMessageTime(contact.Time)).FontSize(10.5).TextColor(ink.faint)
						}
					})
					if chat := d.chats[contact.User.UserID]; chat != nil && chat.unread > 0 {
						privateUnreadBadge(c, chat.unread, "会话未读私信")
					}
				})
				if row.Clicked() {
					a.selectPrivateChat(contact.User)
				}
			}
			if shown > 0 && !laidOut {
				c.AnimationFrame() // Measure new rows before looking up their avatars.
			}
			a.preparePrivateContactsIn(c, query, keys, laidOut)
			for i, row := range entering {
				shown := d.contactsIn.progress(c, contactKeys[i], a.motion)
				if d.contactsIn.pending && a.motion != motionOff {
					shown = 0
				}
				a.privateEntranceStyle(row, shown, 8)
			}
			if d.err != "" {
				ui.Text(c, d.err).FontSize(12).TextColor(ink.muted).LineHeight(1.4)
				if textLink(c.Key("dm-list-retry"), "重试会话", ink.text).Disabled(d.loading).Clicked() {
					a.loadPrivateContacts(false)
				}
			} else if d.loaded && shown == 0 {
				label := "暂无私信"
				if query != "" {
					label = "没有匹配的会话"
				}
				ui.Text(c, label).FontSize(12).TextColor(ink.muted)
			}
			if d.more && textLink(c.Key("dm-more-contacts"), "更多会话", ink.text).Disabled(d.loading).Clicked() {
				a.loadPrivateContacts(true)
			}
		})
	})
}

func (a *app) privateChatBody(c *ui.Context) {
	d := &a.dm
	ink := a.inkOn(false)
	chat := d.chats[d.selected]
	ui.Column(c.Key("dm-body")).Grow(1).Shrink(1).MinWidth(0).MinHeight(0).Padding(0, 24, 20, 20).Gap(12).Children(func() {
		if chat == nil {
			ui.Column(c).Grow(1).Center().Children(func() { ui.Text(c, "选择一个会话开始聊天").FontSize(14).TextColor(ink.muted) })
			return
		}
		ui.Column(c.Key("dm-viewport")).Grow(1).MinWidth(0).MinHeight(0).Clip().Children(func() {
			ui.Column(c.Key(chat.user.UserID)).Grow(1).MinWidth(0).MinHeight(0).Gap(12).
				Children(func() { a.privateChatContent(c, chat) })
		})
		t := c.Theme()
		composer := ui.TextAreaBase(c.Key(fmt.Sprintf("dm-compose-%d", chat.user.UserID)), &chat.draft).
			Bind(&d.composer).Label("私信内容").Placeholder("输入消息…").
			Padding(t.Space(1.5), t.Space(2.5)).Height(80).MinHeight(t.Space(20)).Radius(14).
			Background(t.Surface).Border(1, t.Border).FocusRing(false).FontSize(13)
		if caret := d.stickers.emojiCaret; caret != nil {
			composer.SetTextSelection(*caret, *caret)
			d.stickers.emojiCaret = nil
		}
		// The platform owns IME keys. The pinned MyGo can still deliver a
		// plain Enter during marked text, so leave that composition intact.
		composing := composer.Composing()
		composer.OnTextKey(func(k ui.TextKey) bool {
			if k.Key != ui.KeyEnter || k.Mods != 0 {
				return false
			}
			if !composing {
				chat.draft = k.Text()
				a.sendPrivateMessage(time.Now())
				if chat.draft == "" {
					k.SetText("")
				}
			}
			return true
		})
		ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
			a.privateStickerButton(c, chat, ink)
			ui.Text(c, fmt.Sprintf("%d / 1000 · Enter 发送 · Shift+Enter 换行", len([]rune(chat.draft)))).FontSize(10.5).TextColor(ink.faint).Grow(1).Shrink(1).MinWidth(0)
			primaryButton(c.Key("dm-send"), "发送", nil, a.pal, chat.sending || strings.TrimSpace(chat.draft) == "" || len([]rune(chat.draft)) > 1000).OnClick(func() {
				a.sendPrivateMessage(c.Now())
				d.composer.Focus(c)
			})
		})
	})
}

func (a *app) privateChatContent(c *ui.Context, chat *privateChat) {
	ink := a.inkOn(false)
	historyReady := chat.historyRows != nil && chat.historyCount == len(chat.messages)
	if historyReady && chat.loaded && a.privateReading(chat.user.UserID) && chat.history.AtEnd() {
		if (chat.unread > 0 || chat.hasNewTail) && a.privateChatReadReady(chat) {
			chat.clearPrivateUnread()
			c.After(time.Millisecond)
		}
	}
	ui.Row(c).Height(46).Gap(10).AlignItems(ui.Center).Children(func() {
		a.privateAvatar(c, chat.user, 36, ink.hover, true)
		ui.Text(c, privateDisplayName(chat.user.Nickname)).FontSize(14).FontWeight(600).SingleLine().Grow(1).Shrink(1).MinWidth(0)
		if (chat.unread > 0 || chat.hasNewTail) && textLink(c.Key("dm-new-messages"), "有新消息", a.pal.accent).Clicked() {
			chat.history.ScrollToEnd()
		}
		if textLink(c.Key("dm-profile"), "查看主页", ink.muted).Clicked() {
			id := chat.user.UserID
			a.closePrivateMessages()
			a.open(route{page: pageUser, id: id})
		}
		refresh := iconButton(c.Key("dm-refresh-history"), iconRepeat, 14, ink.muted, "刷新消息").Disabled(chat.loading)
		if chat.err != "" {
			refresh.Tooltip(chat.err)
		}
		if refresh.Clicked() {
			a.loadPrivateHistory(false)
		}
	})
	chat.prepareHistoryRows()
	entries := chat.historyRows // an inline retry/older action may invalidate the next build
	a.preparePrivateMessagesIn(c, chat, historyReady)
	if chat.bottom {
		chat.history.ScrollToEnd()
		chat.bottom = false
	}
	chat.history.FollowAnimation = nil
	if a.motion == motionFull {
		chat.history.FollowAnimation = &privateHistoryFollow
	}
	list := ui.List(c.Key(fmt.Sprintf("dm-history-%d", chat.user.UserID)), &chat.history, len(entries)).
		Label("私信消息列表").Grow(1).MinHeight(0).Gap(12).Padding(4, 0).
		ItemKey(func(i int) any { return entries[i].key })
	if a.dm.messagesIn.pending && (len(chat.messages) > 0 || len(chat.outgoing) > 0) {
		list.Opacity(0) // Measure the tail without flashing the previous viewport.
	}
	list.Rows(func(item ui.ListRow) {
		cc := item.Context
		entry := entries[item.Index]
		switch entry.key.kind {
		case privateHistoryOlder:
			if textLink(cc.Key("dm-older"), "加载更早消息", ink.muted).Disabled(chat.loading).Clicked() {
				a.loadPrivateHistory(true)
			}
		case privateHistoryTime:
			ui.Text(cc, privateMessageTime(entry.time)).FontSize(11).TextColor(ink.faint).TextAlign(ui.Center)
		case privateHistoryError:
			ui.Column(cc).Gap(8).Children(func() {
				ui.Text(cc, chat.err).FontSize(12).TextColor(ink.muted)
				if textLink(cc.Key("dm-history-retry"), "重试消息", ink.text).Disabled(chat.loading).Clicked() {
					a.loadPrivateHistory(false)
				}
			})
		case privateHistoryEmpty:
			ui.Text(cc, "暂无消息").FontSize(13).TextColor(ink.muted)
		case privateHistoryEnd:
			ui.Box(cc).Height(1)
		case privateHistoryOutgoing:
			a.privateOutgoingRow(cc, entry.outgoing, &chat.history)
		case privateHistoryMessage:
			msg := chat.messages[entry.message].WithContentCache()
			chat.messages[entry.message] = msg
			own := msg.From.UserID == a.profile.UserID
			row := ui.Row(cc.Key(entry.key.message)).AlignItems(ui.Start)
			a.privateMessageMotion(cc, row, chat, msg)
			row.Children(func() {
				if own {
					ui.Box(cc).Grow(1)
				}
				if sticker := msg.Content().Sticker; sticker != nil {
					a.privateStickerImage(cc, *sticker, 112, true)
				} else {
					a.privateMessageBubble(cc, msg, own)
				}
				if !own {
					ui.Box(cc).Grow(1)
				}
			})
		}
	})
	if !historyReady && chat.loaded && a.privateReading(chat.user.UserID) {
		// Check after the new list has committed its tail, including when
		// reduced motion and zero local unread would otherwise leave it idle.
		c.After(time.Millisecond)
	} else {
		a.markPrivateRead(chat)
	}
}
func privateMessageTime(ms int64) string { return time.UnixMilli(ms).Format("01-02 15:04") }

func privateDisplayName(nickname string) string {
	if strings.TrimSpace(nickname) == "" {
		return "私信联系人"
	}
	return nickname
}
