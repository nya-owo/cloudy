package app

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"netease-music-mygo/internal/netease"
)

// A partial snapshot used to leave the contact row blank despite a known chat avatar.
func TestPrivateContactInheritsKnownChatAvatar(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(fmt.Sprintf("existing=%t", existing), func(t *testing.T) {
			a, _ := privateTestApp(t)
			known := netease.MessageUser{UserID: 90002, Nickname: "已知昵称", AvatarURL: "known-avatar"}
			chat := a.privateChatFor(known)
			a.dm.contacts = nil
			partial := netease.PrivateConversation{User: netease.MessageUser{UserID: known.UserID, Nickname: "快照昵称"}, Time: 2}
			if existing {
				a.dm.contacts = []netease.PrivateConversation{partial}
			}
			other := netease.PrivateConversation{User: netease.MessageUser{UserID: 90003, Nickname: "另一联系人"}, Time: 1}
			a.mergePrivateContacts([]netease.PrivateConversation{partial, other}, false)
			want := partial.User
			want.AvatarURL = known.AvatarURL
			if len(a.dm.contacts) != 2 || a.dm.contacts[0].User != want || chat.user != want || a.dm.contacts[1].User != other.User {
				t.Fatal("missing avatar was not filled from the matching chat")
			}
			updated := partial
			updated.User.AvatarURL, updated.User.Nickname = "new-avatar", "更新昵称"
			a.mergePrivateContacts([]netease.PrivateConversation{updated}, false)
			if a.dm.contacts[0].User != updated.User || chat.user != updated.User {
				t.Fatal("a new avatar or nickname did not replace the prior value")
			}
			a.accountGen++
			a.resetPrivateMessages()
			a.mergePrivateContacts([]netease.PrivateConversation{partial}, false)
			if a.dm.contacts[0].User != partial.User {
				t.Fatal("a new account inherited the old account's chat avatar")
			}
		})
	}
}

