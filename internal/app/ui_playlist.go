package app

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

// plView is how a list of songs is looked at: what it is searched for and how
// it is sorted, for as long as its page stays open.
type plView struct {
	route route
	query string
	sort  int
	// What these gave last, sorting and searching a thousand songs being more
	// than a frame should do each time.
	of      *netease.Track
	n       int
	asked   string
	sorted  int
	tracks  []netease.Track
	matched bool
	album   string // the album chosen in the album index, by albumKey: only its songs show
	only    string // the album the songs shown were kept to
	// groups are the albums the songs shown fall in, sorted by album; all,
	// those of every song matched, for the album index.
	groups, all []albumGroup
	// The album index: whether it is open, and what it is searched for.
	indexOpen  bool
	indexQuery string
	// backRow is the row the albums showed at the top as one opened, to go
	// back to (ui_album_open.go).
	backRow int
	// move is an album to open or close as the list builds next.
	move *albumMove
}

// albumGroup is an album's songs, together in a list sorted by album: from
// start, n of them.
type albumGroup struct {
	key                 string // albumKey of its songs
	name, artist, cover string
	id                  int64 // the album's, when NetEase knows it
	artistID            int64 // the artist's, for a group of an artist's songs
	start, n            int
	length              time.Duration
}

// Where 按歌手 and 按专辑 are among plSorts: those whose songs show under
// a title for each artist or album.
const (
	sortArtist = 2
	sortAlbum  = 4
)

// artistGroups finds the artists of tracks, sorted by their first artist.
func artistGroups(tracks []netease.Track) []albumGroup {
	var groups []albumGroup
	albums := map[string]bool{}
	for i := range tracks {
		t := &tracks[i]
		name := firstArtist(t)
		if n := len(groups); n > 0 && groups[n-1].name == name {
			g := &groups[n-1]
			g.n++
			g.length += t.Length()
			if g.cover == "" {
				g.cover = t.Album.PicURL
			}
			albums[albumKey(t)] = true
			g.artist = fmt.Sprintf("%d 张专辑", len(albums))
			continue
		}
		clear(albums)
		albums[albumKey(t)] = true
		var id int64
		if len(t.Artists) > 0 {
			id = t.Artists[0].ID
		}
		if name == "" {
			name = "未知歌手"
		}
		groups = append(groups, albumGroup{key: "artist\x1f" + name, name: name, artist: "1 张专辑", cover: t.Album.PicURL,
			artistID: id, start: i, n: 1, length: t.Length()})
	}
	return groups
}

// How a list of songs can be sorted.
var plSorts = []string{"默认排序", "按歌名", "按歌手", "按时长", "按专辑"}

// albumArtist tells apart albums of one name that NetEase does not know,
// as files of the listener's: by their artist. NetEase's own albums are
// told apart by their ID, and keep their songs of several artists together.
func albumArtist(t *netease.Track) string {
	if t.Album.ID != 0 {
		return ""
	}
	return firstArtist(t)
}

// byAlbum orders songs so that each album's keep together: by the album's
// name, then which album of that name it is, and within it as the album
// numbers them (albumOrder). Songs of no album come last.
func byAlbum(x, y netease.Track) int {
	if (x.Album.Name == "") != (y.Album.Name == "") {
		if x.Album.Name == "" {
			return 1
		}
		return -1
	}
	if c := strings.Compare(x.Album.Name, y.Album.Name); c != 0 {
		return c
	}
	if x.Album.ID != y.Album.ID {
		if x.Album.ID < y.Album.ID {
			return -1
		}
		return 1
	}
	if c := strings.Compare(albumArtist(&x), albumArtist(&y)); c != 0 {
		return c
	}
	return albumOrder(&x, &y)
}

// albumGroups finds the albums of tracks, sorted by byAlbum.
func albumGroups(tracks []netease.Track) []albumGroup {
	var groups []albumGroup
	for i := range tracks {
		t := &tracks[i]
		if n := len(groups); n > 0 && byAlbumOnly(&tracks[groups[n-1].start], t) {
			g := &groups[n-1]
			g.n++
			g.length += t.Length()
			if g.cover == "" {
				g.cover = t.Album.PicURL
			}
			if g.artist != firstArtist(t) {
				g.artist = "多位歌手"
			}
			continue
		}
		name := t.Album.Name
		if name == "" {
			name = "未知专辑"
		}
		groups = append(groups, albumGroup{key: albumKey(t), name: name, artist: firstArtist(t), cover: t.Album.PicURL, id: t.Album.ID,
			start: i, n: 1, length: t.Length()})
	}
	return groups
}

