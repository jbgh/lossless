#!/bin/sh
# Print the CHANGELOG section for a tag (v0.1.0 or 0.1.0).
set -eu
ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
VER=${1:-}
if [ -z "$VER" ]; then
  echo "usage: $0 <tag>" >&2
  exit 2
fi
VER=${VER#v}
awk -v ver="$VER" '
  $0 ~ /^## / {
    if (seen) exit
    if (index($0, ver)) { seen = 1; print; next }
  }
  seen { print }
' "$ROOT/CHANGELOG.md"
