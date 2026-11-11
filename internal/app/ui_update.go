package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// updateDialog shows the update in the app's own glass: the check under way,
// the release notes of an update, the download of it and the restart into it.
// Its head says where it is at, once, under the dialogs' one title.
func (a *app) updateDialog(c *ui.Context) {
	u := &a.update
	if !u.open {
		return
	}
	p, ink := a.pal, a.inkOn(false)
	a.dialog(c, "update", 0.35, &u.open, func(panel ui.Element) {
		a.pane(panel, 26, false)
		panel.Width(440).TextColor(ink.text)
		current := ""
		if v := mygo.App.Version(); v != "" {
			current = "当前版本 " + v
		}
		title, sub := "已是最新版本", current
		switch {
		case u.checking:
			title, sub = "正在检查更新", current
		case u.ready:
			title, sub = "新版本已经装好", "重启后生效"
		case u.err != "":
			title, sub = "没能检查更新", u.err
		case u.up != nil:
			title, sub = "新版本 "+u.up.Version, current
			if !u.up.Date.IsZero() {
				sub = strings.TrimSuffix(u.up.Date.Format("2006年1月2日")+" 发布 · "+sub, " · ")
			}
		}
		dialogHeader(c, ink, title, sub, func() { u.open = false })
		if (u.downloading || u.up != nil) && !u.ready && u.err == "" {
			ui.Column(c).Padding(0, 28).Gap(12).Children(func() {
				a.updateNotes(c, u.up, ink)
				if u.downloading {
					a.updateProgress(c, ink)
				}
			})
		}
		dialogFooter(c, func() {
			if u.downloading {
				label := "正在下载…"
				if u.total > 0 {
					label = fmt.Sprintf("正在下载 %d%%", int(float64(u.got)*100/float64(u.total)))
				}
				ui.Text(c, label).FontSize(12).TextColor(ink.muted).FontFeatures("tnum")
				return
			}
			later := func(key, label string) {
				if settingButton(c.Key(key), ink, label, nil, false, false).Clicked() {
					u.open = false
				}
			}
			switch {
			case u.ready:
				later("later", "稍后")
				if primaryButton(c.Key("relaunch"), "立即重启", nil, p, false).Clicked() {
					relaunchApp()
				}
			case u.up != nil && u.err == "":
				later("later", "稍后")
				if primaryButton(c.Key("install"), "下载并安装", iconDownload, p, false).Clicked() {
					a.installUpdate()
				}
			case u.err != "":
				// Neither GitHub nor its mirrors: the installers wait on a
				// cloud drive reachable from anywhere.
				if manualDownloadURL != "" && settingButton(c.Key("manual"), ink, "从夸克网盘下载", iconDownload, false, false).Clicked() {
					mygo.Shell.OpenExternal(manualDownloadURL)
				}
				later("close", "关闭")
				if primaryButton(c.Key("retry"), "重试", iconReload, p, false).Clicked() {
					a.checkForUpdates()
				}
			default:
				later("close", "关闭")
			}
		})
	})
}

// updateNotes shows the release notes of up, the Markdown they come in
// tamed to the lines the pane shows.
func (a *app) updateNotes(c *ui.Context, up *mygo.Update, ink paneInk) {
	if up == nil {
		return
	}
	a.notesBox(c, up.Version, up.Notes, ink)
}

// notesBox shows release notes of version in a box of their own, which
// scrolls when they run long.
func (a *app) notesBox(c *ui.Context, version, notes string, ink paneInk) {
	lines := updateNotesLines(notes)
	// The notes repeat the version in a heading, which the pane says.
	if len(lines) > 0 && sameVersion(lines[0], version) {
		lines = lines[1:]
	}
	if len(lines) == 0 {
		return
	}
	height := min(float32(len(lines))*21+28, 210)
	ui.Scroll(c).Height(height).Padding(12, 16).Radius(14).Background(ink.hover).Border(0.5, ink.line).Children(func() {
		ui.Column(c).Gap(6).Children(func() {
			for _, line := range lines {
				ui.Text(c, line).FontSize(12.5).TextColor(ink.muted).LineHeight(1.5)
			}
		})
	})
}

// updateProgress draws how far the download of the update is.
func (a *app) updateProgress(c *ui.Context, ink paneInk) {
	frac := float32(0)
	if a.update.total > 0 {
		frac = min(float32(a.update.got)/float32(a.update.total), 1)
	}
	bar := ui.Box(c).Height(6).Radius(3).Background(ink.press)
	bar.Draw(func(p *ui.Painter, r ui.Rect) {
		if frac <= 0 {
			return
		}
		p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * frac, H: r.H}, a.pal.accent, 3)
	})
}

// updateNotesLines turns the Markdown of release notes into the lines the
// pane shows: headings without their hashes, bullets with a dot.
func updateNotesLines(notes string) []string {
	var out []string
	for _, line := range strings.Split(notes, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "```") || strings.HasPrefix(line, "![") {
			continue
		}
		line = strings.TrimSpace(strings.TrimLeft(line, "#"))
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
			line = "· " + strings.TrimSpace(line[2:])
		}
		out = append(out, line)
		if len(out) == 60 {
			break
		}
	}
	return out
}