// byAlbumOnly reports whether x and y are of one album.
func byAlbumOnly(x, y *netease.Track) bool {
	return albumKey(x) == albumKey(y)
}

// albumKey names the album of t, as byAlbum tells albums apart.
func albumKey(t *netease.Track) string {
	return fmt.Sprintf("%s\x1f%d\x1f%s", t.Album.Name, t.Album.ID, albumArtist(t))
}

// listShown returns the songs of tracks that show, in the order they show.
func (a *app) listShown(tracks []netease.Track) []netease.Track {
	v := &a.plView
	if v.route != a.route {
		// Stash the list being left so returning from an artist or album page
		// restores its search and sort instead of resetting them (#100).
		prev := *v
		if saved, ok := a.plViews[a.route]; ok {
			*v = saved
			delete(a.plViews, a.route)
			if i := slices.Index(a.plViewOrder, a.route); i >= 0 {
				a.plViewOrder = slices.Delete(a.plViewOrder, i, i+1)
			}
		} else {
			*v = plView{route: a.route}
		}
		if prev.route != (route{}) {
			if a.plViews == nil {
				a.plViews = map[route]plView{}
			}
			a.plViews[prev.route] = prev
			if i := slices.Index(a.plViewOrder, prev.route); i >= 0 {
				a.plViewOrder = slices.Delete(a.plViewOrder, i, i+1)
			}
			a.plViewOrder = append(a.plViewOrder, prev.route)
			if n := len(a.plViewOrder) - maxNavHistory; n > 0 {
				for _, old := range a.plViewOrder[:n] {
					delete(a.plViews, old)
				}
				a.plViewOrder = slices.Delete(a.plViewOrder, 0, n)
			}
		}
	}
	q := strings.ToLower(strings.TrimSpace(v.query))
	if len(tracks) == 0 || q == "" && v.sort == 0 {
		// What was sorted before goes with its groups: sorted by album
		// again, it was found here without them, and the switch and the
		// album index were gone (the user, 1.3.2-test.4).
		v.groups, v.all, v.matched = nil, nil, false
		return tracks
	}
	if v.matched && v.of == &tracks[0] && v.n == len(tracks) && v.asked == q && v.sorted == v.sort && v.only == v.album {
		return v.tracks
	}
	var out []netease.Track
	for _, tr := range tracks {
		if q == "" || strings.Contains(trackWords(&tr), q) {
			out = append(out, tr)
		}
	}
	switch v.sort {
	case 1:
		slices.SortStableFunc(out, func(x, y netease.Track) int { return strings.Compare(x.Name, y.Name) })
	case 2:
		slices.SortStableFunc(out, func(x, y netease.Track) int { return strings.Compare(firstArtist(&x), firstArtist(&y)) })
	case 3:
		slices.SortStableFunc(out, func(x, y netease.Track) int { return int(y.Length() - x.Length()) })
	case sortAlbum:
		a.numberTracks(out)
		slices.SortStableFunc(out, byAlbum)
	}
	v.groups, v.all = nil, nil
	switch {
	case v.sort == sortAlbum && a.route.page != pageAlbum:
		v.all = albumGroups(out)
	case v.sort == sortArtist && a.route.page != pageArtist:
		v.all = artistGroups(out)
	}
	if v.all != nil {
		v.groups = v.all
		// Kept to the album chosen in the index, if the songs matched have it.
		if v.album != "" {
			if k := slices.IndexFunc(v.all, func(g albumGroup) bool { return g.key == v.album }); k >= 0 {
				g := v.all[k]
				out = out[g.start : g.start+g.n]
				g.start = 0
				v.groups = []albumGroup{g}
			}
		}
	}
	v.of, v.n, v.asked, v.sorted, v.tracks, v.matched, v.only = &tracks[0], len(tracks), q, v.sort, out, true, v.album
	return out
}

