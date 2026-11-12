package app

import (
	"os"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// songTypes are the files Cloudy opens, as mygo.json's fileAssociations
// list them.
var songTypes = []struct{ ext, name string }{{"mp3", "MP3 音频"}, {"flac", "FLAC 音频"}}

// appProgID is the start of the names of Cloudy's file types: what its
// installer registers, so that the two are one, of the identifier's letters,
// digits and dots.
func appProgID() string {
	return strings.Map(func(r rune) rune {
		if r < 0x80 && (r == '.' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return r
		}
		return -1
	}, appIdentifier)
}

// canRegisterOpenWith reports whether Cloudy can list itself to open songs.
const canRegisterOpenWith = true

// registerOpenWith lists Cloudy, as it runs from where it is, among the apps
// that open MP3 and FLAC files, for the listener: what the installer does,
// for a copy unpacked by hand. Choosing it as the default stays the
// listener's, in the system's settings.
func registerOpenWith() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	set := func(path, name, value string) error {
		k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
		if err != nil {
			return err
		}
		defer k.Close()
		return k.SetStringValue(name, value)
	}
	for _, t := range songTypes {
		prog := appProgID() + "." + t.ext
		class := `Software\Classes\` + prog
		for _, v := range [][3]string{
			{class, "", t.name},
			{class + `\DefaultIcon`, "", exe + ",0"},
			{class + `\shell\open\command`, "", `"` + exe + `" "%1"`},
			{`Software\Classes\.` + t.ext + `\OpenWithProgids`, prog, ""},
		} {
			if err := set(v[0], v[1], v[2]); err != nil {
				return err
			}
		}
	}
	// Tell Explorer the associations changed.
	windows.NewLazySystemDLL("shell32.dll").NewProc("SHChangeNotify").Call(0x08000000, 0, 0, 0)
	return nil
}
