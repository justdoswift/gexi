package main

import (
	"strings"

	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"
)

// The app's dialogs share one frame: Liquid Glass in the chrome's own
// palette, a heading, the form, and a footer rule with the actions at its
// right edge. The members of the panel take their controls from the chrome
// rather than the toolkit's default blue, so a dialog about a warm terminal
// theme does not arrive in system gray.

type dialogActionKind int

const (
	dialogQuiet dialogActionKind = iota
	dialogPrimary
	dialogDanger
)

type dialogAction struct {
	label string
	kind  dialogActionKind
	run   func()
	// auto gives the button the keyboard as the dialog opens, so Return
	// answers what the dialog asks. A form's field takes it instead, and a
	// destructive alert leaves it on Cancel.
	auto bool
}

// alpha returns c at the given opacity.
func alpha(c ui.Color, a float32) ui.Color {
	return ui.RGBA(c.R, c.G, c.B, a)
}

// dialogTheme is the palette the toolkit's controls (fields, check boxes,
// focus rings) take in a dialog: the chrome's text over glass, with the
// accent turned over, since the rest of the chrome is monochrome too. Its
// background is transparent so the glass shows through the theme wrapper.
func dialogTheme(tk tokens) *ui.Theme {
	t := ui.LightTheme()
	if tk.Dark {
		t = ui.DarkTheme()
	}
	t.Background = ui.Transparent
	// The surfaces stay opaque: with Reduce Transparency the glass draws
	// the theme's Surface as itself, and a see-through one would leave the
	// dialog over the dimmed window.
	t.Surface = tk.Background
	t.SurfaceHover = tk.Background.MixOklab(tk.Foreground, 0.06)
	t.SurfacePressed = tk.Background.MixOklab(tk.Foreground, 0.1)
	t.Border = tk.Border
	t.Text = tk.Foreground
	t.TextMuted = tk.MutedForeground
	t.Accent = tk.Foreground
	t.AccentHover = tk.Foreground
	t.AccentPressed = tk.Foreground
	t.AccentText = tk.Background
	t.Danger = tk.Danger
	t.Warning = tk.Waiting
	t.Success = tk.Working
	t.Selection = alpha(tk.Foreground, 0.28)
	t.Focus = alpha(tk.Foreground, 0.45)
	t.Inverse = tk.Foreground
	t.InverseText = tk.Background
	t.Scrollbar = alpha(tk.Foreground, 0.3)
	t.Radius = chromeRadius
	t.Spacing = 4
	t.FontSize = 13
	return t
}

// dialogFrame draws the panel every dialog shares: Liquid Glass over the
// dimmed window, so whatever it covers shows through, frosted and lit at the
// rim. build adds the heading, the form, and the footer to a column that
// fills the panel. Escape and a press outside close the dialog through
// *open.
func dialogFrame(c *ui.Context, tk tokens, open *bool, width float32, build func()) {
	// Light glass keeps a little more of its own surface than dark, as
	// AppKit's material does: text behind a white pane would otherwise ghost
	// through. Clarity lets a pane this large read as glass, not a card.
	shade, tint, clarity := ui.RGBA(0, 0, 0, 0.35), float32(0.32), float32(0.3)
	if !tk.Dark {
		shade, tint = ui.RGBA(0, 0, 0, 0.22), 0.52
	}
	ui.DialogBase(c, open, func(back, panel ui.Element) {
		back.Background(shade)
		panel.Width(width).Clip().Gap(0).Padding(0).
			Radius(18).Material(glass.Glass{Tint: tk.Background.Alpha(tint), Clarity: clarity}).
			Border(1, tk.Foreground.Alpha(0.1)).
			Shadow(0, 30, 70, 0, alpha(ui.RGB(0, 0, 0), 0.45)).
			Children(func() {
				// A light from above: a faint wash down the top of the glass
				// and a bright hairline where its rim catches the light, so
				// the panel reads as a surface rather than a flat card.
				ui.Box(c).Absolute().Left(1).Right(1).Top(1).Height(72).IgnoreInput(true).
					Gradient(ui.RGBA(255, 255, 255, 0.07), ui.Transparent, 180)
				ui.Box(c).Absolute().Left(14).Right(14).Top(0).Height(1).IgnoreInput(true).
					Background(ui.RGBA(255, 255, 255, 0.28))
				ui.Themed(c, dialogTheme(tk)).FillWidth().Children(build)
			})
	})
}

// dialogHeading is the title that every dialog opens with, and the one line
// of explanation that belongs with it.
func dialogHeading(c *ui.Context, tk tokens, title, hint string) {
	ui.Column(c).FillWidth().Padding(16, 20, 14, 20).Gap(6).Children(func() {
		ui.Text(c, title).FontSize(15).FontWeight(600).TextColor(tk.Foreground)
		if hint != "" {
			ui.Text(c, hint).FontSize(12).TextColor(tk.MutedForeground).
				Width(0).MinWidthPercent(100)
		}
	})
}