// toolRows lists tracks as trackRows does, with listTools under the header.
func (a *app) toolRows(c *ui.Context, tracks []netease.Track, covers bool, header func(c *ui.Context)) {
	if a.plView.route == a.route {
		a.albumMoved(c.Now())
	}
	shown := a.listShown(tracks)
	var foot []func(c *ui.Context)
	if len(shown) == 0 && len(tracks) > 0 {
		foot = append(foot, func(c *ui.Context) { a.empty(c, "没有匹配的歌曲") })
	}
	groups := a.plView.groups
	g, open := a.albumOpen()
	if open {
		// The album's own head says which it is: no title over its songs.
		groups = nil
	}
	withTools := func(c *ui.Context) {
		if open {
			a.albumHead(c, g, shown)
		} else {
			header(c)
		}
		a.listTools(c, tracks, shown)
	}
	a.trackRowsGrouped(c, shown, groups, covers, withTools, foot...)
}

// pageArt is the picture a page opens under, if it has one: a cover held from
// the card that opened it, until the page has loaded.
func (a *app) pageArt() string {
	id := a.route.id
	switch a.route.page {
	case pagePlaylist:
		if pl := a.playlists[id]; pl != nil {
			return pl.Cover()
		}
	case pageAlbum:
		if d := a.albums[id]; d != nil && d.album != nil {
			return d.album.PicURL
		}
	case pagePodcast:
		if d := a.podcasts[id]; d != nil && d.podcast != nil {
			return d.podcast.PicURL
		}
	case pageArtist:
		if d := a.artists[id]; d != nil && d.profile != nil {
			if d.profile.Avatar != "" {
				return d.profile.Avatar
			}
			return d.profile.PicURL
		}
	case pageDownloads:
		return a.downloadArtURL()
	case pageStyle:
		if url := styleCover(a.style.songs.Songs); url != "" {
			return url
		}
	default:
		return ""
	}
	return a.heldCover()
}

// headCover is the cover in a page's header, which a flight lands on and
// leaves from. From the top: the words beside it grow down, or a long name
// would push it from where its skeleton and the flight to it put it.
func (a *app) headCover(c *ui.Context, url string) {
	cover := ui.Box(c).Size(212, 212).Radius(14).Shadow(0, 18, 40, -10, a.pal.cardShade).AlignSelf(ui.Start)
	a.headAt = cover.LayoutBounds()
	a.landCover(c, cover)
	cover.Children(func() { a.pageCover(c, url) })
}

// trackWords is what a song can be found by, in lower case.
func trackWords(tr *netease.Track) string {
	words := []string{tr.Name, tr.Album.Name}
	words = append(words, tr.Alias...)
	for _, ar := range tr.Artists {
		words = append(words, ar.Name)
	}
	return strings.ToLower(strings.Join(words, "\n"))
}

func firstArtist(tr *netease.Track) string {
	if len(tr.Artists) == 0 {
		return ""
	}
	return tr.Artists[0].Name
}

// longTime says how long a list plays.
func longTime(d time.Duration) string {
	m := int(d.Round(time.Minute) / time.Minute)
	if m < 60 {
		return fmt.Sprintf("%d 分钟", m)
	}
	return fmt.Sprintf("%d 小时 %d 分钟", m/60, m%60)
}

// playPicked plays the song at i of a list it was clicked in: the list as
// the queue, or, as the settings choose, the song alone after the one
// playing, as a song searched for always does.
func (a *app) playPicked(tracks []netease.Track, i int) {
	if a.settings.ClickAdds {
		a.pl.PlayNow(tracks[i])
		return
	}
	a.pl.PlayQueue(tracks, i)
}

// playList plays tracks from the first that can, shuffled or not.
func (a *app) playList(tracks []netease.Track, shuffled bool) {
	if shuffled {
		// Shuffled, from any song that plays: not always the first.
		a.pl.SetMode(player.ModeShuffle)
		a.pl.PlayQueue(tracks, randomPlayable(tracks))
		return
	}
	if a.pl.Shuffle() {
		a.pl.ToggleShuffle()
	}
	a.pl.PlayQueue(tracks, firstPlayable(tracks))
}

// randomPlayable returns where a song that plays is in tracks, picked at
// random, or 0 when none does.
func randomPlayable(tracks []netease.Track) int {
	var ok []int
	for i := range tracks {
		if tracks[i].Playable() {
			ok = append(ok, i)
		}
	}
	if len(ok) == 0 {
		return 0
	}
	return ok[rand.IntN(len(ok))]
}

