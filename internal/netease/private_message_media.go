package netease

import "errors"

// PrivateMessageMediaDetail fills a shared resource from the existing catalog
// APIs. Only its kind and resource ID leave the message view.
func PrivateMessageMediaDetail(kind string, id int64) (PrivateMessageMedia, error) {
	m := PrivateMessageMedia{Kind: kind, ID: id}
	if id <= 0 {
		return m, errors.New("missing shared resource ID")
	}
	switch kind {
	case "song":
		tracks, err := SongDetails([]int64{id})
		if err != nil {
			return m, err
		}
		for _, t := range tracks {
			if t.ID == id {
				m.Title, m.Subtitle, m.CoverURL = t.Name, t.ArtistNames(), t.Album.PicURL
				break
			}
		}
	case "album":
		al, err := AlbumDetail(id)
		if err != nil {
			return m, err
		}
		if al != nil && al.ID == id {
			m.Title, m.Subtitle, m.CoverURL = al.Name, al.ArtistNames(), al.PicURL
		}
	case "playlist":
		// Summary avoids fetching a playlist's tracks just to show its cover.
		p, err := PlaylistSummary(id)
		if err != nil {
			return m, err
		}
		if p != nil && p.ID == id {
			m.Title, m.Subtitle, m.CoverURL = p.Name, p.Creator.Nickname, p.Cover()
		}
	default:
		return m, errors.New("unknown shared resource kind")
	}
	m.Title = privateDisplayText(m.Title, 120, true)
	m.Subtitle = privateDisplayText(m.Subtitle, 160, true)
	m.CoverURL = privateMediaCover(m.CoverURL)
	return m, nil
}
