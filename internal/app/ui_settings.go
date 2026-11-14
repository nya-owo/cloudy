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
	settingsLyrics
	settingsPodcast
	settingsAppearance
	settingsEffects
	settingsStorage
	settingsSystem
	settingsAbout
	settingsCategoryCount
)

// settingsCategories are the settings' destinations, in the order the
// sidebar lists them: a section names the ones from it to the next.
var settingsCategories = [...]struct {
	label, section string
	icon           *ui.SVG
}{
	{"账号", "", iconUser},
	{"播放", "听歌", iconHeadset},
	{"歌词", "", iconLyrics},
	{"播客", "", iconPodcast},
	{"外观", "界面", iconSun},
	{"视觉效果", "", iconSparkles},
	{"下载与缓存", "系统", iconDownload},
	{"快捷键与窗口", "", iconKeyboard},
	{"关于", "", iconInfo},
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
	a.dialog(c, "settings", 0.3, &a.settingsView.open, func(panel ui.Element) {
		// It grows from the corner of who listens, whose row opens it.
		panel.Width(min(940, w-48)).Height(max(h-72, 320)).TextColor(ink.text).Label("设置").TransformOrigin(0, 1)
		a.pane(panel, 26, false)
		panel.Children(func() {
			where := settingsCategories[a.settingsView.tab].label
			if strings.TrimSpace(a.settingsView.query) != "" {
				where = "搜索结果"
			}
			ui.Row(c).Height(68).Padding(0, 20, 0, 34).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "设置").FontSize(20).FontWeight(700)
				ui.Text(c, "/").FontSize(15).TextColor(ink.faint)
				ui.Text(c, where).FontSize(15).TextColor(ink.muted).Grow(1)
				if iconButton(c.Key("close"), iconX, 16, ink.muted, "关闭").Clicked() {
					a.settingsView.open = false
				}
			})
			ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
				a.settingsSidebar(c)
				a.settingsBody(c)
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
	ui.Column(c.Key("settings-nav")).Width(208).Shrink(0).Padding(2, 12, 16, 22).Gap(3).Label("设置分类").Children(func() {
		field := ui.Row(c.Key("settings-search")).Height(36).PaddingX(12).Gap(8).Radius(999).AlignItems(ui.Center).
			Background(ink.hover).Border(0.5, ink.line).Margin(0, 0, 10, 0)
		field.Children(func() {
			ui.Icon(c, iconSearch).Size(15, 15).Shrink(0).TextColor(ink.muted)
			in := ui.TextInputBase(c.Key("settings-query"), &a.settingsView.query).Bind(&a.settingsView.searchBox).Grow(1).MinWidth(0).
				FontSize(13).Label("搜索设置").Placeholder("搜索设置")
			if in.Changed() {
				a.settingsView.searchScroll = ui.ScrollState{}
			}
			if a.settingsView.query != "" {
				if iconButton(c.Key("settings-clear"), iconX, 11, ink.muted, "清除搜索").Size(22, 22).Clicked() {
					a.settingsView.query = ""
				}
			}
		})
		ui.Scroll(c.Key("settings-nav-list")).Grow(1).MinHeight(0).Gap(3).Margin(0, -6, 0, -6).Padding(0, 6).Children(func() { a.settingsNavItems(c, ink, searching) })
	})
}

// settingsNavItems are the categories, under the names of their sections.
func (a *app) settingsNavItems(c *ui.Context, ink paneInk, searching bool) {
	for i, category := range settingsCategories {
		if category.section != "" {
			ui.Text(c, category.section).FontSize(11.5).FontWeight(600).TextColor(ink.faint).Margin(12, 0, 4, 14)
		}
		if settingsCategory(i) == settingsAbout {
			ui.Box(c).Height(0.5).Background(ink.line).Margin(10, 10, 8, 10)
		}
		selected := a.settingsView.tab == settingsCategory(i) && !searching
		button := ui.ButtonBase(c.Key(category.label)).Height(40).PaddingX(14).Gap(12).Radius(14).
			AlignItems(ui.Center).Justify(ui.Start).Label("设置分类：" + category.label).Selected(selected)
		bg, color, weight := ui.Transparent, ink.muted, 500
		if selected {
			bg, color, weight = ink.hover, ink.text, 600
		} else if button.Hovered() {
			bg, color = ink.hover.Alpha(0.5), ink.text
		}
		bar := float32(0)
		if selected {
			bar = 1
		}
		bar = button.AnimateTo("bar", bar, ui.Snappy)
		accent := a.pal.accent
		button.Background(button.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond})).TextColor(color).
			Draw(func(p *ui.Painter, r ui.Rect) {
				if bar > 0.01 {
					h := 14 * bar
					p.Fill(ui.Rect{X: r.X, Y: r.Y + (r.H-h)/2, W: 3, H: h}, accent.Alpha(bar), 1.5)
				}
			})
		button.Children(func() {
			ui.Icon(c, category.icon).Size(18, 18)
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
	hits      int  // rows that matched in a probe
	rows      int  // rows on the card being built
}

// A settingRow is a row of a setting, as it says it.
type settingRow struct {
	label, desc string
	tip         string // said in full as the pointer is over the ⓘ
	warn        bool   // its desc is a word of caution
	badge       string // a word on a capsule beside its label, as 未开放
	sub         bool   // it belongs to the row above it
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
	ui.Text(k.c, title).FontSize(12.5).FontWeight(600).TextColor(k.ink.muted).Margin(18, 0, 8, 18)
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
	ui.Column(k.c).Radius(20).Background(fill).Border(0.5, edge).Children(rows)
	k.rows = n
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
	if ui.Switch(k.c.Key(key), on).Label(label).Disabled(disabled).Changed() {
		if then != nil {
			then()
		}
		k.a.changed()
	}
}

// settingsBody shows one category, keeping its scroll position between
// visits, or while the listener searches, what matches in all of them.
func (a *app) settingsBody(c *ui.Context) {
	k := &settingsKit{a: a, c: c, ink: a.inkOn(false), query: strings.ToLower(strings.TrimSpace(a.settingsView.query))}
	k.searching = k.query != ""
	key, scroll, label := any(a.settingsView.tab), &a.settingsView.scroll[a.settingsView.tab], settingsCategories[a.settingsView.tab].label
	if k.query != "" {
		key, scroll, label = "search", &a.settingsView.searchScroll, "搜索"
	}
	viewport := ui.Box(c.Key("settings-viewport")).Grow(1).Shrink(1).MinWidth(0)
	// Keep settings text on the final target. An edge Effect resamples the
	// entire viewport, softening even the text well outside its fade band.
	viewport.Children(func() {
		ui.Scroll(c.Key(key)).Label(label+"设置项").TrackScroll(scroll).Fill().Padding(0, 30, 30, 18).Children(func() {
			if k.query == "" {
				ui.Column(c.Key(a.settingsView.tab)).Margin(-12, 0, 0, 0).Children(func() { a.settingsPage(k, a.settingsView.tab) })
				return
			}
			found := false
			for cat := settingsCategory(0); cat < settingsCategoryCount; cat++ {
				if k.count(func() { a.settingsPage(k, cat) }) == 0 {
					continue
				}
				found = true
				ui.Column(c.Key(cat)).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Margin(16, 0, -6, 6).Children(func() {
						ui.Icon(c, settingsCategories[cat].icon).Size(16, 16).TextColor(k.ink.muted)
						ui.Text(c, settingsCategories[cat].label).FontSize(15).FontWeight(700)
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
	case settingsLyrics:
		a.settingsLyricsPage(k)
	case settingsPodcast:
		k.group("播客", func() {
			a.podcastSpeedRow(k.c, k.row)
			if a.captionsShown() {
				a.captionSettings(k)
			}
		})
	case settingsAppearance:
		a.settingsAppearancePage(k)
	case settingsEffects:
		a.settingsEffectsPage(k)
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
			k.row("未登录", "登录后，每日推荐与歌单将根据你的口味生成", func() {
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
				ui.Switch(c.Key("scrobble"), &off).Label("计入网易云听歌记录（未开放）").Disabled(true)
			})
			return
		}
		k.rowOf(settingRow{label: "计入网易云听歌记录", desc: "使用未公开的接口，有一定账号风险",
			tip: "听完或听满 30 秒的歌会计入账号的听歌时长和排行；这是网易云未公开的接口，非官方客户端使用存在一定账号风险，默认关闭", warn: true}, func() {
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
		k.row("音效", desc, func() {
			if k.button("effects-open", "调节…", nil, false, false).Clicked() {
				a.openEffects()
			}
			if ui.Switch(c.Key("effects-on"), &s.Effects.On).Label("开启音效").Changed() {
				a.effectsChanged()
			}
		})
		a.outputRow(c, k.row)
	})
	k.group("定时", func() { a.sleepRow(c, k.row) })
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

func (a *app) settingsLyricsPage(k *settingsKit) {
	s := &a.settings
	k.group("播放页歌词", func() {
		k.row("透视歌词", "当前句在画面中部高亮，上下歌词沿圆弧滚动", func() {
			k.toggle("lyric-perspective", "透视歌词", &s.LyricPerspective, false, nil)
		})
		k.sub("倾斜度", "0% 为直上直下，50% 为原来的圆弧幅度", s.LyricPerspective, func() {
			a.settingSlider(k.c.Key("lyric-tilt"), "倾斜度", &s.LyricTilt, s.LyricPerspective, k.ink)
		})
		k.sub("回弹强度", "调节切换到下一句时的回弹幅度，0% 为平滑滚动", s.LyricPerspective, func() {
			a.settingSlider(k.c.Key("lyric-bounce"), "回弹强度", &s.LyricBounce, s.LyricPerspective, k.ink)
		})
		k.row("歌词翻译", "在歌词下方显示翻译；播放栏的翻译按钮也能切换", func() {
			k.toggle("lyric-trans", "歌词翻译", &s.LyricTrans, false, nil)
		})
		k.row("热评模式", "歌词处换成这首歌的热门评论，一条接一条地轮换", func() {
			// Turned on, it shows them all along; those who had them in
			// the quiet parts only keep that.
			k.toggle("hot-comments", "热评模式", &s.HotComments, false, func() { s.HotAll = s.HotComments })
		})
		k.sub("仅在间奏时显示热评", "只在前奏、间奏与尾奏时显示热评，有歌词的地方照常显示歌词", s.HotComments, func() {
			quiet := !s.HotAll
			k.toggle("hot-quiet", "仅在间奏时显示热评", &quiet, false, func() { s.HotAll = !quiet })
		})
		k.row("逐字高亮", "有逐字歌词的歌，唱到哪个字亮到哪个字；关闭后整行一起亮起", func() {
			words := !s.NpWholeLines
			k.toggle("np-words", "逐字高亮", &words, false, func() { s.NpWholeLines = !words })
		})
		k.row("隐藏制作信息", "不显示作词、作曲、编曲、制作人等行，播放页与桌面歌词都生效", func() {
			k.toggle("hide-credits", "隐藏制作信息", &s.HideCredits, false, nil)
		})
		k.row("歌词字号", "播放页歌词的大小；鼠标移到播放页也能调", func() {
			labels, values := scaleChoices(70, 160)
			k.choose("lyric-scale", "歌词字号", scaleLabel(s.LyricScale), labels, func(i int) {
				s.LyricScale = values[i]
				a.changed()
			})
		})
		k.row("默认显示律动", "打开播放页时显示随歌声跳动的律动歌词，而不是滚动歌词", func() {
			k.toggle("lyrics-stage", "默认显示律动", &s.LyricsStage, false, func() {
				if a.npPane == paneLyrics || a.npPane == paneStage {
					a.npPane = paneLyrics
					if s.LyricsStage {
						a.npPane = paneStage
					}
				}
			})
		})
		k.sub("律动效果", "律动歌词的排版与动画", s.LyricsStage, func() {
			labels := make([]string, len(stageStyles))
			chosen := stageStyles[0].label
			for i, st := range stageStyles {
				labels[i] = st.label
				if st.id == s.StageStyle {
					chosen = st.label
				}
			}
			k.choose("stage-style", "律动效果", chosen, labels, func(i int) {
				s.StageStyle = stageStyles[i].id
				a.changed()
			})
		})
		k.sub("火花", "唱到的字迸出火花", s.LyricsStage, func() {
			k.toggle("stage-sparks", "火花", &s.StageSparks, false, nil)
		})
	})
	k.group("桌面歌词", func() {
		k.row("桌面歌词", "在其他窗口之上显示当前歌词", func() {
			k.toggle("desk-lyrics", "桌面歌词", &s.DeskLyrics, false, a.applyDeskLyrics)
		})
		k.sub("样式", "也可以把鼠标移到歌词上点“样式”切换", s.DeskLyrics, func() {
			labels := make([]string, len(deskStyles))
			chosen := deskStyles[0].label
			for i, d := range deskStyles {
				labels[i] = d.label
				if d.id == s.DeskStyle {
					chosen = d.label
				}
			}
			k.choose("desk-style", "桌面歌词样式", chosen, labels, func(i int) {
				s.DeskStyle = deskStyles[i].id
				a.changed()
			})
		})
		k.sub("字号", "桌面歌词的大小", s.DeskLyrics, func() {
			labels, values := scaleChoices(70, 130)
			k.choose("desk-scale", "桌面歌词字号", scaleLabel(s.DeskScale), labels, func(i int) {
				s.DeskScale = values[i]
				a.changed()
			})
		})
		k.sub("锁定", "锁定后鼠标会穿过歌词，不会挡住下面的内容", s.DeskLyrics, func() {
			k.toggle("desk-lock", "锁定桌面歌词", &s.DeskLock, false, a.applyDeskLyrics)
		})
		k.sub("逐字高亮", "唱到哪个字亮到哪个字；关掉后当前这一行整行亮起", s.DeskLyrics, func() {
			k.toggle("desk-words", "逐字高亮", &s.DeskWords, false, nil)
		})
	})
}

func (a *app) settingsAppearancePage(k *settingsKit) {
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
					dot.Border(1, ink.line)
				}
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
			if ui.Switch(k.c.Key("accent-system"), &on).Label("跟随系统主题色").Disabled(a.sysAccent == "" && !on).Changed() {
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
	k.group("文字与布局", func() {
		k.row("界面缩放", "整个窗口的文字、图标与图片的大小；也可以按 Ctrl 加 + 或 - 调整，Ctrl 加 0 恢复", func() {
			labels, values := scaleChoices(80, 200)
			k.choose("ui-zoom", "界面缩放", scaleLabel(s.UIZoom), labels, func(i int) {
				s.UIZoom = values[i]
				if s.UIZoom == 1 {
					s.UIZoom = 0
				}
				a.changed()
			})
		})
		a.fontRows(c, k)
		k.rowOf(settingRow{label: "侧边栏大小", desc: "歌单、专辑的封面和文字大小",
			tip: "拖动侧边栏右边缘可以调整宽度，双击边缘恢复"}, func() {
			labels := make([]string, len(sidebarSizes))
			for i, sz := range sidebarSizes {
				labels[i] = sz.label
			}
			k.choose("sidebar-size", "侧边栏大小", a.sidebarSizeLabel(), labels, func(i int) {
				s.SidebarSize = sidebarSizes[i].id
				a.changed()
			})
		})
		k.row("侧边栏列出全部歌单", "关闭时每组只列出前 8 个，其余在「我的主页」里查看和搜索", func() {
			k.toggle("sidebar-all", "侧边栏列出全部歌单", &s.SidebarAll, false, nil)
		})
	})
}

func (a *app) settingsEffectsPage(k *settingsKit) {
	c, s := k.c, &a.settings
	// Turning one of the effects that cost power on leaves the saver.
	effect := func(key, label string, on *bool) {
		k.toggle(key, label, on, false, a.leavePowerSaver)
	}
	slider := func(label, desc string, v *float64, on bool) {
		k.sub(label, desc, on, func() { a.settingSlider(c.Key(label), label, v, true, k.ink) })
	}
	k.group("省电", func() {
		k.row("省电模式", "一键关闭液态玻璃、封面旋转、微光和烟雾；再关一次恢复原样", func() {
			on := s.PowerSaver
			if ui.Switch(c.Key("power-saver"), &on).Label("省电模式").Changed() {
				a.setPowerSaver(on)
			}
		})
	})
	k.group("液态玻璃", func() {
		k.row("全局液态玻璃", "使用通透的封面色背景，悬浮播放栏、菜单和对话框采用折射玻璃", func() {
			on := s.GlobalGlass
			if ui.Switch(c.Key("global-glass"), &on).Label("切换全局液态玻璃").Changed() {
				if on {
					a.settingsView.confirmGlass = true
				} else {
					s.GlobalGlass = false
					a.changed()
				}
			}
		})
		part := func(key, label string, stored *bool, after func()) {
			if s.GlobalGlass {
				on := true
				k.toggle(key, label, &on, true, nil)
				return
			}
			k.toggle(key, label, stored, false, after)
		}
		// The cover's colors take the place of the accent chosen: said, in a
		// toast, as either is turned on.
		coverOn := func(on *bool) func() {
			return func() {
				a.leavePowerSaver()
				if *on && a.chosenAccent() != "" {
					a.toast = "已改用封面色背景，所选主题色将不再显示"
				}
			}
		}
		k.row("侧栏封面色背景", "透出当前歌曲封面的颜色，不使用实时折射", func() {
			part("sidebar-glass", "侧栏封面色背景", &s.SidebarGlass, coverOn(&s.SidebarGlass))
		})
		k.row("主界面封面色背景", "与侧栏共享连续的封面色背景", func() {
			part("main-glass", "主界面封面色背景", &s.MainGlass, coverOn(&s.MainGlass))
		})
		k.row("液态玻璃播放栏", "播放栏化作悬浮的玻璃胶囊，透出并折射其下的内容", func() {
			part("liquid-glass", "液态玻璃播放栏", &s.LiquidGlass, a.leavePowerSaver)
		})
		k.row("界面玻璃", "菜单（含托盘菜单）、弹窗和设置面板使用玻璃材质，内部卡片保持平面", func() {
			part("glass-panels", "界面玻璃", &s.GlassPanels, a.leavePowerSaver)
		})
		refractive := s.playerGlass() || s.GlobalGlass
		slider("玻璃透明度", "调节玻璃的通透程度", &s.GlassClear, refractive)
		slider("折射强度", "调节玻璃边缘弯折应用内背景的程度", &s.GlassBend, refractive)
	})
	k.group("播放栏", func() {
		k.row("封面旋转", "播放时胶囊里的封面像黑胶一样缓慢转动", func() {
			effect("bar-spin", "播放栏封面旋转", &s.BarSpin)
		})
	})
	k.group("播放页", func() {
		k.row("微光", "当前歌词、进度条与音量带一层柔和的微光", func() {
			effect("np-light", "播放页微光", &s.NpLight)
		})
		slider("微光强度", "", &s.GlowLevel, s.NpLight)
		k.row("烟雾背景", "背景化作封面色调的烟雾，随低音起伏", func() {
			effect("fog", "烟雾背景", &s.Fog)
		})
		k.row("进度条烟雾", "进度条化作拖着烟雾的光点，随音乐起伏", func() {
			effect("smoke", "进度条烟雾特效", &s.Smoke)
		})
	})
	k.group("界面", func() {
		k.row("界面动画", "精简时只保留淡入淡出", func() {
			modes := []string{"", "lite", "off"}
			i := 0
			for k, m := range modes {
				if m == s.Motion {
					i = k
				}
			}
			if a.segmented(c.Key("motion"), &i, "完整", "精简", "关闭") {
				s.Motion = modes[i]
				a.changed()
			}
		})
	})
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
			k.row(filepath.Base(dir), dir, func() {
				if k.button("local-open-"+dir, "打开", iconExternal, false, false).Clicked() {
					mygo.Shell.OpenPath(dir)
				}
				if k.button("local-remove-"+dir, "移除", nil, false, true).Clicked() {
					a.removeLibraryFolder(dir)
				}
			})
		}
		k.row("添加文件夹", "读取文件夹和子文件夹里的 MP3、FLAC，显示在「本地音乐」，文件夹有变化时自动更新", func() {
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
		k.row("歌曲缓存", "听过的歌保存在本机，再听不用等也不耗流量；"+a.cacheUsage(), func() {
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
		k.row("全局快捷键", keys, func() {
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
			if ui.Switch(k.c.Key("open-at-login"), &on).Label("开机时启动").Changed() {
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
			k.row("提醒新版本", desc, func() {
				k.toggle("update-checks", "提醒新版本", &s.UpdateChecks, false, nil)
			})
			// Installing in the background goes with the checks,
			// which find what it installs.
			k.sub("自动安装更新", "在后台下载并装好新版本，不弹窗，下次打开软件时生效", s.UpdateChecks, func() {
				k.toggle("update-downloads", "自动安装更新", &s.UpdateInstall, false, nil)
			})
			k.row("当前版本 "+mygo.App.Version(), "新版本来自 GitHub 的 nya-owo/cloudy", func() {
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
		s := ui.SliderBase(c.Key("slider"), v, 0, 1).Width(150).Height(22).Step(0.01).FocusRing(false).Label(label).Disabled(!on).Cursor(ui.CursorPointer)
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
			const knob = 16
			k := ui.Rect{X: r.X + (r.W-knob)*at, Y: r.Y + (r.H-knob)/2, W: knob, H: knob}
			p.Shadow(k, knob/2, 0, 1, 4, 0, ui.RGBA(0, 0, 0, 0.3))
			p.Fill(k, white, knob/2)
		})
		ui.Text(c, fmt.Sprintf("%d%%", int(*v*100+0.5))).FontSize(12).FontFeatures("tnum").TextColor(ink.muted).Width(36).TextAlign(ui.End)
	})
}
