package app

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"

	"netease-music-mygo/internal/player"
)

// settings are the listener's choices, kept in settings.json in the user
// data directory.
type settings struct {
	// Notch shows the quick player around the notch: the listener turns
	// it on.
	Notch bool `json:"notch"`
	// NotchActivity shows the cover and the music's levels beside the
	// notch while music plays, which hides the menu bar there.
	NotchActivity bool `json:"notchActivity"`
	// NotchVirtual keeps a notch of the app's own showing at the top of a
	// display without one; else it shows only while the pointer asks.
	NotchVirtual bool `json:"notchVirtual"`
	// NotchGreeted says the notch showed itself, once, as the first song
	// played (greet).
	NotchGreeted bool `json:"notchGreeted"`
	// Quality is the level of the streams: standard, higher, exhigh,
	// lossless or hires.
	Quality string `json:"quality"`
	// Appearance is light, dark, or system to follow the system.
	Appearance string `json:"appearance"`
	// Font is the font of the app's text (uiFonts): empty for the default.
	Font string `json:"font,omitempty"`
	// LiquidGlass floats the player bar as a capsule of glass. Off is kept
	// too, without omitempty: left out, it would load as the default.
	LiquidGlass  bool `json:"liquidGlass"`
	GlobalGlass  bool `json:"globalGlass,omitempty"`
	SidebarGlass bool `json:"sidebarGlass"`
	MainGlass    bool `json:"mainGlass"`
	// NpLight gives the line sung, the progress bar and the volume a faint
	// glow in Now Playing (ui_light.go). As for LiquidGlass, off is kept too.
	NpLight bool `json:"npLight"`
	// GlowLevel is how strong that glow is, from 0 to 1.
	GlowLevel float64 `json:"glowLevel"`
	// GlassClear is how clear the glass of the player bar is, from 0 to 1,
	// and GlassBend how far it bends what shows through.
	GlassClear float64 `json:"glassClear"`
	GlassBend  float64 `json:"glassBend"`
	// GlassPanels makes menus, popovers, dialogs and the settings panes of
	// glass, else plain, which takes less of the GPU. Off is kept too.
	GlassPanels bool `json:"glassPanels"`
	// Motion is how much the app moves: full, lite or off.
	Motion string `json:"motion,omitempty"`
	// AccentCover takes the accent from the cover playing: else the app's red.
	AccentCover bool `json:"accentCover"`
	// Accent is the color the listener gave the app, #RRGGBB, or empty for
	// the app's red: the light and the dark take their own shade of it, and
	// the window and the sidebar a breath of it.
	Accent string `json:"accent,omitempty"`
	// UpdateChecks checks for new versions soon after launch and then once
	// a day; UpdateInstall installs them without asking. UpdateChecked is
	// when the app last asked for one, in Unix seconds.
	UpdateChecks  bool  `json:"updateChecks"`
	UpdateInstall bool  `json:"updateInstall"`
	UpdateChecked int64 `json:"updateChecked,omitempty"`
	// AlbumSort is how the albums the listener saved are listed: by time,
	// name or artist; empty for the time.
	AlbumSort string `json:"albumSort,omitempty"`
	// BarSpin turns the cover in the bar as a record while music plays. Off
	// is kept too.
	BarSpin bool `json:"barSpin"`
	// GlobalKeys has the shortcuts work from any app.
	GlobalKeys bool `json:"globalKeys"`
	// Shortcuts are the listener's own global shortcuts, by action id; an
	// action absent here uses its defaults.
	Shortcuts map[string]string `json:"shortcuts,omitempty"`
	// LyricScale is the size of lyrics, of their size as designed: 0 for 1.
	LyricScale float64 `json:"lyricScale,omitempty"`
	// LyricPerspective displays scrolling lyrics on an arc in Now Playing.
	LyricPerspective bool `json:"lyricPerspective,omitempty"`
	// Zero is meaningful for both sliders, so keep it when saving.
	LyricTilt   float64 `json:"lyricTilt"`
	LyricBounce float64 `json:"lyricBounce"`
	// LyricShift is how many milliseconds a song's lyrics run early, by its
	// ID, where they don't keep time with it.
	LyricShift map[int64]int `json:"lyricShift,omitempty"`
	// DeskLyrics shows the lyrics in a window of their own, over the others,
	// at DeskX and DeskY, or at the foot of the screen, DeskScale of their
	// size as designed. DeskLock lets the pointer through them.
	DeskLyrics bool    `json:"deskLyrics"`
	DeskLock   bool    `json:"deskLock"`
	DeskStyle  string  `json:"deskStyle,omitempty"`
	DeskScale  float64 `json:"deskScale,omitempty"`
	DeskX      int     `json:"deskX,omitempty"`
	DeskY      int     `json:"deskY,omitempty"`
	// DeskWords lights the line sung word by word, as it is sung; off, the
	// whole line is lit at once.
	DeskWords bool `json:"deskWords"`
	// NpWholeLines lights the line sung in Now Playing whole, at once,
	// rather than word by word as it is sung.
	NpWholeLines bool `json:"npWholeLines,omitempty"`
	// HideCredits leaves the lines naming who made a song, as 作词 and
	// 作曲, out of the lyrics shown.
	HideCredits bool `json:"hideCredits,omitempty"`
	// UIZoom is how much larger than as designed the window shows
	// everything: 0 for 1.
	UIZoom float64 `json:"uiZoom,omitempty"`
	// TaskbarProgress shows how far the song has played on the window's
	// taskbar button.
	TaskbarProgress bool `json:"taskbarProgress,omitempty"`
	// MiniX and MiniY are where the mini window was left.
	MiniX int `json:"miniX,omitempty"`
	MiniY int `json:"miniY,omitempty"`
	// Smoke makes the progress bars comets trailing smoke as music plays,
	// for a few percent of a core: the listener turns it on.
	Smoke bool `json:"smoke"`
	// LyricsStage shows lyrics on the stage, Folia's style, in Now
	// Playing, rather than scrolling.
	LyricsStage bool `json:"lyricsStage"`
	// StageStyle is how the stage sets its lyrics (stageStyles): empty for
	// Folia's classic, or the id of another of its visualizers.
	StageStyle string `json:"stageStyle,omitempty"`
	// StageSparks throws sparks off each word on the stage as it is sung,
	// whatever its style.
	StageSparks bool `json:"stageSparks,omitempty"`
	// Volume is where the volume was left, from 0 to 1.
	Volume float64 `json:"volume"`
	// Normalize evens out the loudness of tracks, by what NetEase
	// measured of each.
	Normalize bool `json:"normalize"`
	// Crossfade fades each song out as it ends into the next fading in,
	// over CrossfadeSec seconds (0 for 3).
	Crossfade    bool `json:"crossfade,omitempty"`
	CrossfadeSec int  `json:"crossfadeSec,omitempty"`
	// HotComments shows a song's hot comments in its quiet parts, in
	// place of the lyrics (quietComment): the listener turns it on.
	HotComments bool `json:"hotComments"`
	// HotAll shows the hot comments all through the song, in place of
	// the lyrics, as NetEase’s 心动乐评 does, not only in its quiet parts.
	HotAll bool `json:"hotAll,omitempty"`
	// Fog fills Now Playing's backdrop with fog of the cover's color,
	// rising and swelling with the bass: an experiment the listener turns
	// on.
	Fog bool `json:"fog"`
	// AppIcon is where the app's icon shows on macOS (appIcons); elsewhere
	// it is always in the notification area.
	AppIcon string `json:"appIcon"`
	// Captions writes down episodes as they play, for their captions,
	// once the listener has downloaded the model.
	Captions bool `json:"captions"`
	// CacheMB is how many megabytes the songs kept on disk may take; 0
	// keeps none.
	CacheMB int `json:"cacheMB"`
	// DownloadFolder is where the songs they download are kept; empty for
	// the Cloudy Downloads folder among their music.
	DownloadFolder string `json:"downloadFolder,omitempty"`
	// SearchHistory keeps submitted keywords, most recent first.
	SearchHistory []string `json:"searchHistory,omitempty"`
	// LyricTrans shows the translation under each line of lyrics. Off is
	// kept too.
	LyricTrans bool `json:"lyricTrans"`
	// DownloadQuality is the level songs download at; empty for Quality.
	DownloadQuality string `json:"downloadQuality,omitempty"`
	// StartHidden opens the app to the tray, its window hidden, when it
	// starts as the listener logs in.
	StartHidden bool `json:"startHidden,omitempty"`
	// CloseQuits quits the app as its window is closed, rather than hiding
	// it to the tray with the music playing on.
	CloseQuits bool `json:"closeQuits,omitempty"`
	// PodcastSpeed is how fast episodes play, their pitch kept: 0 for 1.
	PodcastSpeed float64 `json:"podcastSpeed,omitempty"`
	// OutputDevice is the endpoint ID of the output music plays through;
	// empty for the system's default, which it follows.
	OutputDevice string `json:"outputDevice,omitempty"`
	// SidebarWidth is how wide the listener dragged the sidebar, 0 for its
	// own width; SidebarSize how large its rows are: "", large or huge.
	SidebarWidth int    `json:"sidebarWidth,omitempty"`
	SidebarSize  string `json:"sidebarSize,omitempty"`
	// SidebarAll lists every playlist and album in the sidebar, rather than
	// the first few with a way to the listener's page for the rest.
	SidebarAll bool `json:"sidebarAll,omitempty"`
	// PlaylistSort is how the listener's page lists playlists: empty for
	// NetEase's own order, name or songs.
	PlaylistSort string `json:"playlistSort,omitempty"`
	// Scrobble tells NetEase of the songs played, for them to count in the
	// listener's listening: off unless they turn it on, as NetEase does not
	// document it.
	Scrobble bool `json:"scrobble,omitempty"`
	// Effects shape the sound, from the preset EffectsPreset names, or
	// "custom" once a band moved.
	Effects       player.Effects `json:"effects"`
	EffectsPreset string         `json:"effectsPreset,omitempty"`
	// PowerSaver has the effects that cost power turned off, SaverBefore
	// keeping how they were, to put back as it is turned off again.
	PowerSaver  bool          `json:"powerSaver,omitempty"`
	SaverBefore *savedEffects `json:"saverBefore,omitempty"`

	path string // where they were loaded from, and are saved to
}

