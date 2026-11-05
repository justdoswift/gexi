package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestShimmerSpans(t *testing.T) {
	const label = "Claude Code"
	spans := shimmerSpans(label, 0.5, ui.RGB(0, 0, 0))
	if len(spans) != len([]rune(label)) {
		t.Fatalf("spans = %d; want one per rune (%d)", len(spans), len([]rune(label)))
	}
	base := shimmerAlpha(0)
	for i, s := range spans {
		if len([]rune(s.Text)) != 1 {
			t.Errorf("span %d holds %q; want one rune", i, s.Text)
		}
		if s.Color.A < base || s.Color.A > 255 {
			t.Errorf("span %d alpha = %d; want within [%d, 255]", i, s.Color.A, base)
		}
	}
}

func TestShimmerBandSweeps(t *testing.T) {
	const label = "Claude Code"
	brightest := func(phase float64) int {
		spans := shimmerSpans(label, phase, ui.RGB(0, 0, 0))
		at := 0
		for i, s := range spans {
			if s.Color.A > spans[at].Color.A {
				at = i
			}
		}
		return at
	}
	start, middle, end := brightest(0.2), brightest(0.5), brightest(0.8)
	if !(start < middle && middle < end) {
		t.Fatalf("band moves %d → %d → %d; want it to travel left to right", start, middle, end)
	}
}

func TestShimmerIdleIsDim(t *testing.T) {
	spans := shimmerSpans("Claude Code", 0, ui.RGB(0, 0, 0))
	for i, s := range spans {
		if s.Color.A > shimmerAlpha(0) {
			t.Errorf("span %d alpha = %d at the start of a sweep; want the band off the label", i, s.Color.A)
		}
	}
}
