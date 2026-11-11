package app

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// fakeUpdateCheck swaps the check for one that says it was asked, and an
// install that does nothing, until the returned function puts them back.
func fakeUpdateCheck(asked chan<- struct{}) func() {
	check, install := checkUpdate, installUpdate
	checkUpdate = func(context.Context) (*mygo.Update, error) {
		select {
		case asked <- struct{}{}:
		default:
		}
		return nil, nil
	}
	installUpdate = func(*mygo.Update, func(int64, int64)) error { return nil }
	return func() { checkUpdate, installUpdate = check, install }
}

// 自动安装更新 has the app install updates without asking, and turning off
// the reminder turns checking off with it, as nothing would find the updates
// to install.
func TestUpdateSettings(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.updates = true
	a.settingsView.tab = settingsAbout
	tt := ui.NewTester(a.settingsBody, 560, 3000)
	tt.Frame()
	// The row's title is labelled as its switch is: click the switch, at
	// the row's end.
	toggle := func(title, desc string) {
		t.Helper()
		top, ok1 := tt.Find(title)
		bottom, ok2 := tt.Find(desc)
		if !ok1 || !ok2 {
			t.Fatalf("no row %s", title)
		}
		tt.ClickAt(560-24-16-12, (top.Y+bottom.Y+bottom.H)/2)
		tt.Frame()
	}
	toggle("自动安装更新", "在后台下载并装好新版本，不弹窗，下次打开软件时生效")
	if !a.settings.UpdateInstall {
		t.Fatal("still asks before installing updates")
	}
	toggle("提醒新版本", "每天检查一次是否有新版本")
	if a.settings.UpdateChecks {
		t.Fatal("checks for updates still")
	}
	if tt.HasText("自动安装更新") {
		t.Error("offers installing updates with no checks to find them")
	}
}

// 检查更新 in the settings checks for updates as the user asked.
func TestCheckForUpdatesFromSettings(t *testing.T) {
	asked := make(chan struct{}, 1)
	defer fakeUpdateCheck(asked)()
	a := newTestApp()
	a.settings = defaultSettings()
	a.updates = true
	a.settingsView.tab = settingsAbout
	a.profile = &netease.Profile{UserID: 1}
	tt := ui.NewTester(a.settingsBody, 560, 3000)
	tt.Frame()
	if err := tt.Click("检查更新"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-asked:
	case <-time.After(2 * time.Second):
		t.Fatal("did not check for updates")
	}
}

// A check that finds nothing says so, one that finds an update offers it,
// and one that fails says why; an install the listener asked for leaves the
// asking out.
func TestCheckedUpdates(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.checkedUpdates(nil, nil)
	if !a.update.checked || a.update.up != nil || a.update.err != "" {
		t.Fatalf("after an up-to-date check: %+v", a.update)
	}
	a.checkedUpdates(&mygo.Update{Version: "9.9.9"}, nil)
	if a.update.up == nil || !a.update.open {
		t.Fatalf("an update found is not offered: %+v", a.update)
	}
	a.checkedUpdates(nil, mygo.ErrUpdatesDisabled)
	if !strings.Contains(a.update.err, "不能自动更新") {
		t.Errorf("a check that cannot run says %q", a.update.err)
	}

	installed := make(chan struct{}, 1)
	real := installUpdate
	installUpdate = func(*mygo.Update, func(int64, int64)) error {
		select {
		case installed <- struct{}{}:
		default:
		}
		return nil
	}
	defer func() { installUpdate = real }()
	a.settings.UpdateInstall = true
	for _, auto := range []bool{true, false} {
		a.update = updateState{auto: auto}
		a.checkedUpdates(&mygo.Update{Version: "9.9.9"}, nil)
		select {
		case <-installed:
		case <-time.After(2 * time.Second):
			t.Fatal("did not install the update it was told to")
		}
		// Its own check installs in the background; one the listener
		// asked for says what it found as it downloads.
		if a.update.open == auto {
			t.Errorf("auto %v: the pane open is %v", auto, a.update.open)
		}
	}
}

// With NM_FRAMES set, saves the update pane in its states, in the light and
// the dark, for looking at them.
func TestUpdatePaneFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip("set NM_FRAMES to a directory to save frames")
	}
	up := &mygo.Update{
		Version: "1.2.0",
		Date:    time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC),
		Notes:   "## 1.2.0\n\n- 设置里可以选背景色，深浅两套各自适配\n- 侧栏不再跟着封面变色\n- 修好了几个小问题",
	}
	save := func(name string, tt *ui.Tester) {
		tt.Advance(time.Second)
		tt.Frame()
		f, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}
	show := func(dark bool, u updateState) *ui.Tester {
		a := newTestApp()
		accentHome(a)
		a.update = u
		tt := ui.NewTester(a.view, 1280, 820)
		tt.SetDark(dark)
		return tt
	}
	for _, dark := range []bool{false, true} {
		appearance := "light"
		if dark {
			appearance = "dark"
		}
		save("update-found-"+appearance, show(dark, updateState{open: true, checked: true, up: up}))
	}
	save("update-checking", show(true, updateState{open: true, checking: true}))
	save("update-downloading", show(true, updateState{open: true, up: up, downloading: true, got: 38, total: 100}))
	save("update-ready", show(true, updateState{open: true, up: up, ready: true}))
	save("update-error", show(true, updateState{open: true, checked: true, err: updateError(mygo.ErrUpdatesDisabled)}))
	save("update-current", show(false, updateState{open: true, checked: true}))
}

// Cloud-nya's data moves to Cloudy's directory, rather than the data of the
// name before it, kept beside it too.
func TestMoveFromCloudNya(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "Cloudy")
	for name, text := range map[string]string{lastName: "nya", oldName: "older"} {
		os.MkdirAll(filepath.Join(root, name), 0o700)
		os.WriteFile(filepath.Join(root, name, "settings.json"), []byte(text), 0o600)
	}
	os.MkdirAll(dir, 0o700)
	moveFromOldNames(dir)
	if b, _ := os.ReadFile(filepath.Join(dir, "settings.json")); string(b) != "nya" {
		t.Fatalf("settings %q after moving", b)
	}
	if _, err := os.Stat(filepath.Join(root, oldName, "settings.json")); err != nil {
		t.Fatalf("the older data went too: %v", err)
	}
}

// The data kept under the app's old name moves to the directory of its
// name, which Path made empty, but not over data kept there already.
func TestMoveOldData(t *testing.T) {
	root := t.TempDir()
	old, dir := filepath.Join(root, oldName), filepath.Join(root, "MyGo 云音乐")
	os.MkdirAll(old, 0o700)
	os.WriteFile(filepath.Join(old, "settings.json"), []byte("old"), 0o600)
	os.MkdirAll(dir, 0o700)
	moveDir(old, dir)
	if b, _ := os.ReadFile(filepath.Join(dir, "settings.json")); string(b) != "old" {
		t.Fatalf("settings %q after moving", b)
	}

	os.MkdirAll(old, 0o700)
	os.WriteFile(filepath.Join(old, "settings.json"), []byte("older"), 0o600)
	moveDir(old, dir)
	if b, _ := os.ReadFile(filepath.Join(dir, "settings.json")); string(b) != "old" {
		t.Fatalf("settings %q, moved over what was there", b)
	}
}
