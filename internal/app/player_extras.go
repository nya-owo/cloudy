package app

import (
	"strconv"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// toggleLyricTrans shows or hides the translations of lyrics, and says
// which, as the player bar's button does.
func (a *app) toggleLyricTrans() {
	a.settings.LyricTrans = !a.settings.LyricTrans
	a.changed()
	if a.settings.LyricTrans {
		a.say("lyric-trans", "已显示歌词翻译")
	} else {
		a.say("lyric-trans", "已隐藏歌词翻译")
	}
	a.redraw()
}

// addToPlaylistMenu opens, above the player bar's button b, the playlists
// the song tr can be added to; signed out, it asks the listener to sign in.
func (a *app) addToPlaylistMenu(b ui.Element, tr *netease.Track) {
	if a.profile == nil {
		a.toast = "登录后才能添加到歌单"
		a.openLogin()
		a.redraw()
		return
	}
	song := *tr
	acts := a.playlistActions(&song, 0)
	if len(acts) == 0 || len(acts[0].sub) == 0 {
		return
	}
	// The submenu's groups, as a menu of their own: new, then the lists.
	a.menuBelow(b, false, acts[0].sub, a.songHead(&song))
}

// podcastSpeeds are the speeds episodes play at, their pitch kept.
var podcastSpeeds = []float64{0.75, 1, 1.25, 1.5, 1.75, 2}

// speedLabel says a speed as players do: 1.5×.
func speedLabel(v float64) string {
	if v <= 0 {
		v = 1
	}
	return strconv.FormatFloat(v, 'f', -1, 64) + "×"
}

// setPodcastSpeed has episodes play at v, the one playing too.
func (a *app) setPodcastSpeed(v float64) {
	if v == 1 {
		v = 0
	}
	a.settings.PodcastSpeed = v
	a.pl.SetPodcastSpeed(v)
	a.changed()
	a.redraw()
}

// speedButton is an episode's speed in Now Playing: a click goes to the
// next of podcastSpeeds, after the last back to the first.
func (a *app) speedButton(c *ui.Context) {
	cur := a.settings.PodcastSpeed
	if cur <= 0 {
		cur = 1
	}
	b := ui.ButtonBase(c.Key("np-speed")).Height(18).PaddingX(7).Radius(999).AlignItems(ui.Center).FocusRing(false).Label("播放速度")
	bg := white.Alpha(0.12)
	if b.Hovered() {
		bg = white.Alpha(0.2)
	}
	b.Background(bg).Children(func() {
		ui.Text(c, speedLabel(cur)).FontSize(10.5).FontWeight(600).FontFeatures("tnum").TextColor(white.Alpha(0.85))
	})
	if b.Clicked() {
		next := podcastSpeeds[0]
		for i, v := range podcastSpeeds {
			if v == cur && i+1 < len(podcastSpeeds) {
				next = podcastSpeeds[i+1]
			}
		}
		a.setPodcastSpeed(next)
		a.say("speed", "播放速度 "+speedLabel(next))
	}
}

// podcastSpeedRow chooses the speed of episodes in the settings.
func (a *app) podcastSpeedRow(c *ui.Context, row func(string, string, func())) {
	row("播客播放速度", "加快或放慢节目，声音的音调保持不变；也可以在播放页点速度切换", func() {
		labels := make([]string, len(podcastSpeeds))
		chosen := speedLabel(a.settings.PodcastSpeed)
		for i, v := range podcastSpeeds {
			labels[i] = speedLabel(v)
		}
		if a.paneSelect(c.Key("podcast-speed"), "播客播放速度", &chosen, labels) {
			for _, v := range podcastSpeeds {
				if speedLabel(v) == chosen {
					a.setPodcastSpeed(v)
				}
			}
		}
	})
}
