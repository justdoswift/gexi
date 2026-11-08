package main

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"
)

// shimmerCycle is how long the highlight takes to cross a label; the words
// stay readable at that pace and still read as moving.
const shimmerCycle = 1600 * time.Millisecond

// shimmerBand is how wide the highlight is, as a share of the label: wide
// enough that a short label glows as a whole rather than as one letter.
const shimmerBand = 0.4

// shimmerText draws label with a highlight sweeping across it from left to
// right, for a header button at work: the words say what runs, and the
// sweep says it is still running. Where the desktop asks for less motion,
// the label stands still in the color it would have without the sweep.
func shimmerText(c *ui.Context, tk tokens, label string) ui.Element {
	runes := []rune(label)
	if len(runes) == 0 {
		return shimmerRest(c, tk, label)
	}
	if c.Preferences().ReduceMotion {
		return shimmerRest(c, tk, label)
	}
	// The sweep moves the text's colors, not its layout, so the view is
	// built again each frame for them.
	c.AnimationFrame()
	// The highlight travels a little past both ends, so it enters from off
	// the left and leaves to the right.
	phase := math.Mod(float64(c.Now().UnixNano())/float64(shimmerCycle), 1)
	center := -shimmerBand + (1+2*shimmerBand)*phase
	spans := make([]ui.Span, len(runes))
	for i, r := range runes {
		x := 0.0
		if len(runes) > 1 {
			x = float64(i) / float64(len(runes)-1)
		}
		glow := 1 - math.Abs(x-center)/shimmerBand
		if glow < 0 {
			glow = 0
		}
		// Smoothstep, so the band has no hard edge as it passes.
		glow = glow * glow * (3 - 2*glow)
		spans[i] = ui.Span{
			Text:   string(r),
			Size:   11,
			Weight: 600,
			Color:  tk.MutedForeground.Mix(tk.Foreground, float32(glow)),
		}
	}
	return ui.RichText(c, spans...).SingleLine()
}

// shimmerRest is the label at rest, as the chip's other labels read.
func shimmerRest(c *ui.Context, tk tokens, label string) ui.Element {
	return ui.Text(c, label).FontSize(11).FontWeight(600).
		TextColor(tk.Foreground).SingleLine()
}
