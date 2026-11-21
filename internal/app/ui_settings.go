package app

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type settingsCategory int

const (
	settingsAccount settingsCategory = iota
	settingsPlayback
	settingsNowPlaying
	settingsLyrics
	settingsAppearance
	settingsHome
	settingsStorage
	settingsSystem
	settingsAbout
	settingsCategoryCount
)

// settingsCategories are the settings' destinations, in the order the
// sidebar lists them, the account first, as a card of its own: gap sets
// one apart from those above it, and blurb says under its name what it
// holds.
var settingsCategories = [...]struct {
	label string
	gap   bool
	icon  *ui.SVG
	blurb string
}{
	{"账号", false, iconUser, "网易云账号和听歌记录。"},
	{"播放", false, iconHeadset, "音质、声音和定时关闭。"},
	{"播放页", false, iconSparkles, "背景、光效和歌词排版，改动会立刻出现在下面的预览里。"},
	{"歌词", false, iconLyrics, "歌词显示什么，以及桌面歌词。"},
	{"外观", true, iconSun, "主题、颜色和窗口材质。"},
	{"首页与侧栏", false, iconHome, "首页台历和侧边栏。"},
	{"下载与缓存", true, iconDownload, "下载、本地音乐和歌曲缓存。"},
	{"快捷键与窗口", false, iconKeyboard, "快捷键、开机启动和窗口行为。"},
	{"关于", true, iconInfo, "版本、更新和反馈。"},
}

var (
	iconSparkles = strokeIcon(`<path d="M12 3l1.8 4.7L18.5 9.5l-4.7 1.8L12 16l-1.8-4.7L5.5 9.5l4.7-1.8z"/><path d="M19 15l.8 2.2L22 18l-2.2.8L19 21l-.8-2.2L16 18l2.2-.8z"/>`)
	iconKeyboard = strokeIcon(`<rect x="2.5" y="6" width="19" height="12" rx="2.5"/><path d="M6.5 10h.01M10 10h.01M14 10h.01M17.5 10h.01M8 14h8"/>`)
	iconExternal = strokeIcon(`<path d="M14 4h6v6"/><path d="M20 4l-9 9"/><path d="M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5"/>`)
	iconWarn     = strokeIcon(`<path d="M12 4 2.5 20h19z"/><path d="M12 10v4.5M12 17.5h.01"/>`)
)

// warnColor is the color of a word of caution under a setting.
var warnColor = ui.Hex("#e0a84e")

// dangerColor is the color of a button that deletes what it names.
var dangerColor = ui.Hex("#e5484d")

// openSettings shows the settings over the main window, where its glass has
// the app to show through: a window of its own would have nothing under it.
func (a *app) openSettings() {
	a.measureCache()
	a.dl.pathDraft = a.downloadFolder()
	a.settingsView.query = ""
	a.settingsView.open = true
	a.showMain()
	a.redraw()
}

// openSettingsAt shows the settings at a category.
func (a *app) openSettingsAt(category settingsCategory) {
	a.settingsView.tab = category
	a.openSettings()
}

// changed applies the settings everywhere and keeps them.
func (a *app) changed() {
	a.settings.save()
	if a.win != nil {
		// The theme source reaches the app's loop, which a view built in a
		// test has none of.
		a.settings.applyAppearance()
		if material := a.settings.mainWindowMaterial(); material != a.windowMaterial {
			a.windowMaterial = material
			a.win.SetVibrancy(material)
		}
	}
	if a.notch != nil {
		a.notch.setEnabled(a.settings.Notch)
	}
	a.redraw()
}

// settingsPanel is the settings, a pane over the window while they are open.
func (a *app) settingsPanel(c *ui.Context) {
	w, h := c.Size()
	ink := a.inkOn(false)
	a.dialogAt(c, "settings", 0.3, &a.settingsView.open, 0, 1, func(panel ui.Element) {
		// It grows from the corner of who listens, whose row opens it.
		panel.Width(min(940, w-48)).Height(max(h-72, 320)).TextColor(ink.text).Label("设置")
		a.pane(panel, 26, false)
		panel.Children(func() {
			ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
				a.settingsSidebar(c)
				a.settingsBody(c)
			})
			ui.Box(c).Absolute().Top(16).Right(16).Children(func() {
				if iconButton(c.Key("close"), iconX, 14, ink.muted, "关闭").Background(ink.hover).Clicked() {
					a.settingsView.open = false
				}
			})
		})
	})
}

func (a *app) globalGlassDialog(c *ui.Context) {
	sv := &a.settingsView
	if !sv.open {
		sv.confirmGlass = false
	}
	a.dialog(c, "confirm-global-glass", 0.25, &sv.confirmGlass, func(panel ui.Element) {
		ink := a.inkOn(false)
		panel.Width(410).TextColor(ink.text).Label("全局液态玻璃确认")
		a.pane(panel, 26, false)
		panel.Children(func() {
			dialogHeader(c, ink, "开启全局液态玻璃？", "", func() { sv.confirmGlass = false })
			ui.Row(c).PaddingX(28).Gap(8).AlignItems(ui.Start).Children(func() {
				ui.Icon(c, iconWarn).Size(16, 16).Shrink(0).TextColor(warnColor)
				ui.Text(c, "实时折射会增加性能开销，部分背景下文字可能略难读。").
					FontSize(13).LineHeight(1.4).TextColor(ink.muted).Shrink(1).MinWidth(0)
			})
			dialogFooter(c, func() {
				if settingButton(c.Key("cancel"), ink, "取消", nil, false, false).Clicked() {
					sv.confirmGlass = false
				}
				if primaryButton(c.Key("confirm"), "确认开启", nil, a.pal, false).Clicked() {
					sv.confirmGlass = false
					a.settings.GlobalGlass = true
					a.leavePowerSaver()
					a.changed()
				}
			})
		})
	})
}

