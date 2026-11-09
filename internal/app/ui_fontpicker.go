package app

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// fontPicker is the dialog of the fonts beyond those on offer: every
// family of the system's, and the files the listener imports.
type fontPicker struct {
	open   bool
	query  string
	box    ui.Handle
	scroll ui.ScrollState
	names  []string // the system's families, read as the dialog opens
}

// moreFonts is the last choice of the font select, which opens the picker.
const moreFonts = "更多字体…"

// fontPickerLimit is how many families the list shows at once: each is
// drawn in its own font, which DirectWrite loads the first time.
const fontPickerLimit = 200

func (a *app) openFontPicker() {
	p := &a.settingsView.fontPick
	p.open, p.query, p.scroll = true, "", ui.ScrollState{}
	p.names = systemFontNames()
	a.redraw()
}

// setFont makes id the app's font.
func (a *app) setFont(id string) {
	a.settings.Font = id
	a.loadFont()
	a.changed()
	a.redraw()
}

// fontPickerDialog lists the system's fonts, each in itself, to search and
// choose from, and imports a font file.
func (a *app) fontPickerDialog(c *ui.Context) {
	p := &a.settingsView.fontPick
	if !p.open {
		return
	}
	ink := a.inkOn(false)
	w, h := c.Size()
	a.dialog(c, "font-picker", 0.35, &p.open, func(panel ui.Element) {
		panel.Width(min(560, w-80)).Height(min(640, h-56)).TextColor(ink.text)
		a.pane(panel, 26, false)
		panel.Children(func() {
			dialogHeader(c, ink, "选择字体", "本机已安装的字体；缺少的汉字会用默认字体显示", func() { p.open = false })
			field := ui.Row(c).Height(34).PaddingX(12).Gap(7).Radius(999).AlignItems(ui.Center).Margin(0, 28, 10, 28)
			if c.Focused(p.box) {
				field.Background(a.pal.bg).Shadow(0, 0, 0, 3, a.pal.accent.Alpha(0.25)).Border(1, a.pal.accent.Alpha(0.6))
			} else {
				field.Background(ink.hover).Border(1, ink.line)
			}
			field.Children(func() {
				ui.Icon(c, iconSearch).Size(13, 13).TextColor(ink.muted)
				ui.TextInputBase(c.Key("font-filter"), &p.query).Bind(&p.box).Grow(1).MinWidth(0).AutoFocus().
					Placeholder("搜索字体名称").FontSize(12.5).Label("搜索字体名称")
				if p.query != "" && iconButton(c.Key("clear"), iconX, 11, ink.muted, "清除搜索").Size(20, 20).Clicked() {
					p.query = ""
				}
			})
			query := strings.ToLower(strings.TrimSpace(p.query))
			var shown []string
			more := 0
			for _, name := range p.names {
				if query != "" && !strings.Contains(strings.ToLower(name), query) {
					continue
				}
				if len(shown) == fontPickerLimit {
					more++
					continue
				}
				shown = append(shown, name)
			}
			chosen := a.settings.Font
			ui.Scroll(c.Key("font-list")).Grow(1).Shrink(1).TrackScroll(&p.scroll).Padding(0, 20, 4, 20).Children(func() {
				if len(shown) == 0 {
					ui.Text(c, "没有找到匹配的字体").FontSize(13).TextColor(ink.faint).Margin(24, 0, 0, 10)
				}
				for _, name := range shown {
					id := systemFontPrefix + name
					row := ui.ButtonBase(c.Key(name)).Height(52).PaddingX(12).Gap(10).Radius(14).AlignItems(ui.Center).
						Justify(ui.Start).FocusRing(false).Label(name)
					if id == chosen {
						row.Background(a.pal.accent.Alpha(0.12))
					} else if row.Hovered() {
						row.Background(ink.hover)
					}
					row.Children(func() {
						ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
							ui.Text(c, name).FontSize(12).TextColor(ink.muted).SingleLine()
							ui.Text(c, "云音乐 Cloudy 0123").Font(name).FontSize(16).SingleLine()
						})
						if id == chosen {
							ui.Icon(c, iconCheck).Size(14, 14).Shrink(0).TextColor(a.pal.accent)
						}
					})
					if row.Clicked() {
						a.setFont(id)
					}
				}
				if more > 0 {
					ui.Text(c, fmt.Sprintf("还有 %d 个字体，输入名称缩小范围", more)).FontSize(12).TextColor(ink.faint).Margin(10, 0, 6, 12)
				}
			})
			dialogFooter(c, func() {
				if settingButton(c.Key("font-import"), ink, "导入字体文件…", iconPlus, false, false).Clicked() {
					a.importFont()
				}
				if settingButton(c.Key("font-default"), ink, "恢复默认", iconReload, false, false).Clicked() {
					a.setFont("")
				}
				ui.Spacer(c)
				if primaryButton(c.Key("font-done"), "完成", iconCheck, a.pal, false).Clicked() {
					p.open = false
				}
			})
		})
	})
}

// chooseFontFile asks for a font file; tests replace it.
var chooseFontFile = func(parent *mygo.Window) (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: parent, Title: "选择字体文件",
		Filters: []mygo.FileFilter{{Name: "字体", Extensions: []string{"ttf", "otf", "ttc", "otc"}}}})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

// importFont copies a font file the listener picks into the app's data,
// where it is registered at every start, and makes it the app's font. The
// dialog waits for the main thread, which shows it.
func (a *app) importFont() {
	win := a.win
	go func() {
		path, err := chooseFontFile(win)
		if err != nil || path == "" {
			return
		}
		file, err := copyFontFile(path)
		a.post(func() {
			if err == nil {
				err = registerImportedFont(file)
			}
			if err != nil {
				log.Printf("import font %s: %v", path, err)
				a.toast = "无法导入这个字体文件"
				a.redraw()
				return
			}
			id := importedFontPrefix + file
			if a.fontsLoaded == nil {
				a.fontsLoaded = map[string]bool{}
			}
			a.fontsLoaded[id] = true
			a.setFont(id)
			a.toast = "已导入并使用「" + fontOf(id).label + "」"
		})
	}()
}

// copyFontFile copies a font into the imported fonts' directory, under its
// own name, and returns that name.
func copyFontFile(path string) (string, error) {
	dir, err := importedFontsDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := filepath.Base(path)
	dst := filepath.Join(dir, name)
	if filepath.Clean(path) == filepath.Clean(dst) {
		return name, nil
	}
	in, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return "", err
	}
	return name, out.Close()
}
