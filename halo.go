package main

import (
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/shader"
)

// A task's completion sweeps a ring of light up the pane it ran in: a wide
// arc rises from the bottom edge, bends the terminal's pixels a few points
// as it passes, and leaves a thin light on the edge it climbs out of.
//
// The shader reads the terminal's own layer, so the text it moves is the
// live grid rather than a snapshot. It rules over the terminal only; the
// pane's header stays still, as a toolbar does in the effect it follows.
const (
	haloDuration      = 1000 * time.Millisecond
	haloStillDuration = 600 * time.Millisecond
)

// The four rings, inside to outside, tuned once for each appearance. Spark
// runs violet into gold, the icon's own purple at the bottom of the page.
var (
	sparkDark = [4]ui.Color{
		ui.RGB(140, 100, 255),
		ui.RGB(200, 130, 255),
		ui.RGB(255, 200, 110),
		ui.RGB(255, 235, 170),
	}
	sparkLight = [4]ui.Color{
		ui.RGB(130, 95, 240),
		ui.RGB(185, 125, 245),
		ui.RGB(245, 185, 80),
		ui.RGB(250, 215, 120),
	}
)

// haloShader draws the halo over the element it is attached to. Its
// uniforms are the frame's progress, the palette, the page's own colour
// under the terminal's transparent background, and which of the lens's
// parts run (less motion asks for the rings alone).
//
// The circle's centre sits below the pane's bottom edge, two thirds of the
// pane's width down, so a wide arc climbs into the terminal. Its radius
// starts at that distance and ends past the farthest corner; progress is
// fast at first and crawls near the top, and the light fades out by the
// end.
var haloShader = shader.MustCompile(`package main

var Ring0 vec4
var Ring1 vec4
var Ring2 vec4
var Ring3 vec4
var Bg vec4
var T float
var Dark float
var Still float
var Bend float
var Split float
var Bevel float

// ringAt blends the four rings at x, the distance from the halo's front in
// widths: ring i sits at i - 1.5, and its neighbours bleed into it.
func ringAt(x float) vec3 {
	w0 := exp(-(x+1.5)*(x+1.5)/0.64)
	w1 := exp(-(x+0.5)*(x+0.5)/0.64)
	w2 := exp(-(x-0.5)*(x-0.5)/0.64)
	w3 := exp(-(x-1.5)*(x-1.5)/0.64)
	w := w0 + w1 + w2 + w3
	if w < 0.00001 {
		return Ring0.rgb
	}
	return (Ring0.rgb*w0 + Ring1.rgb*w1 + Ring2.rgb*w2 + Ring3.rgb*w3) / w
}

func LayerEffect(pos vec2) vec4 {
	size := rectSize()
	if size.x < 1.0 || size.y < 1.0 {
		return layerAt(pos)
	}

	// The circle starts with its crest on the bottom edge and ends past the
	// farthest corner.
	origin := vec2(size.x*0.5, size.y+size.x*0.66)
	r0 := size.x * 0.66

	// A pane is smaller than the page these defaults were tuned on: keep the
	// rings wide enough to read as one wash, and shrink the bend and the
	// bevel so a short terminal is not smeared by either.
	scale := clamp(min(size.x, size.y)/320.0, 0.5, 1.0)
	width := 110.0
	bendMax := 4.0 * scale
	bevelDepth := 40.0 * scale
	bevelPush := 10.0 * scale
	lightWidth := 6.0 * scale

	far := max(distance(origin, vec2(0, 0)), distance(origin, vec2(size.x, 0)))
	r1 := far + 40.0*scale

	ease := (1.0 - pow(2.0, -9.0*T)) / (1.0 - pow(2.0, -9.0))
	strength := min(1.0, T/0.03) * (1.0 - pow(T, 2.2))
	if Still > 0.5 {
		// Less motion: the light stays where it is and fades in and out.
		ease = 0.55
		strength = 0.5 * sin(3.14159265*T)
	}
	r := r0 + (r1-r0)*ease

	d := distance(pos, origin)
	x := (d - r) / width
	a := strength * exp(-1.1*(x/2.0)*(x/2.0))
	if a <= 0.002 {
		return layerAt(pos)
	}

	dir := (pos - origin) / max(d, 0.0001)

	// One lens moves the page, not each ring: the middle squeezes and the
	// sides stretch, and the frame keeps the last 56 points straight.
	push := 0.0
	if Bend > 0.5 {
		z := x * 0.75
		push = bendMax * 2.33 * z * exp(-z*z)
		edge := min(min(pos.x, size.x-pos.x), min(pos.y, size.y-pos.y))
		hold := clamp(edge/(56.0*scale), 0.0, 1.0)
		hold = hold * hold * (3.0 - 2.0*hold)
		push = push * hold
	}

	sample := pos - dir*push

	// At the sides and the bottom the page reaches toward its edge, as
	// glass does, and a thin line of light sits where the halo meets it.
	glow := 0.0
	if Bevel > 0.5 {
		eL := pos.x
		eR := size.x - pos.x
		eB := size.y - pos.y
		dEdge := min(eL, min(eR, eB))
		if dEdge < bevelDepth {
			k := 1.0 - dEdge/bevelDepth
			inward := vec2(0, -1)
			if eL < eR && eL < eB {
				inward = vec2(1, 0)
			} else if eR < eB {
				inward = vec2(-1, 0)
			}
			sample = sample + inward*(bevelPush*k*k*a)
			glow = exp(-dEdge/lightWidth) * a
		}
	}

	sample = vec2(clamp(sample.x, 0.0, size.x), clamp(sample.y, 0.0, size.y))

	// A trace of a split at the lens's edge: red a little further out,
	// blue a little nearer. Text stays whole but for the faintest fringe.
	off := dir * push * 0.08 * Split
	cr := layerSample(sample - off)
	cg := layerSample(sample)
	cb := layerSample(sample + off)
	c := vec4(cr.r, cg.g, cb.b, cg.a)

	// The terminal draws no background of its own; the card shows through.
	// Put the page back under its text before tinting it.
	page := c.rgb + Bg.rgb*(1.0-c.a)
	ring := ringAt(x)

	var lit vec3
	if Dark > 0.5 {
		lit = vec3(1.0) - (vec3(1.0)-page)*(vec3(1.0)-ring*(a*0.45))
	} else {
		lit = mix(page, ring, a*0.24) + ring*(a*0.24*0.2)
	}
	if glow > 0.0 {
		edgeCol := mix(ring, vec3(1.0), 0.45)
		lit = lit + (edgeCol-lit)*(glow*0.6)
	}

	// Fade the whole halo over what the terminal drew, so its transparent
	// background keeps showing the card at the edges of the light.
	return mix(layerAt(pos), vec4(lit, 1.0), a)
}
`)

