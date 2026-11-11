package main

import (
	_ "embed"
	"log"
	"slices"
	"sync"

	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
)

// terminalFont is the terminal's family: JetBrains Mono with Nerd Font
// icons, embedded so panes look the same on a Mac that never installed it.
const terminalFont = "JetBrains Mono Nerd Font Mono"

// The terminal's text size, and the range the picker offers.
const (
	terminalFontSizeDefault float32 = 12
	terminalFontSizeMin     float32 = 10
	terminalFontSizeMax     float32 = 20
)

// terminalFontSizes are the choices the size picker shows, in order.
var terminalFontSizes = []string{"10", "11", "12", "13", "14", "15", "16", "17", "18", "20"}

// terminalTextFont is what a pane's terminal draws with: the family and
// size picked in Settings, else the embedded JetBrains Mono at 12.
func (w *workspace) terminalTextFont() terminal.Font {
	family := terminalFont
	if w.settings.TerminalFont != "" {
		family = w.settings.TerminalFont
	}
	size := terminalFontSizeDefault
	if w.settings.TerminalFontSize > 0 {
		size = w.settings.TerminalFontSize
	}
	return terminal.Font{
		Family:     family + ", SF Mono, Menlo, monospace",
		Size:       size,
		Weight:     400,
		LineHeight: 1.2,
	}
}

// terminalFontSize is the size in force, defaulted for the row that shows
// it and for a settings file from before the choice.
func (w *workspace) terminalFontSize() float32 {
	if w.settings.TerminalFontSize > 0 {
		return w.settings.TerminalFontSize
	}
	return terminalFontSizeDefault
}

// interfaceFont is the family the whole app draws in: the terminal's, so
// the sidebar, its menus and the settings window read as one program with
// the panes.
func (w *workspace) interfaceFont() string {
	if w.settings.TerminalFont != "" {
		return w.settings.TerminalFont
	}
	return terminalFont
}

// applyInterfaceFont gives the frame's text the family picked for the
// terminal. Context.SetTheme hands it to the root and to the widgets built
// after it, which is every widget of the frame.
func (w *workspace) applyInterfaceFont(c *ui.Context) {
	family := w.interfaceFont()
	if c.Theme().Font == family {
		return
	}
	t := *c.Theme()
	t.Font = family
	c.SetTheme(&t)
}

var (
	monoFamiliesOnce sync.Once
	monoFamilies     []string
)

// terminalFontChoices lists the families the font picker offers: the
// embedded one first, then the system's monospaced families. current is
// kept at the end when it is not among them, so a hand-picked family a
// font manager has since removed still shows what the file says.
func terminalFontChoices(current string) []string {
	monoFamiliesOnce.Do(func() {
		monoFamilies = []string{terminalFont}
		for _, f := range ui.FontFamilies() {
			if f.Monospaced && f.Name != terminalFont {
				monoFamilies = append(monoFamilies, f.Name)
			}
		}
	})
	if current == "" || slices.Contains(monoFamilies, current) {
		return monoFamilies
	}
	return append(slices.Clone(monoFamilies), current)
}

// The four faces cover regular, bold, italic and bold italic; Core Text
// picks between them by weight and slant. OFL.txt beside them licenses them.
var (
	//go:embed resources/fonts/JetBrainsMonoNerdFontMono-Regular.ttf
	fontRegular []byte

	//go:embed resources/fonts/JetBrainsMonoNerdFontMono-Bold.ttf
	fontBold []byte

	//go:embed resources/fonts/JetBrainsMonoNerdFontMono-Italic.ttf
	fontItalic []byte

	//go:embed resources/fonts/JetBrainsMonoNerdFontMono-BoldItalic.ttf
	fontBoldItalic []byte
)

// registerTerminalFont adds the embedded faces under terminalFont. A face
// that fails leaves the terminal on the fallbacks pane.go names.
func registerTerminalFont() {
	for _, data := range [][]byte{fontRegular, fontBold, fontItalic, fontBoldItalic} {
		if err := ui.RegisterFont(data, terminalFont); err != nil {
			log.Printf("gexi: terminal font: %v", err)
		}
	}
}
