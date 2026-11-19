package app

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// The settings are sorted by where a setting shows, not by how it is
// made: what Now Playing looks like is one page, whatever of it is the
// backdrop, the lyrics or a glow. What is chosen by its look is chosen
// from cards showing it; the rest are rows, which say in a few words
// only what their names don't.

// choiceCards are cards to choose one of, perRow to a row, each a
// picture thumb builds over its label, with nothing about it but, on
// the one chosen, a ring: chosen is the one chosen now, or none of them
// for -1. It returns the one picked, if one was.
func (a *app) choiceCards(c *ui.Context, key, what string, labels []string, chosen, perRow int, thumb func(i int)) (int, bool) {
	ink := a.inkOn(false)
	picked, ok := 0, false
	accent := a.pal.accent
	ui.Column(c.Key(key)).Padding(16, 16, 10, 16).Gap(12).Children(func() {
		for from := 0; from < len(labels); from += perRow {
			ui.Row(c.Key(from)).Gap(14).AlignItems(ui.Stretch).Children(func() {
				for i := from; i < from+perRow; i++ {
					if i >= len(labels) {
						// The last row's cards are as wide as the others'.
						// Padded as they are, to come out as wide.
						ui.Box(c.Key(i)).Grow(1).Basis(0).MinWidth(0).Padding(4)
						continue
					}
					l, on := labels[i], i == chosen
					card := ui.ButtonBase(c.Key(i)).Grow(1).Basis(0).MinWidth(0).Padding(4).Radius(14).FocusRing(false).
						Label(what + "：" + l).Selected(on).Background(ui.Transparent)
					ring := card.AnimateTo("ring", b2f(on), ui.Snappy)
					pose := ui.Pose{}
					switch {
					case card.Pressed():
						pose.Scale = 0.97
					case card.Hovered():
						pose.Scale = 1.04
					}
					card.AnimatePose(pose, ui.Snappy)
					card.Draw(func(p *ui.Painter, r ui.Rect) {
						if ring > 0.01 {
							// About the picture, a little way off it.
							p.Stroke(ui.Rect{X: r.X + 1, Y: r.Y + 1, W: r.W - 2, H: thumbHeight + 6}, accent.Alpha(ring), 14, 2)
						}
					})
					card.Children(func() {
						ui.Column(c).Grow(1).Basis(0).MinWidth(0).Gap(9).Children(func() {
							thumb(i)
							color, weight := ink.muted, 400
							if on {
								color, weight = ink.text, 600
							}
							ui.Text(c, l).FontSize(12.5).FontWeight(weight).TextColor(color).TextAlign(ui.Center).SingleLine()
						})
					})
					if card.Clicked() {
						picked, ok = i, true
					}
				}
			})
		}
	})
	return picked, ok
}

// settingSwitch is the switch of the settings: larger than the theme's,
// its knob springing across.
func settingSwitch(c *ui.Context, on *bool) ui.Element {
	t := c.Theme()
	sw := ui.SwitchBase(c, on).Size(44, 26).Radius(13).Shrink(0).FocusRing(false)
	pos := sw.AnimateTo("knob", b2f(*on), ui.Snappy)
	off := ui.RGBA(120, 120, 128, 0.3)
	if t.Dark {
		off = ui.RGBA(120, 120, 128, 0.42)
	}
	sw.Background(off.Mix(t.Accent, min(max(pos, 0), 1)))
	sw.Draw(func(p *ui.Painter, r ui.Rect) {
		const in = 2
		d := r.H - 2*in
		knob := ui.Rect{X: r.X + in + pos*(r.W-r.H), Y: r.Y + in, W: d, H: d}
		p.Shadow(knob, d/2, 0, 1.5, 4, 0, ui.RGBA(0, 0, 0, 0.28))
		p.Fill(knob, white, d/2)
	})
	return sw
}

// thumbHeight is how tall the cards' pictures are.
const thumbHeight = 58

// sceneThumb is the picture of a backdrop on its card: painted once for
// a cover's color, by the scene's own shaders, into a small canvas.
type sceneThumb struct {
	canvas *ui.Canvas
	tint   ui.Color
	done   bool
}

