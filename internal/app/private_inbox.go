package app

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"netease-music-mygo/internal/netease"
)

const (
	privateFocusInterval        = 30 * time.Second
	privateBlurInterval         = 60 * time.Second
	privateWakeThrottle         = 5 * time.Second
	privateRateLimitBackoff     = time.Minute
	privateMaxRateLimitBackoff  = 5 * time.Minute
	privateInboxMaxHistoryPages = 4
)

type privateWindowState struct {
	visible, focused, minimized, hasWindow bool
}

var privateWindowQuery = func(a *app) privateWindowState {
	if a.win == nil || a.win.ID() == 0 {
		return privateWindowState{visible: true, focused: true}
	}
	return privateWindowState{
		visible:   a.win.IsVisible(),
		focused:   a.win.IsFocused(),
		minimized: a.win.IsMinimized(),
		hasWindow: true,
	}
}

func privateInboxInterval(ws privateWindowState) (time.Duration, bool) {
	if !ws.visible || ws.minimized {
		return 0, false
	}
	if ws.focused {
		return privateFocusInterval, true
	}
	return privateBlurInterval, true
}

type privateInboxChange struct {
	id, after int64
}

// One inbox per open panel. Messages and unread counts belong to the chats.
type privateInbox struct {
	ctx            context.Context
	cancel         context.CancelFunc
	timer          *time.Timer
	requestCancel  context.CancelFunc
	gen            int
	busy           bool
	requestID      int64
	syncAt         time.Time
	lastContactsAt time.Time
	primed         bool
	pending        *privateInboxChange
}

func (in *privateInbox) queuePending(id, after int64) {
	if in == nil || id <= 0 {
		return
	}
	if in.pending != nil && in.pending.id == id && in.pending.after >= 0 && (after < 0 || in.pending.after < after) {
		after = in.pending.after
	}
	in.pending = &privateInboxChange{id: id, after: after}
}

func (a *app) startPrivateInbox() {
	if !a.dm.open || a.profile == nil || a.dm.client == nil || a.dm.inbox != nil {
		return
	}
	now := time.Now()
	ctx, cancel := context.WithCancel(a.dm.ctx)
	var syncAt time.Time
	if interval, ok := privateInboxInterval(privateWindowQuery(a)); ok {
		syncAt = now.Add(interval)
	}
	a.dm.inbox = &privateInbox{
		ctx:    ctx,
		cancel: cancel,
		syncAt: syncAt,
	}
	a.schedulePrivateInbox(now)
}

func (a *app) stopPrivateInbox() {
	if in := a.dm.inbox; in != nil {
		in.cancel()
		if in.timer != nil {
			in.timer.Stop()
		}
		if in.requestCancel != nil {
			in.requestCancel()
		}
		a.dm.inbox = nil
	}
}

func (a *app) privateWindowPaused() {
	in := a.dm.inbox
	if in == nil {
		return
	}
	if in.timer != nil {
		in.timer.Stop()
		in.timer = nil
	}
	if in.busy {
		in.cancelRequest()
	}
}

func (a *app) privateWindowBlur(now time.Time) {
	in := a.dm.inbox
	if in == nil || !a.dm.open || a.profile == nil {
		return
	}
	ws := privateWindowQuery(a)
	if !ws.visible || ws.minimized {
		a.privateWindowPaused()
		return
	}
	if !in.lastContactsAt.IsZero() && in.syncAt.After(now) {
		if target := in.lastContactsAt.Add(privateBlurInterval); target.After(in.syncAt) {
			in.syncAt = target
		}
	}
	a.schedulePrivateInbox(now)
}

func (a *app) privateWindowFocused(now time.Time) {
	a.privateWindowWake(now, true)
}

func (a *app) privateWindowRestored(now time.Time) {
	a.privateWindowWake(now, false)
}