// pageAura fills the top of a page with a cover, blurred, fading
// into the page: it stays as the header scrolls away, fainter, for the songs
// to read over. It is drawn on the page, under all of it.
func (a *app) pageAura(page ui.Element, url string) {
	bmp := backdrop(a.art.get(netease.Sized(url, 600), true))
	in := artIn(page, bmp)
	if in <= 0 {
		return
	}
	bg := a.pal.bg
	strength := float32(0.42)
	if a.pal.dark {
		strength = 0.6
	}
	page.Draw(func(p *ui.Painter, r ui.Rect) {
		gone := clamp01(a.headGone / 420)
		area := ui.Rect{X: r.X, Y: r.Y, W: r.W, H: min(r.H, 620)}
		p.ImageRegion(bmp, ui.Rect{}, area, ui.ImageOptions{Tint: white.Alpha(strength * in * (1 - 0.6*gone))})
		p.FillGradient(area, ui.LinearGradient{Angle: 180, Oklab: true, From: bg.Alpha(0.08), To: bg}, 0)
	})
}

type stackCover struct {
	key any
	url string
}

// coverStack piles the covers of the first songs beside what a playlist says,
// and fans them out under the pointer, as the daily card's. A narrow window
// has no room for it, nor a list of a single album.
func (a *app) coverStack(c *ui.Context, pl *netease.Playlist) {
	var items []stackCover
	for i := range pl.Tracks {
		url := pl.Tracks[i].Album.PicURL
		if url == "" || slices.ContainsFunc(items, func(it stackCover) bool { return it.url == url }) {
			continue
		}
		items = append(items, stackCover{key: a.trackCoverIdentity(&pl.Tracks[i]), url: url})
		if len(items) == 3 {
			break
		}
	}
	a.coverStackItems(c, items)
}

// coverStackURLs piles the covers of a page's first songs, where there is
// room for three.
func (a *app) coverStackURLs(c *ui.Context, urls []string) {
	items := make([]stackCover, len(urls))
	for i, url := range urls {
		items[i] = stackCover{url: url}
	}
	a.coverStackItems(c, items)
}

func (a *app) coverStackItems(c *ui.Context, items []stackCover) {
	w, _ := c.Size()
	if w-a.sideWidth(w) < 860 {
		return
	}
	if len(items) < 3 {
		return
	}
	stack := ui.Box(c.Key("stack")).Size(204, 164).Shrink(0).AlignSelf(ui.Center).Label("页面中的封面")
	fanned := stack.Hovered()
	spring := ui.Animation{Spring: ui.SpringOptions{Duration: 460 * time.Millisecond, Bounce: 0.18}}
	stack.Children(func() {
		for i := 2; i >= 0; i-- {
			k := float32(i)
			pose := ui.Pose{Rotate: -6 * k, X: -20 * k}
			if fanned {
				pose = ui.Pose{Rotate: -11 * k, X: -36 * k, Y: -4 * k}
			}
			cov := ui.Box(c.Key(i)).Absolute().Right(0).Top(16).Size(132, 132).Radius(14).
				Opacity(1-0.14*k).Shadow(0, 10, 22, -6, ui.RGBA(0, 0, 0, 0.4))
			cov.AnimatePose(pose, spring)
			cov.Children(func() { a.coverFillBox(c, items[i].key, items[i].url, 240, 96, 14) })
		}
	})
}

