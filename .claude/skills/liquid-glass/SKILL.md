---
name: liquid-glass
description: Build Liquid Glass surfaces in gexi (mygo's glass plugin) the way macOS 26 draws them — notification banners, menus, dialogs, sidebar rows, header chips. Covers the two material paths (native NSGlassEffectView vs the shader), which one to use where and why, the lit edge that makes a panel read as glass, tint and warmth, the hover and selected states, and the traps that make a surface come out as a grey board. Use this whenever the user asks for glass, frosted, translucent or "like the system notification" surfaces in gexi, or reports that a panel looks grey, flat, too opaque, or that its edge looks wrong.
---

# Liquid Glass in gexi

Everything here was measured off a real macOS 26 notification banner and the
gexi window beside it, or learned by getting it wrong. The numbers are real.

## The one thing to understand first

mygo's glass plugin has **two completely different material paths**, and picking
the wrong one is the source of nearly every "it looks grey" bug.

| | `Native: true` | shader (`Native: false`) |
|---|---|---|
| What it samples | what is **under the window** (desktop, other apps) | pixels the **page has already painted** |
| How it draws | `p.Hole(box, radii)` — punches a hole, material shows through from a lower layer | `paint(p, box, radii, g)` — paints in the page |
| Inset shadows | **land on the hole and vanish** | land on the material and show |
| Works over a flat fill | **yes** | **no** — nothing to blur, comes out identical to a flat fill |
| Works over app content | yes, but samples the desktop, not the content | yes, and blurs the content |

**Decision rule:**

- Surface sits on the **sidebar's flat surface** (a session row, a hovered row)
  → **Native**. The shader has nothing to blur there.
- Surface **floats over app content** and needs a lit edge (menu, dialog, chip)
  → **shader**. It can blur what is under it, and only it can carry the inset
  shadows that make the edge.
- Surface is a **pane card** → **Native** (`paneGlass`), because it should show
  the desktop through the window.

## The lit edge

A glass panel's top edge is **not a white line**. Measured off a notification
banner, scanning down through the top edge:

```
219 → 233 → 240 → 241 → 239 → 255
```

It goes **dark first, then climbs**. The surface turns away from the light at
its very edge, catches it just inside, and the band between is a gradient.

A hairline of pure white at the top (`InsetShadow(0, 1, 0, 0, white)`, blur 0)
is the opposite of that: same weight at every pixel, reads as a drawn border.

The pair that works:

```go
e = e.InsetShadow(0, 1, 2, 0, ui.RGB(0x11, 0x11, 0x1a).Alpha(0.10)). // soft shade outside
     InsetShadow(0, 2, 7, 0, ui.RGB(0xff, 0xff, 0xff).Alpha(0.55))   // light inside it
```

Both **blurred** — a band, not a line. This lives in `lit(e, tk)`.

**Only on the shader path.** On a native glass the shadows land on the hole.

## Tint and warmth — the "grey" bug

Frosted **neutral white** over a light page has no colour of its own, so it
reads as grey. A notification banner measures a **warm** white:

| | top | middle |
|---|---|---|
| macOS notification | `(255, 248, 224)` | `(255, 254, 240)` |
| gexi before the fix | `(251, 250, 245)` | `(251, 250, 245)` |

The fix is the tint, not the material. But **derive it from the theme, never
fix it**:

```go
// glassTint, in theme.go
if tk.Dark { return tk.Background }        // no warmth to add in the dark
warm := ui.RGB(0xff, 0xf8, 0xe4)
return tk.Background.MixOklab(warm, 0.55)  // the paper's own lean, warmed
```

A fixed `#fffbef` looks right on the default theme and wrong on every other:
with Claude Light (`#faf9f5`, already warm) the glass came out the same colour
as on the neutral theme while everything around it got warmer, so the glass
was the one surface that did not belong to the palette. `tokensForTheme`
derives every other chrome colour from the terminal theme's background and
foreground; the glass has to follow the same rule.

`TestGlassTintFollowsTheTheme` guards this: two themes with different papers
must not get the same tint, and the tint must be warmer than its paper.

## Clarity is a trap at small sizes

`paneMaterial` in the glass plugin computes the material from the surface's
**shorter side**:

- up to **100 DIP**: keeps the measured material, whatever `Clarity` says
- from **200 DIP**: `Clarity` takes over
- between: eases

So for a 22–32 DIP chip or row, `Clarity` does nothing. Leave it at `0`; the
frost is the point at that size. Setting it higher only thins the rim.

The shader's frost at that size uses `paneLightLow = 0.541` — black maps to
54% grey. That is the direct source of a grey board when the shader is used
where it should not be.

## Hover and selected states

**A native glass is its own layer. An element's `Opacity` does not reach it.**
A material that is set always is drawn always — that is what once put a pane of
glass behind every button at rest.

So: **set the material only while the state is on**, never fade it.

```go
if hovered {
    return el.Material(noticeGlass(tk))
}
return el.Background(ui.Transparent)
```

The cost is no fade-in. That trade is correct: a fade with glass behind every
control is worse than no fade.

## Menus

`PopoverMenuButton` / `PopoverMenu` take `Panel` and `Item` callbacks. The
theme's own panel is `Background(t.Background)` + `Border(1, t.Border)` — an
opaque fill with a hairline, which is what made a menu read as a white board.
`Panel` runs **after** the theme's look, so both have to be cleared:

```go
e = e.Radius(12).NoShadow().Border(1, ui.Transparent).Shadow(...)
```

The highlighted row: mygo sets `row.Background(t.Accent)` **before** `Item`
runs, so `Item` has to set it again to take its place. Keep the highlight — a
Mac user reads a blue menu selection without thinking — but use the desktop's
own accent, not the toolkit's fixed `#3b82f6` and not the terminal theme's
cursor colour:

```go
accent := c.Preferences().Accent   // what macOS reports
if accent.A == 0 { accent = tk.SystemAccent }
```

`tk.SystemAccent` is the **terminal theme's** cursor colour. Using it for a
menu highlight gives you an orange bar on a Mac set to blue.

**`ContextMenu` has no `Panel` option.** Right-click menus drawn in the window
cannot be restyled without changing mygo or swapping them for `PopoverMenu`.

## Surfaces in gexi, and which path each takes

| Function | Path | Used by |
|---|---|---|
| `paneGlass` | Native | the pane card |
| `noticeGlass` | Native | sidebar rows, chips, menus, dialogs |
| `noticeGlassLit` | shader | anything that also needs `lit` |
| `noticeSkin` | shader + `lit` | menus, dialogs, header chips |
| `rowSkin` | Native | sidebar selected / hovered row |
| `dialogGlass` | Native | the dialog panel |

## Verifying

**The headless renderer does not draw glass.** It is a GPU effect; a
`ui.Tester` frame shows the hole or a flat fill, never the material. Any
measurement taken from a headless frame is measuring the wrong thing.

To check a real surface:

```bash
screencapture -x -R0,0,1600,900 /tmp/shot.png
```

then read pixels with Python. The screenshot is 2x on a Retina display, so
multiply 1x coordinates by 2. Compare against a real macOS notification in the
same shot — that is the reference the user is judging against.

## Tests that guard this

- `TestDialogPanelIsTheSystemMaterial` — the dialog asks for the native glass
- `TestFloatingSurfacesStepOffThePaper` — a floating fill is not the paper's colour
- `TestHeaderChipsAreQuietAtRest` — a chip at rest paints nothing
- `TestHeaderChipsShareOneShape` — one height, one corner, one hover

A test that asserts a material's parameters is worth having. A test that
renders a headless frame and measures pixels is not — it measures the hole.

## The mistakes worth not repeating

1. **Shader over a flat surface.** Comes out identical to a flat fill. Use
   Native.
2. **Native where a lit edge is wanted.** The shadows land on the hole. Use the
   shader.
3. **Neutral white tint.** Reads grey. Use a warm white.
4. **Fading a native glass with `Opacity`.** It does not reach the material;
   the glass is drawn at rest. Set it only while the state is on.
5. **A hard white hairline as the lit edge.** Reads as a drawn border. Use the
   blurred shade-then-light pair.
6. **`tk.SystemAccent` for a menu highlight.** That is the terminal theme's
   cursor colour, not the desktop accent.
7. **Measuring a headless frame.** It does not draw glass.
8. **A fixed tint colour.** Every other chrome colour is derived from the
   terminal theme; a fixed glass tint makes the glass the one surface that
   ignores the theme. Derive it.