// settingsSidebar keeps the destinations, and the search over all of them,
// visible while their contents scroll.
func (a *app) settingsSidebar(c *ui.Context) {
	ink := a.inkOn(false)
	searching := strings.TrimSpace(a.settingsView.query) != ""
	line := ink.line
	ui.Column(c.Key("settings-nav")).Width(224).Shrink(0).Padding(22, 12, 16, 14).Gap(2).Label("设置分类").Draw(func(p *ui.Painter, r ui.Rect) {
		// A hairline between it and what it lists.
		p.Fill(ui.Rect{X: r.X + r.W - 1, Y: r.Y + 20, W: 1, H: r.H - 40}, line, 0)
	}).Children(func() {
		field := ui.Row(c.Key("settings-search")).Height(34).PaddingX(10).Gap(8).Radius(8).AlignItems(ui.Center).
			Background(ink.hover).Margin(0, 0, 12, 0)
		field.Children(func() {
			ui.Icon(c, iconSearch).Size(15, 15).Shrink(0).TextColor(ink.muted)
			in := ui.TextInputBase(c.Key("settings-query"), &a.settingsView.query).Bind(&a.settingsView.searchBox).Grow(1).MinWidth(0).
				FontSize(13).Label("搜索设置").Placeholder("搜索")
			if in.Changed() {
				a.settingsView.searchScroll = ui.ScrollState{}
			}
			if a.settingsView.query != "" {
				if iconButton(c.Key("settings-clear"), iconX, 11, ink.muted, "清除搜索").Size(22, 22).Clicked() {
					a.settingsView.query = ""
				}
			}
		})
		ui.Scroll(c.Key("settings-nav-list")).Grow(1).MinHeight(0).Gap(2).Margin(0, -6, 0, -6).Padding(0, 6).Children(func() { a.settingsNavItems(c, ink, searching) })
	})
}

// settingsNavItems are the categories: who listens first, as a card,
// then the rest, each with its icon on a small square, the accent's
// where it is the one chosen.
func (a *app) settingsNavItems(c *ui.Context, ink paneInk, searching bool) {
	for i, category := range settingsCategories {
		selected := a.settingsView.tab == settingsCategory(i) && !searching
		account := settingsCategory(i) == settingsAccount
		button := ui.ButtonBase(c.Key(category.label)).Gap(10).AlignItems(ui.Center).Justify(ui.Start).
			FocusRing(false).Label("设置分类：" + category.label).Selected(selected)
		if account {
			button.Padding(8, 10).Radius(14).Margin(0, 0, 12, 0)
		} else {
			button.Height(36).PaddingX(8).Radius(8)
			if category.gap {
				button.Margin(12, 0, 0, 0)
			}
		}
		bg, weight := ui.Transparent, 400
		if selected {
			bg, weight = ink.press, 600
		} else if button.Hovered() {
			bg = ink.hover.Alpha(0.6)
		}
		// At once, as the page it names changes at once.
		button.Background(bg).TextColor(ink.text)
		button.Children(func() {
			if account {
				name, sub := "未登录", "登录网易云账号"
				if a.profile != nil {
					name, sub = a.profile.Nickname, "网易云账号"
					a.avatar(c, a.profile.AvatarURL, 42, ink.press)
				} else {
					ui.Box(c).Size(42, 42).Radius(999).Center().Shrink(0).Background(ink.press).Children(func() {
						ui.Icon(c, iconUser).Size(20, 20).TextColor(ink.muted)
					})
				}
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
					ui.Text(c, name).FontSize(15).FontWeight(600).SingleLine()
					ui.Text(c, sub).FontSize(12).TextColor(ink.muted).SingleLine()
				})
				return
			}
			// One color in the whole list: the accent, on the one chosen.
			tile, glyph := ink.hover, ink.muted
			if selected {
				tile, glyph = a.pal.accent, white
			}
			ui.Box(c).Size(24, 24).Radius(8).Center().Shrink(0).Background(tile).Children(func() {
				ui.Icon(c, category.icon).Size(15, 15).TextColor(glyph)
			})
			ui.Text(c, category.label).FontSize(14).FontWeight(weight)
		})
		if button.Clicked() {
			a.settingsView.tab = settingsCategory(i)
			a.settingsView.query = ""
		}
	}
}

// A settingsKit builds the rows of the settings: in groups, on cards, with
// hairlines between. While the listener searches, it builds only the rows
// whose words match, and the groups and categories that have one; to know
// which, a category is first run as a probe, which counts rows and builds
// nothing.
type settingsKit struct {
	a     *app
	c     *ui.Context
	ink   paneInk
	query string // what is searched, lowered; empty while not searching
	// searching is whether the listener searches, while a group whose
	// title matched shows all its rows, query emptied.
	searching bool
	probe     bool // counting the rows that match, building nothing
	// plain is the settings' own way with rows: a row is its name and
	// its control, on one line; what more there is to say of it is said
	// under its card (foot), or as the pointer rests on it, unless it
	// is a caution, or says how things stand (settingRow.keep).
	plain bool
	hits  int // rows that matched in a probe
	rows  int // rows on the card being built
}