// thumbTint is the color the cards' pictures are painted in: that of
// the cover Now Playing last showed, or the accent before any.
func (a *app) thumbTint() ui.Color {
	if t := a.backdropCache.cover; t.A > 0 {
		return t
	}
	return a.pal.accent
}

// sceneThumbnail is the picture of the backdrop of id on its card, the
// still one for still.
func (a *app) sceneThumbnail(c *ui.Context, id string, still bool) {
	key := "scene-" + id
	if still {
		key = "still"
	}
	sv := &a.settingsView
	if sv.thumbs == nil {
		sv.thumbs = map[string]*sceneThumb{}
	}
	th := sv.thumbs[key]
	if th == nil {
		th = &sceneThumb{}
		sv.thumbs[key] = th
	}
	tint := a.settings.tintOf(id, a.thumbTint())
	box := ui.Box(c.Key(key)).Height(thumbHeight).Radius(8).Background(white.Alpha(0.06))
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		const w, h = 176, 96
		if th.canvas == nil {
			th.canvas = ui.NewCanvas(w, h)
		}
		if th.done && th.tint == tint {
			return
		}
		th.canvas.Draw(p, func(cp *ui.Painter) {
			b := ui.Rect{W: w, H: h}
			if still {
				drawBackdrop(cp, b, nil, tint)
				return
			}
			// A moment of it with the music loud enough to show.
			drawScene(cp, b, id, tint, 6.4, 0.4)
			drawSceneOver(cp, b, id, tint, 6.4, 0.4, 1, nil, nil)
		})
		th.done, th.tint = true, tint
		// The canvas shows from the next frame on.
		p.AnimationFrame()
	})
	if th.done {
		box.Children(func() {
			ui.Image(c, th.canvas.Bitmap()).Absolute().Top(0).Left(0).Right(0).Bottom(0).Fit(ui.Cover).Radius(8)
		})
	}
}

// backdropCards choose Now Playing's backdrop: the still one, or a
// scene of the living one.
func (a *app) backdropCards(c *ui.Context) {
	s := &a.settings
	labels := []string{"静态"}
	chosen := 0
	for i, sc := range backdropScenes {
		labels = append(labels, sc.label)
		if s.Fog && backdropSceneOf(s.FogStyle) == &backdropScenes[i] {
			chosen = i + 1
		}
	}
	picked, ok := a.choiceCards(c, "backdrop-cards", "播放页背景", labels, chosen, 5, func(i int) {
		if i == 0 {
			a.sceneThumbnail(c, "", true)
			return
		}
		a.sceneThumbnail(c, backdropScenes[i-1].id, false)
	})
	if !ok {
		return
	}
	s.Fog = picked > 0
	if picked > 0 {
		s.FogStyle = backdropScenes[picked-1].id
		// One of the effects that cost power, turned on, leaves the saver.
		a.leavePowerSaver()
	}
	a.changed()
}

