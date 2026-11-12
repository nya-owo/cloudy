package app

import (
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

// Panes are what floats over the app: menus, popovers, dialogs, toasts and
// the settings. All take their look from pane, so that they are of one
// material, dark glass or, with settings.GlassPanels off, plain.

// A paneInk holds the colors of what is on a pane.
type paneInk struct {
	text, muted, faint ui.Color
	hover, press, line ui.Color
}

// inkOn returns the colors for a pane, white where it is dark. onDark is for
// a pane over Now Playing, which is dark whatever the theme.
func (a *app) inkOn(onDark bool) paneInk {
	if onDark || a.pal.dark {
		return paneInk{white, white.Alpha(0.6), white.Alpha(0.42), white.Alpha(0.1), white.Alpha(0.16), white.Alpha(0.1)}
	}
	ink := ui.Hex("#1d1d1f")
	return paneInk{ink, ink.Alpha(0.62), ink.Alpha(0.42), ink.Alpha(0.07), ink.Alpha(0.12), ink.Alpha(0.1)}
}

// pane gives e the look of a pane, rounded by radius. Glass lightens what
// shows through in a light theme: a dark pane there takes more of its tint.
func (a *app) pane(e ui.Element, radius float32, onDark bool) ui.Element {
	e.Radius(radius).Shadow(0, 18, 44, -12, ui.RGBA(0, 0, 0, 0.45))
	dark := onDark || a.pal.dark
	switch {
	case a.settings.GlobalGlass:
		return e.Material(a.interfaceGlass(dark))
	case !a.settings.panelGlass() && dark:
		return e.Background(ui.Hex("#232327")).Border(0.5, white.Alpha(0.12))
	case !a.settings.panelGlass():
		return e.Background(ui.Hex("#f6f6f8")).Border(0.5, ui.RGBA(0, 0, 0, 0.12))
	case a.pal.dark:
		return e.Material(glass.Glass{Tint: ui.RGBA(14, 14, 18, 0.62)}).Border(0.5, white.Alpha(0.14))
	case onDark:
		return e.Material(glass.Glass{Tint: ui.RGBA(14, 14, 18, 0.86)}).Border(0.5, white.Alpha(0.14))
	}
	return e.Material(glass.Glass{Tint: ui.RGBA(250, 250, 252, 0.58)}).Border(0.5, ui.RGBA(0, 0, 0, 0.1))
}

// A menuAction is a row of a menu: what it does, or more rows it opens.
type menuAction struct {
	label string // what it says, in full
	// title and note say it in two parts where there is room for them: the
	// name of what it opens, and what kind of thing that is.
	title, note string
	icon        *ui.SVG
	checked     bool
	do          func()
	sub         [][]menuAction
}

// paneMenu is the menu open, one at a time, in a pane of its own over all:
// behind a button, or where the pointer pressed.
type paneMenu struct {
	open     bool
	x, y     float32
	up       float32 // what it rises from, where there is no room below y
	onDark   bool
	groups   [][]menuAction
	head     func(c *ui.Context, ink paneInk)
	sub      *menuAction // what shows in place of groups, if anything
	anchor   ui.Handle   // the control it opens under, for a window that changes size
	anchored bool
	closedAt time.Time // when a press outside closed it
	query    string    // what a long list in it is searched for
}

// menuSearchFrom is how many rows a menu's list has for a search over it:
// the playlists to add a song to, say.
const menuSearchFrom = 9

// menuSearchHeight is how tall the search of a long list is.
const menuSearchHeight = 40

// listed is the list of m that shows: its groups, or those of its sub.
func (m *paneMenu) listed() [][]menuAction {
	if m.sub != nil {
		return m.sub.sub
	}
	return m.groups
}

// searchable reports whether m's list is long enough for a search.
func (m *paneMenu) searchable() bool {
	n := 0
	for _, g := range m.listed() {
		n += len(g)
	}
	return n >= menuSearchFrom
}

// matching keeps of groups the actions whose names have q, and the groups
// with any left.
func matching(groups [][]menuAction, q string) [][]menuAction {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return groups
	}
	var out [][]menuAction
	for _, g := range groups {
		var kept []menuAction
		for _, act := range g {
			name := act.title
			if name == "" {
				name = act.label
			}
			if strings.Contains(strings.ToLower(name+" "+act.note), q) {
				kept = append(kept, act)
			}
		}
		if len(kept) > 0 {
			out = append(out, kept)
		}
	}
	return out
}