// A settingRow is a row of a setting, as it says it.
type settingRow struct {
	label, desc string
	tip         string // said in full as the pointer is over the ⓘ
	warn        bool   // its desc is a word of caution
	badge       string // a word on a capsule beside its label, as 未开放
	sub         bool   // it belongs to the row above it
	keep        bool   // its desc says how things stand: shown whatever the kit
}

func (k *settingsKit) match(texts ...string) bool {
	if k.query == "" {
		return true
	}
	for _, t := range texts {
		if strings.Contains(strings.ToLower(t), k.query) {
			return true
		}
	}
	return false
}

// count runs rows as a probe, saying how many match.
func (k *settingsKit) count(rows func()) int {
	probe, hits := k.probe, k.hits
	k.probe, k.hits = true, 0
	rows()
	n := k.hits
	k.probe, k.hits = probe, hits
	return n
}

// group is a titled card of rows. A search matching its title shows all of
// them.
func (k *settingsKit) group(title string, rows func()) {
	q := k.query
	defer func() { k.query = q }()
	if q != "" && k.match(title) {
		k.query = ""
	}
	if k.probe {
		if q != "" && k.query == "" {
			k.hits++
			return
		}
		rows()
		return
	}
	if k.query != "" && k.count(rows) == 0 {
		return
	}
	top := float32(18)
	if k.plain {
		top = 24
	}
	ui.Text(k.c, title).FontSize(12.5).FontWeight(600).TextColor(k.ink.muted).Margin(top, 0, 8, 16)
	k.card(rows)
}

// cardColors are the fill and edge of a card on the settings' pane.
func (a *app) cardColors() (fill, edge ui.Color) {
	if a.pal.dark {
		return white.Alpha(0.05), white.Alpha(0.09)
	}
	return white.Alpha(0.6), ui.RGBA(0, 0, 0, 0.07)
}

func (k *settingsKit) card(rows func()) {
	n := k.rows
	k.rows = 0
	fill, edge := k.a.cardColors()
	card := ui.Column(k.c).Background(fill)
	if k.plain {
		card.Radius(14)
	} else {
		card.Radius(20).Border(0.5, edge)
	}
	card.Children(rows)
	k.rows = n
}

// foot says under the card above it what its rows leave unsaid: a line
// or two for all of them. Not while searching, where each row says its
// own.
func (k *settingsKit) foot(text string) {
	if k.probe || k.searching || !k.plain {
		return
	}
	ui.Text(k.c, text).FontSize(12).LineHeight(1.45).TextColor(k.ink.muted.Alpha(0.85)).Margin(8, 16, 0, 16)
}

// stateRow is a row whose desc says how things stand, not what it is.
func (k *settingsKit) stateRow(label, desc string, control func()) {
	k.rowOf(settingRow{label: label, desc: desc, keep: true}, control)
}

// row is a setting and its control.
func (k *settingsKit) row(label, desc string, control func()) {
	k.rowOf(settingRow{label: label, desc: desc}, control)
}

// sub is a setting of the one above it, shown while that one is on.
func (k *settingsKit) sub(label, desc string, on bool, control func()) {
	if !on && !k.searching {
		return
	}
	k.rowOf(settingRow{label: label, desc: desc, sub: true}, control)
}

func (k *settingsKit) rowOf(r settingRow, control func()) {
	if !k.match(r.label, r.desc, r.tip) {
		return
	}
	if k.probe {
		k.hits++
		return
	}
	c, ink := k.c, k.ink
	first := k.rows == 0
	k.rows++
	left := float32(20)
	if r.sub {
		left = 48
	}
	line := ink.line.Alpha(0.75)
	sub := r.sub
	if k.plain {
		k.plainRow(r, first, control)
		return
	}
	ui.Row(c).Padding(11, 20, 11, left).Gap(20).AlignItems(ui.Center).MinHeight(62).
		Draw(func(p *ui.Painter, b ui.Rect) {
			if !first {
				p.Fill(ui.Rect{X: b.X + 20, Y: b.Y, W: b.W - 40, H: 1}, line, 0)
			}
			if sub {
				p.Fill(ui.Rect{X: b.X + 30, Y: b.Y + 16, W: 2, H: b.H - 32}, line, 1)
			}
		}).
		Children(func() {
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Text(c, r.label).FontSize(14).FontWeight(500).Shrink(1).MinWidth(0)
					if r.badge != "" {
						ui.Box(c).Height(20).PaddingX(8).Radius(999).Center().Shrink(0).Background(ink.hover).Border(0.5, ink.line).Children(func() {
							ui.Text(c, r.badge).FontSize(11).FontWeight(600).TextColor(ink.muted)
						})
					}
					if r.tip != "" {
						infoTip(c, r.tip, ink)
					}
				})
				if r.desc != "" {
					if r.warn {
						ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
							ui.Icon(c, iconWarn).Size(13, 13).Shrink(0).TextColor(warnColor)
							ui.Text(c, r.desc).FontSize(12.5).TextColor(warnColor).LineHeight(1.4).Shrink(1).MinWidth(0)
						})
					} else {
						ui.Text(c, r.desc).FontSize(12.5).TextColor(ink.muted).LineHeight(1.4)
					}
				}
			})
			ui.Row(c).Gap(8).AlignItems(ui.Center).Shrink(0).Children(control)
		})
}

