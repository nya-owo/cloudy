package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"netease-music-mygo/internal/netease"
)

// A message goes as a form, its images as the files of it, with what names
// the app, and an answer that it didn't go says why.
func TestFeedbackPost(t *testing.T) {
	var text, file string
	answer := "<html>Thanks!</html>"
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Referer") != feedbackFrom {
			t.Errorf("sent from %q", r.Header.Get("Referer"))
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		text = r.FormValue("反馈内容")
		if f, _, err := r.FormFile("attachment"); err == nil {
			b, _ := io.ReadAll(f)
			file = string(b)
		}
		w.WriteHeader(status)
		io.WriteString(w, answer)
	}))
	defer srv.Close()
	was := feedbackTo
	feedbackTo = srv.URL
	defer func() { feedbackTo = was }()

	path := filepath.Join(t.TempDir(), "shot.png")
	os.WriteFile(path, []byte("picture"), 0o644)
	if err := postFeedback("歌词错位了", "0.1.24", []string{path}); err != nil {
		t.Fatal(err)
	}
	if text != "歌词错位了" || file != "picture" {
		t.Errorf("got %q with %q", text, file)
	}
	status = http.StatusBadGateway
	if err := postFeedback("再一次", "0.1.24", nil); err == nil {
		t.Error("a refusal went unnoticed")
	}
	status = http.StatusOK
	// As FormSubmit answers the first message to a mailbox.
	answer = "<p>This form needs Activation. We've sent you an email containing an 'Activate Form' link.</p>"
	err := postFeedback("第一次", "0.1.24", nil)
	if !errors.Is(err, errFeedbackInactive) {
		t.Errorf("activation went unnoticed: %v", err)
	}
	if !strings.Contains(feedbackFailure(err), "激活") {
		t.Errorf("it says %q", feedbackFailure(err))
	}
}

// A message that goes leaves the card at once. One that comes back joins what
// has been written since, and the images with it.
func TestFeedbackComesBackIfItDidntGo(t *testing.T) {
	a := newTestApp()
	f := &a.feedback
	f.sending = true
	f.text = "刚写的"
	a.feedbackSent("先写的", []feedbackImage{{path: "a"}}, errors.New("down"))
	if f.sending || !f.failed || f.text != "先写的\n\n刚写的" || len(f.images) != 1 {
		t.Errorf("%+v", f)
	}
	f.text, f.images = "", nil
	f.sending = true
	a.feedbackSent("写的", nil, nil)
	if f.sending || f.failed || f.text != "" || a.toast == "" {
		t.Errorf("%+v", f)
	}
}

// Waiting too long for an answer says so, rather than that it failed.
func TestFeedbackTimeoutSaysSo(t *testing.T) {
	err := &url.Error{Op: "Post", URL: feedbackTo, Err: context.DeadlineExceeded}
	if got := feedbackFailure(err); !strings.Contains(got, "太慢") {
		t.Errorf("it says %q", got)
	}
}

// Images stop at what a message can hold, and say so.
func TestFeedbackLimits(t *testing.T) {
	a := newTestApp()
	a.addFeedbackImages([]feedbackImage{{path: "a", size: 6 << 20}, {path: "b", size: 6 << 20}}, false)
	if len(a.feedback.images) != 1 || !a.feedback.failed {
		t.Errorf("took %d images of 12 MB, failed %v", len(a.feedback.images), a.feedback.failed)
	}
	a = newTestApp()
	var many []feedbackImage
	for range feedbackImages + 2 {
		many = append(many, feedbackImage{path: "a", size: 1})
	}
	a.addFeedbackImages(many, false)
	if len(a.feedback.images) != feedbackImages {
		t.Errorf("took %d images", len(a.feedback.images))
	}
}

// A playlist's songs are found by what they are called and who sings them,
// sorted without touching the playlist, and its search ends with its page.
func TestPlaylistSearchAndSort(t *testing.T) {
	a := newTestApp()
	a.route = route{page: pagePlaylist, id: 1}
	song := func(id int64, name, singer string, ms int64) netease.Track {
		return netease.Track{ID: id, Name: name, Duration: ms, Artists: []netease.Artist{{Name: singer}}}
	}
	pl := &netease.Playlist{ID: 1, Tracks: []netease.Track{song(1, "晴天", "周杰伦", 200000), song(2, "Anchor", "Novo Amor", 300000), song(3, "稻香", "周杰伦", 100000)}}

	if got := a.listShown(pl.Tracks); len(got) != 3 {
		t.Fatalf("%d songs", len(got))
	}
	a.plView.query = "周杰伦"
	if got := a.listShown(pl.Tracks); len(got) != 2 {
		t.Errorf("found %d by their singer", len(got))
	}
	a.plView.query = "anchor"
	if got := a.listShown(pl.Tracks); len(got) != 1 || got[0].ID != 2 {
		t.Errorf("found %v, in another case", got)
	}
	a.plView.query, a.plView.sort = "", 3
	if got := a.listShown(pl.Tracks); got[0].ID != 2 || got[2].ID != 3 {
		t.Errorf("by length: %v", got)
	}
	if pl.Tracks[0].ID != 1 {
		t.Error("sorting moved the playlist's own songs")
	}
	a.route = route{page: pagePlaylist, id: 2}
	if a.listShown(pl.Tracks); a.plView.sort != 0 {
		t.Error("the next playlist opened sorted as the last one")
	}
}

