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
	pageWeek  // 本周听歌排行, the songs played most in the last seven days
	// pageHistoryDaily shows the past daily recommendations NetEase keeps.
	pageHistoryDaily
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
	savingCover bool // a cover fetch or save dialog is already in progress

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
	trayPopupEnter  bool
	trayPopupOrigin ui.Point
	trayBackdrop    *ui.Bitmap
	trayPopupDone   chan struct{}
	trayPopupClosed time.Time

	// thumbsAt is what the taskbar thumbnail shows (thumbbar.go),
	// thumbDark whether the taskbar is dark, and altTab whether Alt+Tab is
	// open (thumbswitch_windows.go).
	thumbsAt  *thumbState
	thumbDark bool
	altTab    bool

	route   route
	history []route
	forward []route // the pages gone back from, the last first to go to

	profile    *netease.Profile
	accountGen int // rejects results from an earlier login, including the same account
	// privAsked is when NetEase was last asked what may be played of a
	// song, and queueRechecked when the queue was (playable_recheck.go).
	privAsked map[int64]time.Time
	// privKnown is what NetEase last said may be played of songs, by ID,
	// for lists kept without it (the listening ledger).
	privKnown map[int64]netease.Privilege
	// vip is the memberships of who is signed in, the highest first
	// (vip_tags.go).
	vip []netease.Membership
	// vipKnown is whether vip is what NetEase said for this sign-in, rather
	// than not asked yet or not answered: only then are levels locked.
	vipKnown bool
	// openWithOn is whether Cloudy is among the apps that open songs, as
	// the settings last asked the system; nil, not asked yet.
	openWithOn     *bool
	queueRechecked time.Time
	mine, saved    []netease.Playlist // the listener's playlists: made, and saved
	savingPl       map[int64]bool     // the playlists whose saving or its end is on its way
	recPlaylists   []netease.Playlist // the day's playlists picked for the listener
	myAlbums       []netease.AlbumProfile
	myAlbumsErr    string
	myAlbumsBusy   bool
	login          loginState
	accountOpen    bool      // the dropdown of the account row shows
	accountMenuH   float32   // the account menu's full height, as last laid out
	accountShut    time.Time // when a press outside put the account menu away
	dm             privateMessagesState
	naming         nameDialog     // the name of a playlist asked for
	deleting       deleteDialog   // whether to delete a playlist, asked
	signingOut     bool           // whether to sign out, asked
	liked          map[int64]bool // the songs the listener likes
	likeEdits      map[int64]*likeEdit
	likeNext       time.Time // earliest next individual song request
	likeRevision   int       // local edits newer than a personal-data request
	likeBatches    int       // bulk requests still being sent
	librarySync    librarySync
	playlistAt     map[int64]time.Time
	redCounts      map[int64]*redCountState
	toast          string   // a message to show once
	toastAs        string   // what the toast says the state of, to take the place of the last
	picked         pickedUp // the song a login picked up, waiting to play

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
	// listScroll is where the list of songs is scrolled, which its floating
	// buttons glide (listFloats); listBox is where it shows, and listLocate
	// the row it glides to, to be set right where the glide fell short, or
	// -1.
	listScroll ui.ScrollState
	listBox    ui.Rect
	listLocate int
	listAim    int // the row a glide is to head for, once it knows where it starts, or 0
	listWait   int // frames until then
	navScroll  ui.ScrollState
	scrolled   route // the page trackList and pageScroll are of

	mineCollapsed, savedCollapsed bool
	albumsCollapsed               bool
	mineView                      mineView // the search on the listener's page, and the kinds opened on it

	query        string
	searchBox    ui.Handle
	filterBox    ui.Handle             // the search among a playlist's songs
	searches     map[route]*searchData // the results of each search, by its page
	pageSeen     map[route]time.Time   // when each page kept loaded last showed (trimPageCaches)
	searched     route                 // the search last shown, which the sidebar goes back to
	suggestions  suggestState          // what the search field's query may be after
	suggestShown suggestShown          // what the search field's popup showed last, as it goes

	songRecognition recognitionView

	nowPlaying  bool
	npFull      bool       // Now Playing taking the whole screen
	fullHintAt  time.Time  // when the bubble telling of F11 first showed in this run
	sysAccent   string     // the desktop's accent as #RRGGBB, "" where it has none
	sysOpaque   bool       // the desktop asks for opaque surfaces: Windows's transparency effects off
	npOpen      bool       // Now Playing as it last showed: open or not
	npChangedAt time.Time  // when it last opened or closed
	mem         memRelease // memory given back when the app is quiet (releaseSoon)
	npPane      int        // paneLyrics, paneQueue, paneStage, paneAnalysis, paneProgram, paneCaptions, paneWall or paneWiki
	// npComments is whether the comments are out, in their drawer at
	// the right of Now Playing (ui_comment_drawer.go).
	npComments bool
	npDrawer   float32 // how far out the drawer is, eased, 0 to 1
	// an is the analysis pane's state (ui_analysis.go), anScrub its
	// progress bar's.
	an           anView
	anScrub      scrubState
	npPaneTop    float32 // how far the cover beside the pane starts below its top
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
	volumeSave  *time.Timer // keeps the volume a moment after it last moved

	rowBars barState
	// underTop and underBottom say whether the page has content under its
	// bars, which show their scroll edges then.
	underTop, underBottom bool
	barScrub              scrubState
	npScrub               scrubState
	glowColor             ui.Color   // the color of Now Playing's glow, from the cover's
	menu                  paneMenu   // the menu open, if one is (ui_pane.go)
	npPill                ui.Rect    // where the tab chosen is, in the row of tabs
	barBox                ui.Rect    // where the player bar is, which Now Playing grows from
	barSpot, npSpot       flightSpot // what stays as it grows, in the bar and in the view
	npK                   float32    // how far Now Playing has opened, from 0 to 1
	npCoverScale          float32    // how its cover stands, which the flight lands at
	motion                int        // how much the app moves, in this frame
	mk                    motionKit  // what the motion of the app keeps from frame to frame (motion_kit.go)
	dialogScaling         bool       // the dialog being built grows in (scalingBody)
	dialogFlying          bool       // the dialog being built flies out of what opened it, dialogFlyK of the way
	dialogFlyK            float32
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
	deskLineAt            ui.Rect          // where the desktop lyrics laid out their line last
	deskStood             []deskStood      // where the standing lyrics laid out each character last
	deskSlider            deskSlideState   // how far the desktop lyrics' long line has slid (desk_scroll.go)
	desk                  deskState        // the desktop lyrics' unlock button, and what they keep out of the way of
	sleep                 sleepState       // the timer that pauses the music
	keys                  shortcutState    // the global keys and the menu keys
	spin                  discState        // how far the bar's cover has turned
	npPointer             bool             // the pointer is over Now Playing
	headAt                ui.Rect          // where the cover of the page's header is
	cameFrom              cardOrigin       // the card this page was opened from
	restoreY              float32          // how far to scroll the page coming back
	plView                plView           // how the playlist open is searched and sorted
	// plViews keeps each list page's search and sort while the listener visits
	// another page (such as an artist or album) and comes back (#100).
	// plViewOrder tracks when each was stashed, oldest first, capped at
	// maxNavHistory.
	plViews              map[route]plView
	plViewOrder          []route
	headGone             float32       // how far the page's header has scrolled away
	rowsRoute            route         // the page whose list last came in
	rowsAt               time.Time     // when it started to
	rowsPending          bool          // waiting for trackList's first layout
	rowsWaited           time.Time     // since when
	rowsStart, rowsCount int           // the first song and count in the entrance batch
	sideColor            ui.Color      // what the sidebar shows, as it eases
	flight               coverFlight   // a cover on its way to the page it opens
	heldRoute            route         // the page opened from a card
	heldURL              string        // and the cover the card gave it
	playSpot             ui.Rect       // where the play button last built is
	backdropCache        backdropCache // the backdrop of Now Playing, as last painted
	pageGlass            pageGlass     // the cover the cover backgrounds show, as it changes
	windowMaterial       mygo.Vibrancy
	npArt                *artwork     // the cover whose colors Now Playing shows
	npCoverState         coverState   // the cover of Now Playing, as it is tilted and flicked
	wallState            wallState    // the wall of the queue's covers, as it is dragged
	quiet                quietState   // the quiet parts of the song playing, for its hot comments
	captions             captionState // the episode playing's captions, and the model that writes them
	sig                  playerSig
	quitting             bool

	settings     settings
	settingsView settingsView    // the settings panel
	shownLines   shownCache      // the lyrics shown, without credits if hidden
	update       updateState     // the update pane, and the update it found
	whatsNew     whatsNewState   // what the version updated to brought, said once
	support      supportState    // the support page (支持 Cloudy), under 关于
	cacheUsed    int64           // the bytes the song cache takes, -1 until measured
	otherUsed    int64           // and the pictures' (art_disk.go)
	updates      bool            // the update plugin is used (useUpdater)
	fontList     []uiFont        // the fonts on offer here (fontChoices)
	fontsLoaded  map[string]bool // the fonts of files registered, by id (loadFont)

	// The songs saved to disk, the ones on their way there, and where both
	// are kept.
	dl  downloadState
	lib localLibrary
	// The covers the listener chose for their playlists and songs.
	ownCovers ownCovers
	opened    *openedFiles // the songs opened with Cloudy, coming in

	restored     bool   // playback picked up where it was left
	savedSession string // the session as last kept
	sessionDue   bool   // a save of the session is due

	style         styleState   // the day's songs in the styles picked
	deck          deckState    // the home page's calendar of the daily and style cards
	week          weekState    // what 听歌周报 shows: its tab, and the past week picked
	listen        listenState  // 听歌账本: every play, by week (listen_log.go)
	hist          historyDaily // the past daily recommendations NetEase keeps
	deckDrawer    bool         // the settings' drawer of what the calendar holds is open
	deckSort      deckSort     // a page dragged to a new place in that drawer
	sideDrawer    bool         // the settings' drawer of what the sidebar shows is open
	sideSort      sideSort     // an item dragged to a new place in it
	queueAdding   bool         // the queue's drawer of playlists to add it to is open
	radar         radarState   // NetEase's radars on the home page
	lyricDownload lyricDownloadState
	progressAt    progressShown // what the taskbar button last showed of the song
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