// plainRow is a row as the settings have them: its name, and its
// control at the end of the line; under its name what it has to say
// only if that is a caution or how things stand, or while searching,
// where it may be what was found.
func (k *settingsKit) plainRow(r settingRow, first bool, control func()) {
	c, ink := k.c, k.ink
	line := ink.line.Alpha(0.75)
	said := r.desc != "" && (r.warn || r.keep || k.searching)
	ui.Row(c).Padding(6, 14, 6, 16).Gap(16).AlignItems(ui.Center).MinHeight(46).
		Draw(func(p *ui.Painter, b ui.Rect) {
			if !first {
				p.Fill(ui.Rect{X: b.X + 16, Y: b.Y, W: b.W - 16, H: 1}, line, 0)
			}
		}).
		Children(func() {
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(3).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					label := ui.Text(c, r.label).FontSize(14).Shrink(1).MinWidth(0)
					if r.sub {
						label.TextColor(ink.muted)
					}
					// Said as the pointer rests on its name, not anywhere on
					// the row.
					if r.desc != "" && !said {
						label.Tooltip(r.desc)
					}
					if r.badge != "" {
						ui.Box(c).Height(20).PaddingX(8).Radius(999).Center().Shrink(0).Background(ink.hover).Children(func() {
							ui.Text(c, r.badge).FontSize(11).FontWeight(600).TextColor(ink.muted)
						})
					}
					if r.tip != "" {
						infoTip(c, r.tip, ink)
					}
				})
				if said {
					color := ink.muted
					if r.warn {
						color = warnColor
					}
					ui.Text(c, r.desc).FontSize(12).TextColor(color).LineHeight(1.4)
				}
			})
			ui.Row(c).Gap(8).AlignItems(ui.Center).Shrink(0).Children(control)
		})
}

// infoTip is an ⓘ that says tip as the pointer is over it.
func infoTip(c *ui.Context, tip string, ink paneInk) {
	ui.Box(c.Key("tip")).Size(16, 16).Center().Shrink(0).Tooltip(tip).Label(tip).Children(func() {
		ui.Icon(c, iconInfo).Size(14, 14).TextColor(ink.faint)
	})
}

// block is a part of a card that is not a row, found by words.
func (k *settingsKit) block(words string, fn func()) {
	if !k.match(words) {
		return
	}
	if k.probe {
		k.hits++
		return
	}
	k.rows++
	fn()
}

// button is a button of the settings: a capsule as the app's others are,
// red when it deletes what it names.
func (k *settingsKit) button(key any, label string, icon *ui.SVG, disabled, danger bool) ui.Element {
	return settingButton(k.c.Key(key), k.ink, label, icon, disabled, danger)
}

func settingButton(c *ui.Context, ink paneInk, label string, icon *ui.SVG, disabled, danger bool) ui.Element {
	b := ui.ButtonBase(c).Height(32).PaddingX(16).Gap(6).Radius(999).AlignItems(ui.Center).Shrink(0).
		FocusRing(false).Label(label).Disabled(disabled)
	bg, fg := ink.hover, ink.text
	if danger {
		fg = dangerColor
	}
	if b.Hovered() && !disabled {
		bg = ink.press
	}
	b.Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond})).Border(0.5, ink.line).TextColor(fg)
	if disabled {
		b.Opacity(0.45)
	}
	pose := ui.Pose{}
	if b.Pressed() && !disabled {
		pose.Scale = 0.96
	}
	b.AnimatePose(pose, ui.Snappy)
	b.Children(func() {
		if icon != nil {
			ui.Icon(c, icon).Size(14, 14)
		}
		ui.Text(c, label).FontSize(13.5).FontWeight(500).SingleLine()
	})
	return b
}

// choose is a select of a setting among labels, calling pick with the one
// chosen.
func (k *settingsKit) choose(key, label, chosen string, labels []string, pick func(i int)) {
	if k.a.paneSelect(k.c.Key(key), label, &chosen, labels) {
		for i, l := range labels {
			if l == chosen {
				pick(i)
			}
		}
	}
}

// toggle is the switch of a setting, calling then as it changes.
func (k *settingsKit) toggle(key, label string, on *bool, disabled bool, then func()) {
	if settingSwitch(k.c.Key(key), on).Label(label).Disabled(disabled).Changed() {
		if then != nil {
			then()
		}
		k.a.changed()
	}
}

// settingsBody shows one category, keeping its scroll position between
// visits, or while the listener searches, what matches in all of them.
func (a *app) settingsBody(c *ui.Context) {
	k := &settingsKit{a: a, c: c, ink: a.inkOn(false), plain: true, query: strings.ToLower(strings.TrimSpace(a.settingsView.query))}
	k.searching = k.query != ""
	key, scroll, label := any(a.settingsView.tab), &a.settingsView.scroll[a.settingsView.tab], settingsCategories[a.settingsView.tab].label
	if k.query != "" {
		key, scroll, label = "search", &a.settingsView.searchScroll, "搜索"
	}
	// Clear of the pane's round corners, above and below, where its
	// scroll bar would stand out past them.
	viewport := ui.Box(c.Key("settings-viewport")).Grow(1).Shrink(1).MinWidth(0).Margin(22, 0, 22, 0)
	// Keep settings text on the final target. An edge Effect resamples the
	// entire viewport, softening even the text well outside its fade band.
	viewport.Children(func() {
		ui.Scroll(c.Key(key)).Label(label+"设置项").TrackScroll(scroll).Fill().Padding(4, 30, 12, 26).Children(func() {
			if k.query == "" {
				cat := settingsCategories[a.settingsView.tab]
				ui.Column(c.Key(a.settingsView.tab)).Children(func() {
					ui.Text(c, cat.label).FontSize(26).FontWeight(700).Margin(0, 40, 0, 4)
					ui.Text(c, cat.blurb).FontSize(13).TextColor(k.ink.muted).Margin(6, 40, 0, 4)
					a.settingsPage(k, a.settingsView.tab)
				})
				return
			}
			ui.Text(c, "搜索结果").FontSize(26).FontWeight(700).Margin(0, 40, 0, 4)
			found := false
			for cat := settingsCategory(0); cat < settingsCategoryCount; cat++ {
				if k.count(func() { a.settingsPage(k, cat) }) == 0 {
					continue
				}
				found = true
				ui.Column(c.Key(cat)).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Margin(22, 0, -10, 4).Children(func() {
						ui.Box(c).Size(22, 22).Radius(8).Center().Shrink(0).Background(k.ink.hover).Children(func() {
							ui.Icon(c, settingsCategories[cat].icon).Size(14, 14).TextColor(k.ink.muted)
						})
						ui.Text(c, settingsCategories[cat].label).FontSize(16).FontWeight(700)
					})
					a.settingsPage(k, cat)
				})
			}
			if !found {
				ui.Column(c).Padding(80, 0).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconSearch).Size(28, 28).TextColor(k.ink.faint)
					ui.Text(c, "没有找到「"+strings.TrimSpace(a.settingsView.query)+"」相关的设置").FontSize(14).TextColor(k.ink.muted)
				})
			}
		})
	})
}

