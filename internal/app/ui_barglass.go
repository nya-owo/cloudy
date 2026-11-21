package app

import (
	"sync"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
)

// barGlass is the liquid frosted glass of the floating bars and, with global
// glass on, of the app's panes. It follows the glass plugin's optics (its
// lens, normal and tone, see glass.hlsl), with these to tell it from them:
//
//   - it blurs what shows through into soft frost, heavier on panes than on
//     the narrow floating bars, so underlying text and covers dissolve cleanly
//     behind foreground labels and controls;
//   - its curve reaches further in, on a rounder profile, and bends further,
//     so that the edges of the capsule and panels visibly swim;
//   - red and blue bend a little apart, as through a thick lens;
//   - what shows through is tone-mapped and softened by a frosted veil so
//     bright covers in dark mode and dark covers in light mode keep contrast.
//
// clear and bend, from 0 to 1, are the listener's: clearer, the frost is
// lighter and lets more color through while staying legible; bent more, the
// edge bends further, and its curve reaches further in.
type barGlass struct {
	dark        bool
	clear, bend float32
	surface     bool // use frosted panel tuning on menus, dialogs and settings
	translucent bool // unpremultiply translucent cover backdrops before toning
}

func (a *app) interfaceGlass(dark bool) barGlass {
	return barGlass{dark: dark, clear: float32(a.settings.GlassClear), bend: float32(a.settings.GlassBend), surface: true, translucent: true}
}

// floatGlass is the glass of the bars floating over the page: the player bar
// and the batch bar. Under the cover background, the page is translucent
// over the window's material, so the glass has to take what it shows through
// as translucent too: read as opaque, the clear parts of the page are black,
// and in light mode the middle of the bar went dark.
func (a *app) floatGlass(dark bool) barGlass {
	return barGlass{dark: dark, clear: float32(a.settings.GlassClear), bend: float32(a.settings.GlassBend),
		translucent: a.settings.GlobalGlass || a.settings.mainGlass()}
}

// How the glass looks, whatever the listener chose.
const (
	barGlassSpread  = 0.05 // how far apart red and blue bend, of the bend
	barGlassRim     = 0.48 // how lit the rim is
	barGlassRimWide = 1.1  // how wide it is, in DIPs
)

const barGlassSource = `package main

var Bend float
var Bezel float
var Spread float
var Rim float
var RimWide float
var Tone vec4
var Ink vec4
var Corner float
var Surface float
var Translucent float
var Veil float
var Sheen float

// edge returns the inward distance and outward normal of a rounded rectangle.
func edge(pos, half vec2) vec3 {
	q := pos - half
	r := min(Corner, min(half.x, half.y))
	a := abs(q) - (half - vec2(r))
	m := max(a, vec2(0))
	inside := r - length(m) - min(max(a.x, a.y), 0)
	n := vec2(0)
	if m.x > 0.0 || m.y > 0.0 {
		n = sign(q) * m / length(m)
	} else if a.x > a.y {
		n.x = sign(q.x)
	} else {
		n.y = sign(q.y)
	}
	return vec3(n, max(inside, 0))
}

// lens returns how far the surface bends what shows through at x, 0 at the
// edge of the curve to 1 where it starts, the glass plugin's Snell's law over
// a surface that is a circle's rather than Apple's squircle: rounder, it
// keeps bending further in.
func lens(x float) float {
	u := clamp(1.0-x, 0.0, 1.0)
	v := sqrt(max(1.0-u*u, 0.0001))
	n := sqrt(u*u + v*v)
	s := u / n
	c := v / n
	st := s / 1.5
	ct := sqrt(1.0 - st*st)
	return (s*ct - c*st) / (c*ct + s*st) / 1.118034
}

func tone(c vec3) vec3 {
	l := dot(c, vec3(0.2126, 0.7152, 0.0722))
	t := Tone.x + (Tone.y-Tone.x)*(1.0-pow(max(1.0-l, 0.0), Tone.z))
	k := (Tone.y - Tone.x) * Tone.w
	return clamp(vec3(t)+(c-vec3(l))*k, vec3(0), vec3(1))
}

func Fragment(dst vec4, pos vec2) vec4 {
	half := rectSize() / 2.0
	e := edge(pos, half)
	bezel := min(half.x, half.y) * Bezel
	if Surface > 0.0 {
		bezel = min(bezel, 36.0)
	}
	n := e.xy
	t := e.z
	bend := vec2(0)
	if t < bezel {
		bend = n * Bend * lens(t/bezel)
	}
	r := backdropSample(pos - bend*(1.0+Spread))
	g := backdropSample(pos - bend)
	b := backdropSample(pos - bend*(1.0-Spread))
	c := tone(vec3(r.r, g.g, b.b))
	if Translucent > 0.0 {
		// Unpremultiply the translucent cover backdrop before tone mapping so
		// clear parts of the page blend into the frosted tint instead of dark.
		rgb := vec3(r.r/max(r.a, 0.01), g.g/max(g.a, 0.01), b.b/max(b.a, 0.01))
		c = mix(Ink.rgb, tone(rgb), g.a)
	}
	// Frost the interior enough that foreground text stays readable over vivid
	// covers and scrolling rows, while easing the veil slightly on the outer
	// bevel so liquid edge refraction stays lively.
	bezelFrac := smoothstep(0.0, max(bezel, 1.0), t)
	veil := Veil * mix(0.76, 1.0, bezelFrac)
	if Surface <= 0.0 {
		// Under what plays and the buttons at the ends of the capsule, keep an
		// extra veil so song titles stay crisp while the middle still breathes.
		width := rectSize().x
		side := max(width*0.5-220.0, 130.0)
		under := max(1.0-smoothstep(side*0.72, side*1.02, pos.x), 0.7*smoothstep(width-side*0.6, width-side*0.35, pos.x))
		veil = max(veil, Ink.a*under)
	}
	c = mix(c, Ink.rgb, clamp(veil, 0.0, 0.92))
	// A soft frosted catchlight inside the curved bezel keeps the thick glass
	// silhouette tactile even over dark regions.
	if Sheen > 0.0 && t < bezel {
		edgeGlow := (1.0 - bezelFrac) * (1.0 - bezelFrac)
		c = mix(c, vec3(1.0), Sheen*edgeGlow*(0.35+0.65*abs(n.y)))
	}
	// The rim, a hairline lit where it faces up or down, and shaded where
	// it faces the sides.
	if Rim > 0.0 && t < 3.0*RimWide {
		x := t / RimWide
		c += (vec3(abs(n.y)) - c) * Rim * exp(-x*x)
	}
	// Output full alpha so the renderer replaces, rather than SRC_OVER-blends
	// over, the unblurred pixels beneath the pane.
	return vec4(c, 1.0)
}
`

