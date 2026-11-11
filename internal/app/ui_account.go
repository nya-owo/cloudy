package app

import (
	"errors"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"rsc.io/qr"

	"netease-music-mygo/internal/netease"
)

// loginState is a QR code login under way.
type loginState struct {
	open     bool
	gen      int // counts logins, to stop the polls of closed ones
	key      string
	code     *qr.Code
	status   int // a QR state, or 0 while the code loads
	scanner  string
	avatar   string
	err      string
	finished bool

	// cookie is the other way in, for those with no phone app to scan
	// with: a session cookie pasted from a browser.
	cookie     bool
	cookieText string
	cookieBusy bool
	cookieErr  string
}

// boot signs in with the session in the keychain, if any, then loads the
// home page for whoever is listening.
func (a *app) boot() {
	a.homeLoading = true
	go func() {
		var prof *netease.Profile
		if netease.RestoreSession() {
			p, err := netease.Account()
			switch {
			case err == nil && p == nil:
				netease.ClearSession() // expired
			case err == nil:
				prof = p
			}
		}
		a.post(func() {
			a.signedIn(prof)
		})
	}()
}

// signedIn shows what is personal to prof, or to nobody.
func (a *app) signedIn(prof *netease.Profile) {
	a.accountGen++
	a.cancelRadioStart()
	a.profile = prof
	// Playback picks up where it was left, once the session that lets
	// the listener play their songs is back.
	a.restoreSession()
	a.restoreDownloadQueue()
	a.uploadsFor(prof)
	a.mine, a.saved, a.recPlaylists, a.liked = nil, nil, nil, nil
	a.style = styleState{}
	a.radar = radarState{open: a.radar.open}
	a.myAlbums, a.myAlbumsErr, a.myAlbumsBusy = nil, "", false
	a.cloud = cloudState{}
	a.likedPrograms, a.askedPrograms = nil, nil
	// What the listener may play, and follows, changed: load the pages
	// again.
	a.playlists = map[int64]*netease.Playlist{}
	a.loadingPl = map[int64]bool{}
	a.plErr = map[int64]string{}
	a.artists, a.albums, a.users, a.podcasts = map[int64]*artistData{}, map[int64]*albumData{}, map[int64]*userData{}, map[int64]*podcastData{}
	a.podcastTab = &podcastTab{}
	a.searches = map[route]*searchData{}
	a.load(a.route)
	a.loadHome()
	if prof == nil {
		return
	}
	a.loadLiked(prof.UserID)
	a.loadMine(prof.UserID)
	a.loadMyAlbums()
}

func (a *app) openLogin() {
	a.login = loginState{open: true, gen: a.login.gen + 1}
	gen := a.login.gen
	go func() {
		key, err := netease.QRKey()
		var code *qr.Code
		if err == nil {
			code, err = qr.Encode(netease.LoginURL(key), qr.M)
		}
		a.post(func() {
			if gen != a.login.gen {
				return
			}
			if err != nil {
				a.login.err = "无法获取二维码，请检查网络"
				return
			}
			a.login.key, a.login.code, a.login.status = key, code, netease.QRWaiting
			a.pollLogin(gen, key)
		})
	}()
}

// pollLogin asks every two seconds whether the code was scanned, until the
// login ends or the dialog closes.
func (a *app) pollLogin(gen int, key string) {
	go func() {
		for {
			time.Sleep(2 * time.Second)
			code, msg, name, avatar, err := netease.QRCheck(key)
			var prof *netease.Profile
			if code == netease.QRConfirmed {
				netease.SaveSession()
				prof, _ = netease.Account()
			}
			done := make(chan bool, 1)
			a.post(func() {
				l := &a.login
				if gen != l.gen || !l.open {
					done <- true
					return
				}
				switch {
				case err != nil:
					// Try again: the network may come back.
				case code == netease.QRConfirmed:
					l.status, l.finished = code, true
					a.signedIn(prof)
					if prof != nil {
						a.pickUp(prof.UserID)
					}
					a.closeLoginSoon(gen)
				case code == netease.QRExpired || code == netease.QRWaiting || code == netease.QRScanned:
					l.status = code
					if name != "" {
						l.scanner, l.avatar = name, avatar
					}
				default:
					l.err = msg
					if l.err == "" {
						l.err = "登录失败，请稍后再试"
					}
				}
				done <- l.status == netease.QRExpired || l.finished || l.err != ""
			})
			if <-done {
				return
			}
		}
	}()
}

