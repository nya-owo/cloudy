package app

import (
	"fmt"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/download"
	"netease-music-mygo/internal/netease"
)

// batchLikePause spaces the likes of a playlist so that NetEase does not
// take a whole batch for one burst. Tests set it to zero.
var batchLikePause = 100 * time.Millisecond

// playlistDetailsMenu is what a playlist's three dots opens: its batch
// actions, and the ways to share it.
func (a *app) playlistDetailsMenu(pl *netease.Playlist) [][]menuAction {
	share := [][]menuAction{{
		{label: "复制歌单链接", icon: iconCopy, do: func() { copyText(pl.URL()) }},
		{label: "复制歌单名和链接", icon: iconCopy, do: func() { copyText(pl.Name + "\n" + pl.URL()) }},
		{label: "在浏览器中打开", icon: iconLink, do: func() {
			if err := mygo.Shell.OpenExternal(pl.URL()); err != nil {
				a.toast = "无法打开浏览器"
			}
		}},
	}}
	return [][]menuAction{
		{{label: "批量操作", icon: iconQueue, do: func() { a.startBatch(pl) }}},
		{{label: "分享", icon: iconLink, sub: share}},
	}
}

// playlistDetailsHead is the playlist the details menu acts on.
func (a *app) playlistDetailsHead(pl *netease.Playlist) func(c *ui.Context, ink paneInk) {
	return func(c *ui.Context, ink paneInk) {
		ui.Row(c).Height(58).Padding(0, 8, 0, 8).Gap(11).AlignItems(ui.Center).Children(func() {
			a.cover(c, pl.Cover(), 96, 40, 8).Shrink(0)
			ui.Column(c).Grow(1).Shrink(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, pl.Name).FontSize(13.5).FontWeight(600).SingleLine()
				meta := "歌单"
				if pl.Creator.Nickname != "" {
					meta += " · " + pl.Creator.Nickname
				}
				if n := max(pl.TrackCount, len(pl.Tracks)); n > 0 {
					meta += fmt.Sprintf(" · %d 首", n)
				}
				ui.Text(c, meta).FontSize(12).TextColor(ink.muted).SingleLine()
			})
		})
	}
}

// downloadAll queues every song of tracks the listener may play and does
// not have yet, in their order.
func (a *app) downloadAll(tracks []netease.Track) {
	a.loadDownloads(false)
	queued, skipped := 0, 0
	for _, tr := range tracks {
		if tr.LocalPath != "" || tr.ProgramID != 0 || !tr.Playable() || a.downloaded(tr.ID) || a.downloadPending(tr.ID) {
			skipped++
			continue
		}
		a.downloadQueue = append(a.downloadQueue, tr)
		queued++
	}
	switch {
	case queued == 0:
		a.toast = "没有可下载的歌曲"
	case skipped > 0:
		a.toast = fmt.Sprintf("已加入 %d 首下载，跳过 %d 首", queued, skipped)
	default:
		a.toast = fmt.Sprintf("已加入 %d 首下载", queued)
	}
	a.pumpDownload()
	a.saveDownloadQueue()
	a.redraw()
}

// downloadPending reports whether the song is being saved or waits in the
// download queue.
func (a *app) downloadPending(id int64) bool {
	if a.downloadJob != nil && a.downloadJob.track.ID == id {
		return true
	}
	for _, tr := range a.downloadQueue {
		if tr.ID == id {
			return true
		}
	}
	return false
}

// likeAll likes every song of tracks not liked yet. The hearts fill at
// once; the requests go one by one, and one NetEase refuses goes back.
func (a *app) likeAll(tracks []netease.Track) {
	a.setLikedAll(tracks, true)
}

func (a *app) unlikeAll(tracks []netease.Track) {
	a.setLikedAll(tracks, false)
}

