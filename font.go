package main

import (
	_ "embed"
	"log"

	"github.com/egoist/mygo/ui"
)

// terminalFont is the terminal's family: JetBrains Mono with Nerd Font
// icons, embedded so panes look the same on a Mac that never installed it.
const terminalFont = "JetBrains Mono Nerd Font Mono"

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
