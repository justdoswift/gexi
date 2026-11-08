package main

import (
	"slices"
	"strings"
	"sync"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"

	"github.com/justdoswift/gexi/locales"
)

// A theme is one of Ghostty's: the terminal palette the app ships (hundreds
// of them, from iTerm2-Color-Schemes) plus the files in the user's Ghostty
// themes folder. The terminal draws with it, and the chrome derives its own
// surfaces from the theme's background, text and cursor, so a theme colours
// the whole window rather than a terminal inside a neutral shell.

type themeEntry struct {
	name  string
	dark  bool
	theme *terminal.Theme
}

var (
	themeCatalogOnce sync.Once
	themeCatalog     [2][]themeEntry // light, dark, as their backgrounds read
)

// catalogFor returns the themes of one appearance, sorted as Ghostty lists
// them. The first call reads every theme, so the settings page builds the
// catalog on demand rather than at launch.
func catalogFor(dark bool) []themeEntry {
	themeCatalogOnce.Do(func() {
		for _, name := range terminal.GhosttyThemes() {
			th, err := terminal.GhosttyTheme(name)
			if err != nil {
				continue
			}
			e := themeEntry{name: name, dark: themeIsDark(th), theme: th}
			i := 0
			if e.dark {
				i = 1
			}
			themeCatalog[i] = append(themeCatalog[i], e)
		}
	})
	return themeCatalog[boolInt(dark)]
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// themeExists reports whether Ghostty (or the user's theme folder) has the
// theme: a saved pick may outlive the file it named.
func themeExists(name string) bool {
	_, err := terminal.GhosttyTheme(name)
	return err == nil
}

// appearanceThemeName is the theme the user picked for one appearance.
func (w *workspace) appearanceThemeName(dark bool) string {
	if dark {
		return w.settings.ThemeDark
	}
	return w.settings.ThemeLight
}

// appearanceTokens is the chrome's palette for an appearance, apart from the
// window's current one: the tint and the window colour need both.
func (w *workspace) appearanceTokens(dark bool) tokens {
	t := ui.LightTheme()
	if dark {
		t = ui.DarkTheme()
	}
	return themeTokensFor(t, w.appearanceThemeName(dark))
}

// tokens is the chrome's palette for the frame: the appearance the window
// draws and the theme picked for it.
func (w *workspace) tokens(c *ui.Context) tokens {
	t := c.Theme()
	return themeTokensFor(t, w.appearanceThemeName(t.Dark))
}

func (w *workspace) chromeTint() string {
	return windowTint(w.appearanceTokens(false), w.appearanceTokens(true))
}

func (w *workspace) chromeBackground() string {
	return windowBackground(w.appearanceTokens(false), w.appearanceTokens(true))
}

// terminalThemes is the pair every terminal takes: the light theme and the
// dark one, so the view follows the window's appearance as it changes.
func (w *workspace) terminalThemes() (light, dark *terminal.Theme) {
	if name := w.settings.ThemeLight; name != "" {
		if th, err := terminal.GhosttyTheme(name); err == nil {
			light = th
		}
	}
	if name := w.settings.ThemeDark; name != "" {
		if th, err := terminal.GhosttyTheme(name); err == nil {
			dark = th
		}
	}
	return
}

// setTheme saves the pick for one appearance and hands it to every terminal
// and to the window's own material, so the change shows at once.
func (w *workspace) setTheme(dark bool, name string) {
	if w.appearanceThemeName(dark) == name {
		return
	}
	if dark {
		w.settings.ThemeDark = name
	} else {
		w.settings.ThemeLight = name
	}
	w.settings.save()

	light, darkTheme := w.terminalThemes()
	apply := func(p *pane) {
		if p.term != nil {
			p.term.SetTheme(light, darkTheme)
		}
		if p.stageMirror != nil {
			p.stageMirror.sink.SetTheme(light, darkTheme)
		}
	}
	for _, p := range w.allPanes() {
		apply(p)
	}
	for _, p := range w.detached {
		apply(p)
	}
	w.updateChrome()
	w.invalidate()
	w.invalidateSettings()
}

// updateChrome repaints the windows' own backgrounds and the glass's tint
// after a theme changes, so the frame behind the page matches it too.
func (w *workspace) updateChrome() {
	if w.win != nil {
		_ = w.win.SetVibrancyTint(w.chromeTint())
		_ = w.win.SetBackgroundColor(w.chromeBackground())
	}
	if w.settingsWin != nil {
		_ = w.settingsWin.SetBackgroundColor(w.chromeBackground())
	}
}

// themePreview is what a card draws: the theme's own colours, as the
// terminal would show them.
type themePreview struct {
	bg, fg, accent ui.Color
	palette        [16]ui.Color
}

func previewFor(dark bool, name string) themePreview {
	if name == "" {
		th := terminal.LightTheme()
		accent := ui.LightTheme().Accent
		if dark {
			th = terminal.DarkTheme()
			accent = ui.DarkTheme().Accent
		}
		tk := defaultTokens(dark, accent)
		return themePreview{
			bg:      tk.Background,
			fg:      tk.Foreground,
			accent:  tk.SystemAccent,
			palette: th.Palette,
		}
	}
	th, err := terminal.GhosttyTheme(name)
	if err != nil {
		return previewFor(dark, "")
	}
	pv := themePreview{bg: th.Background, fg: th.Foreground, accent: th.Cursor, palette: th.Palette}
	if pv.accent.A == 0 {
		pv.accent = th.Palette[4]
	}
	return pv
}

const (
	themeCardW   float32 = 144
	themeCardH   float32 = 88
	themeCardGap float32 = 10
	themeNameH   float32 = 20
)

func (w *workspace) themesPage(c *ui.Context, tk tokens) {
	ui.TextInput(c, &w.themeQuery).Placeholder(locales.ThemesSearch()).
		Height(28).FontSize(12)

	// A new query starts both carousels from the beginning, where the
	// matches are. Building is the only place the field changes, so the
	// last query seen is enough to notice it.
	if w.themeQuery != w.themeQuerySeen {
		w.themeQuerySeen = w.themeQuery
		for i := range w.themeScroll {
			w.themeScroll[i].X = 0
		}
	}

	active := c.Theme().Dark
	w.themeSection(c, tk, false, !active)
	w.themeSection(c, tk, true, active)
}

func (w *workspace) themeSection(c *ui.Context, tk tokens, dark, active bool) {
	title := locales.ThemesLightMode()
	if dark {
		title = locales.ThemesDarkMode()
	}
	ui.Row(c).Gap(6).Children(func() {
		ui.Text(c, title).FontSize(13).FontWeight(600).TextColor(tk.Foreground)
		if active {
			ui.Text(c, locales.ThemesActive()).FontSize(11).TextColor(tk.MutedForeground)
		}
	})

	// Column labels: Default over its own card, Built-in over the carousel,
	// as Rex lays the two out.
	ui.Row(c).Gap(16).Children(func() {
		ui.Row(c).Width(themeCardW).Children(func() {
			ui.Text(c, locales.ThemesDefault()).FontSize(11).TextColor(tk.MutedForeground)
		})
		ui.Text(c, locales.ThemesBuiltIn()).FontSize(11).TextColor(tk.MutedForeground)
	})

	ui.Row(c).Gap(16).Children(func() {
		selected := w.appearanceThemeName(dark) == ""
		w.themeCard(c, tk, "", previewFor(dark, ""), selected, func() {
			w.setTheme(dark, "")
		})
		ui.Box(c).Width(1).Height(themeCardH + 4 + themeNameH).Background(tk.Border)
		w.themeCarousel(c, tk, dark)
	})
}

func (w *workspace) themeCarousel(c *ui.Context, tk tokens, dark bool) {
	entries := catalogFor(dark)
	if q := strings.ToLower(strings.TrimSpace(w.themeQuery)); q != "" {
		entries = slices.DeleteFunc(slices.Clone(entries), func(e themeEntry) bool {
			return !strings.Contains(strings.ToLower(e.name), q)
		})
	}
	if len(entries) == 0 {
		ui.Text(c, locales.ThemesNone()).FontSize(11).TextColor(tk.MutedForeground).
			Padding(2, 0, 0, 0)
		return
	}

	step := themeCardW + themeCardGap
	state := &w.themeScroll[boolInt(dark)]
	// Only the cards around the viewport are built: hundreds of themes are
	// a row long enough to make laying all of them out each frame wasteful.
	// The window is wider than any settings window, so a fling never
	// outruns it between frames.
	const window = 14
	start := int(state.X/step) - 1
	if start < 0 {
		start = 0
	}
	end := min(len(entries), start+window)
	if end == len(entries) && start > 0 {
		start = max(0, end-window)
	}

	ui.ScrollHorizontal(c).Height(themeCardH + 4 + themeNameH).
		FillWidth().TrackScroll(state).Gap(themeCardGap).Children(func() {
		if start > 0 {
			ui.Box(c).Width(float32(start)*step - themeCardGap)
		}
		for _, e := range entries[start:end] {
			entry := e
			selected := w.appearanceThemeName(dark) == entry.name
			w.themeCard(c, tk, entry.name, previewFor(dark, entry.name), selected, func() {
				w.setTheme(dark, entry.name)
			})
		}
		if rest := len(entries) - end; rest > 0 {
			ui.Box(c).Width(float32(rest)*step - themeCardGap)
		}
	})
}

// themeCard is one theme's card: its background, its own text, a prompt in
// the ANSI colours, the cursor, and the sixteen palette dots, with the name
// beneath. The chosen card takes a ring in the system's accent.
func (w *workspace) themeCard(c *ui.Context, tk tokens, name string, pv themePreview, selected bool, pick func()) {
	ui.Column(c).Width(themeCardW).Gap(4).Children(func() {
		border, width := pv.bg.MixOklab(pv.fg, 0.16), float32(1)
		if selected {
			border, width = tk.SystemAccent, 2
		}
		card := ui.Column(c).Width(themeCardW).Height(themeCardH).Radius(8).
			Padding(9).Background(pv.bg).Border(width, border).
			Cursor(ui.CursorPointer)
		card.OnClick(pick).Children(func() {
			ui.Row(c).Gap(6).Children(func() {
				ui.Text(c, "Aa").FontSize(16).FontWeight(600).
					TextColor(pv.fg).OnClick(pick)
				ui.Spacer(c)
				pill := ui.Row(c).Height(20).Padding(0, 6, 0, 6).Radius(4).
					Background(pv.palette[0]).OnClick(pick)
				pill.Children(func() {
					ui.Text(c, "~/").FontSize(11).TextColor(pv.palette[3]).OnClick(pick)
				})
				ui.Box(c).Width(10).Height(20).Radius(4).
					Background(pv.accent).OnClick(pick)
			})
			ui.Spacer(c)
			ui.Row(c).Gap(2).Children(func() {
				for _, col := range pv.palette {
					ui.Box(c).Width(6).Height(6).Radius(3).Background(col)
				}
			})
		})

		label := name
		if label == "" {
			label = locales.ThemesDefault()
		}
		color := tk.MutedForeground
		if selected {
			color = tk.Foreground
		}
		ui.Text(c, label).Width(themeCardW).FontSize(11).TextColor(color).
			TextAlign(ui.Center).SingleLine()
	})
}
