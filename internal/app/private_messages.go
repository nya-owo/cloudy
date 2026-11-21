package app

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

type privateMessageBackend interface {
	Conversations(context.Context, int) (netease.PrivateConversationPage, error)
	History(context.Context, int64, int64) (netease.PrivateHistoryPage, error)
	Send(context.Context, int64, string) error
	Read(context.Context, int64) error
	StickerGroups(context.Context) ([]netease.PrivateStickerGroup, error)
	StickerPage(context.Context, string, string) (netease.PrivateStickerPage, error)
	SendSticker(context.Context, int64, netease.PrivateSticker) error
}

var newPrivateMessageBackend = func(id int64) privateMessageBackend { return netease.NewPrivateMessageClient(id) }

type privateChat struct {
	user                           netease.MessageUser
	messages                       []netease.PrivateMessage
	draft, err                     string
	loaded, loading, sending, more bool
	refreshSent, refreshingSent    bool
	admittingHistory               bool
	before                         int64
	gen                            int
	history                        ui.ListState
	historyRows                    []privateHistoryEntry
	historyCount                   int
	bottom                         bool
	outgoing                       []*privateOutgoing
	nextOutgoing                   uint64
	activeOutgoing                 *privateOutgoing
	unread                         int
	contactAt                      int64 // Latest Conversations(0) snapshot time observed for this chat.
	serverUnreadAt                 int64 // Latest authoritative Conversations snapshot coverage time.
	readAt                         int64 // Latest locally read/confirmed message time (never bumped by incoming).
	gapUntil                       int64 // Unfetched history boundary between cached messages and newer arrivals.
	unreadEstimated                bool  // Local unread count includes unconfirmed History delta beyond serverUnreadAt.
	hasNewTail                     bool  // Observed loaded incoming message at/after tail while not reading at end.
	arriving                       map[privateMessageIdentity]*privateBubbleEntrance
	read                           privateReadState
}

// privateMessagesState is memory-only and is discarded at every account change.
type privateMessagesState struct {
	open, loaded, loading, more bool
	epoch                       int
	query, err                  string
	contacts                    []netease.PrivateConversation
	next, contactsOffset        int
	selected                    int64
	chats                       map[int64]*privateChat
	media                       *privateMediaCache
	play                        *privatePlayRequest
	contactsIn, messagesIn      privateRowEntrance
	listScroll                  ui.ScrollState
	searchBox, composer         ui.Handle
	stickers                    privateStickerPicker
	client                      privateMessageBackend
	ctx                         context.Context
	cancel                      context.CancelFunc
	presentation                privatePresentation
	inbox                       *privateInbox
	retryAt                     time.Time
	failures                    int
	rateLimits                  int
}

func (a *app) resetPrivateMessages() {
	a.closePrivateMessages()
	a.dm = privateMessagesState{}
}

func (a *app) closePrivateMessages() {
	d := &a.dm
	d.presentation = privatePresentation{}
	if !d.open {
		return
	}
	d.open = false
	a.stopPrivateInbox()
	// Keep fetched sticker pages until resetPrivateMessages clears the account.
	d.stickers = d.stickers.afterClose()
	d.epoch++
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
	d.loading = false
	d.contactsIn, d.messagesIn = privateRowEntrance{}, privateRowEntrance{}
	for _, chat := range d.chats {
		clear(chat.arriving)
		chat.read.busy = false // The panel context cancels any in-flight read.
		chat.refreshSent = chat.refreshSent || chat.refreshingSent
		chat.refreshingSent = false
		chat.loading = false
		if chat.sending {
			chat.sending = false
			chat.activeOutgoing.fail(nil)
			chat.activeOutgoing = nil
			chat.refreshSent = true
		}
	}
}

