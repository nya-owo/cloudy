package app

// savedEffects are the effects that cost power, as they were before the
// power saver turned them off.
type savedEffects struct {
	LiquidGlass  bool `json:"liquidGlass"`
	GlobalGlass  bool `json:"globalGlass,omitempty"`
	GlassPanels  bool `json:"glassPanels"`
	SidebarGlass bool `json:"sidebarGlass,omitempty"`
	MainGlass    bool `json:"mainGlass,omitempty"`
	BarSpin      bool `json:"barSpin"`
	NpLight      bool `json:"npLight"`
	Fog          bool `json:"fog"`
	Smoke        bool `json:"smoke"`
}

// setPowerSaver turns the effects that cost power off, keeping how they
// were, or puts them back as they were.
func (a *app) setPowerSaver(on bool) {
	s := &a.settings
	if on == s.PowerSaver {
		return
	}
	if on {
		s.SaverBefore = &savedEffects{
			LiquidGlass: s.LiquidGlass, GlobalGlass: s.GlobalGlass, GlassPanels: s.GlassPanels,
			SidebarGlass: s.SidebarGlass, MainGlass: s.MainGlass,
			BarSpin: s.BarSpin, NpLight: s.NpLight, Fog: s.Fog, Smoke: s.Smoke,
		}
		s.GlobalGlass, s.SidebarGlass, s.MainGlass, s.GlassPanels = false, false, false, false
		s.LiquidGlass, s.BarSpin, s.NpLight, s.Fog, s.Smoke = false, false, false, false, false
	} else if b := s.SaverBefore; b != nil {
		s.GlobalGlass, s.SidebarGlass, s.MainGlass, s.GlassPanels = b.GlobalGlass, b.SidebarGlass, b.MainGlass, b.GlassPanels
		s.LiquidGlass, s.BarSpin, s.NpLight, s.Fog, s.Smoke = b.LiquidGlass, b.BarSpin, b.NpLight, b.Fog, b.Smoke
	}
	s.PowerSaver = on
	if !on {
		s.SaverBefore = nil
	}
	a.changed()
}

// leavePowerSaver ends the saver as the listener turns an effect back on by
// hand, leaving the others as they are now.
func (a *app) leavePowerSaver() {
	s := &a.settings
	if !s.PowerSaver {
		return
	}
	if s.GlassPanels || s.GlobalGlass || s.SidebarGlass || s.MainGlass || s.LiquidGlass || s.BarSpin || s.NpLight || s.Fog || s.Smoke {
		s.PowerSaver, s.SaverBefore = false, nil
	}
}