// Partial private-message users used to erase visible avatars before a complete snapshot arrived.
func TestPrivateAvatarSurvivesPartialUsers(t *testing.T) {
	a, _ := privateTestApp(t)
	red, blue := color.RGBA{R: 255, A: 255}, color.RGBA{B: 255, A: 255}
	cacheAvatar := func(url string, fill color.RGBA) {
		img := image.NewRGBA(image.Rect(0, 0, 40, 40))
		draw.Draw(img, img.Bounds(), &image.Uniform{C: fill}, image.Point{}, draw.Src)
		a.art.add(url, &artwork{bmp: ui.NewBitmap(img), used: time.Now()})
	}
	cacheAvatar("avatar-original", red)
	cacheAvatar("avatar-changed", blue)
	for i := range a.dm.contacts {
		a.dm.contacts[i].User.AvatarURL = "avatar-original"
	}
	first, second := a.dm.contacts[0], a.dm.contacts[1]
	a.openPrivateMessages(&first.User)
	chat := a.dm.chats[first.User.UserID]
	a.dm.loaded = true
	defer a.stopPrivateInbox()
	in, now := a.dm.inbox, time.Unix(100, 0)
	reply := func(request privateInboxChange, contacts []netease.PrivateConversation, messages []netease.PrivateMessage) {
		in.busy = true
		a.privateInboxAnswered(in, a.accountGen, in.gen, request, netease.PrivateConversationPage{Items: contacts}, netease.PrivateHistoryPage{Items: messages}, nil, now)
	}
	reply(privateInboxChange{}, []netease.PrivateConversation{first, second}, nil)
	chat.user.Nickname = "头像回归聊天头部"
	tt := ui.NewTester(a.view, 1180, 780)
	privateShowContents(tt)
	tt.Advance(time.Second)
	locate := func(label string, dx float32) image.Point {
		t.Helper()
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("missing avatar geometry for %s", label)
		}
		return image.Pt(int(r.X+dx), int(r.Y+r.H/2))
	}
	points := []image.Point{
		locate("会话："+first.User.Nickname, 28),
		locate("会话："+second.User.Nickname, 28),
		locate(chat.user.Nickname, -28),
	}
	pixels := func() [3]color.RGBA {
		img := tt.Image()
		return [3]color.RGBA{
			img.RGBAAt(points[0].X, points[0].Y),
			img.RGBAAt(points[1].X, points[1].Y),
			img.RGBAAt(points[2].X, points[2].Y),
		}
	}
	before := pixels()
	for _, pixel := range before {
		if pixel.R < 200 || pixel.G > 32 || pixel.B > 32 {
			t.Fatalf("avatar test fixture is not visible: %v", before)
		}
	}
	message := netease.PrivateMessage{
		ID: 42, From: netease.MessageUser{UserID: first.User.UserID, Nickname: "历史昵称"},
		Time: first.Time, Raw: `{"msg":"合成消息"}`,
	}
	reply(privateInboxChange{id: first.User.UserID, after: -1}, nil, []netease.PrivateMessage{message})
	tt.Frame()
	during := pixels()
	historyKeptAvatar := chat.user.AvatarURL == first.User.AvatarURL && a.dm.contacts[0].User.AvatarURL == first.User.AvatarURL
	first.Time++
	reply(privateInboxChange{}, []netease.PrivateConversation{first, second}, nil)
	tt.Frame()
	tt.Advance(artFade.Duration)
	tt.Frame()
	after := pixels()
	if !historyKeptAvatar || during != before || after != before {
		t.Fatalf("history changed visible avatars, before/during/after = %v / %v / %v", before, during, after)
	}
	firstPartial, secondPartial := first, second
	firstPartial.User.AvatarURL, secondPartial.User.AvatarURL = "", ""
	firstPartial.User.Nickname, secondPartial.User.Nickname = "第一位更新", "第二位更新"
	reply(privateInboxChange{}, []netease.PrivateConversation{firstPartial, secondPartial}, nil)
	tt.Frame()
	if pixels() != before || a.dm.contacts[1].User.AvatarURL != second.User.AvatarURL || chat.user.Nickname != firstPartial.User.Nickname {
		t.Fatal("a partial multi-contact snapshot lost a visible avatar or suppressed a nickname")
	}
	first.User.AvatarURL, first.User.Nickname = "avatar-changed", "真实头像与昵称更新"
	reply(privateInboxChange{}, []netease.PrivateConversation{first}, nil)
	tt.Frame()
	got := pixels()
	if got[0].B < 200 || got[2].B < 200 || got[0].R > 32 || got[2].R > 32 || got[1] != before[1] ||
		chat.user != first.User || a.dm.contacts[0].User != first.User {
		t.Fatalf("an actual same-time profile update did not paint both avatars: %v", got)
	}
}

// Real history sticker redraws must leave the cached contact and header avatars intact.
func TestPrivateAvatarsStayVisibleDuringStickerFrames(t *testing.T) {
	data, err := os.ReadFile("testdata/sticker-motion.webp")
	if err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{0, 1, 3} {
		t.Run(fmt.Sprintf("%d-stickers", count), func(t *testing.T) {
			a, _ := privateTestApp(t)
			avatar := image.NewRGBA(image.Rect(0, 0, 40, 40))
			draw.Draw(avatar, avatar.Bounds(), &image.Uniform{C: color.RGBA{G: 255, A: 255}}, image.Point{}, draw.Src)
			a.art.add("synthetic-avatar", &artwork{bmp: ui.NewBitmap(avatar), used: time.Now()})
			for i := range a.dm.contacts {
				a.dm.contacts[i].User.AvatarURL = "synthetic-avatar"
			}
			user := a.dm.contacts[0].User
			a.openPrivateMessages(&user)
			chat := a.dm.chats[user.UserID]
			chat.user.Nickname = "动图旁的头像"
			for i := range count {
				art, err := decodeStickerArt(data)
				if err != nil || art.sticker == nil {
					t.Fatalf("invalid animation fixture: %v", err)
				}
				name := fmt.Sprintf("合成动图 %d", i)
				url := fmt.Sprintf("https://p1.music.126.net/sticker-motion-fixture-%d.webp", i)
				art.used = time.Now()
				a.art.add(url+"#sticker", art)
				sticker := netease.PrivateSticker{ID: json.Number(fmt.Sprint(i + 1)), Group: "10", Name: name, URL: url}
				raw, err := json.Marshal(map[string]any{"msgType": 1, "body": sticker})
				if err != nil {
					t.Fatal(err)
				}
				chat.messages = append(chat.messages, netease.PrivateMessage{ID: int64(i + 1), From: user, Raw: string(raw)})
			}
			tt := ui.NewTester(a.view, 1180, 780)
			privateShowContents(tt)
			tt.Advance(time.Second)
			locate := func(label string, dx float32) image.Point {
				t.Helper()
				r, ok := tt.Find(label)
				if !ok {
					t.Fatalf("missing %s", label)
				}
				return image.Pt(int(r.X+dx), int(r.Y+r.H/2))
			}
			points := []image.Point{
				locate("会话："+user.Nickname, 28),
				locate("会话："+a.dm.contacts[1].User.Nickname, 28),
				locate(chat.user.Nickname, -28),
			}
			checkAvatars := func() {
				t.Helper()
				for _, point := range points {
					pixel := tt.Image().RGBAAt(point.X, point.Y)
					if pixel.G < 200 || pixel.R > 32 || pixel.B > 32 {
						t.Fatalf("avatar lost coverage during sticker redraw: %v", pixel)
					}
				}
				if len(a.art.busy) != 0 {
					t.Fatal("cached avatars or sticker frames unexpectedly queued artwork requests")
				}
			}
			stickers := make([][2]bool, count)
			for range 10 {
				checkAvatars()
				for i := range count {
					point := locate(fmt.Sprintf("表情：合成动图 %d", i), 56)
					pixel := tt.Image().RGBAAt(point.X, point.Y)
					stickers[i][0] = stickers[i][0] || pixel.R > 200 && pixel.B < 32
					stickers[i][1] = stickers[i][1] || pixel.B > 200 && pixel.R < 32
				}
				tt.Advance(30 * time.Millisecond)
			}
			tt.Frame()
			checkAvatars()
			for i, colors := range stickers {
				if !colors[0] || !colors[1] {
					t.Fatalf("sticker %d did not paint both animation frames: %v", i, colors)
				}
			}
		})
	}
}