// Leaving a searched playlist for an artist page and coming back keeps its
// search and filtered songs; previously switching routes reset plView (#100).
func TestPlaylistStateRestoredOnReturn(t *testing.T) {
	a := newTestApp()
	song := func(id int64, name, singer string) netease.Track {
		return netease.Track{ID: id, Name: name, Artists: []netease.Artist{{Name: singer}}}
	}
	plTracks := []netease.Track{
		song(1, "晴天", "周杰伦"),
		song(2, "Anchor", "Novo Amor"),
		song(3, "稻香", "周杰伦"),
	}
	artistTracks := []netease.Track{song(10, "七里香", "周杰伦")}

	a.route = route{page: pagePlaylist, id: 1}
	a.listShown(plTracks)
	a.plView.query = "周杰伦"
	before := a.listShown(plTracks)
	if len(before) != 2 {
		t.Fatalf("expected 2 matches before leaving, got %d", len(before))
	}

	a.route = route{page: pageArtist, id: 42}
	if got := a.listShown(artistTracks); len(got) != 1 || a.plView.query != "" {
		t.Fatalf("artist page got %d tracks with query %q", len(got), a.plView.query)
	}

	a.route = route{page: pagePlaylist, id: 1}
	after := a.listShown(plTracks)
	if a.plView.query != "周杰伦" {
		t.Fatalf("returning to playlist lost query: got %q", a.plView.query)
	}
	if len(after) != len(before) || after[0].ID != before[0].ID || after[1].ID != before[1].ID {
		t.Errorf("returning to playlist changed filtered songs: before %v, after %v", before, after)
	}

	a.route = route{page: pagePlaylist, id: 2}
	if got := a.listShown(plTracks); a.plView.query != "" || len(got) != len(plTracks) {
		t.Errorf("first visit to another playlist got query %q and %d tracks", a.plView.query, len(got))
	}
}

// Stashed list views stop growing at maxNavHistory, dropping the oldest route
// first (#100).
func TestPlaylistStateEvictsOldest(t *testing.T) {
	a := newTestApp()
	tracks := []netease.Track{{ID: 1, Name: "晴天", Artists: []netease.Artist{{Name: "周杰伦"}}}}

	for id := int64(1); id <= maxNavHistory+2; id++ {
		a.route = route{page: pagePlaylist, id: id}
		a.listShown(tracks)
		a.plView.query = fmt.Sprintf("q%d", id)
	}
	if len(a.plViews) != maxNavHistory || len(a.plViewOrder) != maxNavHistory {
		t.Fatalf("stash has %d views and %d order entries, want %d", len(a.plViews), len(a.plViewOrder), maxNavHistory)
	}

	// Route 2 is the oldest still stashed; returning to it should restore it
	// without evicting it first.
	a.route = route{page: pagePlaylist, id: 2}
	a.listShown(tracks)
	if a.plView.query != "q2" {
		t.Errorf("oldest kept route lost query: got %q, want %q", a.plView.query, "q2")
	}

	// Route 1 was evicted, so opening it starts fresh.
	a.route = route{page: pagePlaylist, id: 1}
	a.listShown(tracks)
	if a.plView.query != "" {
		t.Errorf("evicted route kept query %q", a.plView.query)
	}
}

// Every style can be told from the others, and has something to show. A
// language counts as one wherever its group sits, even when it shows a glyph.
func TestRoamStyles(t *testing.T) {
	seen := map[string]bool{}
	for _, g := range roamGroups {
		for _, s := range g.styles {
			if seen[s.label] {
				t.Errorf("%s twice", s.label)
			}
			seen[s.label] = true
			if s.words == "" || s.icon == nil && s.glyph == "" {
				t.Errorf("%s finds or shows nothing", s.label)
			}
			if isRoamLanguage(s.label) != (g.title == "语种") {
				t.Errorf("%s: language %v in %s", s.label, isRoamLanguage(s.label), g.title)
			}
		}
	}
	if len(seen) != 42 {
		t.Errorf("%d styles", len(seen))
	}
	if !isRoam(styleRadio("摇滚")) || radioLabel(styleRadio("摇滚")) != "风格漫游 · 摇滚" {
		t.Errorf("a style radio reads %q", radioLabel(styleRadio("摇滚")))
	}
}

// A style deals its songs a few at a time, none twice, and none that can't play.
func TestRoamerDealsEachSongOnce(t *testing.T) {
	var songs []netease.Track
	for id := range int64(20) {
		songs = append(songs, netease.Track{ID: id + 1, Privilege: &netease.Privilege{Pl: 320000}})
	}
	songs = append(songs, songs[0])
	r := &roamer{pools: map[string][]netease.Playlist{}, dealing: map[string][]netease.Track{"摇滚": songs}, played: map[int64]bool{}}
	dealt := map[int64]bool{}
	for dealt2 := 0; dealt2 < 2; dealt2++ {
		got, err := r.next("摇滚")
		if err != nil {
			t.Fatal(err)
		}
		if len(got) == 0 || len(got) > roamBatch {
			t.Fatalf("dealt %d songs", len(got))
		}
		for _, tr := range got {
			if dealt[tr.ID] {
				t.Errorf("song %d twice", tr.ID)
			}
			dealt[tr.ID] = true
		}
	}
}

// Ctrl+Alt+V likes the song playing, and Ctrl+V doesn't: that is for pasting.
func TestLikeKey(t *testing.T) {
	a := newTestApp()
	a.settings = defaultSettings()
	a.pl.Cue([]netease.Track{{ID: 1, Name: "一首歌", Duration: 180000}})
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyV)
	tt.Frame()
	if a.login.open {
		t.Fatal("Ctrl+V liked the song")
	}
	tt.Key(ui.Cmd|ui.Alt, ui.KeyV)
	tt.Frame()
	if !a.login.open {
		t.Error("liking, unsigned in, asks to sign in: it didn't")
	}
}