func defaultSettings() settings {
	return settings{UpdateChecks: true, BarSpin: true, GlobalKeys: true, NotchActivity: true, Quality: "exhigh", Appearance: "system", LiquidGlass: true, SidebarGlass: true, MainGlass: true, NpLight: true, GlowLevel: 0.5, GlassClear: 0.6, GlassBend: 0.3, GlassPanels: true, Volume: 0.8, Normalize: true, AppIcon: "dock-and-menubar", CacheMB: 2048, Captions: true, LyricTrans: true, LyricTilt: 0.5, LyricBounce: 0.5, DeskWords: true}
}

func (s settings) playerGlass() bool  { return !s.PowerSaver && (s.GlobalGlass || s.LiquidGlass) }
func (s settings) panelGlass() bool   { return !s.PowerSaver && (s.GlobalGlass || s.GlassPanels) }
func (s settings) sidebarGlass() bool { return !s.PowerSaver && (s.GlobalGlass || s.SidebarGlass) }
func (s settings) mainGlass() bool    { return !s.PowerSaver && (s.GlobalGlass || s.MainGlass) }

// mainWindowMaterial keeps the sidebar's material (Mica on Windows 11)
// under the cover-colored background: the clear one (Acrylic) shows the
// desktop through, costs more, and darkens the light appearance over a
// dark desktop. Only global glass, which asks first, takes the clear one.
func (s settings) mainWindowMaterial() mygo.Vibrancy {
	if s.PowerSaver {
		return mygo.VibrancyNone
	}
	if s.GlobalGlass {
		return mygo.VibrancyGlassClear
	}
	return mygo.VibrancySidebar
}

