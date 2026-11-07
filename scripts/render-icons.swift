import AppKit

let symbols: [(String, String)] = [
    ("sidebar.left", "panel-left"),
    ("plus", "plus"),
    ("magnifyingglass", "search"),
    ("gearshape", "setting"),
    ("rectangle.split.2x1", "columns-2"),
    ("rectangle.split.1x2", "rows-2"),
    ("arrow.up.left.and.arrow.down.right", "expand"),
    ("arrow.down.right.and.arrow.up.left", "collapse"),
    ("xmark", "x"),
    ("folder", "folder"),
    ("server.rack", "server"),
    ("chevron.right", "chevron-right"),
    ("chevron.down", "chevron-down"),
    ("terminal", "terminal"),
    ("bell", "bell"),
    ("keyboard", "keyboard"),
    ("slider.horizontal.3", "sliders"),
    ("doc", "file"),
    ("doc.text", "file-text"),
    ("chevron.left.forwardslash.chevron.right", "file-code"),
    ("curlybraces", "file-braces"),
    ("photo", "file-image"),
    ("lock", "file-lock"),
]

let pointSize: CGFloat = 128
let canvas: CGFloat = 192
let out = FileManager.default.currentDirectoryPath + "/icons"

for (symbol, file) in symbols {
    let config = NSImage.SymbolConfiguration(pointSize: pointSize, weight: .regular)
    guard let image = NSImage(systemSymbolName: symbol, accessibilityDescription: nil)?
        .withSymbolConfiguration(config) else {
        print("missing \(symbol)")
        continue
    }
    let square = NSImage(size: NSSize(width: canvas, height: canvas), flipped: false) { rect in
        let w = image.size.width, h = image.size.height
        image.draw(in: NSRect(x: (rect.width - w) / 2, y: (rect.height - h) / 2, width: w, height: h),
                   from: .zero, operation: .sourceOver, fraction: 1)
        return true
    }
    guard let tiff = square.tiffRepresentation,
          let rep = NSBitmapImageRep(data: tiff),
          let data = rep.representation(using: .png, properties: [:]) else { continue }
    try data.write(to: URL(fileURLWithPath: out).appendingPathComponent(file + ".png"))
    print("wrote \(file).png glyph \(image.size.width)x\(image.size.height)")
}