var barGlassShader = sync.OnceValue(func() *shader.Shader { return shader.MustCompile(barGlassSource) })

// shaderOptions computes the frosted liquid glass shader options for box.
func (g barGlass) shaderOptions(box ui.Rect, radius float32) shader.Options {
	clear := clamp01(g.clear)
	bendLevel := clamp01(g.bend)

	// The lightness of black and white through the frosted glass, how it bends
	// between them, and how much of its color it keeps.
	var tone [4]float32
	var ink [4]float32
	var blur, veil, sheen float32
	rim := float32(barGlassRim)

	if g.surface {
		blur = lerp(20, 11, clear)
		if g.dark {
			tone = [4]float32{0.06, lerp(0.28, 0.48, clear), 1.3, 1.85}
			ink = [4]float32{0.10, 0.105, 0.125, 0.85}
			veil = lerp(0.56, 0.32, clear)
			sheen = 0.08
		} else {
			tone = [4]float32{lerp(0.74, 0.52, clear), 0.99, 1.15, 1.25}
			ink = [4]float32{0.965, 0.97, 0.98, 0.85}
			veil = lerp(0.56, 0.34, clear)
			sheen = 0.14
			rim = 0.32
		}
	} else {
		blur = lerp(14, 6.5, clear)
		if g.dark {
			tone = [4]float32{0.06, lerp(0.30, 0.52, clear), 1.35, 2.0}
			ink = [4]float32{0.09, 0.095, 0.115, lerp(0.84, 0.68, clear)}
			veil = lerp(0.46, 0.22, clear)
			sheen = 0.10
		} else {
			tone = [4]float32{lerp(0.70, 0.48, clear), 0.99, 1.15, 1.35}
			ink = [4]float32{0.965, 0.97, 0.98, lerp(0.84, 0.68, clear)}
			veil = lerp(0.46, 0.24, clear)
			sheen = 0.18
			rim = 0.36
		}
	}

	translucent, surface, bend := float32(0), float32(0), 144*bendLevel
	if g.translucent {
		translucent = 1
	}
	if g.surface {
		surface = 1
		bend = min(bend, 1.2*min(box.W, box.H)*bendLevel, 76*bendLevel)
	}
	return shader.Options{Radius: radius, BackdropBlur: blur, Uniforms: map[string]any{
		"Bend": bend, "Bezel": 0.33 + 0.66*bendLevel, "Spread": barGlassSpread,
		"Rim": rim, "RimWide": barGlassRimWide, "Tone": tone[:], "Ink": ink[:],
		"Corner": radius, "Surface": surface, "Translucent": translucent, "Veil": veil, "Sheen": sheen,
	}}
}

// PaintMaterial paints the liquid frosted glass over box, rounded by radii. On
// the CPU the shader, which runs interpreted, would take a good part of a
// second a frame: there it is the plugin's frosted glass, which is drawn
// natively.
func (g barGlass) PaintMaterial(p *ui.Painter, box ui.Rect, radii [4]float32) {
	if !p.Accelerated() {
		alpha := float32(0.42)
		if g.dark {
			alpha = 0.48
		}
		if g.surface {
			alpha = 0.78 // Keep words behind a software-rendered dialog from competing with its text.
		}
		tint := white.Alpha(alpha)
		if g.dark {
			tint = ui.RGBA(14, 14, 18, alpha)
		}
		if g.translucent && !g.surface {
			// The native backdrop can still show through on software rendering.
			p.Fill(box, tint, radii[0])
			p.Stroke(box, tint.Mix(white.Alpha(0.24), 0.65), radii[0], 1)
		} else {
			glass.Paint(p, box, radii[0], glass.Glass{Tint: tint})
		}
		return
	}
	barGlassShader().Draw(p, box, g.shaderOptions(box, radii[0]))
}