func settingsPath() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

// oldName is what the app was called before it took its bundle's name, and
// kept its data under.
const oldName = "网易云音乐"

// lastName is what the app was called before Cloudy. The identifier stayed
// as it was then, so the login, kept under it, goes on by itself.
const lastName = "Cloud-nya"

// moveOldData moves the user data and the song cache kept under an old name
// to the directories of the app's name, unless those hold something already:
// the login, settings and where playback was left go on. It must run before
// anything reads them.
func moveOldData() {
	for _, name := range []mygo.PathName{mygo.PathUserData, mygo.PathCache} {
		if dir, err := mygo.App.Path(name); err == nil {
			moveFromOldNames(dir)
		}
	}
}

// moveFromOldNames moves to dir what is kept beside it under the latest of
// the app's old names that has any: once one has moved, dir holds something
// and the older ones stay where they are.
func moveFromOldNames(dir string) {
	for _, old := range []string{lastName, oldName} {
		moveDir(filepath.Join(filepath.Dir(dir), old), dir)
	}
}

// formerName is what this fork's app was called before it had a name of its
// own, and kept its data under.
const formerName = "MyGo 云音乐"

// copyFormerData starts a new installation from the settings and the place
// in the queue of the app before the fork, which stays as it was, for it can
// still be opened. The login is the system's, under the app's identity: it
// asks for the QR code again.
func copyFormerData() {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) > 0 {
		return
	}
	for _, name := range []string{"settings.json", "session.json"} {
		if b, err := os.ReadFile(filepath.Join(filepath.Dir(dir), formerName, name)); err == nil {
			os.WriteFile(filepath.Join(dir, name), b, 0o600)
		}
	}
}

