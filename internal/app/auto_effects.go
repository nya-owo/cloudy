package app

import (
	"strings"
	"time"

	"netease-music-mygo/internal/lyrics"
	"netease-music-mygo/internal/netease"
	"netease-music-mygo/internal/player"
)

// Effects that choose themselves, for each song: 智能音效 by its genre,
// 情绪调音 by its mood, as NetEase's song wiki tags them, and else by what
// its lyrics say or how it sounds. What is chosen glides in over a couple
// of seconds, as the song starts.

// The presets that choose for each song.
const (
	smartPreset = "smart"
	moodPreset  = "mood"
)

// songTags reads a song's genres and moods; tests replace it.
var songTags = netease.SongTags

// autoState is what was chosen for the song playing.
type autoState struct {
	song   int64           // the song it was chosen for
	fx     *player.Effects // what was chosen, nil until it was
	note   string          // what it was chosen by, as the pane says it
	asked  int64           // the song whose tags are being read
	cache  map[int64][2][]string
	listen time.Time // when the sound began to be listened to, for want of tags
	heard  []float32 // the sound's levels as it was listened to
}

// autoOn reports whether the effects choose themselves.
func (a *app) autoOn() bool {
	p := a.settings.EffectsPreset
	return a.settings.Effects.On && (p == smartPreset || p == moodPreset)
}

// currentEffects are the effects what plays goes through: the listener's,
// or those chosen for the song, with the listener's placing round the head.
func (a *app) currentEffects() player.Effects {
	e := a.settings.Effects
	if !a.autoOn() || a.auto.fx == nil {
		return e
	}
	chosen := *a.auto.fx
	chosen.On, chosen.Spatial, chosen.Orbit = true, e.Spatial, e.Orbit
	return chosen
}

// autoFollow chooses effects for a song that started, while they choose
// themselves: at once from what is known of it, or once its tags come.
func (a *app) autoFollow() {
	if !a.autoOn() {
		return
	}
	tr := a.pl.Current()
	if tr == nil || tr.ID == a.auto.song {
		a.autoListen()
		return
	}
	a.auto.song, a.auto.heard, a.auto.listen = tr.ID, nil, time.Time{}
	if tr.ID <= 0 || tr.ProgramID != 0 {
		a.autoChoose(nil, nil, tr)
		return
	}
	if t, ok := a.auto.cache[tr.ID]; ok {
		a.autoChoose(t[0], t[1], tr)
		return
	}
	a.auto.asked = tr.ID
	id, song := tr.ID, *tr
	go func() {
		genres, moods, _ := songTags(id)
		a.post(func() {
			if a.auto.cache == nil {
				a.auto.cache = map[int64][2][]string{}
			}
			a.auto.cache[id] = [2][]string{genres, moods}
			a.trimAutoCache()
			if a.auto.song == id && a.autoOn() {
				a.autoChoose(genres, moods, &song)
			}
		})
	}()
}

// autoChoose chooses the song's effects from its genres and moods, else
// from its lyrics, else, a few seconds in, from how it sounds.
func (a *app) autoChoose(genres, moods []string, tr *netease.Track) {
	var fx player.Effects
	var note string
	if a.settings.EffectsPreset == smartPreset {
		p, by, ok := presetForGenres(genres)
		if !ok {
			a.autoStartListening("正在聆听这首歌的特点…")
			return
		}
		fx, note = presetEffects(p), "已识别曲风："+by
	} else {
		lines, _ := a.pl.Lyrics()
		m, by := moodOf(moods, lines)
		if m == nil {
			a.autoStartListening("正在感受这首歌的情绪…")
			return
		}
		fx, note = m.fx, "当前情绪："+m.label+by
	}
	a.autoApply(fx, note)
}

// autoApply glides to fx, chosen for the song by note.
func (a *app) autoApply(fx player.Effects, note string) {
	fx.On = true
	a.auto.fx, a.auto.note, a.auto.listen = &fx, note, time.Time{}
	a.pl.GlideEffects(a.currentEffects())
	a.redraw()
}

