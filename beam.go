package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
)

// A pane whose agent is at work wears a border beam, after Magic UI's
// BorderBeam and its music player demo: a gradient segment of light runs the
// card's border, and a second one half a revolution behind it, the demo's red
// beam and its wider blue one. The light says the work is still going; a pane
// at rest has none.
const (
	// beamCycle is how long one beam takes to go round the card.
	beamCycle = 6000 * time.Millisecond
)

// beamShader paints the beams on the card's border. Each pixel works out how
// far it is from the rounded edge, so only the border ring lights, and where
// it lies around the card, so the segments travel the whole way round.
//
// Its uniforms are the two colors, the first beam's angle (T, 0 at the top
// going clockwise), the strength (A), the card's corner radius, and Dark for
// the appearance the surface belongs to.
var beamShader = shader.MustCompile(`package main

var Red vec4
var Blue vec4
var T float
var A float
var Dark float
var Radius float

const TAU = 6.28318530718
const WINDOW = 0.08

// wrap is a - b wrapped to -0.5..0.5, the short way round the card.
func wrap(a float, b float) float {
	d := a - b
	return d - floor(d + 0.5)
}

// band is how strongly a beam at angle at lights the point at angle ang: full
// at its middle, nothing past half a window either way.
func band(ang float, at float) float {
	u := wrap(ang, at) / WINDOW
	s := max(1.0-abs(u), 0.0)
	return s * s * (3.0 - 2.0*s)
}

// edge is how much of the border a point d from the edge is in: full at the
// edge, nothing width in.
func edge(d float, width float) float {
	if d > 0.0 {
		return 0.0
	}
	return clamp(1.0 + d/width, 0.0, 1.0)
}

func Fragment(dst vec4, pos vec2) vec4 {
	size := rectSize()
	if size.x < 1.0 || size.y < 1.0 {
		return vec4(0.0)
	}
	half := size * 0.5
	p := pos - half
	r := Radius
	q := abs(p) - (half - vec2(r, r))
	d := length(max(q, vec2(0.0))) + min(max(q.x, q.y), 0.0) - r
	if d > 0.5 || d < -3.5 {
		return vec4(0.0)
	}
	// 0 at the top of the card, growing clockwise.
	ang := atan2(p.x, -p.y) / TAU

	red := band(ang, T) * edge(d+0.5, 1.8)
	blue := band(ang, T+0.5) * edge(d+0.5, 2.6)
	aRed := A * red * 0.9
	aBlue := A * blue * 0.9
	redCol := Red.rgb
	blueCol := Blue.rgb
	if Dark < 0.5 {
		// On a light surface the beam is colored ink, as the demo's border
		// is drawn in its own colors on a white card.
		redCol = mix(vec3(0.10, 0.10, 0.12), Red.rgb, 0.85)
		blueCol = mix(vec3(0.10, 0.10, 0.12), Blue.rgb, 0.85)
		aRed = aRed * 0.8
		aBlue = aBlue * 0.8
	}
	a := aRed + aBlue*(1.0-aRed)
	rgb := redCol*aRed + blueCol*aBlue*(1.0-aRed)
	return vec4(rgb, a)
}
`)

// beamOn turns a flag into the 0 or 1 a shader uniform takes.
func beamOn(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// beamPhaseAt is where the first beam sits around the card, from 0 to 1,
// 0 at the top and clockwise.
func beamPhaseAt(now time.Time, still bool) float32 {
	if still {
		return 0.25
	}
	return float32(math.Mod(float64(now.UnixNano())/float64(beamCycle), 1))
}

// beamSmooth clamps x to 0..1 and eases its ends.
func beamSmooth(x float32) float32 {
	x = max(0, min(x, 1))
	return x * x * (3 - 2*x)
}

// beamPair is the two beams of an appearance: the first, and the one half a
// revolution behind it, which the demo draws a little wider.
type beamPair struct {
	first, second ui.Color
}

var beamPairDark = beamPair{
	first:  ui.RGB(239, 68, 68),
	second: ui.RGB(59, 130, 246),
}

var beamPairLight = beamPair{
	first:  ui.RGB(220, 38, 38),
	second: ui.RGB(37, 99, 235),
}

// paintPaneBeam draws the beams around the pane's border. r is the pane's box
// and card says the pane is a rounded card; the light is clipped to it, so it
// rides the border rather than spilling over the canvas. still is the
// desktop's reduced-motion form: two beams, held where they are. start is
// when the pane's work began, for the light's fade-in; zero for none.
func paintPaneBeam(p *ui.Painter, r ui.Rect, tk tokens, card bool, now time.Time, still bool, start time.Time) {
	if r.W < 48 || r.H < 24 {
		return
	}
	phase := beamPhaseAt(now, still)
	fade := float32(1)
	if !still {
		// The beams move in frames that only paint, so a working pane does
		// not build its view again for every step of the light.
		p.AnimationFrameRate(60)
		// The light eases in as the work starts, as the demo's does.
		if !start.IsZero() {
			fade = beamSmooth(float32(now.Sub(start)) / float32(600*time.Millisecond))
		}
	}
	pair := beamPairDark
	if !tk.Dark {
		pair = beamPairLight
	}
	radius := float32(0)
	if card {
		radius = windowRadius
	}
	p.Clip(r, radius, func() {
		beamShader.Draw(p, r, shader.Options{Uniforms: map[string]any{
			"Red":    pair.first,
			"Blue":   pair.second,
			"T":      phase,
			"A":      fade,
			"Dark":   beamOn(tk.Dark),
			"Radius": radius,
		}})
	})
}