// setLikedAll likes or unlikes the songs of tracks that are not already in
// that state. The hearts change at once; the requests go one by one, and
// one NetEase refuses goes back.
func (a *app) setLikedAll(tracks []netease.Track, like bool) {
	if a.profile == nil {
		a.showMain()
		a.openLogin()
		return
	}
	seen := map[int64]bool{}
	var ids []int64
	for _, tr := range tracks {
		if tr.LocalPath != "" || tr.ProgramID != 0 || tr.ID == 0 || seen[tr.ID] || a.liked[tr.ID] == like {
			continue
		}
		seen[tr.ID] = true
		ids = append(ids, tr.ID)
	}
	if len(ids) == 0 {
		if like {
			a.toast = "这些歌曲都已经在我喜欢的音乐里"
		} else {
			a.toast = "这些歌曲都不在我喜欢的音乐里"
		}
		a.redraw()
		return
	}
	for _, id := range ids {
		a.setLiked(id, like)
	}
	doing, done := "正在喜欢 %d 首…", "已喜欢 %d 首"
	if !like {
		doing, done = "正在取消喜欢 %d 首…", "已取消喜欢 %d 首"
	}
	a.toast = fmt.Sprintf(doing, len(ids))
	gen := a.accountGen
	go func() {
		failed := 0
		for i, id := range ids {
			if err := likeSong(id, like); err != nil {
				failed++
				a.post(func() {
					if gen == a.accountGen {
						a.setLiked(id, !like)
					}
				})
			}
			if i+1 < len(ids) && batchLikePause > 0 {
				time.Sleep(batchLikePause)
			}
		}
		a.post(func() {
			if gen != a.accountGen {
				return
			}
			if failed == 0 {
				a.toast = fmt.Sprintf(done, len(ids))
			} else {
				a.toast = fmt.Sprintf("操作完成，%d 首失败", failed)
			}
		})
	}()
	a.redraw()
}

// addAllToQueue puts every playable song of tracks at the end of the play
// queue, skipping the songs already there.
func (a *app) addAllToQueue(tracks []netease.Track) {
	var add []netease.Track
	skipped := 0
	for _, tr := range tracks {
		if tr.Playable() && tr.LocalPath == "" && tr.ProgramID == 0 {
			add = append(add, tr)
		} else {
			skipped++
		}
	}
	added := a.pl.AddToQueue(add)
	switch {
	case added == 0:
		a.toast = "这些歌曲已经在播放队列里"
	case skipped > 0:
		a.toast = fmt.Sprintf("已加入 %d 首到播放队列，跳过 %d 首", added, skipped)
	default:
		a.toast = fmt.Sprintf("已加入 %d 首到播放队列", added)
	}
	a.redraw()
}

type batchSelection struct {
	on        bool
	playlist  int64
	songs     bool  // the songs a page other than a playlist lists
	at        route // that page
	downloads bool
	selected  map[int64]bool
}

// startBatch enters selection in the playlist itself: no dialog, just the
// songs, a check box each, and the actions that wait until songs are chosen.
func (a *app) startBatch(pl *netease.Playlist) {
	a.batch = batchSelection{on: true, playlist: pl.ID, selected: map[int64]bool{}}
	a.redraw()
}

func (a *app) startDownloadBatch() {
	a.batch = batchSelection{on: true, downloads: true, selected: map[int64]bool{}}
	a.redraw()
}

func (a *app) cancelBatch() {
	if !a.batch.on {
		return
	}
	a.batch = batchSelection{}
	a.redraw()
}

func (a *app) batchActive(plID int64) bool {
	return a.batch.on && !a.batch.downloads && a.route.page == pagePlaylist && a.route.id == plID && a.batch.playlist == plID
}

func (a *app) downloadBatchActive() bool {
	return a.batch.on && a.batch.downloads && a.route.page == pageDownloads
}

func (a *app) rowsBatchActive() bool {
	if !a.batch.on {
		return false
	}
	if a.batch.songs {
		return a.route == a.batch.at
	}
	if a.batch.downloads {
		return a.route.page == pageDownloads
	}
	return a.batchActive(a.route.id)
}

// startSongsBatch chooses among the songs the page lists, as a playlist's
// batch does: an album's, an artist's, the daily songs, those found, those
// played lately.
func (a *app) startSongsBatch() {
	a.batch = batchSelection{on: true, songs: true, at: a.route, selected: map[int64]bool{}}
	a.redraw()
}

// songsBatchActive reports whether the page's songs are being chosen.
func (a *app) songsBatchActive() bool {
	return a.batch.on && a.batch.songs && a.route == a.batch.at
}

