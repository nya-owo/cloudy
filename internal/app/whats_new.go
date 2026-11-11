package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// updateNotes are the release notes of an update the app installed, kept
// for the first launch of it.
type updateNotes struct {
	Version string `json:"version"`
	Notes   string `json:"notes"`
}

// whatsNewState is the pane that says, at the first launch after an
// update and only then, what the version brought.
type whatsNewState struct {
	open    bool
	version string
	notes   string
	loading bool // the notes are on their way from GitHub
	// cover also says that the cover backgrounds are on, which came on by
	// themselves with the update that brought them.
	cover bool
}

// releaseRepo is where the releases, and their notes, are.
const releaseRepo = "nya-owo/cloudy"

// fetchReleaseNotes asks GitHub for the notes of version's release, for an
// update the app did not install itself; tests swap it.
var fetchReleaseNotes = func(ctx context.Context, version string) (string, error) {
	url := "https://api.github.com/repos/" + releaseRepo + "/releases/tags/v" + strings.TrimPrefix(version, "v")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("release notes: %s", res.Status)
	}
	var release struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(res.Body).Decode(&release); err != nil {
		return "", err
	}
	return release.Body, nil
}

// sameVersion reports whether a and b name one version, with or without
// the v of a tag.
func sameVersion(a, b string) bool {
	a, b = strings.TrimPrefix(strings.TrimSpace(a), "v"), strings.TrimPrefix(strings.TrimSpace(b), "v")
	return a != "" && a == b
}

// noteVersion notes the version running. The first launch after an update
// opens the pane of what it brought, once: a first install has nothing to
// say, and a development build no version.
func (a *app) noteVersion(current string) {
	s := &a.settings
	if current == "" || sameVersion(s.LastVersion, current) {
		return
	}
	// Settings from before LastVersion was kept are an update too.
	updated := s.LastVersion != "" || s.loaded
	notes := ""
	if n := s.UpdateNotes; n != nil && sameVersion(n.Version, current) {
		notes = n.Notes
	}
	cover := updated && !s.CoverNoticed && !s.GlobalGlass && (s.sidebarGlass() || s.mainGlass())
	s.LastVersion, s.UpdateNotes, s.CoverNoticed = current, nil, true
	s.save()
	if !updated {
		return
	}
	w := &a.whatsNew
	*w = whatsNewState{open: true, version: current, notes: notes, cover: cover}
	if notes != "" {
		return
	}
	w.loading = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		notes, err := fetchReleaseNotes(ctx, current)
		a.post(func() {
			w := &a.whatsNew
			w.loading = false
			if err == nil {
				w.notes = notes
			}
			a.redraw()
		})
	}()
}

// whatsNewDialog is that pane: the version updated to, what it brought,
// and once, that the cover backgrounds are on and where they are turned off.
func (a *app) whatsNewDialog(c *ui.Context) {
	w := &a.whatsNew
	if !w.open {
		return
	}
	p, ink := a.pal, a.inkOn(false)
	a.dialog(c, "whats-new", 0.35, &w.open, func(panel ui.Element) {
		a.pane(panel, 26, false)
		panel.Width(440).TextColor(ink.text).Label("更新内容")
		dialogHeader(c, ink, "已更新到 "+strings.TrimPrefix(w.version, "v"), "本次更新内容", func() { w.open = false })
		ui.Column(c).Padding(0, 28).Gap(12).Children(func() {
			if w.cover {
				ui.Row(c).Gap(8).AlignItems(ui.Start).Padding(10, 12).Radius(14).Background(p.accent.Alpha(0.1)).Children(func() {
					ui.Icon(c, iconSparkles).Size(16, 16).Shrink(0).TextColor(p.accent)
					ui.Text(c, "侧栏和主界面的封面色背景已默认开启，开启时不再使用所选的主题色。不需要的话，可以在「设置 → 视觉效果 → 液态玻璃」里关闭。").
						FontSize(13).LineHeight(1.5).TextColor(ink.text).Shrink(1).MinWidth(0)
				})
			}
			switch {
			case w.loading:
				ui.Text(c, "正在获取更新内容…").FontSize(13).TextColor(ink.muted)
			case len(updateNotesLines(w.notes)) > 0:
				a.notesBox(c, w.version, w.notes, ink)
			default:
				ui.Text(c, "没能获取更新内容，可以到 GitHub 查看。").FontSize(13).TextColor(ink.muted)
			}
		})
		dialogFooter(c, func() {
			if w.cover && settingButton(c.Key("open-effects"), ink, "去设置", nil, false, false).Clicked() {
				w.open = false
				a.openSettingsAt(settingsEffects)
			}
			if !w.loading && w.notes == "" && settingButton(c.Key("github"), ink, "在 GitHub 查看", nil, false, false).Clicked() {
				mygo.Shell.OpenExternal("https://github.com/" + releaseRepo + "/releases/tag/v" + strings.TrimPrefix(w.version, "v"))
			}
			if primaryButton(c.Key("ok"), "知道了", nil, p, false).Clicked() {
				w.open = false
			}
		})
	})
}
