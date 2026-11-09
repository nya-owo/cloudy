package app

import (
	"fmt"

	"netease-music-mygo/internal/netease"

	"github.com/egoist/mygo/ui"
)

// cloudState is the listener's music cloud drive, as the 我的音乐云盘 page
// lists it.
type cloudState struct {
	drive   *netease.CloudDrive
	loading bool
	err     string
	gen     int  // the account the drive is loading for (accountGen)
	stale   bool // read again once the reading under way is done
}

// fetchCloud reads the drive; tests replace it.
var fetchCloud = netease.CloudSongs

// loadCloud reads the drive once, or again with again.
func (a *app) loadCloud(again bool) {
	s := &a.cloud
	if s.loading && again {
		s.stale = true
	}
	if a.profile == nil || s.loading || (s.drive != nil && !again) {
		return
	}
	s.loading, s.err = true, ""
	gen := a.accountGen
	s.gen = gen
	go func() {
		drive, err := fetchCloud()
		a.post(func() {
			if a.accountGen != gen {
				return
			}
			s.loading = false
			if s.stale {
				s.stale = false
				defer a.loadCloud(true)
			}
			if err != nil {
				s.err = "无法读取音乐云盘，请检查网络后重试"
				return
			}
			s.drive = drive
		})
	}()
}

// cloudUsage says how much of the drive the songs take.
func cloudUsage(d *netease.CloudDrive) string {
	gb := func(n int64) string {
		if n < 1<<30 {
			return fmt.Sprintf("%d MB", n>>20)
		}
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	}
	if d.Max <= 0 {
		return fmt.Sprintf("%d 首", len(d.Songs))
	}
	return fmt.Sprintf("%d 首 · 已用 %s / %s", len(d.Songs), gb(d.Used), gb(d.Max))
}

// cloudPage lists the songs the listener uploaded to their cloud drive,
// which play whole whatever their rights: those taken down, or for VIPs.
func (a *app) cloudPage(c *ui.Context) {
	s := &a.cloud
	if a.profile == nil {
		a.centered(c, "登录后可以播放上传到网易云音乐云盘的歌曲", nil)
		return
	}
	a.loadCloud(false)
	switch {
	case s.err != "":
		a.centered(c, s.err, func() { a.loadCloud(true) })
		return
	case s.drive == nil:
		a.centered(c, "正在读取音乐云盘…", nil)
		return
	}
	tracks := s.drive.Songs
	header := func(c *ui.Context) {
		p := a.pal
		a.heading(c, "我的音乐云盘", cloudUsage(s.drive))
		ui.Row(c).Wrap().GapX(10).GapY(8).Margin(0, 0, 18, 0).Children(func() {
			if len(tracks) > 0 {
				if pillButton(c.Key("play"), iconPlay, "播放全部", p.accent, p.onAccent).Clicked() {
					a.playList(tracks, false)
				}
				if pillButton(c.Key("shuffle"), iconShuffle, "随机播放", p.hover.Over(p.bg), p.text).Clicked() {
					a.playList(tracks, true)
				}
			}
			if pillButton(c.Key("upload"), iconUpload, "上传音乐", p.hover.Over(p.bg), p.text).Clicked() {
				a.pickUploads()
			}
			queueLabel := "上传列表"
			if n := a.uploadQueueCount(); n > 0 {
				queueLabel = fmt.Sprintf("上传列表 %d", n)
				if q := &a.up.queue; q.running != nil && q.size > 0 && q.done > 0 {
					queueLabel += fmt.Sprintf(" · %d%%", q.done*100/q.size)
				}
			}
			if pillButton(c.Key("uploads"), iconQueue, queueLabel, p.hover.Over(p.bg), p.text).Clicked() {
				a.openUploadQueue()
			}
			label := "刷新"
			if s.loading {
				label = "正在刷新…"
			}
			if pillButton(c.Key("reload"), iconReload, label, p.hover.Over(p.bg), p.text).Clicked() {
				a.loadCloud(true)
			}
		})
	}
	if len(tracks) == 0 {
		a.trackRows(c, nil, true, header, func(c *ui.Context) {
			a.empty(c, "云盘里还没有歌曲，点「上传音乐」把电脑里的歌传上来")
		})
		return
	}
	a.toolRows(c, tracks, true, header)
}
