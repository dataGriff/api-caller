#!/bin/sh
# Generate THIRD_PARTY_NOTICES.md: the licence of every module compiled into apic.
# Usage: scripts/notices.sh [output-file]
set -eu
out="${1:-THIRD_PARTY_NOTICES.md}"
{
  echo "# Third-party notices"
  echo
  echo "apic is built from the following open source modules. Each is used under its own licence, reproduced below."
  echo
  go list -deps -f '{{if and .Module (not .Standard)}}{{.Module.Path}}{{end}}' ./cmd/apic | grep -v '^$' | grep -v '^github.com/dataGriff/api-caller' | sort -u |
  while read -r mod; do
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
    [ "$found" = 1 ] || echo "_No licence file found in module; see the module's repository._"
    echo
  done
} > "$out"
echo "wrote $out ($(grep -c '^## ' "$out") modules)"
