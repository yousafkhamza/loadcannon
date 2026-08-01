#!/usr/bin/env bash
# install.sh — fetches the latest (or pinned) loadcannon release binary for
# this OS/arch from GitHub Releases and installs it to /usr/local/bin.
#
# Usage:
#   curl -fsSL https://yousafkhamza.github.io/loadcannon/install.sh | bash
#   curl -fsSL https://yousafkhamza.github.io/loadcannon/install.sh | bash -s -- v1.0.0
set -euo pipefail

REPO="yousafkhamza/loadcannon"
VERSION="${1:-latest}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$arch" in
  x86_64) arch="amd64" ;;
  aarch64|arm64) arch="arm64" ;;
  *) echo "error: unsupported arch $arch"; exit 1 ;;
esac

if [ "$VERSION" = "latest" ]; then
  url="https://github.com/${REPO}/releases/latest/download/loadcannon_${os}_${arch}.tar.gz"
else
  url="https://github.com/${REPO}/releases/download/${VERSION}/loadcannon_${os}_${arch}.tar.gz"
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "[info] downloading $url"
curl -fsSL "$url" -o "$tmp/loadcannon.tar.gz"
tar -xzf "$tmp/loadcannon.tar.gz" -C "$tmp"

if [ -w "$INSTALL_DIR" ]; then
  mv "$tmp/loadcannon" "$INSTALL_DIR/loadcannon"
else
  echo "[info] $INSTALL_DIR not writable, using sudo"
  sudo mv "$tmp/loadcannon" "$INSTALL_DIR/loadcannon"
fi
chmod +x "$INSTALL_DIR/loadcannon"

echo "[ok] installed to $INSTALL_DIR/loadcannon"
"$INSTALL_DIR/loadcannon" version

echo
echo "loadcannon also shells out to k6 to actually generate load — install it if you haven't:"
echo "  https://k6.io/docs/get-started/installation/"