// closeLoginSoon closes the dialog a moment after it showed the welcome.
func (a *app) closeLoginSoon(gen int) {
	go func() {
		time.Sleep(1200 * time.Millisecond)
		a.post(func() {
			if gen == a.login.gen {
				a.login.open = false
			}
		})
	}()
}

func (a *app) signOut() {
	a.login.gen++
	a.signedIn(nil)
	go netease.Logout()
}

// loginDialog shows the QR code to scan with the NetEase app on a phone.
func (a *app) loginDialog(c *ui.Context) {
	p := a.pal
	l := &a.login
	a.dialog(c, "login", 0.35, &l.open, func(panel ui.Element) {
		a.pane(panel, 26, false)
		panel.Width(340).Padding(28, 28, 26, 28).Gap(6).AlignItems(ui.Center)
		if l.cookie && !l.finished {
			a.cookieLogin(c, panel)
			return
		}
		title, sub := "扫码登录", "打开网易云音乐 App，扫一扫登录"
		switch {
		case l.finished:
			title, sub = "登录成功", "欢迎回来，"+a.profileName()
		case l.err != "":
			sub = l.err
		case l.status == netease.QRScanned:
			title, sub = "扫描成功", "请在手机上确认登录"
		case l.status == netease.QRExpired:
			sub = "二维码已失效"
		}
		ui.Text(c, title).FontSize(20).FontWeight(700)
		ui.Text(c, sub).FontSize(13).TextColor(p.muted).TextAlign(ui.Center).Margin(0, 0, 14, 0)

		ui.Box(c).Size(212, 212).Radius(14).Background(ui.RGB(255, 255, 255)).Border(1, p.hairline).Children(func() {
			if l.code != nil {
				code := l.code
				opacity := float32(1)
				if l.status == netease.QRScanned || l.status == netease.QRExpired || l.finished || l.err != "" {
					opacity = 0.12
				}
				ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Opacity(opacity).
					Draw(func(pt *ui.Painter, r ui.Rect) { drawQR(pt, r, code, 14) })
			} else if l.err == "" {
				// On white whatever the appearance, as the code is.
				s := shimmer{base: ui.RGBA(0, 0, 0, 0.08), sheen: ui.RGBA(0, 0, 0, 0.03)}.in(c)
				qrSkeleton(c, s, 14).Absolute().Top(0).Left(0).Right(0).Bottom(0)
			}
			ui.Column(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Center().Gap(10).Children(func() {
				switch {
				case l.finished || l.status == netease.QRScanned:
					if bmp := bitmap(a.art.get(netease.Sized(l.avatar, 160), false)); bmp != nil {
						pic := ui.Box(c).Size(72, 72).Radius(36).Shadow(0, 6, 16, -4, ui.RGBA(0, 0, 0, 0.3))
						in := artIn(pic, bmp)
						pic.Children(func() { artImage(c, bmp, 36, in) })
					}
					if l.scanner != "" {
						ui.Text(c, l.scanner).FontSize(14).FontWeight(600).TextColor(ui.Hex("#1d1d1f"))
					}
				case l.status == netease.QRExpired || l.err != "":
					if pillButton(c.Key("refresh"), iconRepeat, "刷新二维码", p.accent, p.onAccent).Clicked() {
						a.openLogin()
					}
				}
			})
		})
		ui.Text(c, "登录后，每日推荐与歌单将根据你的口味生成").FontSize(11.5).TextColor(p.faint).Margin(16, 0, 0, 0)
		if !l.finished && textLink(c.Key("to-cookie"), "没有手机 App？使用 Cookie 登录", p.muted).Clicked() {
			l.cookie, l.cookieErr = true, ""
		}
	})
}

// cookieLogin is the other face of the login dialog: a field for the
// MUSIC_U cookie of a browser signed in at music.163.com.
func (a *app) cookieLogin(c *ui.Context, panel ui.Element) {
	p := a.pal
	l := &a.login
	panel.AlignItems(ui.Stretch)
	ui.Text(c, "Cookie 登录").FontSize(20).FontWeight(700).TextAlign(ui.Center)
	ui.Text(c, "在浏览器登录 music.163.com，从开发者工具复制 Cookie 里的 MUSIC_U，粘贴到下面").
		FontSize(12.5).TextColor(p.muted).LineHeight(1.45).TextAlign(ui.Center).Margin(0, 0, 12, 0)
	in := ui.TextInput(c.Key("cookie"), &l.cookieText).Placeholder("MUSIC_U=… 或整段 Cookie").Label("Cookie").
		Password().FontSize(13).AutoFocus().Disabled(l.cookieBusy)
	if in.Submitted() {
		a.importCookie()
	}
	if l.cookieErr != "" {
		ui.Text(c, l.cookieErr).FontSize(12).TextColor(dangerColor).LineHeight(1.4).Margin(4, 0, 0, 0)
	}
	ui.Text(c, "Cookie 等同于账号密码，不要发给任何人。Cloudy 只把它存在本机的系统凭据里").
		FontSize(11.5).TextColor(p.faint).LineHeight(1.45).Margin(8, 0, 0, 0)
	ui.Row(c).Gap(10).AlignItems(ui.Center).Margin(16, 0, 0, 0).Children(func() {
		if textLink(c.Key("to-qr"), "返回扫码登录", p.muted).Clicked() {
			l.cookie, l.cookieErr = false, ""
		}
		ui.Box(c).Grow(1)
		label := "登录"
		if l.cookieBusy {
			label = "正在验证…"
		}
		if primaryButton(c.Key("cookie-ok"), label, nil, p, l.cookieBusy || strings.TrimSpace(l.cookieText) == "").Clicked() {
			a.importCookie()
		}
	})
}

// importCookie signs in with the cookie in the login dialog, off the frame.
func (a *app) importCookie() {
	l := &a.login
	if l.cookieBusy || strings.TrimSpace(l.cookieText) == "" {
		return
	}
	l.cookieBusy, l.cookieErr = true, ""
	gen, text := l.gen, l.cookieText
	go func() {
		prof, err := netease.ImportCookie(text)
		a.post(func() {
			l := &a.login
			if gen != l.gen {
				return
			}
			l.cookieBusy = false
			switch {
			case errors.Is(err, netease.ErrBadCookie):
				l.cookieErr = "Cookie 无效或已过期，请重新复制 MUSIC_U"
			case prof == nil:
				l.cookieErr = "无法验证 Cookie，请检查网络"
			default:
				// Signed in, even if the keychain would not keep the session:
				// then it lasts until the app quits.
				l.cookieText, l.finished = "", true
				l.scanner, l.avatar = prof.Nickname, prof.AvatarURL
				a.signedIn(prof)
				a.pickUp(prof.UserID)
				a.closeLoginSoon(gen)
			}
		})
	}()
}

// textLink is a line of small words that does something when clicked,
// underlined under the pointer.
func textLink(c *ui.Context, label string, fg ui.Color) ui.Element {
	b := ui.ButtonBase(c).Padding(4, 2).FocusRing(false).Label(label)
	underline := b.Hovered()
	b.Children(func() {
		t := ui.Text(c, label).FontSize(12).TextColor(fg)
		if underline {
			t.Underline()
		}
	})
	return b
}

// drawQR draws a QR code in r, as crisp squares on white, inside a quiet
// zone of pad.
func drawQR(p *ui.Painter, r ui.Rect, code *qr.Code, pad float32) {
	n := code.Size
	cell := (min(r.W, r.H) - 2*pad) / float32(n)
	x0 := r.X + (r.W-cell*float32(n))/2
	y0 := r.Y + (r.H-cell*float32(n))/2
	ink := ui.Hex("#141416")
	for y := range n {
		for x := range n {
			if code.Black(x, y) {
				// A hair larger, so neighbors join without seams.
				p.Fill(ui.Rect{X: x0 + float32(x)*cell, Y: y0 + float32(y)*cell, W: cell + 0.3, H: cell + 0.3}, ink, 0)
			}
		}
	}
}

func (a *app) profileName() string {
	if a.profile == nil {
		return ""
	}
	return a.profile.Nickname
}

// accountRow sits at the foot of the sidebar: who is signed in, with a
// dropdown to open their page or the settings or sign out, or a way to
// sign in.
func (a *app) accountRow(c *ui.Context) {
	p := a.pal
	b := ui.ButtonBase(c.Key("account")).Height(40).PaddingX(8).Gap(10).Radius(8).AlignItems(ui.Center).Justify(ui.Start).FocusRing(false)
	if b.Hovered() || a.accountOpen && a.profile != nil {
		b.Background(p.hover)
	}
	if a.profile == nil {
		a.accountOpen = false
		b.Label("登录").Children(func() {
			ui.Box(c).Size(26, 26).Radius(13).Background(p.press).Center().Children(func() {
				ui.Icon(c, iconUser).Size(14, 14).TextColor(p.muted)
			})
			ui.Text(c, "登录网易云音乐").FontSize(13).FontWeight(500).TextColor(p.muted)
		})
		if b.Clicked() {
			a.openLogin()
		}
		return
	}
	prof := a.profile
	b.Label(prof.Nickname).Expanded(a.accountOpen).Children(func() {
		a.avatar(c, prof.AvatarURL, 26, p.press)
		ui.Text(c, prof.Nickname).FontSize(13).FontWeight(600).SingleLine().Grow(1).Shrink(1).MinWidth(0)
		ui.Icon(c, iconUpDown).Size(13, 13).TextColor(p.faint)
	})
	if b.Clicked() {
		a.accountOpen = !a.accountOpen
	}
	a.accountMenu(c, b)
}

// accountMenu drops up from the account row, as wide as it, in the colors
// of the volume popover: the listener's own page and the settings, then
// signing out.
func (a *app) accountMenu(c *ui.Context, row ui.Element) {
	pop := popoverColors(a.pal.dark)
	ui.PopoverBase(c.Key("account-menu"), row, &a.accountOpen, func(panel ui.Element) {
		a.pane(panel, 20, false)
		panel.AttachTo(row, ui.AnchorTopStart, ui.AnchorBottomStart).Margin(0, 0, 6, 0).Padding(6).Gap(1).
			Role(ui.RoleMenu).FocusGroup(ui.Vertical).Label("账户").TransformOrigin(0.5, 1).
			Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Scale: 0.96, Y: 4}, Exit: &ui.Motion{Scale: 0.98}})
		if w := row.Bounds().W; w > 0 {
			panel.Width(w)
		}
		shortcut := "Ctrl+,"
		if runtime.GOOS == "darwin" {
			shortcut = "⌘,"
		}
		if a.menuItem(c, iconUser, "我的主页", "").AutoFocus().Clicked() {
			a.accountOpen = false
			a.open(route{page: pageUser, id: a.profile.UserID})
		}
		if a.menuItem(c, iconSettings, "设置…", shortcut).Clicked() {
			a.accountOpen = false
			a.openSettings()
		}
		ui.Box(c).Height(0.5).Margin(4, 8).Background(pop.edge)
		if a.menuItem(c, iconSignOut, "退出登录", "").Clicked() {
			a.accountOpen = false
			a.signOut()
		}
	})
}