func (a *app) openPrivateMessages(user *netease.MessageUser) {
	a.accountOpen = false
	a.settingsView.open = false
	if a.login.open {
		a.login.open = false
		a.login.gen++
	}
	d := &a.dm
	opening := !d.open
	if opening {
		d.open = true
		d.epoch++
		d.ctx, d.cancel = context.WithCancel(context.Background())
		d.loaded = false
		if !d.presentation.contents {
			d.contactsIn = privateRowEntrance{pending: true}
		}
		d.messagesIn = privateRowEntrance{pending: true}
	}
	if a.profile != nil {
		if d.client == nil {
			d.client = newPrivateMessageBackend(a.profile.UserID)
		}
		if opening {
			a.startPrivateInbox()
			a.preloadPrivateStickers()
		}
		if d.chats == nil {
			d.chats = map[int64]*privateChat{}
		}
		if !d.loaded && !d.loading {
			a.loadPrivateContacts(false)
		}
		if user != nil && user.UserID > 0 && user.UserID != a.profile.UserID {
			a.selectPrivateChat(*user)
		}
		if chat := d.chats[d.selected]; opening && chat != nil {
			chat.bottom = true
			if !chat.loading {
				a.requestPrivateHistory(false, true)
			}
		}
	}
	a.showMain()
	a.redraw()
}

func (a *app) selectPrivateChat(user netease.MessageUser) {
	d := &a.dm
	prevID := d.selected
	changed := prevID != user.UserID
	chat := a.privateChatFor(user)
	if changed {
		d.stickers.open = false
		if previous := d.chats[prevID]; previous != nil {
			clear(previous.arriving)
		}
		if in := d.inbox; in != nil && prevID != 0 {
			if in.pending != nil && in.pending.id == prevID {
				in.pending = nil
			}
			a.yieldPrivateInbox(prevID)
		}
		chat.bottom = true
		d.messagesIn = privateRowEntrance{pending: true}
	}
	d.selected = user.UserID
	if (changed || !chat.loaded || chat.refreshSent) && !chat.loading {
		a.requestPrivateHistory(false, changed)
	} else if changed {
		a.schedulePrivateInbox(time.Now())
	}
}

func (a *app) privateRequestCurrent(account, epoch int) bool {
	return a.profile != nil && a.accountGen == account && a.dm.epoch == epoch && a.dm.open
}

func (a *app) loadPrivateContacts(older bool) {
	d := &a.dm
	if a.profile == nil || !d.open || d.client == nil || d.loading {
		return
	}
	offset := 0
	if older {
		if !d.more {
			return
		}
		offset = d.next
	}
	a.yieldPrivateInbox(0)
	if offset == 0 {
		a.notePrivateContactsStart(time.Now())
	}
	d.loading = true
	d.contactsOffset = offset
	d.err = ""
	account, epoch, client, ctx := a.accountGen, d.epoch, d.client, d.ctx
	go func() {
		page, err := client.Conversations(ctx, offset)
		a.post(func() { a.privateContactsAnswered(account, epoch, offset, page, err) })
	}()
}

func (a *app) privateContactsAnswered(account, epoch, offset int, page netease.PrivateConversationPage, err error) {
	if !a.privateRequestCurrent(account, epoch) {
		return
	}
	d := &a.dm
	d.loading = false
	now := time.Now()
	defer a.schedulePrivateInbox(now)
	if err != nil {
		a.recordPrivateBackoff(err, now)
		d.err = err.Error()
		return
	}
	a.clearPrivateBackoff(now)
	a.applyPrivateContactsSnapshot(offset, page.Items, false, now)
	d.loaded = true
	d.more = page.More && page.Next > offset
	d.next = page.Next
}

func (a *app) loadPrivateHistory(older bool) {
	a.requestPrivateHistory(older, false)
}

func (a *app) requestPrivateHistory(older, admission bool) {
	d := &a.dm
	chat := d.chats[d.selected]
	if a.profile == nil || !d.open || d.client == nil || chat == nil || chat.loading {
		return
	}
	before := int64(0)
	if older {
		if !chat.more {
			return
		}
		before = chat.before
	}
	a.yieldPrivateInbox(d.selected)
	chat.loading = true
	chat.admittingHistory = admission
	chat.refreshingSent = !older && chat.refreshSent
	if chat.refreshingSent {
		chat.refreshSent = false
	}
	if chat.err != "" {
		chat.historyRows = nil
	}
	chat.err = ""
	chat.gen++
	account, epoch, gen, id, client, ctx := a.accountGen, d.epoch, chat.gen, d.selected, d.client, d.ctx
	go func() {
		page, err := client.History(ctx, id, before)
		for i := range page.Items {
			page.Items[i] = page.Items[i].WithContentCache()
		}
		a.post(func() { a.privateHistoryAnswered(account, epoch, id, gen, before, page, err) })
	}()
}

