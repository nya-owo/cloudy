package app

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// The listener comments on the song playing from Now Playing, as on
// NetEase's own apps, where the comment shows too; and deletes their own.

// These post a comment and delete one with NetEase; tests replace them.
var (
	addComment    = netease.AddComment
	deleteComment = netease.DeleteComment
)

// commentLimit is how long a comment NetEase takes, in characters.
const commentLimit = 140

// commentDraft is the comment being written, for the song of track.
type commentDraft struct {
	track   int64
	text    string
	box     ui.Handle
	sending bool
	// armed is the comment of the listener's a first click on 删除 asked
	// about, until when a second deletes it.
	armed     int64
	armedTill time.Time
}

// commentable reports whether tr is a song with comments at NetEase.
func commentable(tr *netease.Track) bool {
	return tr.ID > 0
}

// commentComposer is the field to write a comment in, over the comments,
// with its button to send it. Signed out, it asks the listener to sign in.
func (a *app) commentComposer(c *ui.Context, tr *netease.Track) {
	d := &a.more.draft
	if d.track != tr.ID {
		*d = commentDraft{track: tr.ID}
	}
	if !commentable(tr) {
		return
	}
	n := utf8.RuneCountInString(d.text)
	over := n > commentLimit
	ui.Row(c.Key("compose")).Margin(4, 8, 6, 12).Padding(4, 4, 4, 16).Gap(8).Radius(999).AlignItems(ui.Center).
		Background(white.Alpha(0.08)).Border(1, white.Alpha(0.1)).Children(func() {
		if a.profile == nil {
			b := ui.ButtonBase(c.Key("compose-login")).Grow(1).Height(32).AlignItems(ui.Center).FocusRing(false).Label("登录后发表评论")
			b.Children(func() {
				ui.Text(c, "登录后发表评论，评论会同步到网易云音乐").FontSize(13).TextColor(white.Alpha(0.5))
			})
			if b.Clicked() {
				a.openLogin()
			}
			return
		}
		in := ui.TextInputBase(c.Key("compose-input"), &d.text).Bind(&d.box).Grow(1).MinWidth(0).
			Placeholder("说点什么，评论会同步到网易云音乐").FontSize(13.5).TextColor(white).Label("发表评论")
		if n > 0 {
			fg := white.Alpha(0.4)
			if over {
				fg = a.pal.accent
			}
			ui.Text(c, fmt.Sprintf("%d/%d", n, commentLimit)).FontSize(11).FontFeatures("tnum").TextColor(fg)
		}
		ready := n > 0 && !over && !d.sending
		bg := white.Alpha(0.1)
		if ready {
			bg = a.pal.accent
		}
		send := ui.ButtonBase(c.Key("compose-send")).Height(32).PaddingX(16).Radius(999).AlignItems(ui.Center).FocusRing(false).Label("发送")
		send.Background(send.AnimateColor("bg", bg, ui.Animation{Duration: 120 * time.Millisecond}))
		send.Children(func() {
			label := "发送"
			if d.sending {
				label = "发送中…"
			}
			ui.Text(c, label).FontSize(13).FontWeight(600).TextColor(white.Alpha(0.55 + 0.45*b2f(ready)))
		})
		if (send.Clicked() || in.Submitted()) && ready {
			a.sendComment(tr)
		}
	})
}

// sendComment posts the draft, and puts it first among the latest once
// NetEase took it.
func (a *app) sendComment(tr *netease.Track) {
	d := &a.more.draft
	cs := a.commentsFor(tr)
	thread, text, id := cs.thread, d.text, tr.ID
	d.sending = true
	go func() {
		cm, err := addComment(thread, text)
		a.post(func() {
			if d.track == id {
				d.sending = false
			}
			if err != nil {
				a.say("comment", commentFailure(err))
				return
			}
			if d.track == id {
				d.text = ""
			}
			a.say("comment", "评论已发送，已同步到网易云音乐")
			if cs.thread != thread {
				return
			}
			if cm.ID == 0 || cm.Content == "" {
				// NetEase took it but said nothing of it: load them again.
				*cs = commentState{track: tr.ID, thread: thread}
				a.loadComments()
				return
			}
			if cm.User.ID == 0 && a.profile != nil {
				cm.User.ID, cm.User.Nickname, cm.User.AvatarURL = a.profile.UserID, a.profile.Nickname, a.profile.AvatarURL
			}
			if cm.Time == 0 {
				cm.Time = time.Now().UnixMilli()
			}
			cs.latest = append([]netease.Comment{*cm}, cs.latest...)
			cs.total++
		})
	}()
}

// commentFailure says why a comment did not go, as far as NetEase said.
func commentFailure(err error) string {
	var ce *netease.CommentError
	if errors.As(err, &ce) && ce.Message != "" {
		return "评论未发送：" + ce.Message
	}
	return "评论发送失败，请稍后再试"
}