func (a *app) settingsNowPlayingPage(k *settingsKit) {
	c, s := k.c, &a.settings
	effect := func(key, label string, on *bool) {
		k.toggle(key, label, on, false, a.leavePowerSaver)
	}
	slider := func(label string, v *float64, on bool) {
		k.sub(label, "", on, func() { a.settingSlider(c.Key(label), label, v, true, k.ink) })
	}
	if !k.probe && !k.searching {
		a.nowPlayingPreview(c)
	}
	k.group("背景", func() {
		k.block("背景 动态背景 静态 烟雾 光斑 极光 星空 深海 波纹 月夜 封面色", func() { a.backdropCards(c) })
		// Of the scene chosen alone: each keeps its own answer.
		scene := backdropSceneOf(s.FogStyle)
		_, own := sceneTints[scene.id]
		k.sub("颜色跟随封面", "关闭时"+scene.label+"用自己的颜色；打开后用当前歌曲封面的颜色。每种背景各记各的", s.Fog && own, func() {
			on := s.SceneCover[scene.id]
			k.toggle("scene-cover-"+scene.id, scene.label+"颜色跟随封面", &on, false, func() {
				if s.SceneCover == nil {
					s.SceneCover = map[string]bool{}
				}
				if on {
					s.SceneCover[scene.id] = true
				} else {
					delete(s.SceneCover, scene.id)
				}
			})
		})
	})
	k.group("光效", func() {
		k.row("微光", "当前歌词、进度条与音量带一层柔光", func() {
			effect("np-light", "播放页微光", &s.NpLight)
		})
		slider("微光强度", &s.GlowLevel, s.NpLight)
		k.row("进度条烟雾", "进度条化作拖着烟雾的光点", func() {
			effect("smoke", "进度条烟雾特效", &s.Smoke)
		})
	})
	k.foot("微光给当前歌词、进度条和音量加一层柔光；进度条烟雾让进度条拖着一个发光的光点。")
	k.group("歌词", func() {
		k.row("歌词排版", "滚动，或当前句居中、上下沿圆弧排开的透视", func() {
			i := 0
			if s.LyricPerspective {
				i = 1
			}
			if a.segmented(c.Key("lyric-layout"), &i, "滚动", "透视") {
				s.LyricPerspective = i == 1
				a.changed()
			}
		})
		k.sub("倾斜度", "0% 为直上直下", s.LyricPerspective, func() {
			a.settingSlider(c.Key("lyric-tilt"), "倾斜度", &s.LyricTilt, s.LyricPerspective, k.ink)
		})
		k.sub("回弹强度", "0% 为平滑滚动", s.LyricPerspective, func() {
			a.settingSlider(c.Key("lyric-bounce"), "回弹强度", &s.LyricBounce, s.LyricPerspective, k.ink)
		})
		k.row("模糊其余歌词", "离正在唱的一句越远越模糊", func() {
			k.toggle("lyric-blur", "模糊其余歌词", &s.LyricBlur, false, nil)
		})
		// Only while some lyrics blur: the perspective ones always do.
		blurs := s.LyricBlur || s.LyricPerspective
		k.sub("模糊强度", "", blurs, func() {
			a.settingSlider(c.Key("lyric-blur-level"), "模糊强度", &s.LyricBlurLevel, blurs, k.ink)
		})
		k.row("歌词字号", "鼠标移到播放页也能调", func() {
			labels, values := scaleChoices(70, 160)
			k.choose("lyric-scale", "歌词字号", scaleLabel(s.LyricScale), labels, func(i int) {
				s.LyricScale = values[i]
				a.changed()
			})
		})
	})
	k.foot("透视让当前句居中，上下的歌词沿圆弧排开。把鼠标移到播放页上，也能直接调字号。")
	k.group("行为", func() {
		k.row("空闲时隐藏控件", "鼠标静止 3 秒后只留封面、歌名、进度和播放键", func() {
			k.toggle("np-calm", "播放页空闲时隐藏控件", &s.NpCalm, false, nil)
		})
	})
	k.foot("鼠标静止 3 秒后，只留封面、歌名、进度和播放键。")
}

