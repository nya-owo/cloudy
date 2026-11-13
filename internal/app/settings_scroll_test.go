package app

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// Scrolling settings must keep the viewport's text as glyphs, not a resampled image.
func TestSettingsScrollKeepsDirectText(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.path = filepath.Join(t.TempDir(), "settings.json")
	a.settingsView.open, a.settingsView.tab, a.updates = true, settingsEffects, true
	tt := ui.NewTester(a.view, 1332, 849)
	tt.Frame()
	tt.Advance(time.Second)
	pane, ok := tt.Find("视觉效果设置项")
	if !ok {
		t.Fatal("settings viewport missing")
	}
	tt.Scroll(pane.X+50, pane.Y+pane.H/2, 0, 320)
	tt.Advance(time.Second)
	tt.Frame()
	if a.settingsView.scroll[settingsEffects].Y <= 24 {
		t.Fatal("settings did not scroll past the edge fade")
	}
	// The pinned Tester's display list exposes whether the middle of the
	// viewport still has text coverage in the final scene. Read only: no unsafe.
	glyphs := reflect.ValueOf(tt).Elem().FieldByName("h").Elem().FieldByName("last").Elem().FieldByName("Glyphs")
	direct := 0
	for i := 0; i < glyphs.Len(); i++ {
		g := glyphs.Index(i)
		x, y := g.FieldByName("X").Float(), g.FieldByName("Y").Float()
		if x > float64(pane.X+18) && x < float64(pane.X+pane.W-30) && y > float64(pane.Y+60) && y < float64(pane.Y+pane.H-30) {
			direct++
		}
	}
	if direct < 30 {
		t.Fatalf("scrolled viewport text was flattened into an image: direct glyphs=%d", direct)
	}
	before := a.settings.BarSpin
	if err := tt.Click("播放栏封面旋转"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.settings.BarSpin == before {
		t.Fatal("scrolled switch did not toggle")
	}
	tt.Scroll(pane.X+50, pane.Y+pane.H/2, 0, -1000)
	tt.Advance(time.Second)
	tt.Frame()
	if a.settingsView.scroll[settingsEffects].Y != 0 {
		t.Fatal("settings failed to return to top")
	}
}
