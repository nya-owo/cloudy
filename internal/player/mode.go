package player

// PlayMode is how the queue plays on, as one button cycles it: in order,
// round and round, the one song over, or shuffled round and round.
type PlayMode int

const (
	ModeOrder PlayMode = iota
	ModeLoop
	ModeOne
	ModeShuffle
)

// Name is what the mode is called, for its button and the menu.
func (m PlayMode) Name() string {
	switch m {
	case ModeLoop:
		return "列表循环"
	case ModeOne:
		return "单曲循环"
	case ModeShuffle:
		return "随机播放"
	}
	return "顺序播放"
}

// Mode returns the mode the shuffle and the repeat make.
func (p *Player) Mode() PlayMode {
	switch {
	case p.repeat == RepeatOne:
		return ModeOne
	case p.shuffle:
		return ModeShuffle
	case p.repeat == RepeatAll:
		return ModeLoop
	}
	return ModeOrder
}

// SetMode plays on in mode m from the track playing.
func (p *Player) SetMode(m PlayMode) {
	shuffle := m == ModeShuffle
	if p.shuffle != shuffle {
		p.ToggleShuffle()
	}
	switch m {
	case ModeOrder:
		p.repeat = RepeatOff
	case ModeOne:
		p.repeat = RepeatOne
	default:
		p.repeat = RepeatAll
	}
}

// CycleMode goes on to the next mode: in order, round and round, the one
// song, shuffled, and back.
func (p *Player) CycleMode() PlayMode {
	m := (p.Mode() + 1) % 4
	p.SetMode(m)
	return m
}

// normalizeMode makes a shuffle and repeat set apart, as before the one
// button, one of its modes: shuffled always goes round.
func (p *Player) normalizeMode() {
	if p.shuffle && p.repeat == RepeatOff {
		p.repeat = RepeatAll
	}
}
