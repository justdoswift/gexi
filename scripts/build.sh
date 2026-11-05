#!/usr/bin/env bash
# go tool mygo build, then widen AppIcon.icns past mygo's 256 px cap.
# The .dmg mygo packs still carries the 256 px icon; drag-to-Applications from
# it is the only place that matters, and it does not affect the installed app.
set -euo pipefail
cd "$(dirname "$0")/.."

go tool mygo build "$@"

shopt -s nullglob
for app in build/*/*.app; do
  scripts/fix-app-icon.sh "$app"
done
