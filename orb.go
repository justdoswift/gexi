package main

import (
	"math"
	"sort"
	"time"

	"github.com/egoist/mygo/ui"
)

// The sidebar's activity loader follows libraries.dev/orbs (MIT), whose
// "working" state paints particles on tilted orbits around a ghost sphere of
// dots. This is a port of the 20pt inline preset — the preset's own count,
// radius and speed tuning, not a scale of the 64pt one — onto mygo's painter.
//
// The orb is strictly monochrome: black ink on a light window, white on a
// dark one. Depth is read from size and ink alone — far dots are pale and
// small, near dots dark (or bright) and large.

const (
	orbSize float32 = 20
	// The 20pt preset clocks in at 3.9 for an orb the eye meets inline and
	// leaves at once; in a sidebar row that stays on screen the same pace
	// reads as fidgeting, so the clock runs at a calmer speed here.
	orbSpeed float64 = 1.6
	// The preset scales the base profile: 12 orbits and 40 ghost dots per
	// orbit become 3 and 10, the dot radii grow 2.4× before the sub-linear
	// size falloff below.
	orbOrbitN             = 3
	orbGhostN             = 10
	orbParticles          = 3
	orbGhostR     float32 = 2.16
	orbGhostAlpha float32 = 0.5
	orbPartR      float32 = 2.88
	orbPartRDepth float32 = 3.84
	orbMinR       float32 = 0.3
)

// orbDot is one painted dot, in the orb's own 20pt box and depth-sorted.
type orbDot struct {
	x, y, z float32
	r       float32
	// white is the ink value: 0 is the darkest ink, 1 the palest.
	white float32
	alpha float32
}

// orbWorkingFrame is one instant of the working orb. t is already in the
// preset's time scale: seconds × orbSpeed.
func orbWorkingFrame(t float64) []orbDot {
	const (
		cx = float64(orbSize) / 2
		cy = float64(orbSize) / 2
		R  = float64(orbSize) / 2 * 0.82
	)
	proj := orbProjector(t*0.12, 0.3, cx, cy, 1)
	rs := math.Pow(float64(orbSize)/300, 0.6)

	dots := make([]orbDot, 0, orbOrbitN*(orbGhostN+orbParticles))
	for orb := range orbOrbitN {
		h1 := orbHash(float64(orb), 1.7)
		h2 := orbHash(float64(orb), 5.2)
		h3 := orbHash(float64(orb), 8.9)
		ro := R * (0.45 + 0.52*h1)
		th := h1 * 2 * math.Pi
		phi := math.Acos(2*h2 - 1)
		// The orbit plane's normal n, and the basis (u, v) perpendicular to it.
		nx := math.Sin(phi) * math.Cos(th)
		ny := math.Cos(phi)
		nz := math.Sin(phi) * math.Sin(th)
		ux := -ny
		uy := nx
		uz := 0.0
		ul := math.Max(1e-6, math.Hypot(ux, uy))
		ux /= ul
		uy /= ul
		vx := ny*0 - nz*uy
		vy := nz*ux - nx*0
		vz := nx*uy - ny*ux
		speed := 0.25 + 0.55*h3
		if h3 <= 0.5 {
			speed = -speed
		}

		// The ghost path: the orbit itself, a ring of pale dots.
		for k := range orbGhostN {
			a := float64(k) / orbGhostN * 2 * math.Pi
			x, y, z := proj(
				(ux*math.Cos(a)+vx*math.Sin(a))*ro,
				(uy*math.Cos(a)+vy*math.Sin(a))*ro,
				(uz*math.Cos(a)+vz*math.Sin(a))*ro,
			)
			depth := (z/ro + 1) / 2
			dots = append(dots, orbDot{
				x:     float32(x),
				y:     float32(y),
				z:     float32(z),
				r:     orbGhostR * float32(rs),
				white: 0.72,
				alpha: orbGhostAlpha * float32(0.4+0.6*depth),
			})
		}
		// The particles doing the work: brighter and larger as they near.
		for m := range orbParticles {
			a := t*speed + float64(m)/orbParticles*2*math.Pi + h2*6
			x, y, z := proj(
				(ux*math.Cos(a)+vx*math.Sin(a))*ro,
				(uy*math.Cos(a)+vy*math.Sin(a))*ro,
				(uz*math.Cos(a)+vz*math.Sin(a))*ro,
			)
			depth := (z/ro + 1) / 2
			dots = append(dots, orbDot{
				x:     float32(x),
				y:     float32(y),
				z:     float32(z),
				r:     (orbPartR + orbPartRDepth*float32(depth)) * float32(rs),
				white: 0.3 - 0.22*float32(depth),
				alpha: 1,
			})
		}
	}

	visible := dots[:0]
	for _, d := range dots {
		if d.alpha < 0.02 {
			continue
		}
		d.r = max(orbMinR, d.r)
		visible = append(visible, d)
	}
	// Far dots first, so near ones sit on top.
	sort.Slice(visible, func(i, j int) bool { return visible[i].z < visible[j].z })
	return visible
}

// orbProjector is the orb's shared spin and tilt, seen orthographically.
func orbProjector(yaw, tilt, cx, cy, scale float64) func(x, y, z float64) (float64, float64, float64) {
	st, ct := math.Sin(tilt), math.Cos(tilt)
	sy, cyw := math.Sin(yaw), math.Cos(yaw)
	return func(x, y, z float64) (float64, float64, float64) {
		x1 := x*cyw + z*sy
		z1 := -x*sy + z*cyw
		y1 := y*ct - z1*st
		z2 := y*st + z1*ct
		return cx + x1*scale, cy - y1*scale, z2
	}
}

// orbHash is the engine's deterministic hash in [0, 1).
func orbHash(a, b float64) float64 {
	h := math.Sin(a*12.9898+b*78.233) * 43758.5453
	return h - math.Floor(h)
}

// orbPhase returns the orb's clock and keeps frames coming while it runs.
func orbPhase(c *ui.Context) float64 {
	c.After(16 * time.Millisecond)
	return float64(time.Now().UnixNano()) / 1e9 * orbSpeed
}

// drawOrb paints one frame of a thinking orb in r. The orb keeps its own
// 20pt geometry and shrinks or grows as one piece.
func drawOrb(p *ui.Painter, r ui.Rect, t float64, dark bool) {
	scale := min(r.W, r.H) / orbSize
	ox := r.X + (r.W-orbSize*scale)/2
	oy := r.Y + (r.H-orbSize*scale)/2
	for _, d := range orbWorkingFrame(t) {
		rad := d.r * scale
		p.Fill(ui.Rect{
			X: ox + d.x*scale - rad,
			Y: oy + d.y*scale - rad,
			W: 2 * rad,
			H: 2 * rad,
		}, orbInk(d.white, d.alpha, dark), rad)
	}
}

// orbInk maps an ink value to its grey: white ink on a dark window, black on
// a light one, with nearer dots at full ink and farther ones pale.
func orbInk(white, alpha float32, dark bool) ui.Color {
	white = min(1, max(0, white))
	g := uint8(white*255 + 0.5)
	if dark {
		g = uint8((1-white)*255 + 0.5)
	}
	return ui.RGBA(g, g, g, alpha)
}
