package netease

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The public NetEase Cloud Music API, as the web player calls it, without
// an account: what anyone may listen to.

const apiBase = "https://music.163.com"

const UserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15"

var httpClient = &http.Client{Timeout: 20 * time.Second}

type Artist struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type Album struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	PicURL string `json:"picUrl"`
}

type Track struct {
	ID       int64    `json:"id"`
	Name     string   `json:"name"`
	Artists  []Artist `json:"ar"`
	Album    Album    `json:"al"`
	Duration int64    `json:"dt"` // milliseconds
	Fee      int      `json:"fee"`
	Alias    []string `json:"alia"`
	// Reason says why a daily recommendation was picked.
	Reason string `json:"reason"`
	// Privilege is what the listener may do with the track, if known.
	Privilege *Privilege `json:"privilege"`
	// A podcast's episode plays as a track of its audio, ID, with the
	// episode's ProgramID, the PodcastID it is in, and what it is about.
	ProgramID   int64  `json:"programId,omitempty"`
	PodcastID   int64  `json:"podcastId,omitempty"`
	Description string `json:"description,omitempty"`
	// LocalPath is where a downloaded or otherwise local song is, rather
	// than streamed from NetEase: its cover is a file too, in PicURL.
	LocalPath string `json:"localPath,omitempty"`
}

// Privilege is what NetEase lets the listener signed in, or nobody, do
// with a track.
type Privilege struct {
	ID int64 `json:"id"`
	St int   `json:"st"` // below 0: taken down
	Pl int   `json:"pl"` // the best bit rate the listener may play; 0 for none
	// Cs is whether the listener uploaded the track to their cloud drive,
	// which they play from whatever its rights.
	Cs bool `json:"cs"`
}

// withPrivileges gives tracks their privileges, matched by ID.
func withPrivileges(tracks []Track, privileges []Privilege) {
	byID := make(map[int64]*Privilege, len(privileges))
	for i := range privileges {
		byID[privileges[i].ID] = &privileges[i]
	}
	for i := range tracks {
		if p := byID[tracks[i].ID]; p != nil {
			tracks[i].Privilege = p
		}
	}
}

// ArtistNames joins the track's artists as the credits read.
func (t *Track) ArtistNames() string {
	names := make([]string, len(t.Artists))
	for i, a := range t.Artists {
		names[i] = a.Name
	}
	return strings.Join(names, ", ")
}

// Playable reports whether the listener can play the whole track: as their
// privilege says, or else as its fee does for anyone, VIP songs (1) and
// paid albums (4) giving only a preview.
func (t *Track) Playable() bool {
	if t.ProgramID > 0 {
		// An episode's audio has no privilege: whether it streams, as a
		// paid one bought, is up to its stream.
		return true
	}
	if t.Privilege != nil {
		if t.Privilege.Cs {
			return true
		}
		return t.Privilege.St >= 0 && t.Privilege.Pl > 0
	}
	return t.Fee != 1 && t.Fee != 4
}

func (t *Track) Length() time.Duration { return time.Duration(t.Duration) * time.Millisecond }

// URL is the track's page on the NetEase website.
func (t *Track) URL() string {
	if t.ProgramID > 0 {
		return apiBase + "/program?id=" + strconv.FormatInt(t.ProgramID, 10)
	}
	return apiBase + "/song?id=" + strconv.FormatInt(t.ID, 10)
}

// Thread names the track's comments, and what NetEase likes it by, for an
// episode: the episode's, not its audio's.
func (t *Track) Thread() string {
	if t.ProgramID > 0 {
		return programThread(t.ProgramID)
	}
	return "R_SO_4_" + strconv.FormatInt(t.ID, 10)
}

