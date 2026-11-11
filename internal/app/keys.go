package app

import (
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type shortcutAction struct {
	id, label string
	keys      []string
	do        func(a *app)
}

// shortcutActions are the keys that work from any app. The first key of
// each that no other app holds is used, for no one key is free on every
// computer. A custom key in the settings replaces the defaults.
var shortcutActions = []shortcutAction{
	{"play", "播放/暂停", []string{"Ctrl+Alt+P", "Ctrl+Alt+Space", "Ctrl+Alt+Home"}, func(a *app) { a.pl.TogglePlay() }},
	{"next", "下一首", []string{"Ctrl+Alt+Right"}, func(a *app) { a.pl.Next() }},
	{"prev", "上一首", []string{"Ctrl+Alt+Left"}, func(a *app) { a.pl.Prev() }},
	{"volume-up", "音量 +", []string{"Ctrl+Alt+Up"}, func(a *app) { a.nudgeVolume(0.05) }},
	{"volume-down", "音量 -", []string{"Ctrl+Alt+Down"}, func(a *app) { a.nudgeVolume(-0.05) }},
	{"mute", "静音/取消静音", []string{"Ctrl+Alt+Shift+M"}, func(a *app) { a.toggleMute() }},
	{"like", "喜欢当前歌曲", []string{"Ctrl+Alt+V", "Ctrl+Alt+L"}, func(a *app) { a.likeCurrent() }},
	{"show", "显示/隐藏窗口", []string{"Ctrl+Alt+M"}, func(a *app) { a.toggleWindow() }},
	{"desk", "桌面歌词 开/关", []string{"Ctrl+Alt+D", "Ctrl+Alt+Shift+D"}, func(a *app) { a.toggleDeskLyrics() }},
	{"desk-lock", "桌面歌词 锁定/解锁", []string{"Ctrl+Alt+Shift+K"}, func(a *app) { a.toggleDeskLock() }},
}

// These are the system's shortcut registry; tests replace them.
var (
	registerShortcut   = mygo.GlobalShortcut.Register
	unregisterShortcut = mygo.GlobalShortcut.Unregister
)

// applyGlobalKeys registers the keys that work from any app, or lets them go.
// Other apps may hold all of those of something, which keys.taken counts.
func (a *app) applyGlobalKeys() {
	for _, key := range a.keys.held {
		unregisterShortcut(key)
	}
	a.keys.held, a.keys.taken = nil, 0
	a.keys.bound = map[string]string{}
	if !a.settings.GlobalKeys {
		return
	}
	for _, k := range shortcutActions {
		keys := k.keys
		if custom := strings.TrimSpace(a.settings.Shortcuts[k.id]); custom != "" {
			keys = []string{custom}
		}
		do, taken := k.do, true
		for _, key := range keys {
			err := registerShortcut(key, func() {
				do(a)
				a.syncThumbs()
				a.redraw()
			})
			if err == nil {
				taken = false
				a.keys.held = append(a.keys.held, key)
				a.keys.bound[k.id] = key
				break
			}
			log.Printf("shortcut %s: %v", key, err)
		}
		if taken {
			a.keys.taken++
		}
	}
}

// pauseKeys lets go of the global shortcuts and turns off the menu items
// with keys while a shortcut is recorded, and takes them back after: the
// system runs a registered hot key, and the window a menu item's key,
// before the recording sees the keys, so that Ctrl+← skipped back a song
// instead of being recorded.
func (a *app) pauseKeys(on bool) {
	if a.keys.paused == on {
		return
	}
	a.keys.paused = on
	for _, it := range a.keys.items {
		it.SetEnabled(!on)
	}
	if !on {
		a.applyGlobalKeys()
		return
	}
	for _, key := range a.keys.held {
		unregisterShortcut(key)
	}
	a.keys.held = nil
}

// toggleWindow shows the window when it is hidden, and hides it when it is
// showing, for the global shortcut.
func (a *app) toggleWindow() {
	if a.win == nil {
		return
	}
	if a.win.IsVisible() {
		a.hideMain()
		return
	}
	a.showMain()
}

func (a *app) nudgeVolume(by float64) {
	a.volume = math.Max(0, math.Min(a.volume+by, 1))
	a.pl.SetVolume(a.volume)
}

// sleepChoices are how long music can play on for.
var sleepChoices = []struct {
	label   string
	minutes int
}{
	{"不定时", 0},
	{"15 分钟后", 15},
	{"30 分钟后", 30},
	{"45 分钟后", 45},
	{"60 分钟后", 60},
	{"90 分钟后", 90},
	{"播完当前歌曲", sleepAfterSong},
}

// sleepAfterSong is the choice that pauses as the song playing ends.
const sleepAfterSong = -1

// setSleep pauses the music after so many minutes, none to not, or as the
// song playing ends for sleepAfterSong.
func (a *app) setSleep(minutes int) {
	if a.sleep.timer != nil {
		a.sleep.timer.Stop()
		a.sleep.timer = nil
	}
	a.sleep.minutes, a.sleep.at = minutes, time.Time{}
	a.pl.StopAfterSong = minutes == sleepAfterSong
	if minutes <= 0 {
		return
	}
	d := time.Duration(minutes) * time.Minute
	a.sleep.at = time.Now().Add(d)
	a.sleep.timer = time.AfterFunc(d, func() { a.post(a.sleepNow) })
}

// sleepNow is the time up: the music fades out as it pauses.
func (a *app) sleepNow() {
	a.setSleep(0)
	if a.pl.Playing() {
		a.pl.TogglePlay()
		a.toast = "定时结束，已暂停播放"
	}
}

// sleepRow lets the listener choose when the music stops, and says when.
func (a *app) sleepRow(c *ui.Context, row func(string, string, func())) {
	desc := "到时间后暂停播放，声音会慢慢淡出；重新打开软件后需要再设置"
	if a.sleep.minutes == sleepAfterSong {
		desc = "当前这首播完后暂停播放"
	}
	if !a.sleep.at.IsZero() {
		left := time.Until(a.sleep.at)
		desc = "还有 " + longTime(left.Round(time.Minute)) + "后暂停播放"
		// Asks for the settings to say so again.
		c.After(30 * time.Second)
	}
	row("定时关闭", desc, func() {
		labels := make([]string, len(sleepChoices))
		chosen := sleepChoices[0].label
		for i, s := range sleepChoices {
			labels[i] = s.label
			if s.minutes == a.sleep.minutes {
				chosen = s.label
			}
		}
		if a.paneSelect(c.Key("sleep"), "定时关闭", &chosen, labels) {
			for _, s := range sleepChoices {
				if s.label == chosen {
					a.setSleep(s.minutes)
				}
			}
		}
	})
}

// lyricShift is how far the lyrics of a song are moved on from what plays,
// as the listener set them.
func (a *app) lyricShift(id int64) time.Duration {
	return time.Duration(a.settings.LyricShift[id]) * time.Millisecond
}

// lyricPos is where in the lyrics of the song playing the music is.
func (a *app) lyricPos() time.Duration {
	pos := a.pl.Position()
	if tr := a.pl.Current(); tr != nil {
		pos += a.lyricShift(tr.ID)
	}
	return pos
}

// lyricScale is how much larger than as designed the lyrics are.
func (a *app) lyricScale() float32 {
	if s := a.settings.LyricScale; s > 0 {
		return float32(s)
	}
	return 1
}

// lyricTools are under the volume while the pointer is over the window, in
// at, a box in the row of the cover and the lyrics: their size, and how early
// or late they run, kept for each song. They are of the view rather than over
// it, as the tabs are: two capsules in a hairline, their words as faint as
// the tabs' until the pointer is on them, and a value set brighter than one
// left as it was.
func (a *app) lyricTools(c *ui.Context, id int64, at ui.Rect) {
	s := &a.settings
	shift := s.LyricShift[id]
	move := func(by int) {
		if s.LyricShift == nil {
			s.LyricShift = map[int64]int{}
		}
		if shift += by; shift == 0 {
			delete(s.LyricShift, id)
		} else {
			s.LyricShift[id] = shift
		}
		a.changed()
	}
	size := func(by float64) {
		s.LyricScale = math.Max(0.7, math.Min(float64(a.lyricScale())+by, 1.6))
		a.changed()
	}
	scale := int(math.Round(float64(a.lyricScale()) * 100))
	// Hidden till the pointer comes over them: most of the time no one
	// needs them, and they are in the way of the cover's calm.
	bar := ui.Row(c.Key("lyric-tools")).Absolute().Left(at.X).Top(at.Y-8).Width(at.W).Padding(8, 0, 8, 0).
		Justify(ui.Center).Gap(10).AlignItems(ui.Center).Label("歌词调整")
	reveal(bar, bar.Hovered()).Children(func() {
		group := func(key string, chips func()) {
			ui.Row(c.Key(key)).Height(32).Padding(2).Radius(999).AlignItems(ui.Center).
				Background(white.Alpha(0.04)).Border(1, white.Alpha(0.1)).Children(chips)
		}
		fade := ui.Animation{Duration: 120 * time.Millisecond}
		// Every button keeps its width, whatever it says: one that changed
		// would move the ones beside it from under the pointer, between two
		// clicks.
		chip := func(key, says string, wide float32, set bool, do func(), label func(fg ui.Color) ui.Element) {
			b := ui.ButtonBase(c.Key(key)).Size(wide, 26).Radius(999).Center().FocusRing(false).Label(says)
			bg, fg := ui.Transparent, white.Alpha(0.55)
			if set {
				fg = white.Alpha(0.85)
			}
			if b.Hovered() {
				bg, fg = white.Alpha(0.1), white
			}
			b.Background(b.AnimateColor("bg", bg, fade))
			fg = b.AnimateColor("fg", fg, fade)
			b.Children(func() { label(fg).FontWeight(600).SingleLine() })
			if b.Clicked() {
				do()
			}
		}
		word := func(text string, size float32) func(ui.Color) ui.Element {
			return func(fg ui.Color) ui.Element { return ui.Text(c, text).FontSize(size).TextColor(fg) }
		}
		number := func(text string) func(ui.Color) ui.Element {
			return func(fg ui.Color) ui.Element {
				return ui.Text(c, text).FontSize(12).FontFeatures("tnum").TextColor(fg)
			}
		}
		// The size, between a small A and a large one.
		group("size", func() {
			chip("smaller", "歌词字号减小", 30, false, func() { size(-0.1) }, word("A", 11))
			chip("scale", "恢复歌词字号", 48, scale != 100, func() {
				s.LyricScale = 1
				a.changed()
			}, number(fmt.Sprintf("%d%%", scale)))
			chip("larger", "歌词字号增大", 30, false, func() { size(0.1) }, word("A", 16))
		})
		group("time", func() {
			chip("earlier", "歌词提前 0.1 秒", 44, false, func() { move(100) }, word("提前", 13))
			chip("reset", "恢复歌词时间", 52, shift != 0, func() { move(-shift) }, number(fmt.Sprintf("%+.1fs", float64(shift)/1000)))
			chip("later", "歌词延后 0.1 秒", 44, false, func() { move(-100) }, word("延后", 13))
		})
		// The rest of the lyrics' settings, in the settings.
		group("more", func() {
			chip("more", "更多歌词设置", 30, false, func() { a.openSettingsAt(settingsLyrics) }, func(fg ui.Color) ui.Element {
				return ui.Icon(c, iconSettings).Size(14, 14).TextColor(fg)
			})
		})
	})
}
