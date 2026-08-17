#!/bin/sh
# Build GitHub Release assets for the current VERSION.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
cd "$ROOT"
VERSION="${VERSION:-0.1.0}"
VERSION="${VERSION#v}"
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo unknown)
OUT="${OUT:-dist}"

rm -rf "$OUT"
mkdir -p "$OUT"

for pair in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64; do
  goos=${pair%/*}
  goarch=${pair#*/}
  name="lossless-${goos}-${goarch}"
  echo "building $name"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath \
    -ldflags "-s -w -X lossless/internal/version.Version=${VERSION} -X lossless/internal/version.Commit=${COMMIT}" \
    -o "$OUT/$name" ./cmd/lossless
done

cp scripts/install.sh "$OUT/install.sh"
chmod 755 "$OUT/install.sh"

(
  cd "$OUT"
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 lossless-* install.sh > SHA256SUMS
  else
    sha256sum lossless-* install.sh > SHA256SUMS
  fi
)

ls -l "$OUT"
echo "version ${VERSION} commit ${COMMIT}"
