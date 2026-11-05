#!/usr/bin/env bash
# mygo compiles AppIcon.icon into an .icns that stops at 256 px. macOS 26+
# reads the full-size icon from Assets.car, but older systems only see the
# .icns. This rebuilds every size from the brand PNG, swaps it into the built
# bundle, and re-signs (the signature covers Resources).
#
# A Developer ID signed bundle is left alone: replacing a resource voids its
# signature and the notarization ticket that covers it. Set
# FORCE_ICNS_PATCH=1 to patch such a bundle anyway, re-signing it.
set -euo pipefail
cd "$(dirname "$0")/.."

app="${1:-build/darwin-arm64/Gexi.app}"
src="${2:-brand/icon-product/final/png/gexi-icon-1024.png}"

[ -d "$app" ] || { echo "fix-app-icon: no app bundle at $app" >&2; exit 1; }
[ -f "$src" ] || { echo "fix-app-icon: no source icon at $src" >&2; exit 1; }

# codesign prints to stderr and grep -q closes the pipe early, which pipefail
# would report as a failure; capture first, then match.
signature=$(codesign -dv --verbose=2 "$app" 2>&1 || true)
if printf '%s\n' "$signature" | grep -q "^Authority=Developer ID Application"; then
  if [ "${FORCE_ICNS_PATCH:-0}" != "1" ]; then
    echo "fix-app-icon: $app is Developer ID signed; leaving its icns alone"
    exit 0
  fi
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
iconset="$work/AppIcon.iconset"
mkdir -p "$iconset"

for size in 16 32 128 256 512; do
  sips -z "$size" "$size" "$src" --out "$iconset/icon_${size}x${size}.png" >/dev/null
  sips -z "$((size * 2))" "$((size * 2))" "$src" --out "$iconset/icon_${size}x${size}@2x.png" >/dev/null
done

iconutil -c icns "$iconset" -o "$app/Contents/Resources/AppIcon.icns"

identity="${SIGN_IDENTITY:--}"
if [ "$identity" = "-" ]; then
  codesign --force --deep --sign - "$app"
else
  codesign --force --deep --options runtime --timestamp --sign "$identity" "$app"
fi
echo "fix-app-icon: wrote 16–1024 px AppIcon.icns into $app"