// settingsPage is the groups of a category.
func (a *app) settingsPage(k *settingsKit, category settingsCategory) {
	switch category {
	case settingsAccount:
		a.settingsAccountPage(k)
	case settingsPlayback:
		a.settingsPlaybackPage(k)
		k.group("播客", func() {
			a.podcastSpeedRow(k.c, k.row)
			if a.captionsShown() {
				a.captionSettings(k)
			}
		})
	case settingsNowPlaying:
		a.settingsNowPlayingPage(k)
	case settingsLyrics:
		a.settingsLyricsPage(k)
	case settingsAppearance:
		a.settingsAppearancePage(k)
	case settingsHome:
		a.settingsHomePage(k)
	case settingsStorage:
		a.settingsStoragePage(k)
	case settingsSystem:
		a.settingsSystemPage(k)
	case settingsAbout:
		a.settingsAboutPage(k)
	}
}

func (a *app) settingsAccountPage(k *settingsKit) {
	c, ink, s := k.c, k.ink, &a.settings
	k.group("网易云账号", func() {
		if a.profile == nil {
			k.stateRow("未登录", "登录后，每日推荐与歌单将根据你的口味生成", func() {
				if ui.PrimaryButton(c.Key("login"), "扫码登录").Clicked() {
					a.settingsView.open = false
					a.openLogin()
				}
			})
			return
		}
		prof := a.profile
		k.block("账号 登录 头像 昵称 退出登录 "+prof.Nickname, func() {
			ui.Row(c).Padding(16, 20).Gap(14).AlignItems(ui.Center).Children(func() {
				a.avatar(c, prof.AvatarURL, 52, ink.press)
				ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
					ui.Text(c, prof.Nickname).FontSize(16).FontWeight(700).SingleLine()
					ui.Text(c, "已登录网易云音乐").FontSize(12.5).TextColor(ink.muted)
				})
				if k.button("logout", "退出登录", iconSignOut, false, true).Clicked() {
					a.askSignOut()
				}
			})
		})
	})
	k.group("听歌记录", func() {
		if !scrobbleWorks {
			// Shown, but not to be turned on, while NetEase counts none of
			// the plays told it: off whatever 1.2.0 kept.
			k.rowOf(settingRow{label: "计入网易云听歌记录", badge: "未开放", desc: "网易云目前不会计入第三方客户端发来的听歌记录，修好后再开放",
				tip: "把听完或听满 30 秒的歌计入账号的听歌时长和排行"}, func() {
				off := false
				settingSwitch(c.Key("scrobble"), &off).Label("计入网易云听歌记录（未开放）").Disabled(true)
			})
			return
		}
		k.rowOf(settingRow{label: "计入网易云听歌记录", desc: "以网易云 Mac 客户端的身份上报，理论上有账号风险，社区暂无封号案例",
			tip: "听完或听满 30 秒的歌会计入最近播放、听歌排行和累计听歌，网易云几分钟后才显示。这是网易云未公开的接口，以官方 Mac 客户端的身份发送：理论上存在账号风险，目前社区里还没有因此被封号的案例；网易云也可能随时不再计入。默认关闭", warn: true}, func() {
			k.toggle("scrobble", "计入网易云听歌记录", &s.Scrobble, a.profile == nil, nil)
		})
	})
}