// menuWidth is how wide a menu is; the heights are what its rows take, to
// place it before it is built.
const (
	menuWidth      = 268
	menuRowHeight  = 39
	menuLineHeight = 9
	menuHeadHeight = 66
	menuListHeight = 300
	menuTallest    = 520 // the most a menu's own groups take before they scroll
)

// menuRows is how tall groups are in a menu.
func menuRows(groups [][]menuAction) float32 {
	h := float32(0)
	for i, g := range groups {
		if i > 0 {
			h += menuLineHeight
		}
		h += float32(len(g)) * menuRowHeight
	}
	return h
}

// height returns how tall m shows in a window winH tall.
func (m *paneMenu) height(winH float32) float32 {
	search := float32(0)
	if m.searchable() {
		search = menuSearchHeight
	}
	if m.sub != nil {
		return 12 + menuRowHeight + menuLineHeight + search + min(menuRows(m.sub.sub), menuListHeight)
	}
	return m.chrome() + search + min(menuRows(m.groups), m.room(winH))
}

// chrome is how tall a menu is around its groups: its padding, and its head
// if it has one.
func (m *paneMenu) chrome() float32 {
	h := float32(12)
	if m.head != nil {
		h += menuHeadHeight
	}
	return h
}

// room is how tall the groups of m may be before they scroll: a long menu,
// such as the playlists to add a song to, keeps in the window.
func (m *paneMenu) room(winH float32) float32 {
	room := winH - 16 - m.chrome()
	if m.searchable() {
		room -= menuSearchHeight
	}
	return max(min(room, menuTallest), 3*menuRowHeight)
}

// openMenu opens a menu at x, y, rising from up where there is no room below.
func (a *app) openMenu(x, y, up float32, onDark bool, groups [][]menuAction, head func(c *ui.Context, ink paneInk)) {
	a.menu = paneMenu{open: true, x: x, y: y, up: up, onDark: onDark, groups: groups, head: head}
	a.redraw()
}

// menuBelow opens a menu under b, a button, unless the press on b just closed
// it: a click on a button of an open menu closes it, as a press outside it.
func (a *app) menuBelow(b ui.Element, onDark bool, groups [][]menuAction, head func(c *ui.Context, ink paneInk)) {
	if time.Since(a.menu.closedAt) < 300*time.Millisecond {
		return
	}
	r := b.Bounds()
	a.openMenu(r.X, r.Y+r.H+8, r.Y-8, onDark, groups, head)
	b.Bind(&a.menu.anchor)
	a.menu.anchored = true
}

// contextMenu opens a menu where the secondary button presses el.
func (a *app) contextMenu(el ui.Element, onDark bool, groups func() [][]menuAction, head func(c *ui.Context, ink paneInk)) {
	if !el.RightClicked() {
		return
	}
	// Asked only now: an element asking for the pointer is built each time
	// it moves over it, which over the rows of a list adds up.
	x, y, _ := el.PointerPosition()
	r := el.Bounds()
	a.openMenu(r.X+x, r.Y+y, r.Y+y, onDark, groups(), head)
}