// moveDir moves the directory old to dir when dir is empty, as Path made
// it, and old exists.
func moveDir(old, dir string) {
	if old == dir {
		return
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) > 0 {
		return
	}
	if _, err := os.Stat(old); err != nil {
		return
	}
	if os.Remove(dir) == nil {
		if err := os.Rename(old, dir); err != nil {
			os.MkdirAll(dir, 0o700)
		}
	}
}

// loadSettings reads the settings, or returns the defaults.
func loadSettings() settings {
	s := defaultSettings()
	if path, err := settingsPath(); err == nil {
		s = loadSettingsFrom(path)
	}
	return s
}

// loadSettingsFrom reads the settings at path, or returns the defaults. A
// file that does not parse is moved aside to settings.json.bad first, so
// the next save, of the defaults, does not lose what it held for good.
func loadSettingsFrom(path string) settings {
	s := defaultSettings()
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &s); err != nil {
			log.Println("settings:", err)
			os.Rename(path, path+".bad")
			s = defaultSettings()
		}
	}
	s.path = path
	return s
}

// save keeps the settings where they were loaded from; defaults that were
// never loaded, as in tests, are not saved.
func (s settings) save() error {
	path := s.path
	if path == "" {
		return nil
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(path, b, 0o600); err != nil {
		log.Println("settings:", err)
		return err
	}
	return nil
}

// applyAppearance makes the windows light, dark, or follow the system.
func (s settings) applyAppearance() {
	switch s.Appearance {
	case "light":
		mygo.Theme.SetSource(mygo.ThemeLight)
	case "dark":
		mygo.Theme.SetSource(mygo.ThemeDark)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}
}

// qualityLevel is the stream level NetEase names a quality by.
var qualityLevels = []struct{ level, label string }{
	{"standard", "标准（128k）"},
	{"higher", "较高（192k）"},
	{"exhigh", "极高（320k）"},
	{"lossless", "无损（FLAC）"},
	{"hires", "Hi-Res"},
}

// cacheLimits are the sizes the song cache may take, in megabytes.
var cacheLimits = []struct {
	mb    int
	label string
}{
	{0, "关闭"},
	{1024, "1 GB"},
	{2048, "2 GB"},
	{5120, "5 GB"},
	{10240, "10 GB"},
}
