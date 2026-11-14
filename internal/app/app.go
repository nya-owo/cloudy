package app

import (
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

type pageKind int

const (
	pageHome pageKind = iota
	pageDaily
	pageCharts
	pagePlaylist
	pageSearch
	pageFM
	pageArtist
	pageAlbum
	pageUser
	pagePodcast
	pagePodcasts
	pageDownloads
	pageRecent
	pageCloud
	pageStyle // the day's songs in the styles picked
	pageLocal // the listener's own music, in the folders they chose
)

type route struct {
	page pageKind
	// id is the playlist, artist, album, user or podcast the page shows.
	id int64
	// query and kind are what a search page searched for.
	query string
	kind  netease.SearchType
}

// app is the state both windows show. Everything in it is read and changed
// on the main thread: by the views, and by goroutines through post.
type app struct {
	win   *mygo.Window
	notch *notch
	tray  *mygo.Tray // the icon in the menu bar or notification area, while it shows
	art   *artCache
	pl    *player.Player
	pal   palette

	trayPopup trayPopupWindow // Windows' independent, short-lived tray menu
	// trayPopupGlass is whether it is glass, over trayBackdrop, the desktop
	// under it; trayPopupDone stops watching for a press outside it, and
	// trayPopupClosed is when a press outside last closed it.
	trayPopupGlass  bool
	trayBackdrop    *ui.Bitmap
	trayPopupDone   chan struct{}
	trayPopupClosed time.Time

	// thumbsAt is what the taskbar thumbnail shows (thumbbar.go), and
	// thumbDark whether the taskbar is dark.
	thumbsAt  *thumbState
	thumbDark bool

	route   route
	history []route
	forward []route // the pages gone back from, the last first to go to

	profile      *netease.Profile
	accountGen   int                // rejects results from an earlier login, including the same account
	mine, saved  []netease.Playlist // the listener's playlists: made, and saved
	savingPl     map[int64]bool     // the playlists whose saving or its end is on its way
	recPlaylists []netease.Playlist // the day's playlists picked for the listener
	myAlbums     []netease.AlbumProfile
	myAlbumsErr  string
	myAlbumsBusy bool
	login        loginState
	accountOpen  bool           // the dropdown of the account row shows
	naming       nameDialog     // the name of a playlist asked for
	deleting     deleteDialog   // whether to delete a playlist, asked
	signingOut   bool           // whether to sign out, asked
	liked        map[int64]bool // the songs the listener likes
	likeEdits    map[int64]*likeEdit
	likeNext     time.Time // earliest next individual song request
	likeRevision int       // local edits newer than a personal-data request
	likeBatches  int       // bulk requests still being sent
	librarySync  librarySync
	playlistAt   map[int64]time.Time
	redCounts    map[int64]*redCountState
	toast        string   // a message to show once
	toastAs      string   // what the toast says the state of, to take the place of the last
	picked       pickedUp // the song a login picked up, waiting to play

	// The episodes the listener likes, and doesn't, as far as NetEase has
	// answered, and those asked about: see ui_like.go.
	likedPrograms, askedPrograms map[int64]bool

	daily             []netease.Track
	dailySpin         spinState // the record on the daily card
	dailyBars         barState  // the bars on its play button
	pickBars          barState  // the bars by the song playing on it, or on the style card
	featured          []netease.Playlist
	playlistFilter    int
	playlistPill      ui.Rect
	filteredPlaylists [len(playlistFilters)]*playlistBrowseData
	charts            []netease.Playlist
	homeErr           string
	homeLoading       bool
	homeAt            time.Time // when what the home page shows was asked for

	playlists   map[int64]*netease.Playlist
	loadingPl   map[int64]bool
	plErr       map[int64]string
	artists     map[int64]*artistData
	artistTab   int // what an artist's page lists: artistHot, artistLatest, artistAlbums or artistEvents
	albums      map[int64]*albumData
	users       map[int64]*userData
	podcasts    map[int64]*podcastData
	podcastTab  *podcastTab // the podcasts tab's lists
	trackList   ui.ListState
	trackLayout trackRowLayout
	trackRow    []int // the row of trackList each song shown is at
	groupRow    []int // the row of trackList each album's title is at, sorted by album
	batch       batchSelection
	// Where the page and the sidebar are scrolled, kept while Now Playing
	// covers them, unbuilt.
	pageScroll ui.ScrollState
	navScroll  ui.ScrollState
	scrolled   route // the page trackList and pageScroll are of

	mineCollapsed, savedCollapsed bool
	albumsCollapsed               bool
	mineView                      mineView // the search on the listener's page, and the kinds opened on it

	query       string
	searchBox   ui.Handle
	filterBox   ui.Handle             // the search among a playlist's songs
	searches    map[route]*searchData // the results of each search, by its page
	searched    route                 // the search last shown, which the sidebar goes back to
	suggestions suggestState          // what the search field's query may be after

	nowPlaying   bool
	npFull       bool      // Now Playing taking the whole screen
	fullHintAt   time.Time // when the bubble telling of F11 first showed in this run
	sysAccent    string    // the desktop's accent as #RRGGBB, "" where it has none
	npOpen       bool      // Now Playing as it last showed: open or not
	npChangedAt  time.Time // when it last opened or closed
	npPane       int       // paneLyrics, paneQueue, paneStage, paneComments, paneProgram or paneCaptions
	npPaneTop    float32   // how far the cover beside the pane starts below its top
	npRest       float32   // how tall what goes under the cover is, as last laid out
	lyricScroll  ui.ScrollState
	lyricGlide   lyricGlide
	lyricWheel   float32   // the wheel over a lyric value, short of a step
	lyricWheelAt time.Time // when it last turned, its value not kept yet
	lyricAt      int
	lyricTrack   int64
	queueList    ui.ListState
	queueAt      int             // the row of the queue pane last scrolled to, -1 while it is hidden
	more         moreState       // what 1.2.13's additions keep (more_state.go)
	loginKnown   *bool           // whether the app starts at login, once asked
	sideDragging bool            // the sidebar's edge is being dragged
	shownTracks  []netease.Track // the songs the page lists now, for choosing among them
	recent       recentState     // the songs played on this computer
	cloud        cloudState      // the listener's music cloud drive
	up           uploadState     // the songs going up to the cloud drive
	outputList   outputList      // the audio outputs, as last listed
	comments     commentState
	tabCount     rollState // the count on the comments' tab, as it rolls

	fmMode        int // the FM mode chosen, in fmModes
	fmCoversState fmCoverStack
	radioStarting string // the radio asked for its first songs, or ""
	radioGen      int    // distinguishes requests for the same radio

	volume      float64
	volumeOpen  bool        // the volume popover of the player bar shows
	volumeHover hoverOpen   // how the pointer opened it, and when it last was over it
	effects     effectsView // the pane of the sound effects
	auto        autoState   // the effects chosen for the song playing
	mutedFrom   float64

	rowBars barState
	// underTop and underBottom say whether the page has content under its
	// bars, which show their scroll edges then.
	underTop, underBottom bool
	barScrub              scrubState
	npScrub               scrubState
	glowColor             ui.Color         // the color of Now Playing's glow, from the cover's
	menu                  paneMenu         // the menu open, if one is (ui_pane.go)
	npPill                ui.Rect          // where the tab chosen is, in the row of tabs
	barBox                ui.Rect          // where the player bar is, which Now Playing grows from
	barSpot, npSpot       flightSpot       // what stays as it grows, in the bar and in the view
	npK                   float32          // how far Now Playing has opened, from 0 to 1
	npCoverScale          float32          // how its cover stands, which the flight lands at
	motion                int              // how much the app moves, in this frame
	playFlies             func(ui.Element) // what hides the play button being built as it flies
	pageDir               int              // the way the page last changed: 1 in, -1 back, 0 across
	feedback              feedbackState    // the message being written, in the settings
	roam                  *roamer          // what deals the songs of styles
	deskWin, miniWin      *mygo.Window     // the windows that float, once opened
	splashed              bool             // the record has come in, as the app started
	splashAt              time.Time        // when it started to
	miniScrub             scrubState       // the progress of the mini window
	morph                 floatMorph       // the player on its way to or from it
	deskBtn               ui.Rect          // the player bar's button of the desktop lyrics, while it shows
	deskFly               deskFlight       // the desktop lyrics on their way out of it or back
	sleep                 sleepState       // the timer that pauses the music
	keys                  shortcutState    // the global keys and the menu keys
	spin                  discState        // how far the bar's cover has turned
	npPointer             bool             // the pointer is over Now Playing
	headAt                ui.Rect          // where the cover of the page's header is
	cameFrom              cardOrigin       // the card this page was opened from
	restoreY              float32          // how far to scroll the page coming back
	fontsAsked            bool             // the fonts that come as files were asked for
	plView                plView           // how the playlist open is searched and sorted
	headGone              float32          // how far the page's header has scrolled away
	rowsRoute             route            // the page whose list last came in
	rowsAt                time.Time        // when it started to
	sideColor             ui.Color         // what the sidebar shows, as it eases
	flight                coverFlight      // a cover on its way to the page it opens
	heldRoute             route            // the page opened from a card
	heldURL               string           // and the cover the card gave it
	playSpot              ui.Rect          // where the play button last built is
	backdropCache         backdropCache    // the backdrop of Now Playing, as last painted
	pageGlass             pageGlass        // the cover the cover backgrounds show, as it changes
	windowMaterial        mygo.Vibrancy
	npArt                 *artwork     // the cover whose colors Now Playing shows
	npCoverState          coverState   // the cover of Now Playing, as it is tilted and flicked
	wallState             wallState    // the wall of the queue's covers, as it is dragged
	quiet                 quietState   // the quiet parts of the song playing, for its hot comments
	captions              captionState // the episode playing's captions, and the model that writes them
	sig                   playerSig
	quitting              bool

	settings     settings
	settingsView settingsView    // the settings panel
	shownLines   shownCache      // the lyrics shown, without credits if hidden
	update       updateState     // the update pane, and the update it found
	whatsNew     whatsNewState   // what the version updated to brought, said once
	cacheUsed    int64           // the bytes the song cache takes, -1 until measured
	updates      bool            // the update plugin is used (useUpdater)
	fontList     []uiFont        // the fonts on offer here (fontChoices)
	fontsLoaded  map[string]bool // the fonts of files registered, by id (loadFont)

	// The songs saved to disk, the ones on their way there, and where both
	// are kept.
	dl     downloadState
	lib    localLibrary
	opened *openedFiles // the songs opened with Cloudy, coming in

	restored     bool   // playback picked up where it was left
	savedSession string // the session as last kept
	sessionDue   bool   // a save of the session is due

	style      styleState    // the day's songs in the styles picked
	radar      radarState    // NetEase's radars on the home page
	progressAt progressShown // what the taskbar button last showed of the song
}

// post runs fn on the main thread and redraws both windows. Without a
// window, as in tests, it drops fn.
func (a *app) post(fn func()) {
	if a.win == nil {
		return
	}
	a.win.Update(func() {
		fn()
		a.syncThumbs()
		a.redraw()
	})
}

func (a *app) redraw() {
	if a.trayPopup != nil {
		a.trayPopup.Invalidate()
	}
	if a.win != nil {
		a.win.Invalidate()
	}
	if a.notch != nil && a.notch.win != nil {
		a.notch.win.Invalidate()
	}
	if a.deskWin != nil {
		a.deskWin.Invalidate()
	}
	if a.miniWin != nil {
		a.miniWin.Invalidate()
	}
}

// shotHook, when set, drives the app to check its design.
var shotHook func(a *app)

func (a *app) showMain() {
	if a.win != nil {
		if a.win.IsMinimized() {
			a.win.Restore()
		}
		a.win.Show()
		a.win.Focus()
		// From the menu bar alone the app may be inactive.
		mygo.App.Focus()
	}
}

// Run runs the app until it quits.
func Run() error {
	a := &app{
		playlists:  map[int64]*netease.Playlist{},
		loadingPl:  map[int64]bool{},
		plErr:      map[int64]string{},
		artists:    map[int64]*artistData{},
		albums:     map[int64]*albumData{},
		users:      map[int64]*userData{},
		podcasts:   map[int64]*podcastData{},
		podcastTab: &podcastTab{},
		searches:   map[route]*searchData{},
		lyricAt:    -1,
		queueAt:    -1,
		cacheUsed:  -1,
	}
	// One Cloudy at a time: launched again, it shows the window of the one
	// running, from the tray too, and goes. An update starts the new
	// version after the old one let go of the lock.
	if !mygo.App.RequestSingleInstanceLock() {
		return nil
	}
	mygo.App.OnSecondInstance(func([]string, string) { a.showMain() })
	a.listenForOpenedFiles()
	moveOldData()
	copyFormerData()
	mygo.App.SetLanguages(appLanguage)
	a.useUpdater()
	mygo.App.WhenReady(func() {
		a.settings = loadSettings()
		a.applyOutput()
		a.settings.applyAppearance()
		a.loadFont()
		a.npPane = a.lyricsPaneChosen()
		// Before the window: on Windows it lays out its content as it is
		// made, and the view asks the player what plays.
		a.art = newArtCache(a.post)
		a.pl = player.New(a.post)
		a.pl.PodcastSpeed = a.settings.PodcastSpeed
		a.pl.SetEffects(a.currentEffects(), false)
		a.pl.Quality = func() string { return a.settings.Quality }
		a.pl.Normalize = func() bool { return a.settings.Normalize }
		a.pl.Crossfade = a.crossfade
		a.pl.Cache = newSongCache(a.settings.CacheFolder, a.settings.CacheMB)
		a.pl.LocalFile = a.downloadedFile
		a.noteDownloadsAtStart()
		a.pl.Listened = func(t netease.Track, played time.Duration, ended bool) {
			a.listened(t, played, ended)
			a.remember(t, played, ended)
		}
		a.pl.SongStopped = func() {
			a.sleep.minutes = 0
			a.toast = "定时结束，本首播完后已暂停"
		}
		a.pl.Changed = func() {
			a.keepSession()
			a.autoFollow()
			a.syncCaptions()
			a.syncThumbs()
		}
		a.volume = a.settings.Volume
		a.pl.SetVolume(a.volume)
		a.windowMaterial = a.settings.mainWindowMaterial()
		a.win = mygo.NewWindow(mygo.WindowOptions{
			Title:         mygo.App.Name(),
			Width:         1180,
			Height:        780,
			MinWidth:      880,
			MinHeight:     580,
			StateKey:      "main",
			TitleBarStyle: mygo.TitleBarHidden,
			// As tall as the edge over a page (Linux, Windows).
			TitleBarHeight: buttonsHeight,
			// Inset from the corner, within the sidebar, as macOS 26's
			// apps with a sidebar place them.
			TrafficLightPosition: &mygo.Point{X: 20, Y: 20},
			Vibrancy:             a.windowMaterial,
			BackgroundColor:      "light-dark(#ffffff, #141416)",
			Content:              ui.View(a.view),
			// Started at login, as the listener asked: to the tray.
			Hidden: a.startsHidden(),
		})
		a.setMenu()
		a.boot()
		a.applyGlobalKeys()
		// The notch player is macOS's, over its menu bar: elsewhere it would
		// hang at the top of the screen as a black box.
		if runtime.GOOS == "darwin" {
			a.notch = newNotch(a)
		}
		// From the menu bar alone, the Dock icon shows while the window
		// does.
		a.syncDock()
		a.win.OnShow(a.syncDock)
		a.win.OnHide(a.syncDock)
		// Shown or switched to the morning after, it shows the new day's.
		a.win.OnFocus(a.freshenPersonal)
		a.listenForDrops()
		a.loadLibraryLater()
		a.applyTray()
		a.applyDeskLyrics()
		// The thumbnail's buttons, and their glyphs as the taskbar turns
		// light or dark.
		a.readTaskbarDark()
		a.readSysAccent()
		mygo.Theme.OnUpdated(func() {
			a.post(func() {
				a.readTaskbarDark()
				a.readSysAccent()
			})
		})
		// A version that came out since the app last ran shows soon after
		// it opens, and one a day after that, if the listener asks for it.
		a.noteVersion(mygo.App.Version())
		go a.autoCheckUpdates()
		if shotHook != nil {
			shotHook(a)
		}
		// Closing the window hides it: music plays on, and the Dock icon
		// or the tray's brings it back. A Linux desktop without a tray
		// has neither, so there closing it quits, as it does wherever the
		// listener asked for that (settings.CloseQuits).
		a.win.OnClose(func(e *mygo.CloseEvent) {
			if a.quitting {
				return
			}
			e.PreventDefault()
			if !a.settings.CloseQuits && (runtime.GOOS == "darwin" || a.tray != nil) {
				a.hideMain()
			} else {
				mygo.App.Quit()
			}
		})
	})
	a.followProgress()
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		a.quitting = true
		a.settings.Volume = a.volume
		a.settings.save()
		a.saveSession()
		if write := a.transcriptWrite(); write != nil {
			write()
		}
	})
	// Windows leaves an icon in the notification area after its app until
	// the pointer passes over it.
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		a.closeTrayPopup()
		if a.tray != nil {
			a.tray.Destroy()
		}
	})
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnActivate(func(bool) { a.showMain() })
	return mygo.App.Run()
}
