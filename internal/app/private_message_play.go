package app

import (
	"errors"

	"netease-music-mygo/internal/netease"
)

// A pending share belongs to one login, pane opening and playback selection.
// Pointer identity distinguishes repeated requests, including the same resource.
type privatePlayRequest struct {
	key                          privateMediaKey
	account, epoch, queue, radio int
	track                        *netease.Track
	playing                      bool
}

func privateMediaPlayable(media netease.PrivateMessageMedia) bool {
	if media.ID <= 0 {
		return false
	}
	return media.Kind == "song" || media.Kind == "album" || media.Kind == "playlist"
}

func (a *app) privatePlayCurrent(req *privatePlayRequest) bool {
	return a.profile != nil && a.dm.open && req.account == a.accountGen && req.epoch == a.dm.epoch &&
		req.queue == a.pl.RadioGeneration() && req.radio == a.radioGen && req.track == a.pl.Current() && req.playing == a.pl.Playing()
}

func (a *app) playPrivateMedia(media netease.PrivateMessageMedia) {
	if !privateMediaPlayable(media) || a.profile == nil || !a.dm.open {
		return
	}
	key := privateMediaKey{media.Kind, media.ID}
	if req := a.dm.play; req != nil && req.key == key && a.privatePlayCurrent(req) {
		return
	}
	a.cancelRadioStart()
	req := &privatePlayRequest{key: key, account: a.accountGen, epoch: a.dm.epoch,
		queue: a.pl.RadioGeneration(), radio: a.radioGen, track: a.pl.Current(), playing: a.pl.Playing()}
	a.dm.play = req
	// Reuse full lists already loaded in this account, never the cover summary.
	if media.Kind == "playlist" {
		if pl := a.playlists[media.ID]; pl != nil && len(pl.Tracks) > 0 {
			a.privatePlayAnswered(req, pl.Tracks, nil)
			return
		}
	}
	if media.Kind == "album" {
		if d := a.albums[media.ID]; d != nil && d.album != nil && len(d.album.Songs) > 0 {
			a.privatePlayAnswered(req, d.album.Songs, nil)
			return
		}
	}
	go func() {
		tracks, err := privateMediaTracks(media.Kind, media.ID)
		a.post(func() { a.privatePlayAnswered(req, tracks, err) })
	}()
}

func privateMediaTracks(kind string, id int64) ([]netease.Track, error) {
	switch kind {
	case "song":
		tracks, err := trackDetails([]int64{id})
		for _, track := range tracks {
			if track.ID == id {
				return []netease.Track{track}, err
			}
		}
		return nil, err
	case "album":
		al, err := albumDetail(id)
		if al != nil && al.ID == id {
			return al.Songs, err
		}
		return nil, err
	case "playlist":
		pl, err := fetchPlaylist(id)
		if pl != nil && pl.ID == id {
			return pl.Tracks, err
		}
		return nil, err
	}
	return nil, errors.New("unknown shared resource kind")
}

func (a *app) privatePlayAnswered(req *privatePlayRequest, tracks []netease.Track, err error) {
	if a.dm.play != req {
		return
	}
	a.dm.play = nil
	if !a.privatePlayCurrent(req) {
		return
	}
	if err != nil {
		a.toast = "分享载入失败，请稍后再试"
		return
	}
	if len(tracks) == 0 {
		a.toast = "此分享暂无可播放的歌曲"
		return
	}
	playable := false
	for _, t := range tracks {
		playable = playable || t.Playable()
	}
	if !playable {
		a.toast = "此分享中的歌曲暂不可播放"
		return
	}
	a.playList(tracks, false)
}