// menuOverlay builds the menu open, if one is.
func (a *app) menuOverlay(c *ui.Context) {
	m := &a.menu
	if !m.open {
		return
	}
	if m.anchored {
		r := m.anchor.Bounds(c)
		if e := c.Resolve(m.anchor); e.Valid() {
			r = e.LayoutBounds()
		}
		if r.W <= 0 || r.H <= 0 {
			m.open, m.sub = false, nil
			return
		}
		m.x, m.y, m.up = r.X, r.Y+r.H+8, r.Y-8
	}
	w, h := c.Size()
	ink := a.inkOn(m.onDark)
	x, y := min(max(m.x, 8), w-menuWidth-8), m.y
	if tall := m.height(h); y+tall > h-8 {
		y = max(m.up-tall, 8)
	}
	ui.Overlay(c, func() {
		// It grows from where it was opened.
		originY := float32(0)
		if y != m.y {
			originY = 1
		}
		panel := ui.Column(c.Key("pane-menu")).Absolute().Left(x).Top(y).Width(menuWidth).Padding(6).Gap(1).TextColor(ink.text).
			Role(ui.RoleMenu).FocusGroup(ui.Vertical).Label("菜单").TransformOrigin(clamp01((m.x-x)/menuWidth), originY)
		a.paneMotion(panel)
		a.pane(panel, 20, m.onDark)
		panel.Children(func() {
			if sub := m.sub; sub != nil {
				if a.menuRow(c.Key("back"), menuAction{label: "返回", title: sub.label, icon: iconBack}, false, ink).Clicked() {
					m.sub, m.query = nil, ""
				}
				menuLine(c.Key("back-line"), ink)
				a.menuSearch(c, ink)
				ui.Scroll(c.Key("menu-list")).MaxHeight(menuListHeight).Gap(1).Children(func() { a.menuGroups(c, matching(sub.sub, m.query), ink) })
				return
			}
			if m.head != nil {
				m.head(c, ink)
				menuLine(c.Key("head-line"), ink)
			}
			a.menuSearch(c, ink)
			groups := matching(m.groups, m.query)
			if room := m.room(h); menuRows(m.groups) > room {
				ui.Scroll(c.Key("menu-groups")).MaxHeight(room).Gap(1).Children(func() { a.menuGroups(c, groups, ink) })
			} else {
				a.menuGroups(c, groups, ink)
			}
		})
		if panel.PressedOutside() || panel.OverlayShortcut(0, ui.KeyEscape) {
			m.open, m.sub, m.query, m.closedAt = false, nil, "", time.Now()
		}
	})
}

// menuSearch searches a long list of the menu open, as of the playlists to
// add a song to; a list that finds nothing says so.
func (a *app) menuSearch(c *ui.Context, ink paneInk) {
	m := &a.menu
	if !m.searchable() {
		return
	}
	field := ui.Row(c.Key("menu-search-row")).Height(32).Margin(0, 2, 6, 2).PaddingX(10).Gap(7).Radius(14).AlignItems(ui.Center).Background(ink.hover)
	field.Children(func() {
		ui.Icon(c, iconSearch).Size(13, 13).TextColor(ink.muted)
		ui.TextInputBase(c.Key("menu-search"), &m.query).AutoFocus().Grow(1).MinWidth(0).
			Placeholder("搜索").FontSize(12.5).Label("搜索")
		if m.query != "" && iconButton(c.Key("menu-search-clear"), iconX, 11, ink.muted, "清除搜索").Size(20, 20).Clicked() {
			m.query = ""
		}
	})
	if len(matching(m.listed(), m.query)) == 0 {
		ui.Text(c.Key("menu-search-none"), "没有找到").FontSize(12.5).TextColor(ink.muted).Padding(10, 12)
	}
}

// menuGroups lists groups in the menu, a line between them.
func (a *app) menuGroups(c *ui.Context, groups [][]menuAction, ink paneInk) {
	for i, group := range groups {
		if i > 0 {
			menuLine(c.Key(i), ink)
		}
		for _, act := range group {
			if a.menuRow(c, act, act.sub != nil, ink).Clicked() {
				if act.sub != nil {
					a.menu.sub, a.menu.query = &act, ""
				} else {
					a.menu.open, a.menu.sub, a.menu.query = false, nil, ""
					act.do()
				}
			}
		}
	}
}