func (a *app) settingsLyricsPage(k *settingsKit) {
	s := &a.settings
	k.group("歌词内容", func() {
		k.row("歌词翻译", "播放栏的翻译按钮也能切换", func() {
			k.toggle("lyric-trans", "歌词翻译", &s.LyricTrans, false, nil)
		})
		k.row("逐字高亮", "唱到哪个字亮到哪个字；关闭后整行一起亮起", func() {
			words := !s.NpWholeLines
			k.toggle("np-words", "逐字高亮", &words, false, func() { s.NpWholeLines = !words })
		})
		k.row("其他来源逐字歌词", "网易云只有逐行歌词时，从 AMLL 歌词库或酷狗找逐字歌词", func() {
			k.toggle("more-words", "其他来源逐字歌词", &s.MoreWords, false, func() { moreWordsOn.Store(s.MoreWords) })
		})
		k.row("隐藏制作信息", "不显示作词、作曲、编曲等行", func() {
			k.toggle("hide-credits", "隐藏制作信息", &s.HideCredits, false, nil)
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
	})
	k.foot("逐字高亮关闭后整行一起亮起。网易云只有逐行歌词时，可以从 AMLL 歌词库或酷狗找逐字歌词。热评模式把歌词换成这首歌的热门评论。")
	k.group("桌面歌词", func() {
		k.row("桌面歌词", "在其他窗口之上显示当前歌词", func() {
			k.toggle("desk-lyrics", "桌面歌词", &s.DeskLyrics, false, a.applyDeskLyrics)
		})
		k.sub("样式", "鼠标移到歌词上点“样式”也能切换", s.DeskLyrics, func() {
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
		k.sub("字号", "", s.DeskLyrics, func() {
			labels, values := scaleChoices(70, 130)
			k.choose("desk-scale", "桌面歌词字号", scaleLabel(s.DeskScale), labels, func(i int) {
				s.DeskScale = values[i]
				a.changed()
			})
		})
		k.sub("锁定", "鼠标穿过歌词，不挡住下面的内容", s.DeskLyrics, func() {
			k.toggle("desk-lock", "锁定桌面歌词", &s.DeskLock, false, a.applyDeskLyrics)
		})
		k.sub("逐字高亮", "", s.DeskLyrics, func() {
			k.toggle("desk-words", "逐字高亮", &s.DeskWords, false, nil)
		})
	})
	k.foot("桌面歌词浮在其他窗口之上；锁定后鼠标会穿过它，不挡住下面的内容。")
}

// The materials the window is made of, as presets of the five switches
// under 自定义: flat color, the cover's colors with a glass bar and glass
// panels, as the app comes, or liquid glass all through.
const (
	materialFlat = iota
	materialCover
	materialGlass
)

// material returns the preset the settings are at, or -1 for a mix of
// its own.
func (s *settings) material() int {
	switch {
	case s.GlobalGlass:
		return materialGlass
	case s.SidebarGlass && s.MainGlass && s.LiquidGlass && s.GlassPanels:
		return materialCover
	case !s.SidebarGlass && !s.MainGlass && !s.LiquidGlass && !s.GlassPanels:
		return materialFlat
	}
	return -1
}

// materialThumb paints the picture of a material: a window, its
// sidebar and its bar.
func materialThumb(p *ui.Painter, r ui.Rect, kind int, tint ui.Color, dark bool) {
	base, bar := ui.RGB(236, 236, 239), ui.RGBA(0, 0, 0, 0.1)
	if dark {
		base, bar = ui.RGB(42, 42, 46), white.Alpha(0.12)
	}
	main, side := base, base.Mix(ui.RGB(128, 128, 128), 0.1)
	switch kind {
	case materialCover:
		main, side = base.Mix(tint, 0.3), base.Mix(tint, 0.42)
	case materialGlass:
		main, side = base.Mix(tint, 0.5), base.Mix(tint, 0.62)
		bar = white.Alpha(0.3)
	}
	p.Fill(r, main, 8)
	sw := r.W * 0.3
	p.Fill(ui.Rect{X: r.X, Y: r.Y, W: sw, H: r.H}, side, 8)
	line := white.Alpha(0.28)
	if !dark {
		line = ui.RGBA(0, 0, 0, 0.16)
	}
	for i := range 3 {
		p.Fill(ui.Rect{X: r.X + 8, Y: r.Y + 10 + float32(i)*9, W: sw - 16 - float32(i)*5, H: 3}, line, 1.5)
	}
	pill := ui.Rect{X: r.X + sw + 8, Y: r.Y + r.H - 17, W: r.W - sw - 16, H: 10}
	p.Fill(pill, bar, 5)
	if kind == materialGlass {
		p.Stroke(pill, white.Alpha(0.55), 5, 1)
	}
}

// materialGroup chooses the window's material: a preset, or under
// 自定义 each part of it.
func (a *app) materialGroup(k *settingsKit) {
	c, s := k.c, &a.settings
	sv := &a.settingsView
	k.group("材质", func() {
		k.block("材质 纯色 封面色 液态玻璃 玻璃 背景", func() {
			tint, dark := a.thumbTint(), a.pal.dark
			picked, ok := a.choiceCards(c, "material-cards", "材质", []string{"纯色", "封面色", "液态玻璃"}, s.material(), 3, func(i int) {
				ui.Box(c.Key("material")).Height(thumbHeight).Draw(func(p *ui.Painter, r ui.Rect) {
					materialThumb(p, r, i, tint, dark)
				})
			})
			if !ok || picked == s.material() {
				return
			}
			switch picked {
			case materialGlass:
				// Asked first, as its switch asks.
				sv.confirmGlass = true
				return
			case materialFlat:
				s.GlobalGlass, s.SidebarGlass, s.MainGlass, s.LiquidGlass, s.GlassPanels = false, false, false, false, false
			case materialCover:
				s.GlobalGlass, s.SidebarGlass, s.MainGlass, s.LiquidGlass, s.GlassPanels = false, true, true, true, true
				a.leavePowerSaver()
				if a.chosenAccent() != "" {
					a.toast = "已改用封面色背景，所选主题色将不再显示"
				}
			}
			a.changed()
		})
		open := sv.materialOpen
		k.row("自定义", "分别设置侧栏、主界面、播放栏和弹窗", func() {
			if k.disclosure("material-custom", "自定义材质", open).Clicked() {
				sv.materialOpen = !open
				a.redraw()
			}
		})
		// What it opens: in a drawer, but as rows where a search looks.
		parts := func() {
			k.sub("全局液态玻璃", "通透的封面色背景，播放栏、菜单和对话框用折射玻璃", true, func() {
				on := s.GlobalGlass
				if settingSwitch(c.Key("global-glass"), &on).Label("切换全局液态玻璃").Changed() {
					if on {
						sv.confirmGlass = true
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
			k.sub("侧栏封面色背景", "透出当前歌曲封面的颜色", true, func() {
				part("sidebar-glass", "侧栏封面色背景", &s.SidebarGlass, coverOn(&s.SidebarGlass))
			})
			k.sub("主界面封面色背景", "", true, func() {
				part("main-glass", "主界面封面色背景", &s.MainGlass, coverOn(&s.MainGlass))
			})
			k.sub("液态玻璃播放栏", "悬浮的玻璃胶囊，折射其下的内容", true, func() {
				part("liquid-glass", "液态玻璃播放栏", &s.LiquidGlass, a.leavePowerSaver)
			})
			k.sub("界面玻璃", "菜单、弹窗和设置面板使用玻璃材质", true, func() {
				part("glass-panels", "界面玻璃", &s.GlassPanels, a.leavePowerSaver)
			})
			refractive := s.playerGlass() || s.GlobalGlass
			k.sub("玻璃透明度", "", refractive, func() {
				a.settingSlider(c.Key("玻璃透明度"), "玻璃透明度", &s.GlassClear, true, k.ink)
			})
			k.sub("折射强度", "", refractive, func() {
				a.settingSlider(c.Key("折射强度"), "折射强度", &s.GlassBend, true, k.ink)
			})
		}
		if k.probe || k.searching {
			parts()
		} else {
			drawer(c, "material-parts", open, parts)
		}
		k.row("省电模式", "一键关闭玻璃、封面旋转、微光和动态背景；再关一次恢复原样", func() {
			on := s.PowerSaver
			if settingSwitch(c.Key("power-saver"), &on).Label("省电模式").Changed() {
				a.setPowerSaver(on)
			}
		})
	})
	k.foot("省电模式会一键关闭玻璃、封面旋转、微光和动态背景；再关一次恢复原样。")
}

func (a *app) settingsAppearancePage(k *settingsKit) {
	c, s := k.c, &a.settings
	a.settingsThemeGroup(k)
	a.materialGroup(k)
	k.group("动效", func() {
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
		k.row("无视系统动效设置", "系统关闭动画效果时，仍按应用内设置播放动效", func() {
			k.toggle("ignore-system-motion", "切换无视系统动效设置", &s.IgnoreSystemMotion, false, nil)
		})
		k.row("封面旋转", "播放时播放栏里的封面像黑胶一样转动", func() {
			k.toggle("bar-spin", "播放栏封面旋转", &s.BarSpin, false, a.leavePowerSaver)
		})
	})
	k.foot("精简时界面动画只保留淡入淡出；开启「无视系统动效设置」后，即使系统关闭了动画效果也仍按此处设置播放。封面旋转是播放栏里的封面像黑胶一样转动。")
	k.group("文字", func() {
		k.row("界面缩放", "也可以按 Ctrl 加 + 或 - 调整，Ctrl 加 0 恢复", func() {
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
	})
}

func (a *app) settingsHomePage(k *settingsKit) {
	s := &a.settings
	a.deckSettings(k)
	k.group("侧边栏", func() {
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
		k.row("列出全部歌单", "关闭时每组只列出前 8 个，其余在「我的主页」里", func() {
			k.toggle("sidebar-all", "侧边栏列出全部歌单", &s.SidebarAll, false, nil)
		})
	})
	k.foot("关闭「列出全部歌单」时每组只列前 8 个，其余在「我的主页」里。拖动侧边栏右边缘可以调整宽度。")
}

// disclosure is the arrow at the end of a row that opens what is under
// it: pointing at it while closed, and down once open.
func (k *settingsKit) disclosure(key, label string, open bool) ui.Element {
	c := k.c
	b := ui.ButtonBase(c.Key(key)).Size(30, 30).Radius(999).Center().Shrink(0).FocusRing(false).Role(ui.RoleDisclosure).Label(label)
	b.Expanded(open)
	bg := ui.Color{}
	if b.Hovered() {
		bg = k.ink.hover
	}
	b.Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond}))
	b.Children(func() {
		ui.Icon(c, iconDown).Size(15, 15).TextColor(k.ink.muted).Rotate(b.AnimateTo("turn", -90*(1-b2f(open)), motionFast))
	})
	return b
}

// npPreview is Now Playing in small, at the head of its settings: its
// backdrop, alive, and a few lines of lyrics set as the settings say.
type npPreview struct {
	canvas *ui.Canvas
	start  time.Time
	at     time.Time
	w, h   int
}

// previewDrops are drops as music lets them fall, one after another,
// for a picture that moves, at drift seconds.
func previewDrops(drift float32) []float32 {
	const every = 3.4
	out := make([]float32, 4*sceneDrops)
	for i := range 4 {
		phase := float32(i) * every / 4
		n := float32(math.Floor(float64((drift - phase) / every)))
		x := 0.12 + 0.76*float32(math.Mod(float64(0.37*float32(i)+0.618*n+0.2), 1))
		y := 0.1 + 0.8*float32(math.Mod(float64(0.29*float32(i)+0.414*n+0.5), 1))
		out[4*i], out[4*i+1], out[4*i+2], out[4*i+3] = x, y, n*every+phase, 0.9
	}
	return out
}

// nowPlayingPreview shows Now Playing in small: what the settings under
// it change, as they change it.
func (a *app) nowPlayingPreview(c *ui.Context) {
	s := &a.settings
	pv := &a.settingsView.preview
	still := c.Preferences().ReduceMotion
	cover := a.thumbTint()
	style := s.FogStyle
	tint := cover
	if s.Fog {
		tint = s.tintOf(backdropSceneOf(style).id, cover)
	}
	fog := s.Fog
	box := ui.Box(c.Key("np-preview")).Height(196).Radius(20).Margin(18, 0, 0, 0).Background(white.Alpha(0.06)).Label("播放页预览")
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		w, h := int(r.W), int(r.H)
		if w < 2 || h < 2 {
			return
		}
		if pv.canvas == nil || pv.w != w || pv.h != h {
			pv.canvas, pv.w, pv.h = ui.NewCanvas(w, h), w, h
			pv.at = time.Time{}
		}
		now := p.Now()
		if pv.start.IsZero() {
			pv.start = now
		}
		drift := float32(now.Sub(pv.start).Seconds())
		if still {
			drift = 6.4
		}
		if pv.at.IsZero() || now.Sub(pv.at) >= backdropFrame-2*time.Millisecond {
			pv.at = now
			pv.canvas.Draw(p, func(cp *ui.Painter) {
				b := ui.Rect{W: float32(w), H: float32(h)}
				if !fog {
					drawBackdrop(cp, b, nil, tint)
					return
				}
				drops := previewDrops(drift)
				drawSceneDrops(cp, b, style, tint, drift, 0.3, drops, nil)
				drawSceneOver(cp, b, style, tint, drift, 0.3, 1, drops, nil)
			})
		}
		if fog && !still {
			p.After(backdropFrame)
		}
	})
	glow := float32(0)
	if s.NpLight {
		glow = float32(0.35 + 0.65*s.GlowLevel)
	}
	scale := float32(s.LyricScale)
	if scale <= 0 {
		scale = 1
	}
	box.Children(func() {
		if pv.canvas != nil {
			ui.Image(c, pv.canvas.Bitmap()).Absolute().Top(0).Left(0).Right(0).Bottom(0).Fit(ui.Cover).Radius(20)
		}
		ui.Row(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Padding(0, 36).Gap(32).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Width(132).Shrink(0).Gap(10).Children(func() {
				ui.Box(c).Size(104, 104).Radius(14).Background(cover.Mix(white, 0.3)).Center().
					Shadow(0, 10, 24, 0, ui.RGBA(0, 0, 0, 0.35)).Children(func() {
					ui.Icon(c, iconSparkles).Size(30, 30).TextColor(white.Alpha(0.85))
				})
				// The bar: with the glow, and the smoke's point of light.
				smoke := s.Smoke
				ui.Box(c).Height(12).Draw(func(p *ui.Painter, r ui.Rect) {
					y := r.Y + r.H/2
					p.Fill(ui.Rect{X: r.X, Y: y - 1.5, W: r.W, H: 3}, white.Alpha(0.22), 1.5)
					fill := ui.Rect{X: r.X, Y: y - 1.5, W: r.W * 0.42, H: 3}
					if glow > 0 {
						p.Shadow(fill, 1.5, 0, 0, 3+6*glow, 0, white.Alpha(0.55*glow))
					}
					p.Fill(fill, white, 1.5)
					d := float32(7)
					if smoke {
						d = 9
						tail := ui.Rect{X: fill.X + fill.W - 26, Y: y - 2, W: 26, H: 4}
						p.Shadow(tail, 2, 0, 0, 12, 0, white.Alpha(0.35))
					}
					dot := ui.Rect{X: fill.X + fill.W - d/2, Y: y - d/2, W: d, H: d}
					if smoke {
						p.Shadow(dot, d/2, 0, 0, 9, 1, white.Alpha(0.75))
					}
					p.Fill(dot, white, d/2)
				})
			})
			lines := ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).AlignItems(ui.Start)
			if s.LyricPerspective {
				lines.TransformOrigin(0, 0.5).RotateY(-19 * float32(s.LyricTilt)).Perspective(700)
			}
			lines.Children(func() {
				for i, text := range []string{"更早的一句歌词", "上一句歌词", "正在唱的这一句歌词", "下一句歌词", "再下一句歌词"} {
					d := float32(i - 2)
					if d < 0 {
						d = -d
					}
					size, weight, alpha := 13*scale, 500, 0.62-0.26*(d-1)
					if d == 0 {
						size, weight, alpha = 17*scale, 700, 1
					}
					line := ui.Text(c.Key(i), text).FontSize(size).FontWeight(weight).TextColor(white.Alpha(alpha)).SingleLine()
					blur := float32(0)
					if d > 0 && (s.LyricBlur || s.LyricPerspective) {
						blur = d * float32(0.6+2.4*s.LyricBlurLevel)
					}
					line.Blur(line.AnimateTo("blur", blur, ui.Animation{Duration: 220 * time.Millisecond}))
				}
			})
		})
	})
}