// batchEntry is the button that starts choosing among the page's songs.
func (a *app) batchEntry(c *ui.Context) {
	if a.songsBatchActive() {
		return
	}
	p := a.pal
	if pillButton(c.Key("batch-start"), iconCheck, "批量操作", p.hover.Over(p.bg), p.text).Clicked() {
		a.startSongsBatch()
	}
}

// chosenOf returns those of tracks chosen.
func (a *app) chosenOf(tracks []netease.Track) []netease.Track {
	var out []netease.Track
	for _, tr := range tracks {
		if a.batch.selected[tr.ID] {
			out = append(out, tr)
		}
	}
	return out
}

func (a *app) toggleBatch(id int64) {
	if !a.batch.on {
		return
	}
	if a.batch.selected[id] {
		delete(a.batch.selected, id)
	} else {
		a.batch.selected[id] = true
	}
	a.redraw()
}

func (a *app) batchCount(pl *netease.Playlist) int {
	n := 0
	for _, tr := range pl.Tracks {
		if a.batch.selected[tr.ID] {
			n++
		}
	}
	return n
}

func (a *app) batchAll(pl *netease.Playlist) bool {
	return len(pl.Tracks) > 0 && a.batchCount(pl) == len(pl.Tracks)
}

func (a *app) downloadBatchCount() int {
	n := 0
	for _, it := range a.downloads {
		if a.batch.selected[it.Track.ID] {
			n++
		}
	}
	return n
}

func (a *app) downloadBatchAll() bool {
	return len(a.downloads) > 0 && a.downloadBatchCount() == len(a.downloads)
}

func (a *app) downloadBatchSelectAll() {
	if a.downloadBatchAll() {
		a.batch.selected = map[int64]bool{}
	} else {
		a.batch.selected = make(map[int64]bool, len(a.downloads))
		for _, it := range a.downloads {
			a.batch.selected[it.Track.ID] = true
		}
	}
	a.redraw()
}

func (a *app) downloadBatchItems() []download.Item {
	var out []download.Item
	for _, it := range a.downloads {
		if a.batch.selected[it.Track.ID] {
			out = append(out, it)
		}
	}
	return out
}

// batchCheck is the mark of a song chosen in the playlist.
func (a *app) batchCheck(c *ui.Context, selected bool) {
	pal := a.pal
	// Round, as every check of the app is.
	box := ui.Box(c).Size(18, 18).Radius(999).Shrink(0).Center().Border(1.5, pal.faint)
	if selected {
		box.Background(pal.accent).Border(1.5, pal.accent).Children(func() {
			ui.Icon(c, iconCheck).Size(12, 12).TextColor(pal.onAccent)
		})
	}
}

// batchBar is the row over the player bar while a playlist is being chosen:
// how many are chosen, Select All, the three actions, and Cancel.
func (a *app) batchBar(c *ui.Context) {
	if !a.batch.on {
		return
	}
	if a.batch.downloads {
		if a.route.page == pageDownloads {
			a.downloadBatchBar(c)
		}
		return
	}
	if a.songsBatchActive() {
		a.songsBar(c, a.shownTracks, false)
		return
	}
	if a.route.page != pagePlaylist {
		return
	}
	pl := a.playlists[a.route.id]
	if pl == nil {
		return
	}
	a.songsBar(c, pl.Tracks, pl.SpecialType == 5)
}

