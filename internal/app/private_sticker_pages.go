package app

import "netease-music-mygo/internal/netease"

// Three API pages cover all full rows at the smallest tile size without
// traversing the pack.
const privateStickerWarmPages = 3

type privateStickerPageRequest struct {
	pending bool
	failed  bool
	pages   int
}

func (p *privateStickerPicker) afterClose() privateStickerPicker {
	for group, request := range p.pageRequests {
		request.pending = false
		p.pageRequests[group] = request
	}
	return privateStickerPicker{
		groups: p.groups, groupsLoaded: p.groupsLoaded, groupsErr: p.groupsErr,
		pages: p.pages, pageRequests: p.pageRequests,
	}
}

func (a *app) preloadPrivateStickers() {
	p := &a.dm.stickers
	if p.groupsLoaded {
		a.preloadNextPrivateStickerPage()
	} else if p.groupsErr == "" {
		a.loadPrivateStickerGroups()
	}
}

func (a *app) loadPrivateStickerGroups() {
	d, p := &a.dm, &a.dm.stickers
	if p.groupsLoading || p.groupsLoaded {
		return
	}
	p.groupsLoading, p.groupsErr = true, ""
	account, epoch, ctx, client := a.accountGen, d.epoch, d.ctx, d.client
	go func() {
		groups, err := client.StickerGroups(ctx)
		a.post(func() { a.privateStickerGroupsAnswered(account, epoch, groups, err) })
	}()
}

func (a *app) privateStickerGroupsAnswered(account, epoch int, groups []netease.PrivateStickerGroup, err error) {
	if !a.privateRequestCurrent(account, epoch) {
		return
	}
	p := &a.dm.stickers
	p.groupsLoading = false
	if err != nil {
		p.groupsErr = err.Error()
		return
	}
	p.groups, p.groupsLoaded = groups, true
	if p.open && p.group == "" && len(groups) != 0 {
		a.selectPrivateStickerGroup(groups[0].ID.String())
	}
	a.preloadNextPrivateStickerPage()
}

func (a *app) preloadNextPrivateStickerPage() {
	p := &a.dm.stickers
	if !p.groupsLoaded {
		return
	}
	for _, request := range p.pageRequests {
		if request.pending {
			return
		}
	}
	visibleGroup := p.stickerWarmGroup()
	var nextGroup, nextCursor string
	for _, group := range p.groups {
		id := group.ID.String()
		request := p.pageRequests[id]
		if request.failed {
			continue
		}
		page, cached := p.pages[id]
		if !cached {
			a.requestPrivateStickerPage(id, "")
			return
		}
		// Extra pages are useful only for the selected or default group.
		if id == visibleGroup && request.pages < privateStickerWarmPages && page.Cursor != "" {
			nextGroup, nextCursor = id, page.Cursor
		}
	}
	if nextGroup != "" {
		a.requestPrivateStickerPage(nextGroup, nextCursor)
	}
}

func (p *privateStickerPicker) stickerWarmGroup() string {
	if p.group == "" && len(p.groups) != 0 {
		return p.groups[0].ID.String()
	}
	return p.group
}

func (a *app) selectPrivateStickerGroup(id string) {
	p := &a.dm.stickers
	p.group, p.err, p.scroll.Y = id, "", 0
	p.loading = p.pageRequests[id].pending
	if page, ok := p.pages[id]; ok {
		p.page, p.pageGroup = page, id
		a.preloadNextPrivateStickerPage()
		return
	}
	a.requestPrivateStickerPage(id, "")
}

func (a *app) loadPrivateStickerPage() {
	p := &a.dm.stickers
	if p.loading || p.group == "" {
		return
	}
	if p.pageGroup != p.group {
		a.selectPrivateStickerGroup(p.group)
		return
	}
	if p.page.Cursor != "" {
		a.requestPrivateStickerPage(p.group, p.page.Cursor)
	}
}

func (a *app) requestPrivateStickerPage(group, cursor string) {
	d, p := &a.dm, &a.dm.stickers
	request := p.pageRequests[group]
	if request.pending {
		return
	}
	if p.pageRequests == nil {
		p.pageRequests = make(map[string]privateStickerPageRequest)
	}
	request.pending = true
	p.pageRequests[group] = request
	if p.group == group {
		p.loading, p.err = true, ""
	}
	account, epoch, ctx, client := a.accountGen, d.epoch, d.ctx, d.client
	go func() {
		page, err := client.StickerPage(ctx, group, cursor)
		a.post(func() { a.privateStickerPageAnswered(account, epoch, group, cursor, page, err) })
	}()
}

func (a *app) privateStickerPageAnswered(account, epoch int, group, cursor string, page netease.PrivateStickerPage, err error) {
	if !a.privateRequestCurrent(account, epoch) {
		return
	}
	p := &a.dm.stickers
	request := p.pageRequests[group]
	request.pending, request.failed = false, err != nil
	if err == nil {
		request.pages++
		warm := 0
		if group == p.stickerWarmGroup() && request.pages <= privateStickerWarmPages {
			warm = 10
		} else if cursor == "" && !privateFavoriteStickerGroup(group) {
			warm = 1
		}
		for _, sticker := range page.Items[:min(warm, len(page.Items))] {
			a.privateStickerArtwork(sticker, false)
		}
		if cursor != "" {
			page.Items = append(p.pages[group].Items, page.Items...)
		}
		p.cachePage(group, page)
	}
	p.pageRequests[group] = request
	defer a.preloadNextPrivateStickerPage()
	if p.group != group {
		return
	}
	p.loading = false
	if err != nil {
		if cursor == "" {
			p.page, p.pageGroup = netease.PrivateStickerPage{}, ""
		}
		p.err = err.Error()
		return
	}
	p.page, p.pageGroup = page, group
	if cursor == "" {
		p.scroll.Y = 0
	}
}

func (p *privateStickerPicker) cachePage(group string, page netease.PrivateStickerPage) {
	if p.pages == nil {
		p.pages = make(map[string]netease.PrivateStickerPage)
	}
	p.pages[group] = page
}
