package app

import "github.com/egoist/mygo/ui"

// The sidebar is as wide as the listener drags its edge, to read the long
// names of playlists in full, and its rows as large as they choose in the
// settings, to read them at all on a large display: two things, so that a
// wider sidebar shows more of a name rather than larger letters of as much.

const (
	sidebarMin = 200 // the search field and the account still fit
	sidebarMax = 380
	// pageMin is the least room the sidebar leaves the page, which a
	// narrow window keeps by narrowing the sidebar.
	pageMin = 600
)

// sideWidth is the sidebar's width in a window w wide: what the listener
// dragged it to, or its own, narrowed to leave the page its room.
func (a *app) sideWidth(w float32) float32 {
	sw := float32(a.settings.SidebarWidth)
	if sw <= 0 {
		sw = sidebarWidth
	}
	most := max(sidebarMin, min(sidebarMax, w-pageMin))
	return min(max(sw, sidebarMin), most)
}

// sidebarResizer is the sidebar's right edge, which the pointer drags to
// widen or narrow it; a double click makes it as it was.
func (a *app) sidebarResizer(c *ui.Context, w float32) {
	// Inside the sidebar: over its edge, the page, drawn after it, would
	// take the press.
	edge := ui.Box(c.Key("sidebar-edge")).Absolute().Right(0).Top(0).Bottom(0).Width(6).
		Cursor(ui.CursorResizeEW).Label("调整侧边栏宽度")
	if edge.DoubleClicked() {
		a.settings.SidebarWidth = 0
		a.settings.save()
		return
	}
	if dx, _, ok := edge.Dragged(); ok {
		if dx != 0 {
			a.settings.SidebarWidth = int(a.sideWidth(w) + dx)
			a.sideDragging = true
		}
		return
	}
	if a.sideDragging {
		// Let go: kept for the next launch.
		a.sideDragging = false
		a.settings.SidebarWidth = int(a.sideWidth(w))
		a.settings.save()
	}
	if edge.Hovered() {
		// A line along the edge shows what the pointer may drag.
		edge.Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X + r.W/2 - 1, Y: r.Y, W: 2, H: r.H}, a.pal.accent.Alpha(0.5), 1)
		})
	}
}

// sideDims are the sizes of the sidebar's rows, as the listener chose them.
type sideDims struct {
	row, cover, coverRadius float32 // a playlist's or an album's row
	text, navText, icon     float32
	title                   float32 // the titles of its sections
}

// sidebarSizes are the sizes the listener chooses among, in the settings.
var sidebarSizes = []struct {
	id, label string
	dims      sideDims
}{
	{"", "标准", sideDims{row: 34, cover: 22, coverRadius: 5, text: 13, navText: 13.5, icon: 17, title: 11.5}},
	{"large", "大", sideDims{row: 40, cover: 28, coverRadius: 6, text: 14.5, navText: 15, icon: 19, title: 12.5}},
	{"huge", "特大", sideDims{row: 48, cover: 36, coverRadius: 7, text: 16, navText: 16.5, icon: 21, title: 13.5}},
}

// side is the sizes of the sidebar's rows.
func (a *app) side() sideDims {
	for _, s := range sidebarSizes {
		if s.id == a.settings.SidebarSize {
			return s.dims
		}
	}
	return sidebarSizes[0].dims
}

// sidebarSizeLabel names the size chosen.
func (a *app) sidebarSizeLabel() string {
	for _, s := range sidebarSizes {
		if s.id == a.settings.SidebarSize {
			return s.label
		}
	}
	return sidebarSizes[0].label
}
