package main

import (
	"math"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestOrbWorkingFrame(t *testing.T) {
	frame := orbWorkingFrame(1.2)
	if want := orbOrbitN * (orbGhostN + orbParticles); len(frame) != want {
		t.Fatalf("dots = %d; want %d", len(frame), want)
	}
	for i, d := range frame {
		if math.IsNaN(float64(d.x)) || math.IsInf(float64(d.x), 0) {
			t.Fatalf("dot %d has a bad x", i)
		}
		if d.r < orbMinR {
			t.Errorf("dot %d radius %v is below the floor", i, d.r)
		}
		if d.x < 0 || d.x > orbSize || d.y < 0 || d.y > orbSize {
			t.Errorf("dot %d at (%v, %v) leaves the %vpt box", i, d.x, d.y, orbSize)
		}
	}
}

func TestOrbDepthSort(t *testing.T) {
	frame := orbWorkingFrame(0.7)
	for i := 1; i < len(frame); i++ {
		if frame[i].z < frame[i-1].z {
			t.Fatalf("dot %d is nearer than the one before it", i)
		}
	}
}

func TestOrbInk(t *testing.T) {
	if got := orbInk(0, 1, true); got != ui.RGB(255, 255, 255) {
		t.Errorf("dark ink at 0 = %v; want white", got)
	}
	if got := orbInk(1, 1, true); got != ui.RGB(0, 0, 0) {
		t.Errorf("dark ink at 1 = %v; want black", got)
	}
	if got := orbInk(0, 1, false); got != ui.RGB(0, 0, 0) {
		t.Errorf("light ink at 0 = %v; want black", got)
	}
	if got := orbInk(1, 1, false); got != ui.RGB(255, 255, 255) {
		t.Errorf("light ink at 1 = %v; want white", got)
	}
}
