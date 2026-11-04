package main

import (
	"fmt"
	"sync"

	"github.com/egoist/mygo/ui"
)

// Lucide icons (MIT), drawn from inline SVG so they match shadcn's stroke
// weight instead of the nearest text glyph.
var iconPaths = map[string]string{
	"panel-left":  `<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M9 3v18"/>`,
	"plus":        `<path d="M5 12h14"/><path d="M12 5v14"/>`,
	"columns-2":   `<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M12 3v18"/>`,
	"rows-2":      `<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M3 12h18"/>`,
	"x":           `<path d="M18 6 6 18"/><path d="m6 6 12 12"/>`,
	"terminal":    `<polyline points="4 17 10 11 4 5"/><line x1="12" x2="20" y1="19" y2="19"/>`,
	"layout-grid": `<rect width="7" height="7" x="3" y="3" rx="1"/><rect width="7" height="7" x="14" y="3" rx="1"/><rect width="7" height="7" x="14" y="14" rx="1"/><rect width="7" height="7" x="3" y="14" rx="1"/>`,
	"grid-2x2":    `<rect width="18" height="18" x="3" y="3" rx="2"/><path d="M3 12h18"/><path d="M12 3v18"/>`,
}

var iconCache sync.Map // "name|color" -> ui.ImageSource

func iconSource(name, color string) ui.ImageSource {
	key := name + "|" + color
	if v, ok := iconCache.Load(key); ok {
		return v.(ui.ImageSource)
	}
	paths, ok := iconPaths[name]
	if !ok {
		return nil
	}
	svg := fmt.Sprintf(
		`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="%s" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">%s</svg>`,
		color, paths,
	)
	src, err := ui.DecodeImage([]byte(svg))
	if err != nil {
		return nil
	}
	iconCache.Store(key, src)
	return src
}

func colorHex(c ui.Color) string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

// iconButton is a 26pt hit area with a Lucide icon and a quiet hover state,
// in place of mygo's default button chrome.
func iconButton(c *ui.Context, tk tokens, name string, action func()) {
	el := ui.Row(c).Width(iconSize).Height(iconSize).Center().Cursor(ui.CursorPointer)
	if el.Hovered() {
		el = el.Background(tk.Accent)
	}
	el.OnClick(action).Children(func() {
		if src := iconSource(name, colorHex(tk.MutedForeground)); src != nil {
			ui.Image(c, src).Width(14).Height(14)
		}
	})
}