// listTools are over a list of songs: how many there are and how long they
// play, a search among them, and how they are sorted.
func (a *app) listTools(c *ui.Context, tracks, shown []netease.Track) {
	if len(tracks) == 0 {
		return
	}
	p, v := a.pal, &a.plView
	ui.Row(c.Key("tools")).Gap(10).AlignItems(ui.Center).Margin(0, 12, 10, 12).Children(func() {
		// An album open counts its own songs (ui_album_open.go).
		_, open := a.albumOpen()
		counted := tracks
		if open {
			counted = shown
		}
		var total time.Duration
		for i := range counted {
			total += counted[i].Length()
		}
		line := fmt.Sprintf("共 %d 首 · %s", len(counted), longTime(total))
		if strings.TrimSpace(v.query) != "" {
			line = fmt.Sprintf("找到 %d 首", len(shown))
		}
		ui.Text(c, line).FontSize(12.5).TextColor(p.muted).Grow(1).Shrink(1).MinWidth(0).SingleLine()
		// Back to the song playing is a button floating over the list
		// (listFloats), as it scrolls out of view.
		// A playlist's and the downloads' own menus start theirs.
		if pg := a.route.page; pg != pagePlaylist && pg != pageDownloads && pg != pageLocal {
			a.batchEntry(c)
		}
		field := ui.Row(c).Width(210).Height(32).PaddingX(12).Gap(7).Radius(999).AlignItems(ui.Center)
		if c.Focused(a.filterBox) {
			field.Background(p.bg).Shadow(0, 0, 0, 3, p.accent.Alpha(0.25)).Border(1, p.accent.Alpha(0.6))
		} else {
			field.Background(p.hover).Border(1, p.hairline)
		}
		field.Children(func() {
			ui.Icon(c, iconSearch).Size(13, 13).TextColor(p.muted)
			ui.TextInputBase(c.Key("filter"), &v.query).Bind(&a.filterBox).Grow(1).MinWidth(0).
				Placeholder("在列表内搜索").FontSize(12.5).Label("在列表内搜索")
			if v.query != "" && iconButton(c.Key("clear"), iconX, 11, p.muted, "清除搜索").Size(20, 20).Clicked() {
				v.query = ""
			}
		})
		sort := ui.ButtonBase(c.Key("sort")).Height(32).PaddingX(12).Gap(6).Radius(999).AlignItems(ui.Center).FocusRing(false).Label("排序")
		bg := p.hover
		if sort.Hovered() {
			bg = p.press
		}
		sort.Background(sort.AnimateColor("bg", bg, ui.Animation{Duration: 100 * time.Millisecond}))
		sort.Children(func() {
			ui.Text(c, plSorts[v.sort]).FontSize(12.5).SingleLine()
			ui.Icon(c, iconDown).Size(12, 12).TextColor(p.muted)
		})
		if len(v.all) > 1 {
			a.albumIndexButton(c, v)
			if v.sort == sortAlbum && a.albumGridPage() && !open {
				a.albumViewSwitch(c)
			}
		}
		if sort.Clicked() {
			acts := make([]menuAction, len(plSorts))
			for i, label := range plSorts {
				acts[i] = menuAction{label: label, checked: i == v.sort, do: func() { v.sort = i }}
			}
			a.menuBelow(sort, false, [][]menuAction{acts}, nil)
		}
	})
}

// playlistTop is a playlist over the page once its header has scrolled away,
// in the strip along the window's top, by the back button: a click goes back
// to the header, and the button plays it.
func (a *app) playlistTop(c *ui.Context) {
	pl := a.playlists[a.route.id]
	if a.route.page != pagePlaylist || pl == nil {
		return
	}
	if a.batchActive(pl.ID) {
		return
	}
	p := a.pal
	bar := ui.Row(c.Key("list-top")).Gap(10).AlignItems(ui.Center).Margin(0, 0, 0, 8).Padding(4, 4, 4, 4)
	k := b2f(a.headGone > 250)
	if a.motion != motionOff {
		k = bar.AnimateTo("in", k, motionPane)
	}
	if k <= 0 {
		return
	}
	// On a pane of its own: the songs scroll under it.
	a.pane(bar, 20, false).Opacity(k)
	if a.motion == motionFull {
		bar.Translate(0, 6*(1-k))
	}
	bar.Children(func() {
		up := ui.ButtonBase(c.Key("up")).Gap(10).AlignItems(ui.Center).FocusRing(false).Label("回到歌单开头")
		up.Children(func() {
			a.cover(c, pl.Cover(), 96, 28, 8)
			ui.Text(c, pl.Name).FontSize(13.5).FontWeight(700).SingleLine().MaxWidth(320)
		})
		if up.Clicked() {
			a.trackList.ScrollTo(0, ui.Start)
		}
		play := iconButton(c.Key("list-play"), iconPlay, 13, p.onAccent, "播放").Size(28, 28).Background(p.accent)
		if play.Clicked() {
			a.playList(pl.Tracks, false)
		}
	})
}

// How the visible rows of a list come in, one after another, once it opens.
const (
	rowStep   = 24 * time.Millisecond
	rowTime   = 280 * time.Millisecond
	rowSpread = 360 * time.Millisecond
	// rowsWaitMost is how long the rows wait for the list's first layout
	// before showing without their entrance: a list that is built but not
	// laid out, as in a minimized window, would otherwise keep them hidden
	// and ask for frames on end.
	rowsWaitMost = 250 * time.Millisecond
)