func (a *app) privateWindowWake(now time.Time, focusedEvent bool) {
	in := a.dm.inbox
	if in == nil || !a.dm.open || a.profile == nil || a.dm.client == nil {
		return
	}
	ws := privateWindowQuery(a)
	if focusedEvent {
		ws.focused = true
	}
	interval, active := privateInboxInterval(ws)
	if !active {
		return
	}
	contactsInFlight := (a.dm.loading && a.dm.contactsOffset == 0) || (in.busy && in.requestID == 0)
	throttled := !in.lastContactsAt.IsZero() && now.Sub(in.lastContactsAt) < privateWakeThrottle
	if !contactsInFlight && !throttled {
		in.syncAt = now
		a.tickPrivateInbox(now)
	} else {
		if in.syncAt.IsZero() {
			in.syncAt = now.Add(interval)
		} else if !in.lastContactsAt.IsZero() {
			if capAt := in.lastContactsAt.Add(interval); capAt.Before(in.syncAt) {
				in.syncAt = capAt
			}
		}
		a.schedulePrivateInbox(now)
	}
	if ws.focused && a.dm.selected != 0 {
		a.redraw()
	}
}

func (a *app) notePrivateContactsStart(now time.Time) {
	in := a.dm.inbox
	if in == nil {
		return
	}
	in.lastContactsAt = now
	if interval, ok := privateInboxInterval(privateWindowQuery(a)); ok {
		in.syncAt = now.Add(interval)
	}
}

func (a *app) privateHasPendingTarget(id int64) bool {
	if in := a.dm.inbox; in != nil && ((in.pending != nil && in.pending.id == id) || (in.busy && in.requestID == id)) {
		return true
	}
	if chat := a.dm.chats[id]; chat != nil && chat.contactAt > 0 {
		return privateMessagesTailTime(chat.messages) < chat.contactAt
	}
	return false
}

// One timer serves both low-frequency conversation snapshots and selected-chat
// history catch-up. Panel completions re-arm it if manual work blocked it.
func (a *app) schedulePrivateInbox(now time.Time) {
	in := a.dm.inbox
	if in == nil || a.win == nil {
		return // Testers drive the scheduler with an explicit clock.
	}
	if in.timer != nil {
		in.timer.Stop()
		in.timer = nil
	}
	due, ok := a.privateInboxDue(in)
	if !ok {
		return
	}
	account := a.accountGen
	var timer *time.Timer
	timer = time.AfterFunc(max(0, due.Sub(now)), func() {
		mygo.RunOnMain(func() {
			if a.dm.inbox != in || a.accountGen != account || in.timer != timer {
				return
			}
			in.timer = nil
			a.tickPrivateInbox(time.Now())
		})
	})
	in.timer = timer
}

func (a *app) privateInboxDue(in *privateInbox) (time.Time, bool) {
	due, _, ok := a.privateInboxNext(in)
	return due, ok
}

func (a *app) privateInboxNext(in *privateInbox) (due time.Time, runHistory bool, ok bool) {
	if in == nil || in.busy {
		return time.Time{}, false, false
	}
	if _, active := privateInboxInterval(privateWindowQuery(a)); !active {
		return time.Time{}, false, false
	}
	if p := in.pending; p != nil {
		if chat := a.dm.chats[p.id]; chat == nil || !chat.loading {
			return a.dm.retryAt, true, true
		}
	}
	if !in.syncAt.IsZero() && !a.dm.loading {
		due = in.syncAt
		if a.dm.retryAt.After(due) {
			due = a.dm.retryAt
		}
		return due, false, true
	}
	return time.Time{}, false, false
}

// An explicit refresh supersedes any older inbox request for the same data.
func (a *app) yieldPrivateInbox(id int64) {
	if in := a.dm.inbox; in != nil && in.busy && in.requestID == id {
		in.cancelRequest()
	}
}

func (in *privateInbox) cancelRequest() {
	in.gen++
	if in.requestCancel != nil {
		in.requestCancel()
		in.requestCancel = nil
	}
	in.busy = false
}

func isPrivateRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	var httpErr *netease.PrivateMessageHTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode == http.StatusTooManyRequests
	}
	var statusErr *netease.PrivateMessageStatusError
	if errors.As(err, &statusErr) {
		return statusErr.Code == http.StatusTooManyRequests
	}
	return strings.Contains(err.Error(), "429")
}

