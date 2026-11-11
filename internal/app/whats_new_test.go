package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// The first launch after an update says what it brought, once; a first
// install says nothing. The cover backgrounds are said once, on an update.
func TestNoteVersion(t *testing.T) {
	fetched := make(chan string, 1)
	real := fetchReleaseNotes
	fetchReleaseNotes = func(_ context.Context, version string) (string, error) {
		fetched <- version
		return "## 1.2.11\n- 新功能", nil
	}
	defer func() { fetchReleaseNotes = real }()

	// A first install.
	a := newTestApp()
	a.settings = defaultSettings()
	a.settings.path = filepath.Join(t.TempDir(), "settings.json")
	a.noteVersion("1.2.10")
	if a.whatsNew.open || a.settings.LastVersion != "1.2.10" {
		t.Fatalf("a first install: %+v, last %q", a.whatsNew, a.settings.LastVersion)
	}

	// Settings from before the version was kept, and the notes of the update
	// the app installed.
	a = newTestApp()
	a.settings = defaultSettings()
	a.settings.path = filepath.Join(t.TempDir(), "settings.json")
	a.settings.loaded = true
	a.settings.UpdateNotes = &updateNotes{Version: "v1.2.10", Notes: "- 圆弧歌词"}
	a.noteVersion("1.2.10")
	w := a.whatsNew
	if !w.open || w.notes != "- 圆弧歌词" || !w.cover || w.loading {
		t.Fatalf("the first launch after an update: %+v", w)
	}
	saved := loadSettingsFrom(a.settings.path)
	if saved.LastVersion != "1.2.10" || saved.UpdateNotes != nil || !saved.CoverNoticed {
		t.Fatalf("saved: last %q, notes %+v, noticed %v", saved.LastVersion, saved.UpdateNotes, saved.CoverNoticed)
	}
	a.whatsNew.open = false
	a.noteVersion("1.2.10")
	if a.whatsNew.open {
		t.Fatal("said again at the next launch")
	}

	// An update installed by hand: the notes come from GitHub, and the
	// cover backgrounds were said already.
	a.noteVersion("1.2.11")
	if !a.whatsNew.open || a.whatsNew.cover || !a.whatsNew.loading {
		t.Fatalf("an update installed by hand: %+v", a.whatsNew)
	}
	select {
	case v := <-fetched:
		if v != "1.2.11" {
			t.Errorf("fetched the notes of %q", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("did not fetch the notes")
	}
	// A development build has no version.
	a.whatsNew.open = false
	a.noteVersion("")
	if a.whatsNew.open {
		t.Error("a development build said what's new")
	}
}