// songsBar is the row over the player bar while songs of tracks are being
// chosen: how many are chosen, Select All, what to do with them, and
// Cancel. Those of the liked songs are unliked, the others liked.
func (a *app) songsBar(c *ui.Context, tracks []netease.Track, liked bool) {
	pal := a.pal
	chosenN := len(a.chosenOf(tracks))
	all := len(tracks) > 0 && chosenN == len(tracks)
	bar := a.floatBar(c.Key("batch-bar"))
	bar.Children(func() {
		ui.Icon(c, iconCheck).Size(15, 15).TextColor(pal.accent)
		ui.Textf(c, "已选 %d 首", chosenN).FontSize(12.5).FontWeight(600).TextColor(pal.text)
		label := "全选"
		if all {
			label = "取消全选"
		}
		if pillButton(c.Key("batch-all"), nil, label, pal.hover.Over(pal.bg), pal.text).Clicked() {
			if all {
				a.batch.selected = map[int64]bool{}
			} else {
				a.batch.selected = make(map[int64]bool, len(tracks))
				for _, tr := range tracks {
					a.batch.selected[tr.ID] = true
				}
			}
		}
		ui.Spacer(c)
		download := pillButton(c.Key("batch-download"), iconDownload, "下载", pal.accent, pal.onAccent)
		// 喜欢, as the heart says everywhere else: 收藏 is for playlists
		// and albums.
		likeLabel, likeIcon := "喜欢", iconHeart
		if liked {
			likeLabel, likeIcon = "取消喜欢", iconHeartFill
		}
		like := pillButton(c.Key("batch-like"), likeIcon, likeLabel, pal.hover.Over(pal.bg), pal.text)
		next := pillButton(c.Key("batch-next"), iconNext, "下一首播放", pal.hover.Over(pal.bg), pal.text)
		queue := pillButton(c.Key("batch-queue"), iconQueue, "加入队列", pal.hover.Over(pal.bg), pal.text)
		if chosenN == 0 {
			for _, b := range []ui.Element{download, like, next, queue} {
				b.Disabled(true).Opacity(0.5)
			}
		}
		take := func() []netease.Track {
			chosen := a.chosenOf(tracks)
			a.batch = batchSelection{}
			return chosen
		}
		switch {
		case download.Clicked():
			a.downloadAll(take())
		case like.Clicked():
			if liked {
				a.unlikeAll(take())
			} else {
				a.likeAll(take())
			}
		case next.Clicked():
			a.playNext(take())
		case queue.Clicked():
			a.addAllToQueue(take())
		}
		if pillButton(c.Key("batch-cancel"), nil, "取消", pal.hover.Over(pal.bg), pal.text).Clicked() {
			a.cancelBatch()
		}
	})
}

// floatBar is a bar floating over the player bar, as wide as it and of its
// glass, its corners as round: the batch actions' while songs are chosen.
func (a *app) floatBar(c *ui.Context) ui.Element {
	const h = 52
	bar := ui.Row(c).Absolute().Left(glassBarSide).Right(glassBarSide).Bottom(playerBarHeight + 10).
		Height(h).Gap(8).AlignItems(ui.Center)
	if !a.settings.LiquidGlass {
		a.pane(bar, h/2, false)
		return bar.PaddingX(14)
	}
	// The player bar's own glass, shadow and insets: its edges, its first
	// and its last control line up with the bar's under it.
	p := a.pal
	return bar.Padding(0, 20, 0, glassBarInset+6).Radius(h/2).Shadow(0, 6, 24, -6, ui.RGBA(0, 0, 0, 0.3)).
		Material(barGlass{dark: p.dark, clear: float32(a.settings.GlassClear), bend: float32(a.settings.GlassBend)})
}

// downloadBatchBar is the row over the player bar while downloaded songs
// are chosen for deletion.
func (a *app) downloadBatchBar(c *ui.Context) {
	pal := a.pal
	count := a.downloadBatchCount()
	bar := a.floatBar(c.Key("batch-bar"))
	bar.Children(func() {
		ui.Icon(c, iconCheck).Size(15, 15).TextColor(pal.accent)
		ui.Textf(c, "已选 %d 首", count).FontSize(12.5).FontWeight(600).TextColor(pal.text)
		label := "全选"
		if a.downloadBatchAll() {
			label = "取消全选"
		}
		if pillButton(c.Key("batch-all"), nil, label, pal.hover.Over(pal.bg), pal.text).Clicked() {
			a.downloadBatchSelectAll()
		}
		ui.Spacer(c)
		remove := pillButton(c.Key("download-batch-delete"), iconTrash, "删除选中", pal.accent, pal.onAccent)
		if count == 0 {
			remove.Disabled(true).Opacity(0.5)
		}
		if remove.Clicked() {
			a.confirmDeleteSelectedDownloads()
		}
		if pillButton(c.Key("batch-cancel"), nil, "取消", pal.hover.Over(pal.bg), pal.text).Clicked() {
			a.cancelBatch()
		}
	})
}
