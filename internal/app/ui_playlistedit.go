package app

import (
	"errors"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// The listener's own playlists: making them, renaming them, deleting them,
// and adding songs to them or taking songs out.

// These edit playlists with NetEase; tests replace them.
var (
	createPlaylist     = netease.CreatePlaylist
	renamePlaylist     = netease.RenamePlaylist
	addToPlaylist      = netease.AddToPlaylist
	removeFromPlaylist = netease.RemoveFromPlaylist
	deletePlaylist     = netease.DeletePlaylist
	savePlaylist       = netease.SubscribePlaylist
)

// nameDialog asks for a playlist's name: a new one's, or one's new name.
type nameDialog struct {
	open   bool
	name   string
	rename int64          // the playlist renamed, or 0 to make one
	add    *netease.Track // the song to add to the playlist made, if any
	busy   bool           // NetEase is making the playlist
}

// deleteDialog asks whether to delete one of the listener's playlists.
type deleteDialog struct {
	open bool
	id   int64
	name string
}

// editable returns the listener's playlist id if they may edit its songs
// and name, or nil: the liked songs' are their hearts'.
func (a *app) editable(id int64) *netease.Playlist {
	for i := range a.mine {
		if a.mine[i].ID == id && a.mine[i].SpecialType != 5 {
			return &a.mine[i]
		}
	}
	return nil
}

// isSaved reports whether the listener saved playlist pl, as its page or
// the sidebar says.
func (a *app) isSaved(pl *netease.Playlist) bool {
	return pl.Subscribed || slices.ContainsFunc(a.saved, func(s netease.Playlist) bool { return s.ID == pl.ID })
}

// savable reports whether the listener may save playlist pl: anyone's but
// their own. Signed out, saving asks them to sign in.
func (a *app) savable(pl *netease.Playlist) bool {
	return a.profile == nil || pl.Creator.UserID != a.profile.UserID
}

// toggleSaved saves playlist pl for the listener, or unsaves it, at once
// in the app and then with NetEase, going back if NetEase refuses. Signed
// out, it asks the listener to sign in.
func (a *app) toggleSaved(pl netease.Playlist) {
	if a.profile == nil {
		a.showMain()
		a.openLogin()
		return
	}
	id := pl.ID
	if a.savingPl[id] {
		return
	}
	if a.savingPl == nil {
		a.savingPl = map[int64]bool{}
	}
	a.savingPl[id] = true
	save := !a.isSaved(&pl)
	at := a.setSaved(pl, save, 0)
	uid := a.profile.UserID
	go func() {
		err := savePlaylist(id, save)
		a.post(func() {
			delete(a.savingPl, id)
			switch {
			case a.profile == nil || a.profile.UserID != uid:
				// Signed out or in as someone else meanwhile.
			case err != nil:
				a.setSaved(pl, !save, max(at, 0))
				a.toast = "操作失败，请稍后再试"
			case save:
				a.say("save-playlist", "已收藏")
			default:
				a.say("save-playlist", "已取消收藏")
			}
		})
	}()
}

// setSaved marks playlist pl saved or not, on its page and in the sidebar,
// where a saved one goes at index at, and returns where it was there, or
// -1.
func (a *app) setSaved(pl netease.Playlist, save bool, at int) int {
	if loaded := a.playlists[pl.ID]; loaded != nil {
		loaded.Subscribed = save
	}
	// A new slice, not shifting the one the sidebar may be listing.
	saved := slices.Clone(a.saved)
	was := slices.IndexFunc(saved, func(s netease.Playlist) bool { return s.ID == pl.ID })
	if was >= 0 {
		saved = slices.Delete(saved, was, was+1)
	}
	if save {
		pl.Subscribed = true
		pl.Tracks, pl.TrackIDs = nil, nil
		saved = slices.Insert(saved, min(at, len(saved)), pl)
	}
	a.saved = saved
	return was
}

// loadMine loads the playlists the listener uid made and saved.
func (a *app) loadMine(uid int64) {
	go func() {
		made, saved, err := netease.UserPlaylists(uid)
		a.post(func() {
			if err == nil && a.profile != nil && a.profile.UserID == uid {
				a.mine, a.saved = made, saved
			}
		})
	}()
}

// playlistChanged forgets what was loaded of playlist id, which NetEase
// changed: loaded anew if shown, and the sidebar's covers with it.
func (a *app) playlistChanged(id int64) {
	delete(a.playlists, id)
	if a.route.page == pagePlaylist && a.route.id == id {
		a.loadPlaylist(id, false)
	}
	if a.profile != nil {
		a.loadMine(a.profile.UserID)
	}
}

// newPlaylist asks for the name of a playlist to make, then adds tr to it
// if set. Signed out, it asks the listener to sign in.
func (a *app) newPlaylist(tr *netease.Track) {
	if a.profile == nil {
		a.showMain()
		a.openLogin()
		return
	}
	a.naming = nameDialog{open: true, add: tr}
}

// startRename asks for a new name for the listener's playlist pl.
func (a *app) startRename(pl *netease.Playlist) {
	a.naming = nameDialog{open: true, name: pl.Name, rename: pl.ID}
}

// submitName makes the playlist named, or renames one: at once in the app,
// as NetEase may refuse, when renaming.
func (a *app) submitName() {
	d := &a.naming
	name := strings.TrimSpace(d.name)
	if name == "" || d.busy {
		return
	}
	if d.rename != 0 {
		id := d.rename
		d.open = false
		pl := a.editable(id)
		if pl == nil || pl.Name == name {
			return
		}
		old := pl.Name
		a.setPlaylistName(id, name)
		go func() {
			err := renamePlaylist(id, name)
			a.post(func() {
				if err != nil {
					a.setPlaylistName(id, old)
					a.toast = "重命名失败，请稍后再试"
				}
			})
		}()
		return
	}
	d.busy = true
	add := d.add
	go func() {
		pl, err := createPlaylist(name)
		a.post(func() {
			a.naming.busy = false
			if err != nil {
				a.toast = "创建歌单失败，请稍后再试"
				return
			}
			a.naming.open = false
			// Under the liked songs, as NetEase lists the newest.
			at := 0
			if len(a.mine) > 0 && a.mine[0].SpecialType == 5 {
				at = 1
			}
			a.mine = slices.Insert(a.mine, at, *pl)
			pl.Tracks = nil
			a.playlists[pl.ID] = pl
			a.open(route{page: pagePlaylist, id: pl.ID})
			if add != nil {
				a.addTo(pl.ID, add)
			}
		})
	}()
}

// confirmDelete asks whether to delete the listener's playlist pl.
func (a *app) confirmDelete(pl *netease.Playlist) {
	a.deleting = deleteDialog{open: true, id: pl.ID, name: pl.Name}
}

// deleteChosen deletes the playlist the dialog asked about: at once from
// the sidebar, leaving its page, and then with NetEase, which may refuse:
// it comes back then.
func (a *app) deleteChosen() {
	id := a.deleting.id
	a.deleting.open = false
	i := slices.IndexFunc(a.mine, func(pl netease.Playlist) bool { return pl.ID == id })
	if i < 0 || a.mine[i].SpecialType == 5 {
		return
	}
	pl := a.mine[i]
	a.mine = slices.Delete(slices.Clone(a.mine), i, i+1)
	// Leave no way back to its page, and leave it for the page before.
	gone := func(r route) bool { return r.page == pagePlaylist && r.id == id }
	a.history = slices.DeleteFunc(a.history, gone)
	a.forward = slices.DeleteFunc(a.forward, gone)
	if gone(a.route) {
		r := route{page: pageHome}
		if n := len(a.history); n > 0 {
			r, a.history = a.history[n-1], a.history[:n-1]
		}
		a.show(r)
	}
	go func() {
		err := deletePlaylist(id)
		a.post(func() {
			if err != nil {
				a.mine = slices.Insert(a.mine, min(i, len(a.mine)), pl)
				a.toast = "删除失败，请稍后再试"
				return
			}
			delete(a.playlists, id)
			a.toast = "已删除歌单"
		})
	}()
}

func (a *app) setPlaylistName(id int64, name string) {
	if pl := a.editable(id); pl != nil {
		pl.Name = name
	}
	if pl := a.playlists[id]; pl != nil {
		pl.Name = name
	}
}

// addTo adds tr to the listener's playlist pid; a toast says how it went.
func (a *app) addTo(pid int64, tr *netease.Track) {
	id := tr.ID
	go func() {
		err := addToPlaylist(pid, id)
		a.post(func() {
			switch {
			case errors.Is(err, netease.ErrInPlaylist):
				a.toast = "歌曲已在歌单中"
			case err != nil:
				a.toast = "添加失败，请稍后再试"
			default:
				a.toast = "已添加到歌单"
				a.playlistChanged(pid)
			}
		})
	}()
}

// removeFrom takes tr out of the listener's playlist pid, at once in the
// app and then with NetEase, which may refuse: the song comes back then.
func (a *app) removeFrom(pid int64, tr *netease.Track) {
	id := tr.ID
	pl := a.playlists[pid]
	var before []netease.Track
	if pl != nil {
		// A new list: the player may be playing the old one.
		before = pl.Tracks
		pl.Tracks = slices.DeleteFunc(slices.Clone(before), func(t netease.Track) bool { return t.ID == id })
	}
	go func() {
		err := removeFromPlaylist(pid, id)
		a.post(func() {
			if err != nil {
				if pl != nil && a.playlists[pid] == pl {
					pl.Tracks = before
				}
				a.toast = "移除失败，请稍后再试"
				return
			}
			// The songs listed are right: only the sidebar's covers may
			// have changed.
			if a.profile != nil {
				a.loadMine(a.profile.UserID)
			}
		})
	}()
}

// playlistActions are the actions of a song's menu that put it in the
// listener's playlists, or take it out of pid, the one it is listed in, if
// set.
func (a *app) playlistActions(tr *netease.Track, pid int64) []menuAction {
	if a.profile == nil {
		return nil
	}
	var to []menuAction
	for i := range a.mine {
		if pl := &a.mine[i]; pl.SpecialType != 5 && pl.ID != pid {
			to = append(to, menuAction{label: pl.Name, icon: iconNote, do: func() { a.addTo(pl.ID, tr) }})
		}
	}
	sub := [][]menuAction{{{label: "新建歌单…", icon: iconPlus, do: func() { a.newPlaylist(tr) }}}}
	if len(to) > 0 {
		sub = append(sub, to)
	}
	acts := []menuAction{{label: "添加到歌单", icon: iconPlus, sub: sub}}
	if a.editable(pid) != nil {
		acts = append(acts, menuAction{label: "从歌单中移除", icon: iconTrash, do: func() { a.removeFrom(pid, tr) }})
	}
	return acts
}

// playlistNameDialog asks for the name of a playlist to make, or a new
// name for one.
func (a *app) playlistNameDialog(c *ui.Context) {
	p := a.pal
	d := &a.naming
	a.dialog(c, "playlist-name", 0.35, &d.open, func(panel ui.Element) {
		a.pane(panel, 26, false)
		panel.Width(400).TextColor(a.inkOn(false).text)
		title, action := "新建歌单", "创建"
		if d.rename != 0 {
			title, action = "重命名歌单", "保存"
		}
		if d.busy {
			action = "正在创建…"
		}
		ink := a.inkOn(false)
		dialogHeader(c, ink, title, "", func() { d.open = false })
		ui.Column(c).Padding(0, 28).Children(func() {
			in := ui.TextInput(c.Key("name"), &d.name).Placeholder("歌单名称").Label("歌单名称").FontSize(14).AutoFocus()
			if in.Submitted() {
				a.submitName()
			}
		})
		dialogFooter(c, func() {
			if settingButton(c.Key("cancel"), ink, "取消", nil, false, false).Clicked() {
				d.open = false
			}
			if primaryButton(c.Key("ok"), action, nil, p, strings.TrimSpace(d.name) == "" || d.busy).Clicked() {
				a.submitName()
			}
		})
	})
}

// deletePlaylistDialog asks whether to delete a playlist, which can't be
// undone.
func (a *app) deletePlaylistDialog(c *ui.Context) {
	d := &a.deleting
	a.dialog(c, "playlist-delete", 0.35, &d.open, func(panel ui.Element) {
		a.pane(panel, 26, false)
		ink := a.inkOn(false)
		panel.Width(400).TextColor(ink.text)
		dialogHeader(c, ink, "删除歌单", "确定删除歌单“"+d.name+"”吗？删除后无法恢复。", func() { d.open = false })
		dialogFooter(c, func() {
			if settingButton(c.Key("cancel"), ink, "取消", nil, false, false).Clicked() {
				d.open = false
			}
			if dangerButton(c.Key("ok"), "删除", iconTrash, false).Clicked() {
				a.deleteChosen()
			}
		})
	})
}

// playlistMenu is what can be done with a playlist of the listener's.
func (a *app) playlistMenu(pl *netease.Playlist) [][]menuAction {
	return [][]menuAction{
		{{label: "重命名…", do: func() { a.startRename(pl) }}},
		{{label: "删除歌单…", do: func() { a.confirmDelete(pl) }}},
	}
}
