package app

import (
	"os"
	"path/filepath"
	"testing"
)

// Settings saved go through a whole file: nothing is left beside them, and
// what was saved reads back.
func TestSettingsSaveAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	s := loadSettingsFrom(path)
	s.Volume = 0.37
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".part"); !os.IsNotExist(err) {
		t.Fatal("left the .part file behind")
	}
	if got := loadSettingsFrom(path).Volume; got != 0.37 {
		t.Fatalf("volume read back as %v", got)
	}
}

// A settings.json written halfway, as by a crash, is kept aside as
// settings.json.bad, so saving the defaults over it loses nothing for good.
func TestSettingsHalfWrittenKeptAside(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	half := []byte(`{"volume": 0.37, "downloadDir": "D:\Mu`)
	if err := os.WriteFile(path, half, 0o600); err != nil {
		t.Fatal(err)
	}
	s := loadSettingsFrom(path)
	if s.Volume != defaultSettings().Volume {
		t.Fatalf("volume %v, not the default", s.Volume)
	}
	if err := s.save(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path + ".bad")
	if err != nil || string(b) != string(half) {
		t.Fatalf("the broken file was not kept aside: %q, %v", b, err)
	}
}
