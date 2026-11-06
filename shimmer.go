package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// The sidebar shows an agent at work by running a band of light across the
// session's name: the label's ink dips, and a bright band sweeps from left to
// right and off the end, then starts again. It replaces the spinner that used
// to sit beside the name, so the row says what is happening in the words
// themselves.
//
// The shimmer is monochrome like the rest of the chrome: the band only raises
// the label's own ink back to full strength, it never adds a colour.

const (
	// shimmerCycleMs is one sweep across the label and its pause.
	shimmerCycleMs = 1800
	// shimmerBand is the band's half-width, in label widths.
	shimmerBand = 0.16
	// shimmerBase is the ink's coverage away from the band; the band brings
	// it back to full.
	shimmerBase = 0.55
)

// shimmerPhase returns the sweep's clock in [0, 1) and keeps frames coming
// while it runs.
func shimmerPhase(c *ui.Context) float64 {
	c.After(16 * time.Millisecond)
	return float64(time.Now().UnixNano()/1e6%shimmerCycleMs) / shimmerCycleMs
}

// shimmerSpans inks label one rune at a time: the nearer a rune to the band's
// center, the fuller its ink. phase is 0 at the start of a sweep.
func shimmerSpans(label string, phase float64, ink ui.Color) []ui.Span {
	runes := []rune(label)
	spans := make([]ui.Span, 0, len(runes))
	// The band starts off the left edge and leaves past the right one, so a
	// sweep begins and ends with the label dim.
	center := phase*1.6 - 0.3
	for i, r := range runes {
		at := 0.0
		if len(runes) > 1 {
			at = float64(i) / float64(len(runes)-1)
		}
		k := math.Max(0, 1-math.Abs(at-center)/shimmerBand)
		k = k * k * (3 - 2*k) // smoothstep, so the band has no hard edges
		c := ink
		c.A = shimmerAlpha(k)
		spans = append(spans, ui.Span{Text: string(r), Color: c})
	}
	return spans
}

// shimmerAlpha is the ink's coverage at band strength k, from the dim label
// at 0 to full strength at 1.
func shimmerAlpha(k float64) uint8 {
	return uint8((shimmerBase + (1-shimmerBase)*k) * 255)
}

// sessionLabelElement is the session's name, with a running subagent count
// after it while any run. While the agent works — or a subagent still runs
// after its turn — a band of light sweeps the label; while the agent waits
// for you the label holds full ink; otherwise it keeps the row's colour.
func sessionLabelElement(c *ui.Context, tk tokens, s *session, color ui.Color, pick func()) ui.Element {
	label := sessionLabel(s)
	switch sessionActivityState(s) {
	case activityWorking:
		// The band reaches full ink on every row, so it reads the same in the
		// selected row as in the others.
		return ui.RichText(c, shimmerSpans(label, shimmerPhase(c), tk.Foreground)...).
			FontSize(12).FontWeight(500).SingleLine().OnClick(pick)
	case activityWaiting:
		color = tk.Foreground
	}
	return ui.Text(c, label).FontSize(12).FontWeight(500).SingleLine().TextColor(color).OnClick(pick)
}