type Playlist struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	CoverURL    string  `json:"coverImgUrl"`
	PicURL      string  `json:"picUrl"`
	Description string  `json:"description"`
	PlayCount   float64 `json:"playCount"`
	TrackCount  int     `json:"trackCount"`
	UpdateFreq  string  `json:"updateFrequency"`
	SpecialType int     `json:"specialType"` // 5: the listener's liked songs
	Privacy     int     `json:"privacy"`     // 10: only its creator sees it
	Subscribed  bool    `json:"subscribed"`  // the listener saved it
	Creator     struct {
		UserID   int64  `json:"userId"`
		Nickname string `json:"nickname"`
	} `json:"creator"`
	Tracks   []Track `json:"tracks"`
	TrackIDs []struct {
		ID int64 `json:"id"`
	} `json:"trackIds"`
	// Preview holds a chart's top songs, as its card shows them.
	Preview []ChartLine `json:"-"`
}

// ChartLine is a song at the top of a chart: its name and its artist.
type ChartLine struct {
	First  string `json:"first"`
	Second string `json:"second"`
}

func (p *Playlist) Cover() string {
	if p.CoverURL != "" {
		return p.CoverURL
	}
	return p.PicURL
}

// URL is the playlist's page on the NetEase site.
func (p *Playlist) URL() string {
	return apiBase + "/playlist?id=" + strconv.FormatInt(p.ID, 10)
}

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Msg     string `json:"msg"`
}

func get(path string, query url.Values, out any) error {
	return getContext(context.Background(), path, query, out)
}

func getContext(ctx context.Context, path string, query url.Values, out any) error {
	u := apiBase + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return err
	}
	return do(req, out)
}