// commentDelete is the button on a comment of the listener's own that
// deletes it: a first click asks, a second within a few seconds deletes.
func (a *app) commentDelete(c *ui.Context, cm *netease.Comment) {
	if a.profile == nil || cm.User.ID != a.profile.UserID || cm.ID == 0 {
		return
	}
	d := &a.more.draft
	now := c.Now()
	armed := d.armed == cm.ID && now.Before(d.armedTill)
	if armed {
		c.After(d.armedTill.Sub(now))
	}
	label, fg := "删除", white.Alpha(0.42)
	if armed {
		label, fg = "确认删除", a.pal.accent
	}
	b := ui.ButtonBase(c.Key("delete")).Padding(3, 6).Margin(-3, 0).Radius(999).FocusRing(false).Label(label + "评论")
	if b.Hovered() && !armed {
		fg = white.Alpha(0.8)
	}
	b.Children(func() { ui.Text(c, label).FontSize(12).TextColor(fg) })
	if b.Clicked() {
		if armed {
			d.armed = 0
			a.removeComment(cm.ID)
		} else {
			d.armed, d.armedTill = cm.ID, now.Add(4*time.Second)
		}
	}
}

// removeComment deletes the listener's comment id, from the list at once,
// back if NetEase refuses.
func (a *app) removeComment(id int64) {
	cs := &a.comments
	thread := cs.thread
	var kept *netease.Comment
	for i := range cs.latest {
		if cs.latest[i].ID == id {
			cm := cs.latest[i]
			kept = &cm
			cs.latest = append(cs.latest[:i:i], cs.latest[i+1:]...)
			cs.total = max(0, cs.total-1)
			break
		}
	}
	go func() {
		err := deleteComment(thread, id)
		a.post(func() {
			if err == nil {
				a.say("comment", "评论已删除")
				return
			}
			a.say("comment", "删除失败，请稍后再试")
			if kept != nil && cs.thread == thread {
				cs.latest = append([]netease.Comment{*kept}, cs.latest...)
				cs.total++
			}
		})
	}()
}

// commentCountButton is a speech bubble with how many comments the song
// has, by its heart in Now Playing: a click opens the comments, and
// another goes back to the lyrics.
func (a *app) commentCountButton(c *ui.Context, tr *netease.Track, size float32, color ui.Color) ui.Element {
	if !commentable(tr) {
		return ui.Box(c)
	}
	n := 0
	if cs := a.commentsFor(tr); cs.loaded {
		n = cs.total
	}
	wrap := ui.Box(c).Width(size + max(40, size*1.25)).Height(size + 16).AlignItems(ui.Start)
	wrap.Children(func() {
		on := a.npPane == paneComments
		tint := color
		if on {
			tint = white
		}
		if iconButton(c.Key("comments"), iconComment, size*0.9, tint, "评论").Clicked() {
			if on {
				a.npPane = a.lyricsPaneChosen()
			} else {
				a.npPane = paneComments
			}
		}
		if n > 0 {
			ui.Text(c.Key("comment-count"), commentBadge(n)).FontSize(max(10, size*0.46)).FontWeight(700).FontFeatures("tnum").
				TextColor(tint).Absolute().Top(-2).Left(8 + size*0.84).SingleLine().PassThrough()
		}
	})
	return wrap
}

// commentBadge is a count as NetEase's apps show one by a button: up to
// 999+, then in 万 (w), up to 100w+.
func commentBadge(n int) string {
	switch {
	case n < 1000:
		return fmt.Sprint(n)
	case n < 10000:
		return "999+"
	case n < 100000:
		return fmt.Sprintf("%dw+", n/10000)
	case n < 1000000:
		return "10w+"
	}
	return "100w+"
}

// barCommentButton is commentCountButton in the player bar, by the heart:
// a click opens Now Playing on the comments.
func (a *app) barCommentButton(c *ui.Context, tr *netease.Track, size float32, color ui.Color, showCount bool) ui.Element {
	if !commentable(tr) {
		return ui.Box(c)
	}
	n := 0
	if cs := a.commentsFor(tr); cs.loaded {
		n = cs.total
	}
	width := size + 16
	if showCount {
		width = size + max(40, size*1.6)
	}
	wrap := ui.Box(c).Width(width).Height(size + 16).AlignItems(ui.Start)
	wrap.Children(func() {
		button := iconButton(c.Key("comments"), iconComment, size*0.9, color, "评论")
		if cs := a.commentsFor(tr); cs.loaded {
			button.Tooltip(fmt.Sprintf("%d 条评论", cs.total))
		}
		if button.Clicked() {
			a.npPane = paneComments
			a.nowPlaying = true
		}
		if n > 0 && showCount {
			ui.Text(c.Key("comment-count"), commentBadge(n)).FontSize(max(10, size*0.46)).FontWeight(700).FontFeatures("tnum").
				TextColor(color).Absolute().Top(-1).Left(8 + size*0.84).SingleLine().PassThrough()
		}
	})
	return wrap
}