func (a *app) settingsPlaybackPage(k *settingsKit) {
	c, s := k.c, &a.settings
	k.group("音质", func() {
		k.rowOf(settingRow{label: "播放音质", desc: "无损与 Hi-Res 需要黑胶 VIP，下一首开始生效",
			tip: "没有黑胶 VIP 时，播放所能播放的最高音质"}, func() {
			labels := make([]string, len(qualityLevels))
			chosen := qualityLevels[2].label
			for i, q := range qualityLevels {
				labels[i] = q.label
				if q.level == s.Quality {
					chosen = q.label
				}
			}
			k.choose("quality", "播放音质", chosen, labels, func(i int) {
				s.Quality = qualityLevels[i].level
				a.changed()
			})
		})
	})
	k.group("声音", func() {
		k.row("音量均衡", "按网易云测得的响度调整音量，让每首歌听起来一样响", func() {
			k.toggle("normalize", "音量均衡", &s.Normalize, false, func() { a.pl.SetVolume(a.volume) })
		})
		k.row("淡入淡出", "每首歌快结束时渐渐变轻，下一首渐渐响起，两首歌衔接在一起", func() {
			k.toggle("crossfade", "淡入淡出", &s.Crossfade, false, nil)
		})
		if s.Crossfade {
			k.row("淡入淡出时长", "两首歌重叠的时间", func() {
				labels := make([]string, len(crossfadeChoices))
				chosen := ""
				for i, n := range crossfadeChoices {
					labels[i] = fmt.Sprintf("%d 秒", n)
					if n == a.crossfadeSec() {
						chosen = labels[i]
					}
				}
				k.choose("crossfade-sec", "淡入淡出时长", chosen, labels, func(i int) {
					s.CrossfadeSec = crossfadeChoices[i]
					a.changed()
				})
			})
		}
		desc := "均衡器与预设音效，对所有歌曲生效"
		if s.Effects.On {
			desc = "当前：" + a.presetLabel()
		}
		k.stateRow("音效", desc, func() {
			if k.button("effects-open", "调节…", nil, false, false).Clicked() {
				a.openEffects()
			}
			if settingSwitch(c.Key("effects-on"), &s.Effects.On).Label("开启音效").Changed() {
				a.effectsChanged()
			}
		})
		a.outputRow(c, k.row)
	})
	k.foot("无损与 Hi-Res 需要黑胶 VIP，换音质从下一首开始生效。音量均衡让每首歌听起来一样响；淡入淡出让前后两首衔接在一起。")
	k.group("定时", func() { a.sleepRow(c, k.stateRow) })
}

// scaleChoices are sizes of lyrics, from small to large, in tenths.
func scaleChoices(from, to int) (labels []string, values []float64) {
	for v := from; v <= to; v += 10 {
		labels = append(labels, fmt.Sprintf("%d%%", v))
		values = append(values, float64(v)/100)
	}
	return labels, values
}

// scaleLabel names a size of scaleChoices, 0 being 1.
func scaleLabel(v float64) string {
	if v <= 0 {
		v = 1
	}
	return fmt.Sprintf("%d%%", int(math.Round(v*10))*10)
}

// settingsThemeGroup chooses the theme and its colors.
func (a *app) settingsThemeGroup(k *settingsKit) {
	c, ink, s := k.c, k.ink, &a.settings
	k.group("主题", func() {
		k.row("外观", "浅色、深色，或跟着系统切换", func() {
			modes := []string{"system", "light", "dark"}
			i := 0
			for k, m := range modes {
				if m == s.Appearance {
					i = k
				}
			}
			if a.segmented(c.Key("appearance"), &i, "跟随系统", "浅色", "深色") {
				s.Appearance = modes[i]
				a.changed()
			}
		})
		k.row("背景色", "窗口和侧栏的底色，按钮与高亮跟着它", func() {
			dark := c.Theme().Dark
			base := paletteFor(c.Theme())
			for _, ac := range accentColors {
				dot := ui.ButtonBase(c.Key("accent-"+ac.id)).Size(24, 24).Radius(999).
					FocusRing(false).Label("背景色：" + ac.label)
				fill := base.accent
				if ac.hex != "" {
					fill = accentFor(ui.Hex(ac.hex), dark)
				}
				dot.Background(fill)
				if s.Accent == ac.hex {
					dot.Border(2, ink.text)
				} else {
					dot.Border(0.5, ink.line)
				}
				pose := ui.Pose{}
				if dot.Hovered() {
					pose.Scale = 1.15
				}
				dot.AnimatePose(pose, ui.Snappy)
				if dot.Clicked() {
					s.Accent, s.AccentCover = ac.hex, false
					a.changed()
				}
			}
			own := s.Accent != "" && s.Accent != accentSystem
			for _, ac := range accentColors {
				if ac.hex == s.Accent {
					own = false
				}
			}
			wheel := ui.ButtonBase(c.Key("accent-custom")).Size(24, 24).Radius(999).
				FocusRing(false).Label("自定义主题色")
			wheel.Draw(func(pt *ui.Painter, r ui.Rect) {
				colorWheel(pt, r)
				if own {
					pt.Stroke(r, ink.text, 999, 2)
				} else {
					pt.Stroke(r, ink.line, 999, 1)
				}
			})
			if wheel.Clicked() {
				draft := base.accent
				if hex := a.chosenAccent(); hex != "" {
					draft = ui.Hex(hex)
				}
				a.settingsView.accentDraft = accentPick{open: true, color: draft}
				a.redraw()
			}
		})
		k.row("跟随系统主题色", "使用 Windows 个性化里的主题色，系统换色时跟着变", func() {
			on := s.Accent == accentSystem
			if settingSwitch(k.c.Key("accent-system"), &on).Label("跟随系统主题色").Disabled(a.sysAccent == "" && !on).Changed() {
				s.Accent = ""
				if on {
					s.Accent, s.AccentCover = accentSystem, false
				}
				a.changed()
			}
		})
		k.row("主题色跟随封面", "按钮和高亮使用当前歌曲封面的颜色", func() {
			k.toggle("accent-cover", "主题色跟随封面", &s.AccentCover, false, nil)
		})
	})
	k.foot("背景色决定窗口和侧栏的底色，按钮与高亮跟着它。跟随封面时，改用当前歌曲封面的颜色。")
}

