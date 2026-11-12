package app

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// localPage lists the listener's own music, in the folders they chose, as
// a playlist is listed: searched, sorted, and by album or artist under
// titles of their own.
func (a *app) localPage(c *ui.Context) {
	a.loadLibrary(false)
	if len(a.settings.LocalFolders) == 0 {
		a.localEmpty(c, "还没有添加本地音乐文件夹",
			"添加存放音乐的文件夹后，Cloudy 会读取其中的 MP3 和 FLAC 文件（包括子文件夹），按歌手、专辑整理，文件夹有变化时自动更新。")
		return
	}
	tracks := a.lib.tracks
	if scanning, _, _ := a.scanningLibrary(); scanning && len(tracks) == 0 {
		a.centered(c, a.libraryScanText(c), nil)
		return
	}
	if len(tracks) == 0 {
		msg := "这些文件夹里没有找到 MP3 或 FLAC 文件。"
		if a.lib.err != "" {
			msg = a.lib.err
		}
		a.localEmpty(c, "没有找到音乐", msg)
		return
	}
	a.toolRows(c, tracks, true, a.localHeader)
}

// libraryScanText says how far the reading of the folders has come.
func (a *app) libraryScanText(c *ui.Context) string {
	_, done, all := a.scanningLibrary()
	c.After(300 * time.Millisecond)
	if all == 0 {
		return "正在读取文件夹…"
	}
	return fmt.Sprintf("正在读取文件夹 %d/%d", done, all)
}

// localEmpty says why the page lists nothing, and what to do.
func (a *app) localEmpty(c *ui.Context, title, body string) {
	p := a.pal
	top, bottom := a.insets(c)
	ui.Column(c).Grow(1).Padding(top, 0, bottom, 0).Center().Gap(14).Children(func() {
		ui.Icon(c, iconFolder).Size(56, 56).TextColor(p.faint)
		ui.Text(c, title).FontSize(15).FontWeight(600)
		ui.Text(c, body).FontSize(12.5).TextColor(p.muted).MaxWidth(560).TextAlign(ui.Center)
		ui.Row(c).Wrap().GapX(10).GapY(8).Children(func() {
			if pillButton(c.Key("add"), iconPlus, "添加文件夹", p.accent, p.onAccent).Clicked() {
				a.pickLibraryFolder()
			}
			if len(a.settings.LocalFolders) > 0 {
				a.localFoldersButton(c)
				if pillButton(c.Key("rescan"), iconReload, "重新扫描", p.hover.Over(p.bg), p.text).Clicked() {
					a.scanLibraryLater()
				}
			}
		})
	})
}

// localHeader is the page as a playlist is: a cover of its songs, what it
// says, and what plays it or changes its folders.
func (a *app) localHeader(c *ui.Context) {
	p := a.pal
	tracks := a.lib.tracks
	var total time.Duration
	var size int64
	covers := make([]string, 0, 3)
	seen := map[string]bool{}
	for i := range tracks {
		total += tracks[i].Length()
		if url := tracks[i].Album.PicURL; url != "" && !seen[url] && len(covers) < 3 {
			seen[url] = true
			covers = append(covers, url)
		}
	}
	for _, it := range a.lib.items {
		size += it.Size
	}
	meta := fmt.Sprintf("%d 首 · %s · %s · %d 个文件夹", len(tracks), longTime(total), sizeText(size), len(a.settings.LocalFolders))
	if scanning, _, _ := a.scanningLibrary(); scanning {
		meta += " · " + a.libraryScanText(c)
	}
	cover := ""
	if len(covers) > 0 {
		cover = covers[0]
	}
	ui.Row(c).Gap(28).AlignItems(ui.End).Padding(0, 0, 26, 0).Children(func() {
		a.headCover(c, cover)
		ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(8).Children(func() {
			ui.Text(c, "本地").FontSize(12).FontWeight(600).TextColor(p.accent)
			ui.Text(c, "本地音乐").FontSize(30).FontWeight(800).LetterSpacing(-0.6).MaxLines(1)
			ui.Text(c, meta).FontSize(13).TextColor(p.muted)
			if a.lib.err != "" {
				ui.Text(c, a.lib.err).FontSize(12.5).TextColor(p.accent)
			}
			ui.Row(c).Wrap().GapX(10).GapY(8).Margin(10, 0, 0, 0).Children(func() {
				if pillButton(c.Key("play"), iconPlay, "播放", p.accent, p.onAccent).Clicked() {
					a.playList(a.lib.tracks, false)
				}
				if pillButton(c.Key("shuffle"), iconShuffle, "随机播放", p.hover.Over(p.bg), p.text).Clicked() {
					a.playList(a.lib.tracks, true)
				}
				if pillButton(c.Key("add"), iconPlus, "添加文件夹", p.hover.Over(p.bg), p.text).Clicked() {
					a.pickLibraryFolder()
				}
				a.localFoldersButton(c)
				if iconButton(c.Key("rescan"), iconReload, 16, p.muted, "重新扫描").Size(36, 36).Clicked() {
					a.scanLibraryLater()
					a.toast = "正在重新读取本地音乐"
				}
			})
		})
		a.coverStackURLs(c, covers)
	})
}

// localFoldersButton lists the library's folders, each to open or take out.
func (a *app) localFoldersButton(c *ui.Context) {
	p := a.pal
	b := pillButton(c.Key("folders"), iconFolder, fmt.Sprintf("文件夹 %d", len(a.settings.LocalFolders)), p.hover.Over(p.bg), p.text)
	if !b.Clicked() {
		return
	}
	var acts []menuAction
	for _, dir := range a.settings.LocalFolders {
		acts = append(acts, menuAction{label: dir, title: filepath.Base(dir), note: filepath.Dir(dir), icon: iconFolder,
			sub: [][]menuAction{{
				{label: "打开文件夹", icon: iconExternal, do: func() { mygo.Shell.OpenPath(dir) }},
				{label: "从本地音乐移除", icon: iconTrash, do: func() { a.removeLibraryFolder(dir) }},
			}}})
	}
	a.menuBelow(b, false, [][]menuAction{acts, {{label: "添加文件夹…", icon: iconPlus, do: a.pickLibraryFolder}}}, nil)
}