func (a *app) privateHistoryAnswered(account, epoch int, id int64, gen int, before int64, page netease.PrivateHistoryPage, err error) {
	if !a.privateRequestCurrent(account, epoch) {
		return
	}
	chat := a.dm.chats[id]
	if chat == nil || chat.gen != gen {
		return
	}
	chat.loading = false
	now := time.Now()
	defer a.schedulePrivateInbox(now)
	afterSend := chat.refreshingSent && before == 0
	chat.refreshingSent = false
	if err != nil {
		a.recordPrivateBackoff(err, now)
		chat.err = err.Error()
		chat.historyRows = nil
		if afterSend {
			chat.refreshSent = true
			if a.dm.selected == id {
				a.toast = "消息刷新失败，请稍后重试"
			}
		}
		return
	}
	a.clearPrivateBackoff(now)
	if len(chat.messages) == 0 && len(chat.outgoing) == 0 {
		// Loading can have cached rows without the empty state.
		chat.historyRows = nil
	}
	after := int64(-1)
	if before == 0 {
		if in := a.dm.inbox; in != nil && in.pending != nil && in.pending.id == id {
			after = in.pending.after
		}
		if afterSend && page.Authoritative && privateMessagesTailTime(page.Items) < privateMessagesTailTime(chat.messages) {
			page.Authoritative = false
			page.Exhausted = false
		}
	}
	a.applyPrivateHistory(id, before, page, !chat.admittingHistory, after, false)
	if before == 0 {
		if in := a.dm.inbox; in != nil && in.pending != nil && in.pending.id == id &&
			privateMessagesTailTime(chat.messages) >= chat.contactAt {
			in.pending = nil
		}
	}
}

// All history sources share the existing server collection and native list.
// A nonnegative after enables bounded local unread fallback beyond serverUnreadAt.
func (a *app) updatePrivateHistory(id, before int64, page netease.PrivateHistoryPage, incremental bool, after int64) bool {
	return a.applyPrivateHistory(id, before, page, incremental, after, false)
}

