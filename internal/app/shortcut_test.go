package app

import (
	"time"

	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// A recorded key is written the way the system's shortcut registry reads it.
func TestShortcutFromEvent(t *testing.T) {
	cases := []struct {
		ev   ui.InputEvent
		want string
		ok   bool
	}{
		{ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyP, Mods: ui.Ctrl | ui.Alt}, "Ctrl+Alt+P", true},
		{ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyF5}, "F5", true},
		{ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyRight, Mods: ui.Ctrl | ui.Shift}, "Ctrl+Shift+Right", true},
		{ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyP, Mods: ui.Shift}, "", false},
		{ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyP}, "", false},
		{ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyUnknown, Mods: ui.Ctrl}, "", false},
	}
	for _, c := range cases {
		got, ok := shortcutFromEvent(c.ev)
		if got != c.want || ok != c.ok {
			t.Errorf("%+v: got %q %v, want %q %v", c.ev, got, ok, c.want, c.ok)
		}
	}
}

// Recording an action saves its key and registers that key at once.
func TestShortcutEditorRecords(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.GlobalKeys = true
	oldRegister, oldUnregister := registerShortcut, unregisterShortcut
	var registered []string
	registerShortcut = func(acc string, fn func()) error {
		registered = append(registered, acc)
		return nil
	}
	unregisterShortcut = func(string) {}
	defer func() { registerShortcut, unregisterShortcut = oldRegister, oldUnregister }()
	a.applyGlobalKeys()
	a.keys.setup.recording = "play"
	if !a.recordShortcut(ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyK, Mods: ui.Ctrl | ui.Alt}) {
		t.Fatal("the key was not taken")
	}
	if got := a.settings.Shortcuts["play"]; got != "Ctrl+Alt+K" {
		t.Fatalf("saved %q", got)
	}
	if got := a.keys.bound["play"]; got != "Ctrl+Alt+K" {
		t.Fatalf("registered %q, all %v", got, registered)
	}
	if a.keys.setup.recording != "" {
		t.Fatal("still recording")
	}
}

// A letter without a modifier is too easy to trigger by accident.
func TestShortcutRecordingRejectsALetterAlone(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.keys.setup.recording = "play"
	a.recordShortcut(ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyP})
	if a.settings.Shortcuts["play"] != "" || a.keys.setup.err == "" {
		t.Fatalf("saved %q, err %q", a.settings.Shortcuts["play"], a.keys.setup.err)
	}
}

// A custom key already used by another action is refused, naming that action.
func TestShortcutRecordingRejectsDuplicate(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.Shortcuts = map[string]string{"play": "Ctrl+Alt+K"}
	a.keys.setup.recording = "next"
	a.recordShortcut(ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyK, Mods: ui.Ctrl | ui.Alt})
	if a.settings.Shortcuts["next"] != "" || !strings.Contains(a.keys.setup.err, "播放/暂停") {
		t.Fatalf("saved %q, err %q", a.settings.Shortcuts["next"], a.keys.setup.err)
	}
	if a.keys.setup.recording != "next" {
		t.Fatal("the action stopped recording")
	}
}

// A key held by another app counts as taken rather than replacing its action.
func TestShortcutRegisterFailure(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.GlobalKeys = true
	oldRegister, oldUnregister := registerShortcut, unregisterShortcut
	registerShortcut = func(string, func()) error { return errors.New("taken") }
	unregisterShortcut = func(string) {}
	defer func() { registerShortcut, unregisterShortcut = oldRegister, oldUnregister }()
	a.applyGlobalKeys()
	if a.keys.taken != len(shortcutActions) || len(a.keys.bound) != 0 {
		t.Fatalf("taken %d, keys %v", a.keys.taken, a.keys.bound)
	}
}

// The settings' Custom button opens the editor, where a recorded key is
// saved for the action chosen.
func TestShortcutEditorFromSettings(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	oldRegister, oldUnregister := registerShortcut, unregisterShortcut
	registerShortcut = func(string, func()) error { return nil }
	unregisterShortcut = func(string) {}
	defer func() { registerShortcut, unregisterShortcut = oldRegister, oldUnregister }()
	a.applyGlobalKeys()
	a.openSettingsAt(settingsSystem)
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if err := tt.Click("自定义"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("自定义全局快捷键") {
		t.Fatalf("editor texts %q", tt.Texts())
	}
	if err := tt.Click("录制"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(ui.Ctrl|ui.Alt, ui.KeyK)
	tt.Frame()
	if got := a.settings.Shortcuts["play"]; got != "Ctrl+Alt+K" {
		t.Fatalf("saved %q", got)
	}
}

// With NM_FRAMES set, saves the shortcut editor at regular and compact sizes.
func TestShortcutEditorFrames(t *testing.T) {
	dir := os.Getenv("NM_FRAMES")
	if dir == "" {
		t.Skip()
	}
	for _, size := range []struct{ w, h int }{{1180, 780}, {880, 580}} {
		for _, dark := range []bool{false, true} {
			for _, off := range []bool{false, true} {
				a := newTestApp()
				a.settings = defaultSettings()
				a.settings.GlobalKeys = !off
				a.settings.Shortcuts = map[string]string{
					"play": "Ctrl+Alt+K",
					"mute": "Ctrl+Alt+Shift+X",
				}
				a.keys.bound = map[string]string{
					"play": "Ctrl+Alt+K", "next": "Ctrl+Alt+Right",
					"prev": "Ctrl+Alt+Left", "volume-up": "Ctrl+Alt+Up",
					"volume-down": "Ctrl+Alt+Down", "like": "Ctrl+Alt+V",
					"show": "Ctrl+Alt+M",
				}
				a.keys.setup.open = true
				tt := ui.NewTester(a.view, size.w, size.h)
				tt.SetDark(dark)
				tt.SetPreferences(ui.Preferences{ReduceMotion: true})
				tt.Frame()
				// Once what fades in has.
				tt.Advance(time.Second)
				tt.Frame()
				name := fmt.Sprintf("shortcuts-%d-%t-%t.png", size.w, dark, off)
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
}

// While a key is recorded the global keys are let go, so the system does
// not run them in place of recording them, and come back after.
func TestShortcutRecordingPausesKeys(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.GlobalKeys = true
	oldRegister, oldUnregister := registerShortcut, unregisterShortcut
	held := map[string]bool{}
	registerShortcut = func(acc string, fn func()) error { held[acc] = true; return nil }
	unregisterShortcut = func(acc string) { delete(held, acc) }
	defer func() { registerShortcut, unregisterShortcut = oldRegister, oldUnregister }()
	a.applyGlobalKeys()
	if !held["Ctrl+Alt+Left"] {
		t.Fatal("Ctrl+Alt+Left is not registered")
	}
	a.pauseKeys(true)
	if len(held) != 0 {
		t.Fatalf("keys still held while recording: %v", held)
	}
	if a.keys.bound["prev"] != "Ctrl+Alt+Left" {
		t.Fatal("the keys shown were forgotten")
	}
	a.pauseKeys(false)
	if !held["Ctrl+Alt+Left"] {
		t.Fatal("the keys did not come back")
	}
}