// dialogBody is the form between the heading and the footer.
func dialogBody(c *ui.Context, build func()) {
	ui.Column(c).FillWidth().Padding(0, 20, 16, 20).Gap(8).Children(build)
}

// dialogMessage writes the paragraphs of an alert's message.
func dialogMessage(c *ui.Context, tk tokens, message string) {
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		for _, para := range strings.Split(message, "\n\n") {
			if para == "" {
				continue
			}
			ui.Text(c, para).FontSize(13).TextColor(tk.MutedForeground).
				Width(0).MinWidthPercent(100)
		}
	})
}

// dialogWarning is one consequence that costs files, marked with a danger
// dot so it reads apart from the explanation above it.
func dialogWarning(c *ui.Context, tk tokens, message string) {
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Start).Children(func() {
		ui.Box(c).Width(6).Height(6).Radius(3).Shrink(0).Margin(5, 0, 0, 0).
			Background(tk.Danger)
		ui.Text(c, message).FontSize(12.5).TextColor(tk.Danger).Grow(1)
	})
}

// dialogInput is a text field on the glass: a faint well whose border takes
// the text colour when it has the focus.
func dialogInput(c *ui.Context, tk tokens, value *string, placeholder string) ui.Element {
	return ui.TextInput(c, value).Height(34).FontSize(13).
		Padding(0, 10).Radius(chromeRadius).
		Background(tk.Foreground.Alpha(0.06)).
		Border(1, tk.Foreground.Alpha(0.12)).
		Placeholder(placeholder).PlaceholderColor(tk.MutedForeground).
		FocusRingColor(alpha(tk.Foreground, 0.35))
}

// dialogLabel names a field in the muted text above it.
func dialogLabel(c *ui.Context, tk tokens, label string) {
	ui.Text(c, label).FontSize(12).TextColor(tk.MutedForeground)
}

// dialogError is the line a form shows when its last answer did not work.
func dialogError(c *ui.Context, tk tokens, message string) {
	if message == "" {
		return
	}
	ui.Text(c, message).FontSize(12).TextColor(tk.Danger).
		Width(0).MinWidthPercent(100)
}

// dialogFooter is the rule and the actions at the bottom of a dialog. The
// quiet actions come first, and the one the dialog exists for last, where
// the eye and the Return key expect it.
func dialogFooter(c *ui.Context, tk tokens, actions ...dialogAction) {
	ui.Row(c).FillWidth().Height(48).Padding(0, 14).Gap(8).
		AlignItems(ui.Center).Justify(ui.End).
		BorderWidth(1, 0, 0, 0).BorderColor(tk.Foreground.Alpha(0.1)).Children(func() {
		for _, a := range actions {
			dialogButton(c, tk, a)
		}
	})
}

// dialogButton is one action in the footer: quiet for Cancel and the like,
// the chrome's own high contrast for the action that carries the dialog, and
// the danger colour for one that throws work away.
func dialogButton(c *ui.Context, tk tokens, a dialogAction) {
	b := ui.Button(c, "").Label(a.label).Height(30).Padding(0, 14).
		Radius(7).Cursor(ui.CursorPointer).OnClick(a.run).
		Children(func() {
			ui.Text(c, a.label).FontSize(12.5).FontWeight(500)
		})
	if a.auto {
		b = b.AutoFocus()
	}
	hovered := b.Hovered()
	switch a.kind {
	case dialogPrimary:
		bg := tk.Foreground
		if hovered {
			bg = bg.MixOklab(tk.Background, 0.14)
		}
		b.Background(bg).TextColor(tk.Background)
	case dialogDanger:
		bg := tk.Danger
		if hovered {
			bg = bg.MixOklab(ui.RGB(255, 255, 255), 0.14)
		}
		b.Background(bg).TextColor(ui.RGB(255, 255, 255))
	default:
		bg := ui.Transparent
		if hovered {
			bg = tk.Foreground.Alpha(0.09)
		}
		b.Background(bg).TextColor(tk.Foreground).Border(1, tk.Foreground.Alpha(0.14))
	}
}

// dialogAlert shows a title and a message with the actions under it, in the
// same frame as the forms: a warning about a running agent or a checkout
// should read as part of the app, not as a system sheet that appears out of
// another look.
func dialogAlert(c *ui.Context, tk tokens, open *bool, width float32, title, message string, actions ...dialogAction) {
	dialogFrame(c, tk, open, width, func() {
		ui.Column(c).FillWidth().Padding(18, 20, 16, 20).Gap(8).Children(func() {
			ui.Text(c, title).FontSize(15).FontWeight(600).TextColor(tk.Foreground)
			if message != "" {
				dialogMessage(c, tk, message)
			}
		})
		dialogFooter(c, tk, actions...)
	})
}