func (a *app) settingsStoragePage(k *settingsKit) {
	c, ink, s := k.c, k.ink, &a.settings
	k.group("下载", func() {
		k.row("下载音质", "可以和播放音质不同；无损与 Hi-Res 占用的空间大得多", func() {
			labels := make([]string, len(downloadLevels))
			chosen := downloadLevels[0].label
			for i, d := range downloadLevels {
				labels[i] = d.label
				if d.level == s.DownloadQuality {
					chosen = d.label
				}
			}
			k.choose("download-quality", "下载音质", chosen, labels, func(i int) {
				s.DownloadQuality = downloadLevels[i].level
				a.changed()
			})
		})
		folder := a.downloadFolder()
		k.block("下载文件夹 路径 位置 "+folder, func() {
			first := k.rows == 1
			line := ink.line
			ui.Row(c).Padding(11, 20).Gap(20).AlignItems(ui.Center).MinHeight(62).
				Draw(func(p *ui.Painter, b ui.Rect) {
					if !first {
						p.Fill(ui.Rect{X: b.X + 20, Y: b.Y, W: b.W - 40, H: 1}, line, 0)
					}
				}).
				Children(func() {
					ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
						ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
							ui.Text(c, "下载文件夹").FontSize(14).FontWeight(500)
							infoTip(c, "歌曲会写入歌名、歌手、封面和歌词", ink)
						})
						ui.Text(c, folder).FontSize(12).Font("Cascadia Mono, Consolas, monospace").TextColor(ink.muted).SingleLine()
					})
					if k.button("download-browse", "更改…", nil, false, false).Clicked() {
						a.pickDownloadFolder()
					}
					if k.button("download-open", "打开", iconExternal, false, false).Clicked() {
						a.openDownloadFolder()
					}
				})
		})
	})
	k.group("本地音乐", func() {
		for _, dir := range s.LocalFolders {
			k.stateRow(filepath.Base(dir), dir, func() {
				if k.button("local-open-"+dir, "打开", iconExternal, false, false).Clicked() {
					mygo.Shell.OpenPath(dir)
				}
				if k.button("local-remove-"+dir, "移除", nil, false, true).Clicked() {
					a.removeLibraryFolder(dir)
				}
			})
		}
		k.row("添加文件夹", "读取文件夹和子文件夹里支持的音频，显示在「本地音乐」，文件夹有变化时自动更新", func() {
			if k.button("local-add", "添加…", iconPlus, false, false).Clicked() {
				a.pickLibraryFolder()
			}
		})
		if canRegisterOpenWith {
			k.row("用 Cloudy 打开音乐文件", "加入 MP3、FLAC 的「打开方式」；要设为默认播放器，请在系统的默认应用设置里选择 Cloudy", func() {
				if k.button("open-with", "添加到打开方式", nil, false, false).Clicked() {
					if err := registerOpenWith(); err != nil {
						a.toast = "添加失败：" + err.Error()
					} else {
						a.toast = "已添加：在音乐文件上右键「打开方式」就能选择 Cloudy"
					}
				}
				if k.button("default-apps", "默认应用设置", iconExternal, false, false).Clicked() {
					mygo.Shell.OpenExternal("ms-settings:defaultapps")
				}
			})
		}
	})
	k.group("缓存", func() {
		k.stateRow("歌曲缓存", "听过的歌保存在本机，再听不用等也不耗流量；"+a.cacheUsage(), func() {
			labels := make([]string, len(cacheLimits))
			chosen := cacheLimits[2].label
			for i, l := range cacheLimits {
				labels[i] = l.label
				if l.mb == s.CacheMB {
					chosen = l.label
				}
			}
			k.choose("cache", "歌曲缓存", chosen, labels, func(i int) {
				s.CacheMB = cacheLimits[i].mb
				a.changed()
				a.applyCacheLimit()
			})
		})
		where := songCacheDir(s.CacheFolder)
		k.block("缓存位置 缓存文件夹 路径 "+where, func() {
			line := ink.line
			ui.Row(c).Padding(11, 20).Gap(20).AlignItems(ui.Center).MinHeight(62).
				Draw(func(p *ui.Painter, b ui.Rect) {
					p.Fill(ui.Rect{X: b.X + 20, Y: b.Y, W: b.W - 40, H: 1}, line, 0)
				}).
				Children(func() {
					ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(4).Children(func() {
						ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
							ui.Text(c, "缓存位置").FontSize(14).FontWeight(500)
							infoTip(c, "系统盘空间不够时，可以把缓存放到别的磁盘；更换后原来的缓存会被清除", ink)
						})
						ui.Text(c, where).FontSize(12).Font("Cascadia Mono, Consolas, monospace").TextColor(ink.muted).SingleLine()
					})
					if s.CacheFolder != "" && k.button("cache-default", "恢复默认", nil, false, false).Clicked() {
						a.setCacheFolder("")
					}
					if k.button("cache-browse", "更改…", nil, false, false).Clicked() {
						a.pickCacheFolder()
					}
					if k.button("cache-open", "打开", iconExternal, false, false).Clicked() {
						a.openCacheFolder()
					}
				})
		})
		k.row("清除缓存", "删除保存在本机的全部缓存歌曲，下载的歌曲不受影响", func() {
			if k.button("clear-cache", "清除", nil, a.cacheUsed <= 0, true).Clicked() {
				a.clearCache()
			}
		})
	})
}