// rowsWaitedOut drops the entrance once the rows have waited for the
// list's layout too long.
func (a *app) rowsWaitedOut(c *ui.Context) bool {
	if a.rowsPending && c.Now().Sub(a.rowsWaited) > rowsWaitMost {
		a.rowsPending, a.rowsStart, a.rowsCount = false, 0, 0
		return true
	}
	return false
}

// listOpened prepares the one-time entrance for the tracks visible when this
// page's list first lays out: a cover on its way gets a head start, and back
// to a list, it is there already.
func (a *app) listOpened(c *ui.Context, items []int, grid bool) {
	if a.rowsRoute != a.route {
		if len(items) == 0 {
			return
		}
		a.rowsRoute = a.route
		a.rowsPending, a.rowsStart, a.rowsCount = false, 0, 0
		if grid || a.pageDir < 0 || a.motion != motionFull {
			a.rowsAt = time.Time{}
			return
		}
		a.rowsAt = c.Now()
		if a.flight.url != "" {
			a.rowsAt = a.rowsAt.Add(180 * time.Millisecond)
		}
		// An empty list shown earlier on this route may already have laid out
		// trackList; reset it so Visible waits for these tracks to lay out.
		if first, last := a.trackList.Visible(); last >= first {
			a.trackList = ui.ListState{}
			a.trackList.Visible()
		}
		a.rowsPending, a.rowsWaited = true, c.Now()
		c.AnimationFrame()
		return
	}
	if !a.rowsPending || a.rowsWaitedOut(c) {
		return
	}
	if grid || len(items) == 0 || a.motion != motionFull {
		a.rowsPending, a.rowsStart, a.rowsCount = false, 0, 0
		return
	}
	first, last := a.trackList.Visible()
	if last < first {
		c.AnimationFrame()
		return
	}
	a.rowsPending = false
	a.rowsStart, a.rowsCount = visibleTrackRange(items, first, last)
}

// visibleTrackRange maps trackList's visible row range [first, last] to the
// contiguous song indices it covers, skipping the header, group titles, and
// footers.
func visibleTrackRange(items []int, first, last int) (start, count int) {
	if last < first || len(items) == 0 {
		return 0, 0
	}
	lo := max(0, first-1)
	hi := min(len(items)-1, last-1)
	end := -1
	for slot := lo; slot <= hi; slot++ {
		if idx := items[slot]; idx >= 0 {
			if end < 0 {
				start = idx
			}
			end = idx
		}
	}
	if end < 0 {
		return 0, 0
	}
	return start, end - start + 1
}

// rowIn returns how far song i has come in, from 0 to 1.
func (a *app) rowIn(c *ui.Context, i int) float32 {
	if a.motion != motionFull {
		return 1
	}
	if a.rowsPending && !a.rowsWaitedOut(c) {
		c.AnimationFrame()
		return 0
	}
	if a.rowsCount <= 0 || i < a.rowsStart || i >= a.rowsStart+a.rowsCount {
		return 1
	}
	intervals := time.Duration(a.rowsCount - 1)
	step := min(rowStep, rowSpread/max(1, intervals))
	elapsed := c.Now().Sub(a.rowsAt)
	if elapsed >= rowTime+intervals*step {
		a.rowsCount = 0
		return 1
	}
	t := float32(elapsed-time.Duration(i-a.rowsStart)*step) / float32(rowTime)
	if t >= 1 {
		return 1
	}
	c.AnimationFrame()
	t = 1 - clamp01(t)
	return 1 - t*t*t
}

// rowTint is the color of a song's cover for its row under the pointer, if
// the cover is here and has one.
func (a *app) rowTint(url string) (ui.Color, bool) {
	art := a.art.peek(netease.Sized(url, 96))
	if art == nil || art.failed {
		return ui.Color{}, false
	}
	h, s, _ := hsv(float64(art.tint.R)/255, float64(art.tint.G)/255, float64(art.tint.B)/255)
	if s < 0.15 {
		return ui.Color{}, false
	}
	v := 0.55
	if a.pal.dark {
		v = 0.8
	}
	return hsvColor(h, min(s, 0.65), v), true
}