func (a *app) recordPrivateBackoff(err error, now time.Time) {
	if err == nil {
		return
	}
	a.dm.failures++
	delay := min(privateRateLimitBackoff, 2*time.Second*time.Duration(1<<min(a.dm.failures, 5)))
	if isPrivateRateLimitError(err) {
		a.dm.rateLimits++
		delay = min(privateMaxRateLimitBackoff, privateRateLimitBackoff<<min(a.dm.rateLimits-1, 3))
	}
	if deadline := now.Add(delay); deadline.After(a.dm.retryAt) {
		a.dm.retryAt = deadline
	}
}

func (a *app) clearPrivateBackoff(now time.Time) {
	if !a.dm.retryAt.After(now) {
		a.dm.failures, a.dm.rateLimits = 0, 0
		a.dm.retryAt = time.Time{}
	}
}

func (a *app) tickPrivateInbox(now time.Time) {
	d, in := &a.dm, a.dm.inbox
	if in == nil || !d.open || a.profile == nil {
		return
	}
	due, runHistory, ok := a.privateInboxNext(in)
	if !ok || due.After(now) {
		a.schedulePrivateInbox(now)
		return
	}
	var request privateInboxChange
	if runHistory {
		request = *in.pending
		in.pending = nil
	} else {
		interval, _ := privateInboxInterval(privateWindowQuery(a))
		in.syncAt = now.Add(interval)
		in.lastContactsAt = now
	}
	in.busy = true
	in.requestID = request.id
	in.gen++
	ctx, cancel := context.WithTimeout(in.ctx, 12*time.Second)
	in.requestCancel = cancel
	account, gen, client := a.accountGen, in.gen, d.client
	hasWindow := a.win != nil && a.win.ID() != 0
	lastKnown := int64(0)
	if chat := d.chats[request.id]; chat != nil && len(chat.messages) > 0 {
		lastKnown = chat.messages[len(chat.messages)-1].Time
	}
	if request.after > 0 && (lastKnown == 0 || request.after < lastKnown) {
		lastKnown = request.after
	}
	go func() {
		defer cancel()
		var contacts netease.PrivateConversationPage
		var history netease.PrivateHistoryPage
		var err error
		if request.id == 0 {
			contacts, err = client.Conversations(ctx, 0)
		} else {
			history, err = fetchPrivateInboxHistory(ctx, client, request.id, lastKnown)
		}
		if !hasWindow {
			return // Testers deliver the reply explicitly.
		}
		mygo.RunOnMain(func() {
			if a.privateInboxAnswered(in, account, gen, request, contacts, history, err, time.Now()) {
				a.redraw()
			}
		})
	}()
}

func fetchPrivateInboxHistory(ctx context.Context, client privateMessageBackend, id, lastKnown int64) (netease.PrivateHistoryPage, error) {
	history, err := client.History(ctx, id, 0)
	if err != nil {
		return netease.PrivateHistoryPage{}, err
	}
	if history.Before <= 0 {
		history.More = false
	}
	pages := 1
	for lastKnown > 0 && history.More && history.Before > lastKnown && pages < privateInboxMaxHistoryPages {
		if err := ctx.Err(); err != nil {
			return netease.PrivateHistoryPage{}, err
		}
		cursor := history.Before
		page, err := client.History(ctx, id, cursor)
		if err != nil {
			return netease.PrivateHistoryPage{}, err
		}
		pages++
		history.Items = append(history.Items, page.Items...)
		// Additional pages use before > 0, where History does not establish
		// authoritative coverage; avoid out-of-bounds deletion of cached rows.
		history.Authoritative = false
		if page.Before <= 0 || page.Before >= cursor {
			history.More = false
			history.Exhausted = false
			break
		}
		history.Before = page.Before
		history.More = page.More
		history.Exhausted = page.Exhausted
	}
	if err := ctx.Err(); err != nil {
		return netease.PrivateHistoryPage{}, err
	}
	for i := range history.Items {
		history.Items[i] = history.Items[i].WithContentCache()
	}
	return history, nil
}