func post(path string, form url.Values, out any) error {
	req, err := http.NewRequest("POST", apiBase+path, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return do(req, out)
}

func do(req *http.Request, out any) error {
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Referer", apiBase+"/")
	res, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	var status apiError
	if err := json.Unmarshal(body, &status); err != nil {
		return fmt.Errorf("netease: %s: %w", req.URL.Path, err)
	}
	if status.Code != 200 {
		msg := status.Message
		if msg == "" {
			msg = status.Msg
		}
		return fmt.Errorf("netease: %s: code %d %s", req.URL.Path, status.Code, msg)
	}
	return json.Unmarshal(body, out)
}

// FeaturedPlaylists returns the playlists the home page recommends.
func FeaturedPlaylists(limit int) ([]Playlist, error) {
	var res struct {
		Result []Playlist `json:"result"`
	}
	err := get("/api/personalized/playlist", url.Values{"limit": {strconv.Itoa(limit)}}, &res)
	return res.Result, err
}

// DailySongs returns the day's recommended songs: chosen for the listener
// once signed in, and what many listen to before.
func DailySongs() ([]Track, error) {
	var res struct {
		Data struct {
			DailySongs []Track `json:"dailySongs"`
		} `json:"data"`
	}
	if err := get("/api/v3/discovery/recommend/songs", nil, &res); err != nil {
		return nil, err
	}
	// They come without privileges: the songs' details have them.
	songs := res.Data.DailySongs
	detailPrivileges(songs)
	return songs, nil
}

// detailPrivileges gives songs the privileges their details carry for the
// listener, which are right where other lists have none, or wrong ones.
func detailPrivileges(songs []Track) {
	ids := make([]int64, len(songs))
	for i := range songs {
		ids[i] = songs[i].ID
	}
	details, _ := SongDetails(ids) // as many as came
	var privileges []Privilege
	for _, d := range details {
		if d.Privilege != nil {
			privileges = append(privileges, *d.Privilege)
		}
	}
	withPrivileges(songs, privileges)
}

// Charts returns the charts, each with its top three songs.
func Charts() ([]Playlist, error) {
	var res struct {
		List []struct {
			Playlist
			Tracks []ChartLine `json:"tracks"`
		} `json:"list"`
	}
	if err := get("/api/toplist/detail", nil, &res); err != nil {
		return nil, err
	}
	out := make([]Playlist, len(res.List))
	for i, l := range res.List {
		out[i] = l.Playlist
		out[i].Tracks = nil
		out[i].Preview = l.Tracks
	}
	return out, nil
}

// playlistTrackLimit is far above what an ordinary listener keeps in one
// playlist; NetEase itself may still cap a list below it.
const playlistTrackLimit = 10000

// PlaylistSummary returns a playlist without its tracks: its name, cover
// and who made it.
func PlaylistSummary(id int64) (*Playlist, error) {
	var res struct {
		Playlist Playlist `json:"playlist"`
	}
	q := url.Values{"id": {strconv.FormatInt(id, 10)}, "n": {"0"}}
	if err := get("/api/v6/playlist/detail", q, &res); err != nil {
		return nil, err
	}
	res.Playlist.Tracks = nil
	return &res.Playlist, nil
}

// PlaylistDetail returns a playlist with up to playlistTrackLimit of its
// tracks.
func PlaylistDetail(id int64) (*Playlist, error) {
	var res struct {
		Playlist   Playlist    `json:"playlist"`
		Privileges []Privilege `json:"privileges"`
	}
	q := url.Values{"id": {strconv.FormatInt(id, 10)}, "n": {strconv.Itoa(playlistTrackLimit)}}
	if err := get("/api/v6/playlist/detail", q, &res); err != nil {
		return nil, err
	}
	p := &res.Playlist
	withPrivileges(p.Tracks, res.Privileges)
	// Its privileges mark VIP songs as taken down that a subscriber may
	// play, as Stan's: the songs' details say right. Long playlists list
	// only their first tracks in full, the rest by id: one round of
	// details, its batches side by side, does both.
	ids := make([]int64, len(p.Tracks))
	for i := range p.Tracks {
		ids[i] = p.Tracks[i].ID
	}
	if len(p.Tracks) < len(p.TrackIDs) {
		for _, t := range p.TrackIDs[len(p.Tracks):min(len(p.TrackIDs), playlistTrackLimit)] {
			ids = append(ids, t.ID)
		}
	}
	details, _ := SongDetails(ids)
	var privileges []Privilege
	for _, d := range details {
		if d.Privilege != nil {
			privileges = append(privileges, *d.Privilege)
		}
	}
	withPrivileges(p.Tracks, privileges)
	// Those that came of the rest, as many as did: a batch that failed
	// leaves its songs out, not all of them.
	listed := make(map[int64]bool, len(p.Tracks))
	for _, t := range p.Tracks {
		listed[t.ID] = true
	}
	for _, d := range details {
		if !listed[d.ID] {
			listed[d.ID] = true
			p.Tracks = append(p.Tracks, d)
		}
	}
	return p, nil
}

// SongDetails returns the tracks of ids, in their order. It goes through
// the eapi: the web API's privileges call some VIP songs taken down for
// subscribers, whom the eapi streams them to. Batches go a few at a time;
// one that fails leaves its songs out of those returned, with its error.
func SongDetails(ids []int64) ([]Track, error) {
	var batches [][]int64
	for len(ids) > 0 {
		batch := ids[:min(len(ids), songDetailBatch)]
		ids = ids[len(batch):]
		batches = append(batches, batch)
	}
	got := make([][]Track, len(batches))
	errs := make([]error, len(batches))
	var wg sync.WaitGroup
	slots := make(chan struct{}, songDetailsAtOnce)
	for i, batch := range batches {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer func() { <-slots; wg.Done() }()
			got[i], errs[i] = songDetailBatchOf(batch)
		}()
	}
	wg.Wait()
	var out []Track
	for i := range batches {
		out = append(out, got[i]...)
	}
	return out, errors.Join(errs...)
}

// songDetailBatch is how many songs one request of details asks for, and
// songDetailsAtOnce how many such requests go at once.
const (
	songDetailBatch   = 400
	songDetailsAtOnce = 4
)

