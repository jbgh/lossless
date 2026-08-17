#!/bin/sh
# Install lossless from GitHub Releases into ~/.local/bin.
# Optional: GITHUB_TOKEN / GH_TOKEN for a private repo.
set -eu

REPO="${LOSSLESS_UPDATE_REPO:-jbgh/lossless}"
DEST="${LOSSLESS_DEST:-${HOME}/.local/bin/lossless}"
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *)
    echo "unsupported arch: $ARCH" >&2
    exit 1
    ;;
esac
case "$OS" in
  darwin|linux) ;;
  *)
    echo "unsupported os: $OS" >&2
    exit 1
    ;;
esac

ASSET="lossless-${OS}-${ARCH}"
API="${LOSSLESS_UPDATE_API:-https://api.github.com}"
BASE="https://github.com/${REPO}/releases/latest/download"

curl_gh() {
  if [ -n "${GITHUB_TOKEN:-}" ]; then
    curl -fsSL --proto '=https' --tlsv1.2 -H "Authorization: Bearer ${GITHUB_TOKEN}" -H "User-Agent: lossless-install" "$@"
  elif [ -n "${GH_TOKEN:-}" ]; then
    curl -fsSL --proto '=https' --tlsv1.2 -H "Authorization: Bearer ${GH_TOKEN}" -H "User-Agent: lossless-install" "$@"
  else
    curl -fsSL --proto '=https' --tlsv1.2 -H "User-Agent: lossless-install" "$@"
  fi
}

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

# Prefer the latest/download URLs; fall back to the API for a private repo.
if ! curl_gh -o "$TMP/SHA256SUMS" "${BASE}/SHA256SUMS" 2>/dev/null; then
  TAG=$(curl_gh -H "Accept: application/vnd.github+json" "${API}/repos/${REPO}/releases/latest" | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' | head -1)
  if [ -z "$TAG" ]; then
    echo "could not find a GitHub release for ${REPO}" >&2
    exit 1
  fi
  BASE="https://github.com/${REPO}/releases/download/${TAG}"
  curl_gh -o "$TMP/SHA256SUMS" "${BASE}/SHA256SUMS"
fi
curl_gh -o "$TMP/$ASSET" "${BASE}/${ASSET}"

WANT=$(awk -v f="$ASSET" '$2 == f || $2 == "*"f { print $1; exit }' "$TMP/SHA256SUMS")
if [ -z "$WANT" ]; then
  echo "$ASSET missing from SHA256SUMS" >&2
  exit 1
fi
GOT=$(shasum -a 256 "$TMP/$ASSET" | awk '{ print $1 }')
if [ "$GOT" != "$WANT" ]; then
  echo "checksum mismatch for $ASSET" >&2
  exit 1
fi

mkdir -p "$(dirname "$DEST")"
# Rename replaces a dest symlink instead of writing through it.
mv "$TMP/$ASSET" "$DEST"
chmod 755 "$DEST"

echo "installed $DEST"
if ! echo ":$PATH:" | grep -q ":$(dirname "$DEST"):"; then
  echo "add $(dirname "$DEST") to PATH" >&2
fi
echo "next: $DEST setup"
echo "later: $DEST update"
