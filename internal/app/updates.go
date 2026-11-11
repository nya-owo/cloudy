package app

import (
	"context"
	"errors"
	"time"

	"github.com/egoist/mygo"
)

// The update check and the install, which tests swap: they need a build
// with updates, and the network.
var (
	checkUpdate   = func(ctx context.Context) (*mygo.Update, error) { return mygo.Updater.Check(ctx) }
	installUpdate = func(up *mygo.Update, progress func(got, total int64)) error {
		return up.Install(context.Background(), progress)
	}
	relaunchApp = mygo.App.Relaunch
)

// updateState is what the update pane shows: the check under way, the update
// it found, and the download of it.
type updateState struct {
	open        bool
	checking    bool
	auto        bool // the check is the app's own, not the listener's
	checked     bool // a check finished
	up          *mygo.Update
	err         string
	downloading bool
	got, total  int64
	ready       bool // installed, waiting for the restart
}

// useUpdater makes the app one that checks for its updates itself, and
// offers them in the update pane rather than in a window of the updater
// plugin, whose plain UI sat apart from the app's glass.
func (a *app) useUpdater() {
	a.updates = true
	mygo.Updater.SetMirrors(updateMirrors...)
}

// checkForUpdates checks for updates as the listener asked, and shows what
// it finds in the update pane.
func (a *app) checkForUpdates() {
	a.startCheck(false)
}

// startCheck checks for updates: auto for the app's own check, which tells
// the listener only when there is one, else for the listener, who sees the
// pane from the start.
func (a *app) startCheck(auto bool) {
	u := &a.update
	if u.checking || u.downloading {
		if !auto {
			u.open = true
			a.redraw()
		}
		return
	}
	u.checking, u.auto, u.err, u.ready = true, auto, "", false
	if !auto {
		u.open = true
		a.redraw()
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		up, err := checkUpdate(ctx)
		a.post(func() { a.checkedUpdates(up, err) })
	}()
}

// checkedUpdates applies what a check found, or why it failed.
func (a *app) checkedUpdates(up *mygo.Update, err error) {
	u := &a.update
	u.checking, u.checked = false, true
	a.settings.UpdateChecked = time.Now().Unix()
	a.settings.save()
	switch {
	case err != nil:
		u.up, u.err = nil, updateError(err)
	case up == nil:
		u.up, u.err = nil, ""
	default:
		u.up, u.err = up, ""
	}
	switch {
	case u.up != nil && a.settings.UpdateInstall:
		// Installed in the background, for the next launch: nothing to ask.
		// Asked for, the pane stays, saying what it found as it downloads.
		u.open = !u.auto
		a.installUpdate()
	case u.up != nil:
		u.open = true
	default:
		if u.auto {
			u.open = false
		}
	}
	a.redraw()
}

// installUpdate downloads the update found, and installs it.
func (a *app) installUpdate() {
	u := &a.update
	if u.up == nil || u.downloading || u.ready {
		return
	}
	u.downloading, u.err, u.got, u.total = true, "", 0, 0
	up := u.up
	a.redraw()
	go func() {
		err := installUpdate(up, func(got, total int64) {
			a.post(func() {
				a.update.got, a.update.total = got, total
				a.redraw()
			})
		})
		a.post(func() {
			u := &a.update
			u.downloading = false
			if err != nil {
				u.err = "安装失败：" + err.Error()
			} else {
				u.ready = true
				// Said at the first launch of it.
				a.settings.UpdateNotes = &updateNotes{Version: up.Version, Notes: up.Notes}
				a.settings.save()
				if !u.open {
					a.toast = "新版本已装好，下次打开软件时生效"
				}
			}
			a.redraw()
		})
	}()
}

// updateError says why a check failed, in the listener's words.
func updateError(err error) string {
	if errors.Is(err, mygo.ErrUpdatesDisabled) {
		return "这个版本不能自动更新：开发版，或安装位置不可写。请到 GitHub 下载新版本。"
	}
	return "检查更新失败，请检查网络后重试。"
}

// autoCheckUpdates looks for updates soon after launch, and then once a day,
// while the listener asked to be told of new versions.
func (a *app) autoCheckUpdates() {
	if !a.settings.UpdateChecks {
		return
	}
	if last := time.Unix(a.settings.UpdateChecked, 0); time.Since(last) < 20*time.Hour {
		return
	}
	// Let the app open, and the first page load, before asking GitHub.
	time.Sleep(12 * time.Second)
	a.post(func() { a.startCheck(true) })
}