// A contact's first chat header must paint its cached left-side avatar without a second load.
func TestPrivateAvatarFirstOpenReusesContactImage(t *testing.T) {
	a, _ := privateTestApp(t)
	a.settings.Motion = "off"
	user := &a.dm.contacts[0].User
	path, err := filepath.Abs("testdata/sticker-motion.webp")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	bmp, _, err := decodeArt(data)
	if err != nil {
		t.Fatal(err)
	}
	user.AvatarURL = path
	key := netease.Sized(path, 114)
	cached := &artwork{bmp: bmp, used: time.Now()}
	a.art.add(key, cached)
	delete(a.dm.chats, user.UserID)
	a.openPrivateMessages(nil)
	tt := ui.NewTester(a.view, 1180, 780)
	tt.Frame()

	label := "会话：" + user.Nickname
	row, ok := tt.Find(label)
	if !ok || a.dm.selected != 0 {
		t.Fatal("the contact was not visible before opening its chat")
	}
	left := tt.Image().RGBAAt(int(row.X+28), int(row.Y+row.H/2))
	if left != (color.RGBA{R: 255, A: 255}) || len(a.art.busy) != 0 {
		t.Fatalf("the contact did not paint its cached avatar: %v", left)
	}
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	list, hasList := tt.Find("私信消息列表")
	profile, hasProfile := tt.Find("查看主页")
	if !hasList || !hasProfile || a.dm.selected != user.UserID {
		t.Fatal("the contact's chat header did not mount")
	}
	right := tt.Image().RGBAAt(int(list.X+18), int(profile.Y+profile.H/2))
	if right != left || len(a.art.busy) != 0 || a.art.peek(key) != cached {
		t.Fatalf("first chat avatar = %v, want %v from the shared cache; pending artwork: %v", right, left, a.art.busy)
	}
}