// songDetailBatchOf returns the details of one batch of ids.
func songDetailBatchOf(batch []int64) ([]Track, error) {
	c := make([]map[string]int64, len(batch))
	for i, id := range batch {
		c[i] = map[string]int64{"id": id}
	}
	b, _ := json.Marshal(c)
	var res struct {
		Code       int         `json:"code"`
		Songs      []Track     `json:"songs"`
		Privileges []Privilege `json:"privileges"`
	}
	if err := eapiPost("/api/v3/song/detail", map[string]any{"c": string(b)}, &res); err != nil {
		return nil, err
	}
	if res.Code != 200 {
		return nil, fmt.Errorf("netease: song detail: code %d", res.Code)
	}
	withPrivileges(res.Songs, res.Privileges)
	return res.Songs, nil
}

// ArtistProfile is who an artist is, as their page shows them, with the
// songs they are best known for.
type ArtistProfile struct {
	Artist
	Avatar    string   `json:"img1v1Url"` // square
	PicURL    string   `json:"picUrl"`    // wide
	Alias     []string `json:"alias"`
	Trans     string   `json:"trans"`
	BriefDesc string   `json:"briefDesc"`
	MusicSize int      `json:"musicSize"`
	AlbumSize int      `json:"albumSize"`
	Followed  bool     `json:"followed"` // by the listener signed in
	// AccountID is the artist's own account, which posts their events, or 0.
	AccountID int64   `json:"accountId"`
	Songs     []Track `json:"-"`
}

// ArtistDetail returns an artist with their 50 most popular songs.
func ArtistDetail(id int64) (*ArtistProfile, error) {
	var res struct {
		Artist   ArtistProfile `json:"artist"`
		HotSongs []Track       `json:"hotSongs"`
	}
	if err := get("/api/v1/artist/"+strconv.FormatInt(id, 10), nil, &res); err != nil {
		return nil, err
	}
	ar := &res.Artist
	ar.Songs = res.HotSongs
	detailPrivileges(ar.Songs)
	return ar, nil
}

// ArtistLatestSongs returns up to limit of an artist's songs, the newest
// first.
func ArtistLatestSongs(id int64, limit int) ([]Track, error) {
	var res struct {
		Songs []struct {
			ID int64 `json:"id"`
		} `json:"songs"`
	}
	form := url.Values{"id": {strconv.FormatInt(id, 10)}, "order": {"time"}, "limit": {strconv.Itoa(limit)}, "offset": {"0"}}
	if err := post("/api/v1/artist/songs", form, &res); err != nil {
		return nil, err
	}
	// Some artists' songs come in the old shape (artists, album, duration)
	// that Track doesn't read: their details have them in the new one, as
	// the listener's privileges.
	ids := make([]int64, len(res.Songs))
	for i, s := range res.Songs {
		ids[i] = s.ID
	}
	return SongDetails(ids)
}

var ErrUnavailable = errors.New("this song isn't available to stream")

// ErrNoCopyright is ErrUnavailable for a song NetEase has no rights to,
// which no account plays.
var ErrNoCopyright = fmt.Errorf("%w: no copyright", ErrUnavailable)

// StreamError means the address request failed, not that the song cannot play.
type StreamError struct {
	Code  int
	Cause error
}

func (e *StreamError) Error() string {
	if e.Cause != nil {
		return "获取播放地址失败：" + e.Cause.Error()
	}
	if e.Code == 0 || e.Code == 200 {
		return "暂未获取到播放地址"
	}
	return fmt.Sprintf("获取播放地址失败（%d）", e.Code)
}

func (e *StreamError) Unwrap() error { return e.Cause }

// Stream is where a track streams from, and what it is.
type Stream struct {
	URL  string `json:"-"`
	Size int64  `json:"size"` // in bytes
	// Type is the format: mp3, or flac for the lossless levels.
	Type string `json:"type"`
	// Level is the level served, which may be below the one asked for.
	Level string `json:"level"`
	// Gain is how many dB to change the track by to sound as loud as
	// others, and Peak its loudest sample, 1 at full scale, or 0 if
	// unknown.
	Gain float64 `json:"gain"`
	Peak float64 `json:"peak"`
}

