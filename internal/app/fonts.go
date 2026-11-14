package app

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	_ "github.com/egoist/mygo/locales/zh-Hans"
	"github.com/egoist/mygo/ui"
)

// appLanguage is the language the app is written in. Text is in it, and
// Windows picks the fonts for Han characters by it: in English, as MyGo
// takes an app without catalogs to be, they came from Japanese fonts, which
// lack many simplified characters, so that a line changed font and weight
// from one character to the next.
const appLanguage = "zh-Hans"

// A uiFont is a font the listener can read the app in.
type uiFont struct {
	id    string // kept in settings; empty for the default
	label string
	// family is the list of families text takes, the first one there is
	// drawing it.
	family string
	// files are those of the font in the fonts directory of the app's
	// resources, for a font the system doesn't have.
	files []string
	// imported is the file of a font the listener brought, in the fonts
	// directory of the app's data.
	imported string
}

// Fonts beyond those on offer: any family of the system's, by its name,
// or a font file the listener imported, by the file's name.
const (
	systemFontPrefix   = "sys:"
	importedFontPrefix = "file:"
)

// importedFamily is the family an imported font is registered under, its
// own name aside, so that one file can't hide a system font of its name.
func importedFamily(file string) string {
	return "Cloudy Imported " + strings.TrimSuffix(file, filepath.Ext(file))
}

// uiFonts are the fonts on offer, the default first. Those in the system
// are the ones Windows comes with in Chinese.
var uiFonts = []uiFont{
	{id: "", label: "默认"},
	{id: "yahei", label: "微软雅黑", family: "Microsoft YaHei UI"},
	{id: "dengxian", label: "等线", family: "DengXian"},
	{id: "simhei", label: "黑体", family: "SimHei"},
	{id: "noto-serif", label: "思源宋体", family: "Noto Serif SC"},
	{id: "simsun", label: "宋体", family: "SimSun"},
	{id: "wenkai", label: "霞鹜文楷", family: "LXGW WenKai", files: []string{"LXGWWenKai-Regular.ttf", "LXGWWenKai-Medium.ttf"}},
	{id: "kaiti", label: "楷体", family: "KaiTi"},
	{id: "fangsong", label: "仿宋", family: "FangSong"},
}

// defaultFontFamily is the family of the default font. Elsewhere the
// system's reads Chinese well; on Windows Segoe UI has no Han characters.
// Noto Sans SC has Medium and Black as well, where Microsoft YaHei
// has Light, Regular and Bold alone.
func defaultFontFamily() string {
	if runtime.GOOS == "windows" {
		return "Noto Sans SC, Microsoft YaHei UI"
	}
	return ""
}

// fontOf returns the font of an id, the default for one no longer on offer.
func fontOf(id string) uiFont {
	for _, f := range uiFonts {
		if f.id == id {
			return f
		}
	}
	if family, ok := strings.CutPrefix(id, systemFontPrefix); ok && family != "" {
		return uiFont{id: id, label: family, family: family}
	}
	if file, ok := strings.CutPrefix(id, importedFontPrefix); ok && file != "" && file == filepath.Base(file) {
		return uiFont{id: id, label: strings.TrimSuffix(file, filepath.Ext(file)), family: importedFamily(file), imported: file}
	}
	return uiFonts[0]
}

// custom reports a font beyond those on offer, which may lack Han
// characters: the default then draws those.
func (f uiFont) custom() bool { return f.imported != "" || strings.HasPrefix(f.id, systemFontPrefix) }

// fontFamily returns the family list of the font chosen, for the theme.
func (a *app) fontFamily() string { return familyOf(a.settings.Font) }

// lyricFamily returns the family list of the lyrics' font: the app's,
// unless the listener chose one of their own (settings.LyricFont).
func (a *app) lyricFamily() string {
	if a.settings.LyricFont == "" {
		return a.fontFamily()
	}
	return familyOf(a.settings.LyricFont)
}

// lyricFace gives e, lyrics or what holds them, the lyrics' font, where
// it is not the app's.
func (a *app) lyricFace(e ui.Element) ui.Element {
	if a.settings.LyricFont != "" {
		e.Font(a.lyricFamily())
	}
	return e
}

// familyOf returns the family list of the font of id.
func familyOf(id string) string {
	if f := fontOf(id); f.custom() {
		if def := defaultFontFamily(); def != "" {
			return f.family + ", " + def
		}
		return f.family
	} else if f.id != "" {
		return f.family
	}
	return defaultFontFamily()
}