func (a *app) applyPrivateHistory(id, before int64, page netease.PrivateHistoryPage, incremental bool, after int64, fromAuto bool) bool {
	chat := a.dm.chats[id]
	initial := !chat.loaded
	known := make(map[privateMessageIdentity]bool, len(chat.messages))
	for _, msg := range chat.messages {
		known[privateMessageID(msg)] = true
	}
	selfID := int64(0)
	if a.profile != nil {
		selfID = a.profile.UserID
	}
	oldCount := len(chat.messages)
	var oldTail netease.PrivateMessage
	if oldCount > 0 {
		oldTail = chat.messages[oldCount-1]
	}
	if before == 0 && page.Authoritative {
		pageTail := privateMessagesTailTime(page.Items)
		lagging := (fromAuto && (pageTail < chat.contactAt || (chat.gapUntil > 0 && pageTail < oldTail.Time))) ||
			(!fromAuto && chat.contactAt > oldTail.Time && pageTail < chat.contactAt)
		if lagging {
			page.Authoritative = false
			page.Exhausted = false
		}
	}
	wasUnread := chat.unread
	authoritative := before == 0 && page.Authoritative && (!page.More || page.Before > 0)
	var gapTarget int64
	if before == 0 && page.Before > 0 {
		incomplete := page.More || (!authoritative && !page.Exhausted)
		if incomplete {
			if oldCount > 0 && oldTail.Time > 0 && page.Before > oldTail.Time {
				gapTarget = oldTail.Time
			} else if oldCount == 0 && after > 0 && page.Before > after && page.More {
				gapTarget = after
			}
		}
	}
	nextGapUntil := chat.gapUntil
	if gapTarget > 0 {
		if nextGapUntil == 0 || gapTarget < nextGapUntil {
			nextGapUntil = gapTarget
		}
	} else if nextGapUntil > 0 {
		if before == 0 {
			if authoritative && !page.More || page.Before > 0 && page.Before <= nextGapUntil {
				nextGapUntil = 0
			}
		} else {
			progressed := page.Before > 0 && page.Before < before
			if progressed && (page.Before <= nextGapUntil || page.Exhausted) {
				nextGapUntil = 0
			}
		}
	}
	reading := a.privateReading(id) && chat.history.AtEnd() && nextGapUntil == 0
	if after >= 0 {
		after = max(after, chat.serverUnreadAt)
	}
	newUnread := 0
	newTailIncoming := false
	live := incremental && before == 0 && !initial && a.dm.open && a.dm.selected == id
	var arrivals []privateMessageIdentity
	for _, msg := range page.Items {
		key := privateMessageID(msg)
		if known[key] {
			continue
		}
		known[key] = true
		if live {
			arrivals = append(arrivals, key)
		}
		isPeerIncoming := msg.From.UserID > 0 && msg.From.UserID != selfID
		if before == 0 && isPeerIncoming {
			atOrAfterTail := oldCount == 0 || msg.Time >= oldTail.Time
			if live && !reading && atOrAfterTail {
				newTailIncoming = true
			}
			if after >= 0 && !reading {
				sameMsUncounted := oldCount > 0 && msg.Time == after && msg.Time >= oldTail.Time && wasUnread == 0
				if msg.Time > after || sameMsUncounted {
					newUnread++
				}
			}
		}
	}
	removed, tailRemoved := chat.mergePrivateMessages(page, authoritative)
	changed := initial || removed || oldCount != len(chat.messages)
	if before == 0 {
		if tailRemoved {
			a.refreshPrivateDeletedPreview(chat, oldTail)
		}
		changed = chat.reconcileOutgoing(selfID) || changed
		if initial {
			chat.bottom = true
		}
	}
	chat.loaded = true
	gap := before == 0 && gapTarget > 0 && oldCount > 0
	if before > 0 {
		progressed := page.Before > 0 && page.Before < before
		if progressed {
			more := page.More || nextGapUntil > 0
			changed = changed || chat.more != more || chat.before != page.Before
			chat.more = more
			chat.before = page.Before
		} else if nextGapUntil == 0 {
			changed = changed || chat.more
			chat.more = false
		}
	} else if initial || oldCount == 0 || gap || authoritative && !page.More {
		more := (page.More || gap) && page.Before > 0
		changed = changed || chat.more != more
		chat.more = more
		chat.before = page.Before
	}
	changed = changed || chat.gapUntil != nextGapUntil
	chat.gapUntil = nextGapUntil
	if changed {
		chat.historyRows = nil
	}
	if len(arrivals) > 0 {
		var aliases map[privateMessageIdentity]*privateOutgoing
		if len(chat.outgoing) > 0 {
			aliases = chat.outgoingAliases()
		}
		for _, key := range arrivals {
			if aliases[key] == nil {
				if chat.arriving == nil {
					chat.arriving = make(map[privateMessageIdentity]*privateBubbleEntrance)
				}
				chat.arriving[key] = &privateBubbleEntrance{}
			}
		}
	}
	if newTailIncoming && !chat.hasNewTail {
		chat.hasNewTail = true
		changed = true
	}
	if newUnread > 0 {
		chat.unread += min(newUnread, math.MaxInt-chat.unread)
		chat.unreadEstimated = true
		a.notifyPrivateIncoming(id)
		changed = true
	}
	return changed
}