// menuItem is an item of a dropdown: an icon, its label, and the shortcut
// that does the same, if any.
func (a *app) menuItem(c *ui.Context, icon *ui.SVG, label, shortcut string) ui.Element {
	p := a.pal
	b := ui.ButtonBase(c.Key(label)).Role(ui.RoleMenuItem).Label(label).Height(30).PaddingX(8).Gap(9).Radius(14).
		AlignItems(ui.Center).Justify(ui.Start).FocusRing(false)
	if b.Hovered() || b.FocusVisible() {
		b.Background(p.press)
	}
	b.Children(func() {
		ui.Icon(c, icon).Size(15, 15).TextColor(p.muted)
		ui.Text(c, label).FontSize(13).Grow(1)
		if shortcut != "" {
			ui.Text(c, shortcut).FontSize(12).TextColor(p.faint)
		}
	})
	return b
}

// myPlaylists lists the listener's playlists under a header that opens
// and closes them: those they made, if mine, with a button to make one
// and a menu to rename them, or else with a menu to unsave them.
func (a *app) myPlaylists(c *ui.Context, title string, lists []netease.Playlist, mine bool, collapsed *bool) {
	if len(lists) == 0 {
		return
	}
	p := a.pal
	ui.Row(c.Key(title)).AlignItems(ui.Center).Margin(18, 0, 6, 0).Children(func() {
		b := ui.ButtonBase(c.Key("toggle")).Grow(1).Height(28).PaddingX(10).Gap(6).Radius(8).
			AlignItems(ui.Center).Justify(ui.Start).Role(ui.RoleDisclosure).Label(title)
		if b.Clicked() {
			*collapsed = !*collapsed
		}
		b.Expanded(!*collapsed)
		if b.Hovered() {
			b.Background(p.hover)
		}
		b.Children(func() {
			ui.Text(c, title).FontSize(a.side().title).FontWeight(600).TextColor(p.faint).Grow(1)
			turn := float32(0)
			if *collapsed {
				turn = -90
			}
			ui.Icon(c, iconDown).Size(12, 12).TextColor(p.faint).Rotate(b.AnimateTo("turn", turn, motionFast))
		})
		if mine && iconButton(c.Key("new"), iconPlus, 13, p.faint, "新建歌单").Size(26, 26).Radius(8).Clicked() {
			a.newPlaylist(nil)
		}
	})
	if !*collapsed {
		rows := ui.Column(c.Key(title + "-rows")).Gap(4)
		if a.motion > motionOff {
			rows.Transition(ui.ElementTransition{Animation: &motionPane, Enter: &ui.Motion{Collapse: true}, Exit: &ui.Motion{Collapse: true}})
		}
		rows.Children(func() {
			shown := a.sideShown(len(lists))
			a.playlistRows(c, lists[:shown], mine)
			if shown < len(lists) {
				kind := mineSaved
				if mine {
					kind = mineMade
				}
				a.sideMore(c, len(lists), kind)
			}
		})
	}
}