// loadFont registers the files of the font chosen, the first time it is
// chosen: their tens of MB stay on disk for those who read another font.
// A font that won't load leaves its family to the system's fallback.
// The lyrics' font loads too.
func (a *app) loadFont() {
	a.loadFontOf(a.settings.Font)
	if a.settings.LyricFont != "" {
		a.loadFontOf(a.settings.LyricFont)
	}
}

func (a *app) loadFontOf(id string) {
	f := fontOf(id)
	if f.imported != "" && !a.fontsLoaded[f.id] {
		if err := registerImportedFont(f.imported); err != nil {
			log.Printf("font %s: %v", f.id, err)
			return
		}
		if a.fontsLoaded == nil {
			a.fontsLoaded = map[string]bool{}
		}
		a.fontsLoaded[f.id] = true
		return
	}
	if len(f.files) == 0 || a.fontsLoaded[f.id] {
		return
	}
	dir, err := mygo.App.Path(mygo.PathResources)
	if err != nil {
		log.Printf("font %s: %v", f.id, err)
		return
	}
	for _, name := range f.files {
		b, err := os.ReadFile(filepath.Join(dir, "fonts", name))
		if err == nil {
			err = ui.RegisterFont(b, "")
		}
		if err != nil {
			log.Printf("font %s: %v", f.id, err)
			return
		}
	}
	if a.fontsLoaded == nil {
		a.fontsLoaded = map[string]bool{}
	}
	a.fontsLoaded[f.id] = true
}

// loadFontFiles registers the fonts that come as files, off the main thread
// as far as it can, for the list of fonts to show each in its own.
func (a *app) loadFontFiles() {
	if a.fontsAsked {
		return
	}
	a.fontsAsked = true
	var wanted []uiFont
	for _, f := range a.fontChoices() {
		if len(f.files) > 0 && !a.fontsLoaded[f.id] {
			wanted = append(wanted, f)
		}
	}
	dir, err := mygo.App.Path(mygo.PathResources)
	if err != nil {
		return
	}
	go func() {
		for _, f := range wanted {
			var data [][]byte
			for _, name := range f.files {
				if b, err := os.ReadFile(filepath.Join(dir, "fonts", name)); err == nil {
					data = append(data, b)
				}
			}
			a.post(func() {
				if a.fontsLoaded[f.id] {
					return
				}
				for _, b := range data {
					if err := ui.RegisterFont(b, ""); err != nil {
						log.Printf("font %s: %v", f.id, err)
						return
					}
				}
				if a.fontsLoaded == nil {
					a.fontsLoaded = map[string]bool{}
				}
				a.fontsLoaded[f.id] = true
			})
		}
	}()
}

// fontChoices returns the fonts this computer can show, read once.
func (a *app) fontChoices() []uiFont {
	if a.fontList != nil {
		return a.fontList
	}
	families := map[string]bool{}
	for _, f := range ui.FontFamilies() {
		families[strings.ToLower(f.Name)] = true
	}
	dir, _ := mygo.App.Path(mygo.PathResources)
	a.fontList = slices.DeleteFunc(slices.Clone(uiFonts), func(f uiFont) bool {
		if f.id == "" || families[strings.ToLower(f.family)] {
			return false
		}
		return len(f.files) == 0 || !fileExists(filepath.Join(dir, "fonts", f.files[0]))
	})
	return a.fontList
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// importedFontsDir is where fonts the listener imports are kept.
func importedFontsDir() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "fonts"), nil
}

// registerImportedFont registers a font imported before, by its file.
func registerImportedFont(file string) error {
	dir, err := importedFontsDir()
	if err != nil {
		return err
	}
	b, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		return err
	}
	return ui.RegisterFont(b, importedFamily(file))
}

// systemFontNames lists the families of the system, sorted, without those
// the app registered and the vertical ones Windows names with an @.
func systemFontNames() []string {
	var names []string
	seen := map[string]bool{}
	for _, f := range ui.FontFamilies() {
		key := strings.ToLower(f.Name)
		if f.Registered || f.Name == "" || strings.HasPrefix(f.Name, "@") || seen[key] {
			continue
		}
		seen[key] = true
		names = append(names, f.Name)
	}
	slices.SortFunc(names, func(x, y string) int { return strings.Compare(strings.ToLower(x), strings.ToLower(y)) })
	return names
}
