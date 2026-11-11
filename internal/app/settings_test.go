package app

import (
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// Global glass takes effect only after confirmation, and can be turned off
// directly. Cancelling the prompt keeps the previous setting.
func TestGlobalGlassRequiresConfirmation(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.path = filepath.Join(t.TempDir(), "settings.json")
	a.settingsView.open, a.settingsView.tab = true, settingsEffects
	tt := ui.NewTester(a.view, 1000, 700)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	click := func(label string) {
		t.Helper()
		if err := tt.Click(label); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
	}
	click("切换全局液态玻璃")
	if !a.settingsView.confirmGlass || a.settings.GlobalGlass {
		t.Fatal("enabling glass should wait for confirmation")
	}
	click("取消")
	if a.settingsView.confirmGlass || a.settings.GlobalGlass || !a.settingsView.open {
		t.Fatal("cancelling should leave settings open with global glass off")
	}
	click("切换全局液态玻璃")
	click("确认开启")
	if a.settingsView.confirmGlass || !a.settings.GlobalGlass || !loadSettingsFrom(a.settings.path).GlobalGlass {
		t.Fatal("confirmation should apply and save global glass")
	}
	click("切换全局液态玻璃")
	if a.settingsView.confirmGlass || a.settings.GlobalGlass || loadSettingsFrom(a.settings.path).GlobalGlass {
		t.Fatal("disabling should apply and save directly")
	}
}

// Switching categories keeps edited settings and each category's scroll position.
func TestSettingsCategoryNavigation(t *testing.T) {
	asked := make(chan struct{}, 1)
	defer fakeUpdateCheck(asked)()
	a := newTestApp()
	a.settings = defaultSettings()
	a.settingsView.open, a.updates = true, true
	tt := ui.NewTester(a.view, 1000, 700)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	choose := func(label string, want settingsCategory) {
		t.Helper()
		if err := tt.Click("设置分类：" + label); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if a.settingsView.tab != want {
			t.Fatalf("%s opened category %d", label, a.settingsView.tab)
		}
	}
	toggle := func(label, desc string) {
		t.Helper()
		title, ok1 := tt.Find(label)
		note, ok2 := tt.Find(desc)
		pane, ok3 := tt.Find(settingsCategories[a.settingsView.tab].label + "设置项")
		if !ok1 || !ok2 || !ok3 {
			t.Fatalf("the %s control is not in this category", label)
		}
		tt.ClickAt(pane.X+pane.W-30-20-12, (title.Y+note.Y+note.H)/2)
		tt.Frame()
	}
	choose("视觉效果", settingsEffects)
	wasAccent := a.settings.AccentCover
	a.settings.AccentCover = !wasAccent
	pane, _ := tt.Find("视觉效果设置项")
	tt.Scroll(pane.X+40, pane.Y+pane.H/2, 0, 320)
	tt.Advance(time.Second)
	tt.Frame()
	at := a.settingsView.scroll[settingsEffects].Y
	if at <= 0 {
		t.Fatal("the effects category did not scroll")
	}
	choose("账号", settingsAccount)
	if a.settingsView.scroll[settingsAccount].Y != 0 || a.settings.AccentCover == wasAccent {
		t.Fatal("switching categories lost the edit or shared the scroll position")
	}
	choose("视觉效果", settingsEffects)
	if math.Abs(float64(a.settingsView.scroll[settingsEffects].Y-at)) > 1 {
		t.Fatal("returning to effects lost its scroll position")
	}
	choose("播放", settingsPlayback)
	choose("关于", settingsAbout)
	toggle("自动安装更新", "在后台下载并装好新版本，不弹窗，下次打开软件时生效")
	if !a.settings.UpdateInstall {
		t.Fatal("the update preference did not change")
	}
	choose("账号", settingsAccount)
	choose("关于", settingsAbout)
	if !a.settings.UpdateInstall {
		t.Fatal("returning to about lost the update preference")
	}
	if err := tt.Click("检查更新"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-asked:
	case <-time.After(2 * time.Second):
		t.Fatal("the about category did not check for updates")
	}
}

// With NM_FRAMES set, saves each settings category at regular and compact sizes.
func TestSettingsCategoryFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	for _, size := range []struct{ w, h int }{{1180, 780}, {880, 580}} {
		for category := settingsAccount; category < settingsCategoryCount; category++ {
			a := newTestApp()
			a.settings = defaultSettings()
			a.settingsView.open, a.updates = true, true
			a.settingsView.tab = category
			a.profile = &netease.Profile{UserID: 1, Nickname: "试听者"}
			tt := ui.NewTester(a.view, size.w, size.h)
			tt.SetDark(true)
			tt.SetPreferences(ui.Preferences{ReduceMotion: true})
			tt.Frame()
			name := fmt.Sprintf("settings-%d-%d.png", int(size.w), category)
			f, err := os.Create(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(f, tt.Image()); err != nil {
				t.Fatal(err)
			}
			f.Close()
		}
	}
}

// Every category opens, and each has something in it.
func TestSettingsEveryCategory(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settingsView.open, a.updates = true, true
	tt := ui.NewTester(a.view, 1000, 700)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	for i, cat := range settingsCategories {
		if err := tt.Click("设置分类：" + cat.label); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if a.settingsView.tab != settingsCategory(i) {
			t.Fatalf("%s opened category %d", cat.label, a.settingsView.tab)
		}
		if _, ok := tt.Find(cat.label + "设置项"); !ok {
			t.Fatalf("%s shows no settings", cat.label)
		}
	}
	for _, label := range []string{"律动效果", "桌面歌词字号", "歌词字号"} {
		if err := tt.Click("设置分类：歌词"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		_, ok := tt.Find(label)
		if want := label == "歌词字号"; ok != want {
			t.Fatalf("%s shown: %v, with its switch off", label, ok)
		}
	}
}

// The search finds settings in any category, by their words or their
// group's, and says so when nothing matches.
func TestSettingsSearch(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settingsView.open, a.updates = true, true
	tt := ui.NewTester(a.view, 1000, 700)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	search := func(q string) {
		t.Helper()
		a.settingsView.query = q
		tt.Frame()
		tt.Frame()
	}
	search("翻译")
	if !tt.HasText("歌词翻译") || tt.HasText("音量均衡") {
		t.Fatal("searching 翻译 did not find just the translation")
	}
	search("缓存")
	if !tt.HasText("歌曲缓存") || !tt.HasText("清除缓存") || !tt.HasText("下载与缓存") {
		t.Fatal("searching 缓存 did not find the cache under its category")
	}
	// A group's title shows all its rows.
	search("桌面歌词")
	if !tt.HasText("锁定") || !tt.HasText("字号") {
		t.Fatal("searching a group did not show all its rows")
	}
	search("没有这种设置")
	if !tt.HasText("没有找到「没有这种设置」相关的设置") {
		t.Fatal("an empty search said nothing")
	}
	if err := tt.Click("设置分类：播放"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.settingsView.query != "" || a.settingsView.tab != settingsPlayback {
		t.Fatal("choosing a category did not leave the search")
	}
}

// The power saver turns the costly effects off and puts them back as they
// were; turning one on by hand leaves it.
func TestPowerSaver(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	s := &a.settings
	s.Fog, s.Smoke = true, false
	s.GlobalGlass, s.SidebarGlass, s.MainGlass = true, true, false
	a.setPowerSaver(true)
	if s.panelGlass() || s.playerGlass() || s.sidebarGlass() || s.mainGlass() || s.BarSpin || s.NpLight || s.Fog || s.Smoke || !s.PowerSaver {
		t.Fatal("the saver left an effect on")
	}
	a.setPowerSaver(false)
	if !s.LiquidGlass || !s.GlobalGlass || !s.SidebarGlass || s.MainGlass || !s.BarSpin || !s.NpLight || !s.Fog || s.Smoke || s.PowerSaver {
		t.Fatalf("the saver did not put the effects back: %+v", s)
	}
	a.setPowerSaver(true)
	s.Fog = true
	a.leavePowerSaver()
	if s.PowerSaver || s.LiquidGlass || !s.Fog {
		t.Fatal("turning an effect on did not leave the saver as it was")
	}
}

// With NM_FRAMES set, saves a search of the settings, and a category in the
// light theme.
func TestSettingsSearchFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	for _, shot := range []struct {
		name, query string
		tab         settingsCategory
		dark        bool
	}{{"settings-search.png", "缓存", 0, true}, {"settings-light.png", "", settingsEffects, false}} {
		a := newTestApp()
		a.settings = defaultSettings()
		a.settingsView.open, a.updates = true, true
		a.settingsView.tab, a.settingsView.query = shot.tab, shot.query
		tt := ui.NewTester(a.view, 1180, 780)
		tt.SetDark(shot.dark)
		tt.SetPreferences(ui.Preferences{ReduceMotion: true})
		tt.Frame()
		tt.Frame()
		f, err := os.Create(filepath.Join(dir, shot.name))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}
}

// While NetEase counts none of the plays told it, the setting shows as not
// open yet, its switch off and not to be turned on, whatever was kept.
func TestScrobbleNotOpen(t *testing.T) {
	defer func(works bool) { scrobbleWorks = works }(scrobbleWorks)
	scrobbleWorks = false
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.Scrobble = true
	a.profile = &netease.Profile{UserID: 1, Nickname: "试听者"}
	a.settingsView.open, a.settingsView.tab = true, settingsAccount
	tt := ui.NewTester(a.view, 1000, 700)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	if !tt.HasText("未开放") || !tt.HasText("计入网易云听歌记录") {
		t.Fatal("the setting is not shown as not open")
	}
	_ = tt.Click("计入网易云听歌记录（未开放）")
	tt.Frame()
	if !a.settings.Scrobble {
		t.Fatal("clicking the closed switch changed what was kept")
	}
}

// With NM_FRAMES set, saves the desktop lyrics' capsule on its way out of
// the button, a frame at each tenth of its way.
func TestDeskFlyFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	a := newTestApp()
	a.settings = defaultSettings()
	f := &deskFlight{opening: true, from: ui.Rect{X: 760, Y: 330, W: 32, H: 32},
		to: ui.Rect{X: 60, Y: 40, W: 560, H: 65}, text: "我们在晚风里说再见", size: 32}
	for i := 0; i <= 10; i++ {
		out := float32(i) / 10
		tt := ui.NewTester(func(c *ui.Context) {
			a.pal = paletteFor(c.Theme())
			ui.Box(c).Fill().Background(ui.Hex("#5b6b7a"))
			a.paintDeskFly(c, f, out, 1)
		}, 820, 380)
		tt.Frame()
		file, err := os.Create(filepath.Join(dir, fmt.Sprintf("deskfly-%02d.png", i)))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(file, tt.Image()); err != nil {
			t.Fatal(err)
		}
		file.Close()
	}
}

// The mail is off: its button says so and sending does nothing.
func TestFeedbackMailClosed(t *testing.T) {
	if feedbackMailOpen {
		t.Skip("the mail is open")
	}
	a := newTestApp()
	a.settings = defaultSettings()
	a.settingsView.open, a.settingsView.tab = true, settingsAbout
	a.feedback.text = "你好"
	tt := ui.NewTester(a.view, 1100, 900)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	pane, _ := tt.Find("关于设置项")
	tt.Scroll(pane.X+40, pane.Y+pane.H/2, 0, 2000)
	tt.Advance(time.Second)
	tt.Frame()
	if !tt.HasText("用邮件发送 · 未开放") {
		t.Fatal("the mail button does not say it is closed")
	}
	a.sendFeedback()
	if a.feedback.sending {
		t.Fatal("feedback went by mail while it is closed")
	}
}

// Enabling any glass option exits the saver without restoring unrelated effects.
func TestGlassOptionLeavesPowerSaver(t *testing.T) {
	for _, which := range []string{"global", "sidebar", "main", "panels"} {
		t.Run(which, func(t *testing.T) {
			a := newTestApp()
			a.settings = defaultSettings()
			a.setPowerSaver(true)
			switch which {
			case "global":
				a.settings.GlobalGlass = true
			case "sidebar":
				a.settings.SidebarGlass = true
			case "main":
				a.settings.MainGlass = true
			case "panels":
				a.settings.GlassPanels = true
			}
			a.leavePowerSaver()
			if a.settings.PowerSaver || a.settings.SaverBefore != nil || a.settings.BarSpin {
				t.Fatal("glass option did not leave the saver independently")
			}
		})
	}
}
