package app

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
)

// Choosing either Stage mode must save immediately, while retired values load as standard without rewriting the file.
func TestStageTextRenderSetting(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.path = filepath.Join(t.TempDir(), "settings.json")
	a.settingsView.open, a.settingsView.tab = true, settingsLyrics
	a.settingsView.query = "律动歌词渲染模式"
	tt := ui.NewTester(a.view, 1332, 849)
	tt.SetPreferences(ui.Preferences{ReduceMotion: true})
	tt.Frame()
	tt.Advance(time.Second)
	for _, i := range []int{1, 0, 1, 0} {
		if err := tt.Click("选择律动歌词渲染模式"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if err := tt.Click(stageTextRenderModes[i].label); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if a.settings.StageTextRender != stageTextRenderModes[i].id || loadSettingsFrom(a.settings.path).StageTextRender != stageTextRenderModes[i].id {
			t.Fatal("mode was not applied and persisted immediately")
		}
	}
	for _, raw := range []string{`{}`, `{"stageTextRender":"native"}`, `{"stageTextRender":"native-cached"}`, `{"stageTextRender":"native-real-time"}`, `{"stageTextRender":"obsolete"}`} {
		if err := os.WriteFile(a.settings.path, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		s := loadSettingsFrom(a.settings.path)
		unchanged, _ := os.ReadFile(a.settings.path)
		if string(unchanged) != raw {
			t.Fatal("reading legacy value rewrote settings")
		}
		if s.StageTextRender != "" {
			t.Fatal("old or invalid settings did not fall back to standard")
		}
		if err := s.save(); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(s.path)
		if bytes.Contains(b, []byte("stageTextRender")) {
			t.Fatal("standard mode must remain omitted for compatibility")
		}
	}
}

// High-quality mode must fall back to larger native masks without moving the text box when no engine is available.
func TestStageTextRasterSizeAndLayout(t *testing.T) {
	a := newTestApp()
	mode := ""
	var bounds ui.Rect
	tt := ui.NewTester(func(c *ui.Context) {
		a.settings.StageTextRender = mode
		ui.Box(c).Fill().Background(ui.RGB(20, 20, 24)).Children(func() {
			ui.Box(c).Absolute().Left(40).Top(30).TransformOrigin(0, 0).Scale(1.6, 1.6).Children(func() {
				e := ui.Text(c, "律动 Native").FontSize(36).FontWeight(800).NoWrap().TextColor(white)
				bounds = e.LayoutBounds()
				a.stageTextRaster(e, "律动 Native", ui.Font{Family: c.Theme().Font, Size: 36, Weight: 800}, 1.6, white)
			})
		})
	}, 700, 170)
	tt.SetScale(1.25)
	tt.Frame()
	box := bounds
	textureWidth := func() uint64 {
		glyphs := reflect.ValueOf(tt).Elem().FieldByName("h").Elem().FieldByName("last").Elem().FieldByName("Glyphs")
		var w uint64
		for i := 0; i < glyphs.Len(); i++ {
			g := glyphs.Index(i)
			if g.FieldByName("Color").FieldByName("A").Uint() != 0 {
				w += g.FieldByName("UW").Uint()
			}
		}
		return w
	}
	old := textureWidth()
	for _, m := range []string{"msdf"} {
		mode = m
		tt.Frame()
		if bounds != box {
			t.Fatalf("%s changed layout: %v vs %v", m, bounds, box)
		}
		if textureWidth() < old*14/10 {
			t.Fatalf("%s still rasterized small glyphs before magnification", m)
		}
	}
	mode = ""
	tt.Frame()
	if textureWidth() != old || bounds != box {
		t.Fatal("switching back did not restore the original path")
	}
}

// Physical-size bins must stay half a pixel wide across DPI and nested stage scales.
func TestStageTextPhysicalSizeBins(t *testing.T) {
	for _, dpi := range []float32{1, 1.25, 1.5} {
		for _, scale := range []float32{.5, 1, 1.237, 1.82, 1.05 * 1.07 * 1.7 * 1.4} {
			px := float32(42) * scale * dpi
			cached := stageRasterRatio(42, scale, dpi) * 42 * dpi
			if cached < px-.0001 || cached-px > .5001 {
				t.Fatalf("incorrect physical raster size: dpi=%v scale=%v cached=%v target=%v", dpi, scale, cached, px)
			}
		}
	}
}

// Standard and high-quality fallback must preserve each style, sparks, translation and ordinary lyrics.
func TestStageTextStylesAndIsolation(t *testing.T) {
	line := lyrics.Line{At: time.Second, End: 3 * time.Second, Text: "流光 Native", Translation: "Translation stays put", Words: []lyrics.Word{
		{Text: "流", Start: time.Second, End: 2 * time.Second},
		{Text: "光 ", Start: 2 * time.Second, End: 2500 * time.Millisecond},
		{Text: "Native", Start: 2500 * time.Millisecond, End: 3 * time.Second},
	}}
	for _, style := range stageStyles {
		for _, pos := range []time.Duration{1300 * time.Millisecond, 4200 * time.Millisecond} {
			a := newTestApp()
			a.settings = defaultSettings()
			a.settings.StageStyle = style.id
			tr := netease.Track{ID: 1, Name: "fixture", Duration: 180000}
			a.pl.Cue([]netease.Track{tr})
			a.pl.SetLyrics([]lyrics.Line{line})
			a.pl.Hold(pos)
			tt := ui.NewTester(func(c *ui.Context) { ui.Box(c).Fill().Children(func() { a.stage(c, &tr, ui.RGB(120, 190, 250)) }) }, 900, 520)
			tt.Frame()
			translation, ok := tt.Find(line.Translation)
			if !ok {
				t.Fatal("translation missing from stage fixture")
			}
			for _, sparks := range []bool{false, true} {
				for _, mode := range []string{"", "msdf"} {
					a.settings.StageSparks, a.settings.StageTextRender = sparks, mode
					tt.Frame()
					got, found := tt.Find(line.Translation)
					if !found || got != translation {
						t.Fatalf("%s at %v moved translation in %s", style.id, pos, mode)
					}
				}
			}
		}
	}
	// A normal scrolling-lyrics view must not consult the stage setting.
	a := newTestApp()
	a.settings = defaultSettings()
	a.pl.Cue([]netease.Track{{ID: 1, Name: "fixture", Duration: 180000}})
	a.pl.SetLyrics([]lyrics.Line{line})
	a.pl.Hold(2 * time.Second)
	a.nowPlaying = true
	a.npPane = paneLyrics
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	tt.Advance(time.Second)
	tt.Frame()
	before := append([]byte(nil), tt.Image().Pix...)
	for _, mode := range []string{"msdf", ""} {
		a.settings.StageTextRender = mode
		tt.Frame()
		if !bytes.Equal(before, tt.Image().Pix) {
			t.Fatalf("stage-only mode %s changed ordinary lyrics", mode)
		}
	}
}