// Evicting one or several already-drawn DM avatars used to blank them and start reloads.
func TestPrivateVisibleAvatarsSurviveCachePressure(t *testing.T) {
	data, err := os.ReadFile("testdata/sticker-motion.webp")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name     string
		contacts bool
	}{
		{name: "header"},
		{name: "header-and-contacts", contacts: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := privateTestApp(t)
			posted := make(chan func(), 8)
			a.art.post = func(fn func()) { posted <- fn }
			now := time.Unix(100, 0)
			a.art.now = func() time.Time { return now }
			addArt := func(key string) {
				t.Helper()
				bmp, _, err := decodeArt(data)
				if err != nil {
					t.Fatal(err)
				}
				a.art.add(key, &artwork{bmp: bmp, size: len(data), used: now})
				now = now.Add(time.Second)
			}
			dir := t.TempDir()
			var keys []string
			avatar := func(name string) string {
				t.Helper()
				path := filepath.Join(dir, name+".webp")
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				key := netease.Sized(path, 114)
				keys = append(keys, key)
				addArt(key)
				return path
			}
			if tc.contacts {
				a.dm.contacts[0].User.AvatarURL = avatar("first")
				a.dm.contacts[1].User.AvatarURL = avatar("second")
			}
			user := a.dm.contacts[0].User
			a.openPrivateMessages(&user)
			chat := a.dm.chats[user.UserID]
			chat.user.Nickname = "聊天头像测试"
			chat.user.AvatarURL = avatar("header")
			tt := ui.NewTester(a.view, 1180, 780)
			privateShowContents(tt)
			tt.Advance(time.Second)
			points := make([]image.Point, 0, len(keys))
			if tc.contacts {
				for _, contact := range a.dm.contacts {
					r, ok := tt.Find("会话：" + contact.User.Nickname)
					if !ok {
						t.Fatal("missing contact row")
					}
					points = append(points, image.Pt(int(r.X+28), int(r.Y+r.H/2)))
				}
			}
			header, ok := tt.Find(chat.user.Nickname)
			if !ok {
				t.Fatal("missing chat header")
			}
			points = append(points, image.Pt(int(header.X-28), int(header.Y+header.H/2)))
			countAvatars := func() int {
				frame := tt.Image()
				n := 0
				for _, point := range points {
					pixel := frame.RGBAAt(point.X, point.Y)
					if pixel.R > 200 && pixel.G < 32 && pixel.B < 32 {
						n++
					}
				}
				return n
			}
			if n := countAvatars(); n != len(keys) {
				t.Fatalf("avatar fixture painted %d of %d bitmaps", n, len(keys))
			}
			for _, key := range keys {
				a.art.peek(key).bmp.ReleasePixels()
			}
			snapshot := a.dm.contacts[1]
			snapshot.User.Nickname += "更新"
			if !a.mergePrivateContacts([]netease.PrivateConversation{snapshot}, false) {
				t.Fatal("snapshot did not exercise a real redraw")
			}
			tt.Frame()
			if n := countAvatars(); n != len(keys) || len(a.art.busy) != 0 {
				t.Fatal("releasing CPU pixels or redrawing unchanged URLs blanked the avatars")
			}
			a.art.limit = len(keys) * len(data)
			now = now.Add(time.Second)
			for _, name := range []string{"cover-one", "cover-two", "cover-three"} {
				addArt(name)
			}
			for _, key := range keys {
				if a.art.art[key] != nil {
					t.Fatal("the pressure fixture did not evict every targeted avatar")
				}
			}
			runtime.GC()
			tt.Frame()
			if n := countAvatars(); n != len(keys) || len(a.art.busy) != 0 {
				reloads := len(a.art.busy)
				for range reloads {
					select {
					case fn := <-posted:
						fn()
					case <-time.After(2 * time.Second):
						t.Fatal("local avatar reload did not finish")
					}
				}
				tt.Frame()
				tt.Advance(artFade.Duration)
				tt.Frame()
				t.Fatalf("eviction painted %d/%d, started %d reloads and recovered to %d/%d", n, len(keys), reloads, countAvatars(), len(keys))
			}
			a.closePrivateMessages()
			tt.Frame()
			tt.Advance(time.Second)
			tt.Frame()
			now = now.Add(time.Second)
			for _, name := range []string{"new-one", "new-two", "new-three"} {
				addArt(name)
			}
			runtime.GC()
			for _, key := range keys {
				if a.art.peek(key) != nil {
					t.Fatal("the closed panel kept an evicted avatar alive")
				}
			}
		})
	}
}

