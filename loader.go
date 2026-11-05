package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// The sidebar's activity loader follows math-curve-loaders (Paidax01), whose
// "Three-Petal Spiral" preset runs a particle trail round a hypotrochoid:
// a circle of radius r rolling inside one of radius R, its pen at distance d,
// with R = 3, r = 1 and d = 3. The curve breathes — d and the overall scale
// swell together — and the whole shape turns slowly. This is a port of that
// preset onto mygo's painter, sized for the sidebar's 20pt slot.
//
// The loader is strictly monochrome: black ink on a light window, white on a
// dark one. The trail fades and shrinks from head to tail, so the tail reads
// as motion rather than as a second shape.

const (
	loaderSize float32 = 20

	// The preset's curve lives in a 100-unit box; the sidebar draws that box
	// into loaderSize points.
	loaderUnit = float64(loaderSize) / 100

	// The preset's rolling-circle parameters and breathing.
	spiralR      float64 = 3
	spiralr      float64 = 1
	spirald      float64 = 3
	spiralScale  float64 = 2.2
	spiralBreath float64 = 0.45
	spiralPen    float64 = 0.25 // the pen reaches this much further at full breath

	// The preset's curve only fills the middle of its box; at 20pt that is a
	// few points wide, so the whole curve is scaled up to fill the slot.
	loaderFit float64 = 2.9

	// The preset's 82 particles at a 118px card still read as a comet here:
	// packed along a third of the curve they close into one rope.
	loaderParticles = 82
	loaderTrailSpan = 0.34
	loaderDotR      = 0.9 // units, at the tail
	loaderDotRGrow  = 2.7 // units, added towards the head
	loaderMinR      = 0.3 // pt

	loaderLoopMs   = 4600
	loaderPulseMs  = 4200
	loaderRotateMs = 28000

	// The preset's faint full path, a soft band behind the trail.
	loaderPathWidth float32 = 2.4
	loaderPathAlpha float32 = 0.1
	loaderPathSteps         = 120

	// loaderStillMs freezes the working trail on one frame: the breathing at
	// its fullest, a little way along the loop.
	loaderStillMs = 682
)

// loaderDot is one painted dot, in the loader's own 100-unit box.
type loaderDot struct {
	x, y float32
	r    float32
	// alpha is the ink's coverage: 1 at the head, near 0 at the tail.
	alpha float32
}

// loaderFrame is one instant of the loader. now is milliseconds since the
// loop began. Dots run tail first, so painting them in order lays the head
// over the tail.
func loaderFrame(now float64) ([]loaderDot, [][2]float32) {
	progress := math.Mod(now, loaderLoopMs) / loaderLoopMs
	// The preset's breath: detail swings between 0.52 and 1 once a pulse.
	pulse := math.Mod(now, loaderPulseMs) / loaderPulseMs
	detail := 0.52 + (math.Sin(pulse*2*math.Pi+0.55)+1)/2*0.48
	angle := -math.Mod(now, loaderRotateMs) / loaderRotateMs * 2 * math.Pi

	dots := make([]loaderDot, 0, loaderParticles)
	for i := loaderParticles - 1; i >= 0; i-- {
		tail := float64(i) / float64(loaderParticles-1)
		x, y := curvePoint(normalizeTurn(progress-tail*loaderTrailSpan), detail)
		x, y = turnPoint(x, y, angle)
		fade := math.Pow(1-tail, 0.56)
		dots = append(dots, loaderDot{
			x:     float32(x),
			y:     float32(y),
			r:     float32(loaderDotR + loaderDotRGrow*fade),
			alpha: float32(0.04 + fade*0.96),
		})
	}

	path := make([][2]float32, loaderPathSteps+1)
	for i := range path {
		x, y := curvePoint(float64(i)/loaderPathSteps, detail)
		x, y = turnPoint(x, y, angle)
		path[i] = [2]float32{float32(x), float32(y)}
	}
	return dots, path
}

// curvePoint is the preset's hypotrochoid, in the 100-unit box. t is in turns.
func curvePoint(t, detail float64) (float64, float64) {
	a := t * 2 * math.Pi
	k := (spiralR - spiralr) / spiralr
	d := spirald + detail*spiralPen
	bx := (spiralR-spiralr)*math.Cos(a) + d*math.Cos(k*a)
	by := (spiralR-spiralr)*math.Sin(a) - d*math.Sin(k*a)
	scale := (spiralScale + detail*spiralBreath) * loaderFit
	return 50 + bx*scale, 50 + by*scale
}

// turnPoint spins a point of the 100-unit box about its center.
func turnPoint(x, y, angle float64) (float64, float64) {
	dx, dy := x-50, y-50
	s, c := math.Sin(angle), math.Cos(angle)
	return 50 + dx*c - dy*s, 50 + dx*s + dy*c
}

// normalizeTurn folds a progress into [0, 1).
func normalizeTurn(t float64) float64 {
	return math.Mod(math.Mod(t, 1)+1, 1)
}

// loaderPhase returns the loader's clock in milliseconds and keeps frames
// coming while it runs.
func loaderPhase(c *ui.Context) float64 {
	c.After(16 * time.Millisecond)
	return float64(time.Now().UnixNano()) / 1e6
}

// drawCurveLoader paints one frame of the loader in r. The loader keeps its
// own 20pt geometry and shrinks or grows as one piece.
func drawCurveLoader(p *ui.Painter, r ui.Rect, now float64, dark bool) {
	scale := min(r.W, r.H) / loaderSize
	unit := float32(loaderUnit) * scale
	ox := r.X + (r.W-loaderSize*scale)/2
	oy := r.Y + (r.H-loaderSize*scale)/2

	dots, path := loaderFrame(now)
	pts := make([][2]float32, len(path))
	for i, q := range path {
		pts[i] = [2]float32{ox + q[0]*unit, oy + q[1]*unit}
	}
	p.StrokePolyline(pts, loaderPathWidth*scale, loaderInk(loaderPathAlpha, dark))

	for _, d := range dots {
		rad := max(loaderMinR, d.r*unit) * scale
		p.Fill(ui.Rect{
			X: ox + d.x*unit - rad,
			Y: oy + d.y*unit - rad,
			W: 2 * rad,
			H: 2 * rad,
		}, loaderInk(d.alpha, dark), rad)
	}
}

// loaderInk is the loader's only ink: black on a light window, white on a
// dark one, at the given coverage.
func loaderInk(alpha float32, dark bool) ui.Color {
	alpha = min(1, max(0, alpha))
	if dark {
		return ui.RGBA(255, 255, 255, alpha)
	}
	return ui.RGBA(0, 0, 0, alpha)
}
