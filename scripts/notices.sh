#!/bin/sh
# Generate THIRD_PARTY_NOTICES.md: the licence of every module compiled into apic.
#
# The module set is the union across every released platform, not just the one
# running this script: cobra pulls in mousetrap on Windows only, so a notices
# file generated on Linux alone would ship in the Windows archive incomplete.
#
# Alongside each module's licence, any NOTICE file is reproduced too (Apache
# License 2.0 section 4(d) requires it) and any PATENTS file (the additional
# grant the golang.org/x modules ship next to their BSD licence).
#
# Usage: scripts/notices.sh [output-file]
# Exits non-zero if a module has no licence file, so a release cannot ship
# notices with a hole in them.
set -eu
out="${1:-THIRD_PARTY_NOTICES.md}"

platforms="linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64"
mods=$(
  for p in $platforms; do
    GOOS=${p%/*} GOARCH=${p#*/} go list -deps \
      -f '{{if and .Module (not .Standard)}}{{.Module.Path}}{{end}}' ./cmd/apic
  done | grep -v '^$' | grep -v '^github.com/dataGriff/api-caller' | sort -u
)

missing=""
{
  echo "# Third-party notices"
  echo
  echo "apic is built from the following open source modules. Each is used under its own licence, reproduced below."
  echo
  echo "apic itself is released under the MIT Licence; see LICENSE."
  echo
  for mod in $mods; do
    dir=$(go list -m -f '{{.Dir}}' "$mod")
    ver=$(go list -m -f '{{.Version}}' "$mod")
    echo "## $mod $ver"
    echo
    found=0
    for f in "$dir"/LICENSE "$dir"/LICENSE.txt "$dir"/LICENSE.md "$dir"/LICENCE "$dir"/COPYING "$dir"/License; do
      if [ -f "$f" ]; then
        echo '```'
        cat "$f"
        echo '```'
        found=1
        break
      fi
    done
    if [ "$found" = 1 ]; then
      for extra in NOTICE NOTICE.txt PATENTS; do
        [ -f "$dir/$extra" ] || continue
        echo
        echo "### $extra"
        echo
        echo '```'
        cat "$dir/$extra"
        echo '```'
      done
    else
      echo "_No licence file found in module; see the module's repository._"
      missing="$missing $mod"
    fi
    echo
  done
} > "$out"

echo "wrote $out ($(grep -c '^## ' "$out") modules)"
if [ -n "$missing" ]; then
  echo "no licence file found for:$missing" >&2
  exit 1
fi