func (chat *privateChat) mergePrivateMessages(page netease.PrivateHistoryPage, authoritative bool) (removed, tailRemoved bool) {
	old := chat.messages
	var result []netease.PrivateMessage
	if authoritative && len(old) > 0 {
		present := make(map[privateMessageIdentity]bool, len(page.Items))
		for _, msg := range page.Items {
			present[privateMessageID(msg)] = true
		}
		result = make([]netease.PrivateMessage, 0, len(old))
		for i, msg := range old {
			key := privateMessageID(msg)
			// The cursor is the newest page's oldest time. Other messages at
			// that exact time can be on the next page.
			if present[key] || page.More && msg.Time <= page.Before {
				result = append(result, msg)
			} else {
				tailRemoved = i == len(old)-1
				delete(chat.arriving, key)
			}
		}
		removed = len(result) != len(old)
	} else {
		result = append(result, old...)
	}
	for _, msg := range page.Items {
		found := false
		for _, prior := range result {
			if (msg.ID != 0 && prior.ID == msg.ID) || (msg.ID == 0 && prior.ID == 0 && msg.Time == prior.Time && msg.From.UserID == prior.From.UserID && msg.Raw == prior.Raw) {
				found = true
				break
			}
		}
		if !found {
			result = append(result, msg)
		}
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Time == result[j].Time {
			return result[i].ID < result[j].ID
		}
		return result[i].Time < result[j].Time
	})
	chat.messages = result
	return removed, tailRemoved
}

func (a *app) refreshPrivateDeletedPreview(chat *privateChat, oldTail netease.PrivateMessage) {
	newTailTime, newTailSummary := int64(0), ""
	if n := len(chat.messages); n > 0 {
		latest := chat.messages[n-1]
		newTailTime, newTailSummary = latest.Time, latest.Text()
	}
	rolledBack := len(chat.messages) == 0 || newTailTime < oldTail.Time
	if rolledBack {
		if chat.serverUnreadAt == oldTail.Time {
			chat.serverUnreadAt = newTailTime
		}
		if chat.contactAt == oldTail.Time {
			chat.contactAt = newTailTime
		}
	}
	for i := range a.dm.contacts {
		item := &a.dm.contacts[i]
		// A conversation snapshot may already refer to something newer.
		if item.User.UserID != chat.user.UserID || item.Time != oldTail.Time {
			continue
		}
		if !rolledBack && item.Summary != oldTail.Text() {
			continue
		}
		item.Time, item.Summary = newTailTime, newTailSummary
		sort.SliceStable(a.dm.contacts, func(i, j int) bool { return a.dm.contacts[i].Time > a.dm.contacts[j].Time })
		return
	}
}

func (a *app) sendPrivateMessage(at time.Time) {
	d := &a.dm
	chat := d.chats[d.selected]
	if a.profile == nil || !d.open || d.client == nil || chat == nil || chat.sending || strings.TrimSpace(chat.draft) == "" || len([]rune(chat.draft)) > 1000 {
		return
	}
	chat.sending = true
	d.messagesIn = privateRowEntrance{}
	account, epoch, id, text, client, ctx := a.accountGen, d.epoch, d.selected, chat.draft, d.client, d.ctx
	out := chat.addOutgoing(text, at, a.profile.UserID)
	if !chat.bottom && !chat.history.AtEnd() {
		out.entrance.done = true
	}
	chat.activeOutgoing = out
	chat.draft = ""
	go func() {
		err := client.Send(ctx, id, text)
		a.post(func() { a.privateSendAnswered(account, epoch, id, out.key, err) })
	}()
}

func (a *app) privateSendAnswered(account, epoch int, id int64, key uint64, err error) {
	if !a.privateRequestCurrent(account, epoch) {
		return
	}
	chat := a.dm.chats[id]
	if chat == nil || chat.activeOutgoing == nil || chat.activeOutgoing.key != key {
		return
	}
	chat.sending = false
	out := chat.activeOutgoing
	chat.activeOutgoing = nil
	if err != nil {
		out.fail(err)
		return
	}
	// ACK has no message ID. Only unique server history may reconcile the local row.
	a.refreshPrivateAfterSend(chat)
}

func (a *app) refreshPrivateAfterSend(chat *privateChat) {
	chat.refreshSent = true
	chat.loading = false
	chat.gen++ // supersede every pre-send history request, including another chat
	if a.dm.selected == chat.user.UserID {
		a.loadPrivateHistory(false)
	} else {
		a.schedulePrivateInbox(time.Now())
	}
}