// postQuiet returns a post that runs fn on the main thread but redraws
// only if shown, asked there, says what fn changed is on screen: progress
// every tenth of a second that nothing shows wakes no window.
func (a *app) postQuiet(shown func() bool) func(func()) {
	return func(fn func()) {
		if a.win == nil {
			return
		}
		mygo.RunOnMain(func() {
			fn()
			if shown() {
				a.redraw()
			}
		})
	}
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
		otherUsed:  -1,
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
		a.sideFold()
		netease.StartDiagLog(mygo.App.Version())
		ui.SetIgnoreReduceMotion(a.settings.IgnoreSystemMotion)
		a.applyUpdateSource()
		a.applyOutput()
		a.settings.applyAppearance()
		a.loadFont()
		a.npPane = a.lyricsPaneChosen()
		// Before the window: on Windows it lays out its content as it is
		// made, and the view asks the player what plays.
		a.art = newArtCache(a.post)
		if dir := otherCacheDir(a.settings.CacheFolder); dir != "" {
			a.art.disk = newArtDisk(dir, int64(a.settings.OtherCacheMB)<<20)
		}
		a.pl = player.New(a.post)
		a.pl.PodcastSpeed = a.settings.PodcastSpeed
		a.pl.SetEffects(a.currentEffects(), false)
		a.pl.Quality = func() string { return a.settings.Quality }
		a.useMoreWords()
		a.useOwnLyrics()
		a.useMediaCover(false)
		a.useOwnCovers()
		a.pl.Normalize = func() bool { return a.settings.Normalize }
		a.pl.Crossfade = a.crossfade
		a.pl.Cache = newSongCache(a.settings.CacheFolder, a.settings.CacheMB)
		a.pl.Loud = openLoudness()
		go player.DropStreamSpools()
		a.pl.LocalFile = a.downloadedFile
		a.noteDownloadsAtStart()
		a.pl.Listened = func(t netease.Track, played time.Duration, ended bool) {
			a.listened(t, played, ended)
			a.keepListened(t, played, ended, time.Now())
			a.remember(t, played, ended)
		}
		a.pl.SongStopped = func() {
			a.sleep.minutes = 0
			a.toast = "定时结束，本首播完后已暂停"
		}
		a.pl.Changed = func() {
			a.keepSession()
			a.noteQueueSource()
			a.autoFollow()
			a.syncCaptions()
			a.syncThumbs()
			// 暂停后的桌面歌词已停止定时刷新，恢复播放时需唤醒其他窗口。
			a.redraw()
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
		a.watchAltTab()
		// The notch player is macOS's, over its menu bar: elsewhere it would
		// hang at the top of the screen as a black box.
		if runtime.GOOS == "darwin" {
			a.notch = newNotch(a)
		}
		// From the menu bar alone, the Dock icon shows while the window
		// does.
		a.syncDock()
		a.win.OnShow(a.syncDock)
		a.win.OnHide(func() {
			a.syncDock()
			a.privateWindowPaused()
			a.releaseHidden(true)
		})
		// Shown or switched to the morning after, it shows the new day's.
		a.win.OnFocus(func() {
			a.freshenPersonal()
			a.syncListening(false)
			a.recheckQueue(false)
			a.privateWindowFocused(time.Now())
		})
		a.win.OnBlur(func() {
			a.privateWindowBlur(time.Now())
			a.releaseAfter(blurDelay, eventWorth)
		})
		a.win.OnMinimize(func() {
			a.privateWindowPaused()
			a.releaseHidden(false)
		})
		a.win.OnRestore(func() {
			a.privateWindowRestored(time.Now())
		})
		a.listenForDrops()
		a.watchQuiet()
		a.loadLibraryLater()
		a.applyTray()
		a.applyDeskLyrics()
		mygo.Screen.OnDisplaysChanged(func() { a.post(a.refitFloats) })
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
			a.closePrivateMessages()
			if !a.settings.CloseQuits && (runtime.GOOS == "darwin" || a.tray != nil) {
				a.hideMain()
			} else {
				mygo.App.Quit()
			}
		})
	})
	a.followProgress()
	keep := func() {
		a.settings.Volume = a.volume
		a.settings.save()
		a.saveSession()
		if write := a.transcriptWrite(); write != nil {
			write()
		}
	}
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		a.closeSongRecognition()
		a.closePrivateMessages()
		a.quitting = true
		keep()
	})
	// Windows ending the session (shutting down, signing out) ends the app
	// without a quit: what was kept would be lost, the volume back at 80%
	// at every start (a viewer, 1.3.2).
	mygo.App.OnQuit(func() {
		if !a.quitting {
			a.closeSongRecognition()
			a.closePrivateMessages()
			a.quitting = true
			keep()
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