// autoListen, for a song NetEase says nothing of, listens to its first
// seconds, then chooses by how it sounds: much bass, a voice to the fore,
// bright highs, or none of these.
func (a *app) autoListen() {
	if a.auto.listen.IsZero() {
		return
	}
	if lv, ok := a.pl.Levels(); ok {
		if a.auto.heard == nil {
			a.auto.heard = make([]float32, len(lv))
		}
		for i, v := range lv {
			a.auto.heard[i] += v
		}
	}
	if time.Since(a.auto.listen) < 6*time.Second || a.auto.heard == nil {
		return
	}
	p, label := presetForSound(a.auto.heard)
	if a.settings.EffectsPreset == smartPreset {
		a.autoApply(presetEffects(p), "按声音特点："+label)
		return
	}
	m := moodForSound(a.auto.heard)
	a.autoApply(m.fx, "当前情绪："+m.label+"（按声音特点）")
}

// presetEffects are the effects of a preset.
func presetEffects(p effectPreset) player.Effects {
	return player.Effects{On: true, Bands: p.bands, Bass: p.bass, Width: p.width, Vocal: p.vocal, Loudness: p.loud}
}

// presetByID is the preset of id, the first for none.
func presetByID(id string) effectPreset {
	for _, p := range effectPresets {
		if p.id == id {
			return p
		}
	}
	return effectPresets[0]
}

// genrePresets are the words of NetEase's genres, as "摇滚-另类摇滚", and
// the preset each calls for, the first of them found.
var genrePresets = []struct {
	words  []string
	preset string
}{
	{[]string{"摇滚", "金属", "朋克", "Rock", "Metal"}, "rock"},
	{[]string{"电子", "舞曲", "EDM", "House", "Techno", "Trance"}, "electronic"},
	{[]string{"说唱", "嘻哈", "Rap", "Hip", "Trap"}, "bass"},
	{[]string{"爵士", "蓝调", "Jazz", "Blues", "灵魂", "Soul"}, "jazz"},
	{[]string{"古典", "交响", "器乐", "歌剧", "Classical"}, "classical"},
	{[]string{"民谣", "乡村", "Folk", "Country", "独立"}, "vocal"},
	{[]string{"轻音乐", "New Age", "新世纪", "氛围"}, "night"},
	{[]string{"R&B", "流行", "Pop", "ACG", "国风", "影视"}, "pop"},
}

// presetForGenres is the preset of the first of genres it knows, and the
// genre as it is said.
func presetForGenres(genres []string) (effectPreset, string, bool) {
	for _, g := range genres {
		for _, gp := range genrePresets {
			for _, w := range gp.words {
				if strings.Contains(strings.ToLower(g), strings.ToLower(w)) {
					return presetByID(gp.preset), g, true
				}
			}
		}
	}
	return effectPreset{}, "", false
}

// presetForSound is the preset of how a song sounds, its levels summed
// over its first seconds in the visualizers' 24 bands, low to high.
func presetForSound(heard []float32) (effectPreset, string) {
	low, mid, high := bandShare(heard)
	switch {
	case low > 0.42:
		return presetByID("bass"), "低频饱满"
	case mid > 0.42:
		return presetByID("vocal"), "人声突出"
	case high > 0.3:
		return presetByID("classical"), "高频明亮"
	}
	return presetByID("pop"), "均衡"
}

// bandShare is how much of the sound is low (under about 250 Hz), in the
// voice's range, and high (over about 4 kHz).
func bandShare(heard []float32) (low, mid, high float64) {
	var sum float64
	for i, v := range heard {
		x := float64(v)
		sum += x
		switch {
		case i < 6:
			low += x
		case i < 17:
			mid += x
		default:
			high += x
		}
	}
	if sum == 0 {
		return 0, 0, 0
	}
	return low / sum, mid / sum, high / sum
}

// mood is a feeling a song has, and how it is tuned for it.
type mood struct {
	label string
	tags  []string // NetEase's words for it
	words []string // what its lyrics say of it
	fx    player.Effects
}