// playlistRows lists playlists in the sidebar.
func (a *app) playlistRows(c *ui.Context, lists []netease.Playlist, mine bool) {
	p := a.pal
	for i := range lists {
		pl := &lists[i]
		selected := a.route.page == pagePlaylist && a.route.id == pl.ID && !a.nowPlaying
		d := a.side()
		b := ui.ButtonBase(c.Key(pl.ID)).Height(d.row).Radius(8).FocusRing(false).Label(pl.Name)
		if b.Hovered() && !selected {
			b.Background(p.hover)
		}
		a.navRowContent(c, b, selected, 8, func() {
			a.cover(c, pl.Cover(), 96, d.cover, d.coverRadius)
			label := ui.Text(c, pl.Name).FontSize(d.text).TextColor(p.text).SingleLine().Shrink(1).MinWidth(0)
			if selected {
				label.FontWeight(600)
			}
		})
		switch {
		case mine && pl.SpecialType != 5:
			a.contextMenu(b, false, func() [][]menuAction { return a.playlistMenu(pl) }, nil)
		case !mine:
			a.contextMenu(b, false, func() [][]menuAction {
				return [][]menuAction{{{label: "取消收藏", do: func() { a.toggleSaved(*pl) }}}}
			}, nil)
		}
		if b.Clicked() {
			a.open(route{page: pagePlaylist, id: pl.ID})
		}
	}
}