func (a *app) settingsSystemPage(k *settingsKit) {
	s := &a.settings
	k.group("快捷键", func() {
		keys := "在其他软件里也能用"
		if !s.GlobalKeys {
			keys = "已关闭"
		} else if a.keys.taken > 0 {
			keys = fmt.Sprintf("有 %d 组被其他软件占用", a.keys.taken)
		}
		k.stateRow("全局快捷键", keys, func() {
			if k.button("shortcut-custom", "自定义", nil, false, false).Clicked() {
				a.openShortcutEditor()
			}
			k.toggle("global-keys", "全局快捷键", &s.GlobalKeys, false, a.applyGlobalKeys)
		})
	})
	k.group("启动", func() {
		login := a.openAtLogin()
		k.row("开机时启动", "登录 Windows 后自动打开 Cloudy", func() {
			on := login
			if settingSwitch(k.c.Key("open-at-login"), &on).Label("开机时启动").Changed() {
				a.setOpenAtLogin(on)
			}
		})
		k.sub("隐藏到托盘", "开机自动打开时不弹出窗口，从托盘图标打开", login, func() {
			k.toggle("start-hidden", "开机启动时隐藏到托盘", &s.StartHidden, false, nil)
		})
	})
	k.group("关闭窗口", func() {
		choices := []string{"最小化到托盘", "退出 Cloudy"}
		chosen := choices[0]
		if s.CloseQuits {
			chosen = choices[1]
		}
		k.row("点击关闭按钮时", "最小化到托盘时音乐继续播放，从托盘图标打开", func() {
			k.choose("close-action", "点击关闭按钮时", chosen, choices, func(i int) {
				s.CloseQuits = i == 1
				a.changed()
			})
		})
	})
	k.group("任务栏", func() {
		k.row("播放进度", "在任务栏图标上显示当前歌曲播放到哪里，暂停时显示为黄色", func() {
			k.toggle("taskbar-progress", "任务栏播放进度", &s.TaskbarProgress, false, a.syncProgress)
		})
	})
	k.group("小窗口", func() {
		k.row("迷你窗口", "用一个置顶的小窗口代替主窗口；托盘菜单里也能打开", func() {
			if k.button("mini", "打开", nil, false, false).Clicked() {
				a.settingsView.open = false
				a.showMini()
			}
		})
	})
}

func (a *app) settingsAboutPage(k *settingsKit) {
	s := &a.settings
	k.group("软件信息", func() {
		k.block("软件信息 版本 源码 原项目 开源 "+mygo.App.Version(), func() { a.aboutCard(k) })
	})
	if a.updates {
		k.group("更新", func() {
			desc := "每天检查一次是否有新版本"
			if s.UpdateChecked > 0 {
				desc += "；上次检查：" + commentTime(time.Unix(s.UpdateChecked, 0), time.Now())
			}
			k.stateRow("提醒新版本", desc, func() {
				k.toggle("update-checks", "提醒新版本", &s.UpdateChecks, false, nil)
			})
			// Installing in the background goes with the checks,
			// which find what it installs.
			k.sub("自动安装更新", "后台发现新版本时直接装好，下次打开软件时生效；手动检查仍会先问你", s.UpdateChecks, func() {
				k.toggle("update-downloads", "自动安装更新", &s.UpdateInstall, false, nil)
			})
			k.stateRow("当前版本 "+mygo.App.Version(), "新版本来自 GitHub 的 nya-owo/cloudy", func() {
				if k.button("check-updates", "检查更新", nil, false, false).Clicked() {
					a.checkForUpdates()
				}
			})
		})
	}
	k.group("反馈", func() {
		k.block("反馈 建议 问题 bug 私信 邮件", func() { a.feedbackCard(k.c, k.ink) })
	})
}

// settingSlider is a slider of a setting, from 0 to 1, with how far it is
// said beside it. It keeps the settings as the pointer lets go, rather than
// at each step of a drag.
func (a *app) settingSlider(c *ui.Context, label string, v *float64, on bool, ink paneInk) {
	opacity := float32(1)
	if !on {
		opacity = 0.4
	}
	ui.Row(c).Gap(10).AlignItems(ui.Center).Opacity(opacity).Children(func() {
		s := ui.SliderBase(c.Key("slider"), v, 0, 1).Width(200).Height(22).Step(0.01).FocusRing(false).Label(label).Disabled(!on).Cursor(ui.CursorPointer)
		if s.Changed() {
			a.settingsView.dirty = true
			a.redraw()
		}
		held := s.Pressed()
		if a.settingsView.dirty && !held {
			a.settingsView.dirty = false
			a.settings.save()
		}
		thick := float32(4)
		if s.Hovered() || held {
			thick = 5
		}
		thick = s.AnimateTo("thick", thick, ui.Snappy)
		at, accent := float32(*v), a.pal.accent
		s.Draw(func(p *ui.Painter, r ui.Rect) {
			y := r.Y + (r.H-thick)/2
			p.Fill(ui.Rect{X: r.X, Y: y, W: r.W, H: thick}, ink.press, thick/2)
			p.Fill(ui.Rect{X: r.X, Y: y, W: max(thick, r.W*at), H: thick}, accent, thick/2)
			const knob = 18
			k := ui.Rect{X: r.X + (r.W-knob)*at, Y: r.Y + (r.H-knob)/2, W: knob, H: knob}
			p.Shadow(k, knob/2, 0, 1, 4, 0, ui.RGBA(0, 0, 0, 0.3))
			p.Fill(k, white, knob/2)
		})
		ui.Text(c, fmt.Sprintf("%d%%", int(*v*100+0.5))).FontSize(12).FontFeatures("tnum").TextColor(ink.muted).Width(36).TextAlign(ui.End)
	})
}