// moods are the feelings 情绪调音 knows, each tuned as it calls for: the
// sad warm and near, in a small room; the passionate full and wide; the
// calm soft and even; the sweet with the voice to the fore; the happy
// bright.
var moods = []mood{
	{"伤感", []string{"伤感", "思念", "孤独", "怀旧", "失恋", "遗憾", "寂寞", "忧郁"},
		[]string{"泪", "孤单", "离开", "再见", "回忆", "遗憾", "想你", "心碎", "分手"},
		player.Effects{Bands: [10]float64{1, 1.5, 2, 1.5, 0.5, 0, 0.5, 0, -1.5, -2.5}, Vocal: 0.35, Width: 0.2, Room: "room", Treble: -0.15}},
	{"热血", []string{"励志", "热血", "燃", "兴奋", "运动", "战斗"},
		[]string{"梦想", "燃烧", "冲", "战", "飞翔", "无畏", "勇敢"},
		player.Effects{Bands: [10]float64{4, 3, 1, 0, -1, 0, 1.5, 2.5, 3, 3}, Bass: 0.35, Treble: 0.25, Width: 0.3, Loudness: 0.3}},
	{"治愈", []string{"治愈", "放松", "安静", "助眠", "清新", "舒缓", "温暖"},
		[]string{"晚安", "温柔", "微风", "安静", "星空", "梦"},
		player.Effects{Bands: [10]float64{1, 1, 0.5, 0, 0, 0, -0.5, -1.5, -2.5, -3}, Treble: -0.2, Width: 0.15, Loudness: 0.4, Room: "room"}},
	{"浪漫", []string{"浪漫", "甜蜜", "爱情", "心动", "告白"},
		[]string{"爱你", "喜欢你", "心动", "拥抱", "永远"},
		player.Effects{Bands: [10]float64{0, 0.5, 1, 1.5, 2, 2.5, 2, 1, 0.5, 0}, Vocal: 0.45, Width: 0.2, Room: "room"}},
	{"欢快", []string{"快乐", "欢快", "派对", "KTV", "开心", "夏日"},
		[]string{"快乐", "开心", "阳光", "笑", "跳舞", "派对"},
		player.Effects{Bands: [10]float64{2, 1.5, 1, 1, 0, 1, 2, 2.5, 2.5, 2}, Bass: 0.2, Treble: 0.2, Width: 0.25}},
}

// moodOf is a song's mood: the first NetEase tags it with, else the one
// its lyrics speak of most; and what it was known by.
func moodOf(tags []string, lines []lyrics.Line) (*mood, string) {
	for _, t := range tags {
		for i := range moods {
			for _, w := range moods[i].tags {
				if strings.Contains(t, w) {
					return &moods[i], ""
				}
			}
		}
	}
	var text strings.Builder
	for _, l := range lines {
		text.WriteString(l.Text)
		text.WriteString(l.Translation)
	}
	words := text.String()
	best, most := -1, 0
	for i := range moods {
		n := 0
		for _, w := range moods[i].words {
			n += strings.Count(words, w)
		}
		if n > most {
			best, most = i, n
		}
	}
	if best < 0 || most < 2 {
		return nil, ""
	}
	return &moods[best], "（按歌词）"
}

// moodForSound is the mood of how a song sounds: much low, passionate;
// bright, happy; little of either, calm.
func moodForSound(heard []float32) *mood {
	low, _, high := bandShare(heard)
	switch {
	case low > 0.4:
		return &moods[1]
	case high > 0.3:
		return &moods[4]
	}
	return &moods[2]
}

// autoStartListening listens to the song playing, saying so, a few times
// a second until it has heard enough to choose.
func (a *app) autoStartListening(note string) {
	a.auto.listen, a.auto.note, a.auto.heard = time.Now(), note, nil
	song := a.auto.song
	var tick func()
	tick = func() {
		time.AfterFunc(250*time.Millisecond, func() {
			a.post(func() {
				if a.auto.song != song || a.auto.listen.IsZero() || !a.autoOn() {
					return
				}
				a.autoListen()
				tick()
			})
		})
	}
	tick()
	a.redraw()
}