func privateMessagesTailTime(items []netease.PrivateMessage) int64 {
	var tail int64
	for _, msg := range items {
		if msg.Time > tail {
			tail = msg.Time
		}
	}
	return tail
}

// Report whether the reply changed what the app shows. Polling and retries
// alone should not rebuild the window while only its stickers are animating.
func (a *app) privateInboxAnswered(in *privateInbox, account, gen int, request privateInboxChange, contacts netease.PrivateConversationPage, history netease.PrivateHistoryPage, err error, now time.Time) bool {
	if a.profile == nil || a.accountGen != account || a.dm.inbox != in || in.gen != gen {
		return false
	}
	in.busy = false
	in.requestCancel = nil
	defer a.schedulePrivateInbox(now)
	if err != nil {
		a.recordPrivateBackoff(err, now)
		return false // Automatic errors leave the current messages and UI intact.
	}
	// An overlapping panel request may have set a fresher backoff.
	a.clearPrivateBackoff(now)
	changed := false
	if request.id == 0 {
		changed = a.applyPrivateContactsSnapshot(0, contacts.Items, true, now)
		if !a.dm.loaded {
			a.dm.loaded = true
			a.dm.more = contacts.More
			a.dm.next = contacts.Next
			changed = true
		}
	} else if chat := a.dm.chats[request.id]; chat != nil {
		changed = a.applyPrivateHistory(request.id, 0, history, true, request.after, true)
		if len(chat.messages) > 0 {
			latest := chat.messages[len(chat.messages)-1]
			user := chat.user
			if latest.From.UserID == request.id {
				user = mergePrivateUser(user, latest.From)
			} else if latest.To.UserID == request.id {
				user = mergePrivateUser(user, latest.To)
			}
			changed = changed || chat.user != user
			chat.user = user
			changed = a.mergePrivateContacts([]netease.PrivateConversation{{User: user, Summary: latest.Text(), Time: latest.Time}}, true) || changed
		}
		if in.pending != nil && in.pending.id == request.id && privateMessagesTailTime(chat.messages) >= chat.contactAt {
			in.pending = nil
		}
	}
	return changed
}

func (a *app) applyPrivateContactsSnapshot(offset int, items []netease.PrivateConversation, fromBackground bool, _ time.Time) bool {
	in := a.dm.inbox
	oldUnread := make(map[int64]int, len(items))
	for _, item := range items {
		id := item.User.UserID
		if chat := a.dm.chats[id]; chat != nil {
			oldUnread[id] = chat.unread
		}
	}
	wasPrimed := in != nil && in.primed
	if offset == 0 && in != nil {
		selected := a.dm.selected
		for _, item := range items {
			id := item.User.UserID
			if id <= 0 {
				continue
			}
			chat := a.dm.chats[id]
			if chat == nil {
				chat = a.privateChatFor(item.User)
			}
			prevContactAt := chat.contactAt
			if item.Time > chat.contactAt {
				chat.contactAt = item.Time
			}
			if id != selected || selected == 0 {
				continue
			}
			chatTail := privateMessagesTailTime(chat.messages)
			var needsFetch bool
			if chat.loading {
				needsFetch = wasPrimed && item.Time > prevContactAt
			} else if chat.loaded {
				needsFetch = item.Time > chatTail
			} else {
				needsFetch = wasPrimed && item.Time > prevContactAt
			}
			if needsFetch {
				after := int64(-1)
				if !item.UnreadKnown {
					after = max(chat.serverUnreadAt, chatTail)
				}
				in.queuePending(selected, after)
			}
		}
		in.primed = true
	}
	changed := a.applyPrivateUnreadSnapshot(items)
	changed = a.mergePrivateContacts(items, false) || changed
	if fromBackground && wasPrimed {
		for _, item := range items {
			if !item.UnreadKnown || item.Time <= 0 {
				continue
			}
			id := item.User.UserID
			if chat := a.dm.chats[id]; chat != nil && chat.unread > oldUnread[id] {
				prevToast := a.toast
				a.notifyPrivateIncoming(id)
				changed = changed || a.toast != prevToast
			}
		}
	}
	return changed
}