// menuRow is a row of a menu, its corners concentric with the pane's,
// lighter under the pointer; opens says that it opens more.
func (a *app) menuRow(c *ui.Context, act menuAction, opens bool, ink paneInk) ui.Element {
	b := ui.ButtonBase(c.Key(act.label)).Role(ui.RoleMenuItem).Label(act.label).Height(38).PaddingX(10).Gap(11).Radius(14).
		AlignItems(ui.Center).Justify(ui.Start).FocusRing(false)
	bg := ui.Transparent
	switch {
	case b.Pressed():
		bg = ink.press
	case b.Hovered() || b.FocusVisible():
		bg = ink.hover
	}
	b.Background(b.AnimateColor("bg", bg, ui.Animation{Duration: 100 * time.Millisecond}))
	title := act.title
	if title == "" {
		title = act.label
	}
	b.Children(func() {
		if act.icon != nil {
			ui.Icon(c, act.icon).Size(16, 16).Shrink(0).TextColor(ink.muted)
		}
		ui.Text(c, title).FontSize(13.5).FontWeight(500).SingleLine().Grow(1).Shrink(1).MinWidth(0)
		if act.note != "" {
			ui.Text(c, act.note).FontSize(12).TextColor(ink.faint).Shrink(0)
		}
		if act.checked {
			ui.Icon(c, iconCheck).Size(15, 15).Shrink(0).TextColor(a.pal.accent)
		}
		if opens {
			ui.Icon(c, iconForward).Size(14, 14).Shrink(0).TextColor(ink.faint)
		}
	})
	return b
}

// menuLine parts groups of a menu.
func menuLine(c *ui.Context, ink paneInk) {
	ui.Box(c).Height(0.5).Margin(4, 10).Background(ink.line)
}

// paneSelect is a select of options, its list a pane, reporting a change.
// family, if given, is the font each option is shown in.
func (a *app) paneSelect(c *ui.Context, label string, selected *string, options []string, family ...func(option string) string) bool {
	before := *selected
	font := func(e ui.Element, option string) {
		if len(family) > 0 {
			e.Font(family[0](option))
		}
	}
	ink := a.inkOn(false)
	sel := ui.SelectBase(c, selected)
	bg := ink.hover
	if sel.Trigger.Hovered() {
		bg = ink.press
	}
	sel.Trigger.MinWidth(112).MaxWidth(220).Height(32).PaddingX(14).Gap(8).Radius(999).Border(0.5, ink.line).AlignItems(ui.Center).FocusRing(false).Label(label).
		Background(sel.Trigger.AnimateColor("bg", bg, ui.Animation{Duration: 100 * time.Millisecond})).Children(func() {
		font(ui.Text(c, *selected).FontSize(13).SingleLine().Grow(1).Shrink(1).MinWidth(0).TextColor(ink.text), *selected)
		ui.Icon(c, iconUpDown).Size(13, 13).Shrink(0).TextColor(ink.faint)
	})
	sel.Popup(func(panel ui.Element) {
		panel.Width(190).Margin(6, 0, 0, 0).Padding(6).Gap(1).TextColor(ink.text).TransformOrigin(0.5, 0)
		a.pane(panel, 20, false)
		a.paneMotion(panel)
		for _, o := range options {
			item := sel.Item(o).Height(34).PaddingX(10).Gap(8).Radius(14).AlignItems(ui.Center)
			if item.Highlighted() {
				item.Background(ink.hover)
			}
			item.Children(func() {
				font(ui.Text(c, o).FontSize(13).SingleLine().Grow(1).Shrink(1).MinWidth(0), o)
				if o == *selected {
					ui.Icon(c, iconCheck).Size(14, 14).Shrink(0).TextColor(a.pal.accent)
				}
			})
		}
	})
	return *selected != before
}

// paneToasts builds the window's toasts as panes, above the player.
func (a *app) paneToasts(c *ui.Context) {
	ink := a.inkOn(a.nowPlaying)
	ui.ToastViewportBase(c, func(viewport ui.Element, toasts []ui.Toast) {
		viewport.Padding(0, 0, playerBarHeight+10, 0).AlignItems(ui.Center).Justify(ui.End).Gap(8)
		for _, t := range toasts {
			toast := ui.ToastBase(c, t)
			a.pane(toast.Root, 20, a.nowPlaying)
			toast.Root.Row().Padding(10, 16).TextColor(ink.text).
				Transition(ui.ElementTransition{Animation: &ui.Snappy, Enter: &ui.Motion{Y: 8}, Exit: &ui.Motion{}})
			toast.Root.Children(func() { ui.Text(c, t.Title).FontSize(13).FontWeight(500) })
		}
	})
}