// Levels are the stream levels, from the lowest.
var Levels = []string{"standard", "higher", "exhigh", "lossless", "hires"}

// Lossless reports whether level streams FLAC.
func Lossless(level string) bool { return level == "lossless" || level == "hires" }

// SongURL returns where to stream a track, MP3 or FLAC, at level (one of
// Levels) or the best below it the listener may play. It goes through
// the eapi, which carries the session, so VIP songs play for subscribers.
func SongURL(id int64, level string) (*Stream, error) {
	return SongURLContext(context.Background(), id, level)
}

// SongURLContext stops asking for a stream when the listener changes tracks.
func SongURLContext(ctx context.Context, id int64, level string) (*Stream, error) {
	st, err := eapiSongURL(ctx, id, level)
	if err == nil {
		return st, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	// The desktop eapi sometimes refuses a perfectly playable song with
	// -460, while the web player still serves it. Fall back there when the
	// answer is a service response, not a cancelled request or a preview.
	var service *StreamError
	if !errors.As(err, &service) || service.Cause != nil {
		return nil, err
	}
	if st, webErr := webSongURL(ctx, id, level); webErr == nil {
		return st, nil
	} else if errors.Is(webErr, ErrUnavailable) {
		return nil, ErrUnavailable
	}
	return nil, err
}

type songURLData struct {
	ID            int64           `json:"id"`
	Code          int             `json:"code"`
	URL           string          `json:"url"`
	Size          int64           `json:"size"`
	Type          string          `json:"type"`
	Level         string          `json:"level"`
	Gain          float64         `json:"gain"`
	Peak          *float64        `json:"peak"`
	FreeTrialInfo json.RawMessage `json:"freeTrialInfo"`
}

func eapiSongURL(ctx context.Context, id int64, level string) (*Stream, error) {
	var res struct {
		Code int           `json:"code"`
		Data []songURLData `json:"data"`
	}
	encode := "mp3"
	if Lossless(level) {
		encode = "flac"
	}
	data := map[string]any{"ids": fmt.Sprintf("[%d]", id), "level": level, "encodeType": encode}
	if err := eapiPostContext(ctx, "/api/song/enhance/player/url/v1", data, &res); err != nil {
		return nil, &StreamError{Cause: err}
	}
	if res.Code != 0 && res.Code != 200 {
		return nil, &StreamError{Code: res.Code}
	}
	if len(res.Data) == 0 {
		return nil, &StreamError{Code: res.Code}
	}
	return streamFromSongURLData(res.Code, res.Data[0], level)
}

// webSongURL asks the web player for the same stream, which works when the
// desktop eapi refuses one.
func webSongURL(ctx context.Context, id int64, level string) (*Stream, error) {
	encode := "mp3"
	if Lossless(level) {
		encode = "flac"
	}
	q := url.Values{
		"ids":        {fmt.Sprintf("[%d]", id)},
		"level":      {level},
		"encodeType": {encode},
	}
	var res struct {
		Code int           `json:"code"`
		Data []songURLData `json:"data"`
	}
	if err := getContext(ctx, "/api/song/enhance/player/url/v1", q, &res); err != nil {
		return nil, err
	}
	if res.Code != 0 && res.Code != 200 {
		return nil, &StreamError{Code: res.Code}
	}
	if len(res.Data) == 0 {
		return nil, &StreamError{Code: res.Code}
	}
	return streamFromSongURLData(res.Code, res.Data[0], level)
}

func streamFromSongURLData(code int, d songURLData, level string) (*Stream, error) {
	// A preview of a VIP song is no song.
	if len(d.FreeTrialInfo) > 0 && string(d.FreeTrialInfo) != "null" {
		return nil, ErrUnavailable
	}
	if strings.TrimSpace(d.URL) == "" {
		// The song's own code says why it has no address: 404, NetEase
		// holds no rights to it; -110, the account may not play it. A
		// retry gets the same answer.
		switch d.Code {
		case 404:
			return nil, ErrNoCopyright
		case -110:
			return nil, ErrUnavailable
		}
		return nil, &StreamError{Code: code}
	}
	s := Stream{Size: d.Size, Type: strings.ToLower(strings.TrimSpace(d.Type)), Level: d.Level, Gain: d.Gain}
	if s.Type == "" {
		s.Type = "mp3"
		if Lossless(level) {
			s.Type = "flac"
		}
	}
	if s.Type != "" && s.Type != "mp3" && s.Type != "flac" {
		return nil, fmt.Errorf("unsupported format %s", s.Type)
	}
	if d.Peak != nil {
		s.Peak = *d.Peak
	}
	// The level names the song's file in the cache: one not of Levels is
	// taken for the one asked for.
	if !slices.Contains(Levels, s.Level) {
		s.Level = level
	}
	s.URL = strings.Replace(d.URL, "http://", "https://", 1)
	return &s, nil
}

// Lyrics returns a track's timed lyrics, their translation if any, and
// its word-timed lyrics (YRC) where NetEase has them, which only the
// desktop client's API serves. The web API answers some songs with no
// lyrics that the desktop client's has: their lines come from it then.
func Lyrics(id int64) (lrc, translation, yrc string, err error) {
	var res struct {
		Lrc struct {
			Lyric string `json:"lyric"`
		} `json:"lrc"`
		Tlyric struct {
			Lyric string `json:"lyric"`
		} `json:"tlyric"`
	}
	q := url.Values{"id": {strconv.FormatInt(id, 10)}, "lv": {"1"}, "tv": {"-1"}}
	if err = get("/api/song/lyric", q, &res); err != nil {
		return "", "", "", err
	}
	var words struct {
		Lrc struct {
			Lyric string `json:"lyric"`
		} `json:"lrc"`
		Tlyric struct {
			Lyric string `json:"lyric"`
		} `json:"tlyric"`
		Yrc struct {
			Lyric string `json:"lyric"`
		} `json:"yrc"`
	}
	data := map[string]any{"id": strconv.FormatInt(id, 10), "cp": false, "tv": 0, "lv": 0, "rv": 0, "kv": 0, "yv": 0, "ytv": 0, "yrv": 0}
	if eapiPost("/api/song/lyric/v1", data, &words) == nil {
		yrc = words.Yrc.Lyric
	}
	lrc, translation = res.Lrc.Lyric, res.Tlyric.Lyric
	if lrc == "" {
		lrc = words.Lrc.Lyric
	}
	if translation == "" {
		translation = words.Tlyric.Lyric
	}
	return lrc, translation, yrc, nil
}

// Sized asks the image CDN for a picture of n×n pixels. A local cover is
// already the size it is, and stays where it is.
func Sized(pic string, n int) string {
	if pic == "" {
		return ""
	}
	if !strings.HasPrefix(pic, "http://") && !strings.HasPrefix(pic, "https://") {
		return pic
	}
	pic = strings.Replace(pic, "http://", "https://", 1)
	return fmt.Sprintf("%s?param=%dy%d", pic, n, n)
}

// Fetch returns what is at raw, a cover the image CDN serves or a local
// file.
func Fetch(raw string) ([]byte, error) {
	if path, ok := localPath(raw); ok {
		return os.ReadFile(path)
	}
	req, err := http.NewRequest("GET", raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, errors.New(res.Status)
	}
	return io.ReadAll(res.Body)
}

// localPath returns the file raw names, if it names one rather than a
// picture on the image CDN.
func localPath(raw string) (string, bool) {
	if strings.HasPrefix(raw, "file://") {
		u, err := url.Parse(raw)
		if err != nil {
			return "", false
		}
		path := u.Path
		if runtime.GOOS == "windows" {
			path = strings.TrimPrefix(path, "/")
		}
		return filepath.FromSlash(path), true
	}
	if strings.Contains(raw, "://") {
		return "", false
	}
	return raw, filepath.IsAbs(raw)
}