func (a *app) applyPrivateUnreadSnapshot(items []netease.PrivateConversation) bool {
	changed := false
	for _, item := range items {
		if !item.UnreadKnown || item.Time <= 0 {
			continue
		}
		chat := a.dm.chats[item.User.UserID]
		if chat == nil {
			chat = a.privateChatFor(item.User)
		}
		sameMsHigher := chat.unreadEstimated && item.Time == chat.readAt && item.Unread > chat.unread
		sameMsZero := item.Unread == 0 && item.Time == chat.readAt && (chat.unread > 0 || chat.hasNewTail)
		if item.Time >= chat.serverUnreadAt && (item.Time > chat.readAt || sameMsHigher || sameMsZero) {
			clearedNewTail := item.Unread == 0 && chat.hasNewTail
			changed = changed || chat.unread != item.Unread || clearedNewTail
			chat.unread, chat.serverUnreadAt = item.Unread, item.Time
			chat.unreadEstimated = false
			if item.Unread == 0 {
				chat.hasNewTail = false
			}
		}
	}
	return changed
}

func (a *app) privateChatFor(user netease.MessageUser) *privateChat {
	if a.dm.chats == nil {
		a.dm.chats = map[int64]*privateChat{}
	}
	chat := a.dm.chats[user.UserID]
	if chat == nil {
		for _, contact := range a.dm.contacts {
			if contact.User.UserID == user.UserID {
				user = mergePrivateUser(contact.User, user)
				break
			}
		}
		chat = &privateChat{user: user}
		a.dm.chats[user.UserID] = chat
	} else {
		chat.user = mergePrivateUser(chat.user, user)
	}
	return chat
}

// History and conversation snapshots can carry only part of a user profile.
func mergePrivateUser(prior, incoming netease.MessageUser) netease.MessageUser {
	if prior.UserID == incoming.UserID {
		if incoming.AvatarURL == "" {
			incoming.AvatarURL = prior.AvatarURL
		}
		if incoming.Nickname == "" {
			incoming.Nickname = prior.Nickname
		}
	}
	return incoming
}

func (a *app) mergePrivateContacts(items []netease.PrivateConversation, fromHistory bool) bool {
	merged := append([]netease.PrivateConversation(nil), a.dm.contacts...)
	chatChanged := false
	for _, item := range items {
		incoming := item.User
		index := slices.IndexFunc(merged, func(prior netease.PrivateConversation) bool {
			return prior.User.UserID == incoming.UserID
		})
		if index >= 0 {
			prior := merged[index]
			if item.Time < prior.Time {
				continue
			}
			// Snapshot formatting can differ for the same message.
			if item.Time > 0 && item.Time == prior.Time && prior.Summary != "" && (!fromHistory || item.Summary == "") {
				item.Summary = prior.Summary
			}
			item.User = mergePrivateUser(prior.User, incoming)
		}
		if chat := a.dm.chats[incoming.UserID]; chat != nil {
			item.User = mergePrivateUser(chat.user, item.User)
			user := mergePrivateUser(item.User, mergePrivateUser(chat.user, incoming))
			if user != chat.user {
				chat.user = user
				chatChanged = true
			}
		}
		if index >= 0 {
			merged[index] = item
		} else {
			merged = append(merged, item)
		}
	}
	sort.SliceStable(merged, func(i, j int) bool { return merged[i].Time > merged[j].Time })
	if slices.EqualFunc(merged, a.dm.contacts, func(x, y netease.PrivateConversation) bool {
		return x.User == y.User && x.Summary == y.Summary && x.Time == y.Time
	}) {
		return chatChanged
	}
	a.dm.contacts = merged
	return true
}

func (a *app) privateReading(id int64) bool {
	ws := privateWindowQuery(a)
	return a.dm.open && a.dm.selected == id && a.dm.presentation.chat &&
		ws.visible && ws.focused && !ws.minimized
}

func (a *app) notifyPrivateIncoming(id int64) {
	ws := privateWindowQuery(a)
	if !ws.hasWindow || !ws.visible || !ws.focused || ws.minimized || a.privateReading(id) {
		return
	}
	a.toast, a.toastAs = "收到新私信", "private-inbox"
}
