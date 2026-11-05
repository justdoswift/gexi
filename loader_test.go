package main

import (
	"math"
	"testing"
)

func TestLoaderFrame(t *testing.T) {
	dots, path := loaderFrame(1234)
	if len(dots) != loaderParticles {
		t.Fatalf("dots = %d; want %d", len(dots), loaderParticles)
	}
	if len(path) != loaderPathSteps+1 {
		t.Fatalf("path points = %d; want %d", len(path), loaderPathSteps+1)
	}
	for i, d := range dots {
		if math.IsNaN(float64(d.x)) || math.IsInf(float64(d.x), 0) ||
			math.IsNaN(float64(d.y)) || math.IsInf(float64(d.y), 0) {
			t.Fatalf("dot %d has a bad position", i)
		}
		if d.x-d.r < 0 || d.x+d.r > 100 || d.y-d.r < 0 || d.y+d.r > 100 {
			t.Errorf("dot %d at (%v, %v) r %v leaves the 100-unit box", i, d.x, d.y, d.r)
		}
		if d.alpha < 0 || d.alpha > 1 {
			t.Errorf("dot %d alpha %v is outside [0, 1]", i, d.alpha)
		}
	}
	for i := 1; i < len(dots); i++ {
		if dots[i].alpha < dots[i-1].alpha {
			t.Fatalf("dot %d is dimmer than the one before it", i)
		}
	}
	for i, p := range path {
		if p[0] < 0 || p[0] > 100 || p[1] < 0 || p[1] > 100 {
			t.Errorf("path point %d at (%v, %v) leaves the 100-unit box", i, p[0], p[1])
		}
	}
}

func TestLoaderMoves(t *testing.T) {
	a, _ := loaderFrame(0)
	b, _ := loaderFrame(loaderLoopMs / 4)
	if a[0].x == b[0].x && a[0].y == b[0].y {
		t.Fatal("the trail stands still a quarter of a loop in")
	}
}

func TestLoaderStillFrame(t *testing.T) {
	a, _ := loaderFrame(loaderStillMs)
	b, _ := loaderFrame(loaderStillMs)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("dot %d differs between two frames of the same time", i)
		}
	}
}

func TestLoaderInk(t *testing.T) {
	if got := loaderInk(1, true); got.R != 255 || got.G != 255 || got.B != 255 {
		t.Errorf("dark ink = %v; want white", got)
	}
	if got := loaderInk(1, false); got.R != 0 || got.G != 0 || got.B != 0 {
		t.Errorf("light ink = %v; want black", got)
	}
	if got := loaderInk(0, false); got.A != 0 {
		t.Errorf("zero coverage has alpha %v; want 0", got.A)
	}
	if got := loaderInk(2, false); got.A != 255 {
		t.Errorf("coverage clamps to %v; want 255", got.A)
	}
}
