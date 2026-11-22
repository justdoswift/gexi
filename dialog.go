package main

import (
	"strings"
	"time"

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

// systemDanger is the red a destructive control wears: the system's own, not
// the terminal theme's ANSI red.
//
// A theme's red is chosen to sit on that theme's background as text, so it
// comes out a muted brick (#a84b3a on Claude Light) that reads as a warning
// line in a diff. A button is not a line of text: macOS paints its
// destructive controls in system red, and a brick pill beside a blue one
// looks like a third-party widget rather than part of the sheet.
func systemDanger(tk tokens) ui.Color {
	if tk.Dark {
		return ui.RGB(0xff, 0x45, 0x3a) // systemRed, dark appearance
	}
	return ui.RGB(0xff, 0x3b, 0x30) // systemRed, light appearance
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
	t.Border = tk.Foreground.Alpha(0.22)
	t.Text = tk.Foreground
	t.TextMuted = tk.MutedForeground
	t.Accent = tk.Foreground
	t.AccentHover = tk.Foreground
	t.AccentPressed = tk.Foreground
	t.AccentText = tk.Background
	t.Danger = systemDanger(tk)
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

// dialogGlass is the material the panel is made of. It is the system's own
// glass, the same NSGlassEffectView a notification banner is: a dialog is the
// largest small surface the app draws, and an opaque paper with a hairline
// round it was what made it read as a white board laid over the work.
//
// The tint stays light. The system's material already frosts and tones what
// is behind it, so a dense tint on top would only make it opaque again, which
// is what the panel was before.
func dialogGlass(tk tokens) glass.Glass {
	density := float32(0.55)
	if tk.Dark {
		density = 0.62
	}
	// The system's own material, as a notification banner is, and the same one
	// the menu wears. The shader frosts at a control's density, which over a
	// light page comes out a flat grey board; the system's material samples
	// what is under the window and keeps its body.
	//
	// The tint is the theme's own paper, warmed: glassTint derives it, so a
	// warm theme keeps its warmth instead of being overruled by a fixed value.
	return glass.Glass{
		Native:  true,
		Tint:    glassTint(tk).Alpha(density),
		Clarity: 0,
	}
}

// dialogFrame draws the panel every dialog shares: Liquid Glass over the
// dimmed window, so whatever it covers shows through, frosted and lit at the
// rim. build adds the heading, the form, and the footer to a column that
// fills the panel. Escape and a press outside close the dialog through
// *open.
func dialogFrame(c *ui.Context, tk tokens, open *bool, width float32, build func()) {
	// The panel is a sheet of the system's material, not a hole in the
	// window: dialogGlass carries its tint and the rim the material lights.
	shade := ui.RGBA(0, 0, 0, 0.24)
	if !tk.Dark {
		shade = ui.RGBA(0, 0, 0, 0.12)
	}
	shadow := alpha(ui.RGB(0, 0, 0), 0.5)
	if !tk.Dark {
		shadow = alpha(ui.RGB(0, 0, 0), 0.18)
	}
	ui.DialogBase(c, open, func(back, panel ui.Element) {
		// The window dims as the panel arrives and clears as it leaves, so a
		// dialog reads as a layer coming over the work, not as a full-screen
		// flip. The panel itself rises a hair and settles from just under
		// full size, the way a sheet of glass would be set down.
		back.Background(shade).Transition(ui.ElementTransition{
			Duration:     dialogMotion,
			Ease:         ui.EaseOut,
			ExitDuration: dialogExitMotion,
			Enter:        &ui.Motion{},
			Exit:         &ui.Motion{},
		})
		panel.Width(width).Clip().Gap(0).Padding(0).Radius(22).
			Shadow(0, 40, 100, 0, shadow).
			Transition(ui.ElementTransition{
				Duration:     dialogMotion,
				Ease:         ui.EaseOut,
				ExitDuration: dialogExitMotion,
				Enter:        &ui.Motion{Y: 6, Scale: 0.97},
				Exit:         &ui.Motion{Y: 4, Scale: 0.985},
				Colors:       true,
				Shadows:      true,
			}).
			Children(func() {
				ui.Themed(c, dialogTheme(tk)).FillWidth().Children(func() {
					inner := ui.Column(c).FillWidth().Gap(0).Padding(0).
						Material(glass.Blur{Radius: 18})
					inner.Children(func() {
						// The pane, larger than the clip so its rim falls
						// outside: only the even middle of the glass shows.
						// It is the system's own material, as a notification
						// banner is, so the sheet frosts what is behind it
						// and lights its own rim rather than being an opaque
						// paper with a hairline drawn round it.
						// No inset shadows: the native glass paints a hole, so
						// they would land on it and vanish. The material lights
						// its own rim.
						ui.Box(c).Absolute().Left(-6).Top(-6).Right(-6).Bottom(-6).
							Radius(28).ZIndex(-1).IgnoreInput(true).
							Material(dialogGlass(tk))
						// A light from above, soft and without an edge.
						ui.Box(c).Absolute().Left(2).Right(2).Top(2).Height(96).IgnoreInput(true).
							Gradient(ui.RGBA(255, 255, 255, 0.09), ui.Transparent, 180)
						build()
					})
				})
			})
	})
}

// dialogHeading is the title that every dialog opens with, and the one line
// of explanation that belongs with it.
func dialogHeading(c *ui.Context, tk tokens, title, hint string) {
	ui.Column(c).FillWidth().Padding(24, 24, 18, 24).Gap(7).Children(func() {
		ui.Text(c, title).FontSize(16).FontWeight(600).TextColor(tk.Foreground)
		if hint != "" {
			ui.Text(c, hint).FontSize(12).TextColor(tk.MutedForeground).
				Width(0).MinWidthPercent(100)
		}
	})
}

// dialogBody is the form between the heading and the footer.
func dialogBody(c *ui.Context, build func()) {
	ui.Column(c).FillWidth().Padding(0, 24, 22, 24).Gap(10).Children(build)
}

// dialogMessage writes the paragraphs of an alert's message.
func dialogMessage(c *ui.Context, tk tokens, message string) {
	ui.Column(c).FillWidth().Gap(8).Children(func() {
		for _, para := range strings.Split(message, "\n\n") {
			if para == "" {
				continue
			}
			ui.Text(c, para).FontSize(13).LineHeight(1.45).TextColor(tk.MutedForeground).
				Width(0).MinWidthPercent(100)
		}
	})
}

// dialogWarning is one consequence that costs files, marked with a danger
// dot so it reads apart from the explanation above it.
func dialogWarning(c *ui.Context, tk tokens, message string) {
	ui.Row(c).FillWidth().Gap(8).AlignItems(ui.Start).Children(func() {
		ui.Box(c).Width(6).Height(6).Radius(3).Shrink(0).Margin(5, 0, 0, 0).
			Background(systemDanger(tk))
		ui.Text(c, message).FontSize(12.5).TextColor(systemDanger(tk)).Grow(1)
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
// It drops in under the field and fades away when the next try clears it, so
// a failed attempt does not push the footer down and back in one frame.
func dialogError(c *ui.Context, tk tokens, message string) {
	if message == "" {
		return
	}
	ui.Text(c, message).FontSize(12).TextColor(systemDanger(tk)).
		Width(0).MinWidthPercent(100).
		Transition(ui.ElementTransition{
			Duration: 140 * time.Millisecond,
			Ease:     ui.EaseOut,
			Enter:    &ui.Motion{Y: -2},
			Exit:     &ui.Motion{Y: -2},
		})
}

// dialogFooter is the rule and the actions at the bottom of a dialog. The
// quiet actions come first, and the one the dialog exists for last, where
// the eye and the Return key expect it.
func dialogFooter(c *ui.Context, tk tokens, actions ...dialogAction) {
	// A dialog carries one filled button. Where a primary action is among
	// them, a destructive one is the way out of that action rather than the
	// point of the dialog, and steps back to the quiet glass: two saturated
	// pills side by side leave the eye to guess which one Return takes.
	demoteDanger := false
	for _, a := range actions {
		if a.kind == dialogPrimary {
			demoteDanger = true
		}
	}
	ui.Row(c).FillWidth().Height(60).Padding(0, 18).Gap(8).
		AlignItems(ui.Center).Justify(ui.End).Children(func() {
			// The rule fades out toward the corners, so it reads as a fold in
			// the glass rather than a line drawn across it.
			ui.Box(c).Absolute().Left(18).Right(18).Top(0).Height(1).IgnoreInput(true).
				LinearGradient(ui.LinearGradient{Angle: 90, Stops: []ui.GradientStop{
					{Color: ui.Transparent, At: 0},
					{Color: tk.Foreground.Alpha(0.14), At: 0.5},
					{Color: ui.Transparent, At: 1},
				}})
			for _, a := range actions {
				dialogButton(c, tk, a, demoteDanger)
			}
		})
}

// dialogButton is one action in the footer: quiet for Cancel and the like,
// the accent for the action that carries the dialog, and the danger colour
// for one that throws work away.
//
// These are flat fills, not glass. The panel behind them is already the
// system's material, and a glass button on top of it samples the hole that
// material paints: a shader's frost has nothing under it but transparency,
// which comes out a flat grey board. A control on a sheet of glass is a
// control, not a second sheet.
func dialogButton(c *ui.Context, tk tokens, a dialogAction, demoteDanger bool) {
	b := ui.Button(c, "").Label(a.label).Height(32).Padding(0, 16).
		Radius(16).Cursor(ui.CursorPointer).OnClick(a.run).
		Children(func() {
			ui.Text(c, a.label).FontSize(12.5).FontWeight(500)
		})
	if a.auto {
		b = b.AutoFocus()
	}
	kind := a.kind
	if kind == dialogDanger && demoteDanger {
		kind = dialogQuiet
	}
	// The accent is the one macOS reports, not the terminal theme's cursor
	// colour: a dialog's default button is the system's blue on a Mac set to
	// blue, as a menu's chosen row is.
	accent := c.Preferences().Accent
	if accent.A == 0 {
		accent = tk.SystemAccent
	}
	hover := b.Hovered()
	switch kind {
	case dialogPrimary:
		fill := accent
		if hover {
			fill = accent.MixOklab(ui.RGB(255, 255, 255), 0.14)
		}
		b.Background(fill).TextColor(ui.RGB(255, 255, 255))
	case dialogDanger:
		fill := systemDanger(tk)
		if hover {
			fill = fill.MixOklab(ui.RGB(255, 255, 255), 0.14)
		}
		b.Background(fill).TextColor(ui.RGB(255, 255, 255))
	default:
		// A demoted destructive action keeps the danger colour on its
		// words: still legible as the one that costs something, without
		// competing with the default.
		text := tk.Foreground
		if a.kind == dialogDanger {
			text = systemDanger(tk)
		}
		// The quiet button is the panel's own paper, a step darker under
		// the pointer: a filled control, not a wash that vanishes into the
		// glass behind it.
		fill := tk.Foreground.Alpha(0.07)
		if hover {
			fill = tk.Foreground.Alpha(0.12)
		}
		b.Background(fill).TextColor(text).Border(1, tk.Foreground.Alpha(0.12))
	}
}