// A new avatar URL used to show a placeholder while its image loaded, even when the old image was still available.
func TestPrivateAvatarsKeepDisplayedImageUntilReplacementLoads(t *testing.T) {
	data, err := os.ReadFile("testdata/sticker-motion.webp")
	if err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{1, 2} {
		t.Run(fmt.Sprintf("%d contacts", count), func(t *testing.T) {
			a, _ := privateTestApp(t)
			// The chat header and contact row share one load per avatar URL.
			posts := make(chan func(), count)
			a.art.post = func(fn func()) { posts <- fn }
			old := image.NewRGBA(image.Rect(0, 0, 40, 40))
			green := color.RGBA{G: 255, A: 255}
			draw.Draw(old, old.Bounds(), &image.Uniform{C: green}, image.Point{}, draw.Src)
			a.art.add("old-avatar", &artwork{bmp: ui.NewBitmap(old), used: time.Now()})
			for i := range a.dm.contacts {
				a.dm.contacts[i].User.AvatarURL = "old-avatar"
			}
			a.openPrivateMessages(&a.dm.contacts[0].User)
			chat := a.dm.chats[a.dm.selected]
			chat.user.Nickname = "替换头像的聊天头部"
			tt := ui.NewTester(a.view, 1180, 780)
			privateShowContents(tt)
			tt.Advance(time.Second)
			var points []image.Point
			for _, contact := range a.dm.contacts {
				r, ok := tt.Find("会话：" + contact.User.Nickname)
				if !ok {
					t.Fatal("contact row did not mount")
				}
				points = append(points, image.Pt(int(r.X+28), int(r.Y+r.H/2)))
			}
			header, ok := tt.Find(chat.user.Nickname)
			if !ok {
				t.Fatal("chat header did not mount")
			}
			points = append(points, image.Pt(int(header.X-28), int(header.Y+header.H/2)))
			check := func(replaced bool) {
				t.Helper()
				frame := tt.Image()
				for i, point := range points {
					want := green
					if replaced && (i < count || i == len(points)-1) {
						want = color.RGBA{R: 255, A: 255}
					}
					got := frame.RGBAAt(point.X, point.Y)
					if abs32(float32(got.R)-float32(want.R)) > 32 || abs32(float32(got.G)-float32(want.G)) > 32 || got.B > 32 {
						t.Fatalf("avatar %d: got %v, want %v", i, got, want)
					}
				}
			}
			check(false)
			var updates []netease.PrivateConversation
			dir := t.TempDir()
			for i := range count {
				item := a.dm.contacts[i]
				item.User.AvatarURL = filepath.Join(dir, fmt.Sprintf("avatar-%d.webp", i))
				if err := os.WriteFile(item.User.AvatarURL, data, 0600); err != nil {
					t.Fatal(err)
				}
				updates = append(updates, item)
			}
			if !a.mergePrivateContacts(updates, false) {
				t.Fatal("avatar URL update was not applied")
			}
			tt.Frame()
			check(false)
			for range count {
				select {
				case apply := <-posts:
					apply()
				case <-time.After(5 * time.Second):
					t.Fatal("replacement image did not load")
				}
			}
			tt.Frame()
			check(true)
		})
	}
}

// A newly selected user or account must never display the previous user's image while loading.
func TestPrivateAvatarKeepsImageOnlyForSameIdentity(t *testing.T) {
	for _, changeAccount := range []bool{false, true} {
		t.Run(fmt.Sprintf("change account=%t", changeAccount), func(t *testing.T) {
			a, _ := privateTestApp(t)
			user := netease.MessageUser{UserID: 42, AvatarURL: "loaded-avatar"}
			green := color.RGBA{G: 255, A: 255}
			old := image.NewRGBA(image.Rect(0, 0, 40, 40))
			draw.Draw(old, old.Bounds(), &image.Uniform{C: green}, image.Point{}, draw.Src)
			a.art.add(user.AvatarURL, &artwork{bmp: ui.NewBitmap(old), used: time.Now()})
			a.art.add("waiting-avatar", &artwork{failed: true, retryAt: time.Now().Add(time.Hour)})
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Row(c).Children(func() {
					a.privateAvatar(c, user, 40, a.inkOn(false).hover, true)
				})
			}, 100, 100)
			if got := tt.Image().RGBAAt(20, 20); got != green {
				t.Fatalf("first avatar pixel = %v, want %v", got, green)
			}
			user.AvatarURL = "waiting-avatar"
			if changeAccount {
				a.accountGen++
			} else {
				user.UserID++
			}
			tt.Frame()
			if got := tt.Image().RGBAAt(20, 20); got == green {
				t.Fatal("a different user or account inherited the old avatar")
			}
		})
	}
}