// startHalo begins the completion sweep, or starts it again when one is
// already running: a second task finishing in the same pane reads as a new
// sweep, not as two lights over each other.
func (p *pane) startHalo() {
	p.haloStart = time.Now()
}

// stopHalo ends a sweep early, for a pane whose agent has started new work
// before the old light was gone.
func (p *pane) stopHalo() {
	p.haloStart = time.Time{}
}

// haloAt draws the sweep at t, from 0 to 1, in the chrome's appearance.
// still is the reduced-motion form: the rings alone, held in place.
func haloAt(t float32, tk tokens, still bool) ui.Effect {
	on := func(b bool) float32 {
		if b {
			return 1
		}
		return 0
	}
	rings := sparkLight
	if tk.Dark {
		rings = sparkDark
	}
	return haloShader.Effect(shader.Options{Uniforms: map[string]any{
		"Ring0": rings[0],
		"Ring1": rings[1],
		"Ring2": rings[2],
		"Ring3": rings[3],
		"Bg":    tk.Background,
		"T":     t,
		"Dark":  on(tk.Dark),
		"Still": on(still),
		"Bend":  on(!still),
		"Split": on(!still),
		"Bevel": on(!still),
	}})
}

// haloEffect is the haloing pane's effect for this frame, or nil once the
// sweep is over. It asks for the next frame while it runs, and reads the
// appearance from the chrome's tokens so a light or dark switch moves its
// colours with the page.
func (p *pane) haloEffect(c *ui.Context, tk tokens) ui.Effect {
	if p.haloStart.IsZero() {
		return nil
	}
	dur := haloDuration
	still := c.Preferences().ReduceMotion
	if still {
		dur = haloStillDuration
	}
	elapsed := c.Now().Sub(p.haloStart)
	if elapsed >= dur {
		p.haloStart = time.Time{}
		return nil
	}
	c.AnimationFrame()
	return haloAt(float32(elapsed)/float32(dur), tk, still)
}
