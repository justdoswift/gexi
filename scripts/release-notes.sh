#!/usr/bin/env bash
# Write RELEASE_NOTES.md, the cumulative notes `mygo build` reads.
#
# mygo takes only the "## <version>" section of a changelog as release
# notes, so a jump of several versions would show the newest one alone.
# This file holds one such section whose "### <version>" headings are every
# version; the app cuts them to the versions newer than the one running
# before it shows the update (update_notes.go).
set -euo pipefail
cd "$(dirname "$0")/.."

version=$(awk -F'"' '/^[[:space:]]*"version"/ { print $4; exit }' mygo.json)
if [ -z "$version" ]; then
  echo "release-notes: mygo.json has no version" >&2
  exit 1
fi

# --current prints the version's own section, for the GitHub release body:
# the manifest's notes are cumulative so the app can show what a jump of
# several versions skipped, but a release page should read as that release.
if [ "${1-}" = "--current" ]; then
  awk -v v="$version" '
    $0 == "## " v { body = 1; next }
    /^## / { body = 0 }
    body { print }
  ' CHANGELOG.md
  exit 0
fi

{
  printf '## %s\n\n' "$version"
  awk '
    /^## / { print "### " substr($0, 4); next }
    /^# /  { next }
    { print }
  ' CHANGELOG.md
} >RELEASE_NOTES.md
