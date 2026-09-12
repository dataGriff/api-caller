#!/bin/sh
# Install the latest apic release binary from GitHub.
#   curl -fsSL https://raw.githubusercontent.com/dataGriff/api-caller/main/install.sh | sh
# Options: APIC_VERSION=v1.2.3 to pin, APIC_INSTALL_DIR to choose the directory.
set -eu

REPO="dataGriff/api-caller"
VERSION="${APIC_VERSION:-}"
INSTALL_DIR="${APIC_INSTALL_DIR:-}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "unsupported architecture: $arch" >&2; exit 1 ;;
esac
case "$os" in
  linux|darwin) ;;
  *) echo "unsupported OS: $os (download a Windows zip from the releases page)" >&2; exit 1 ;;
esac

if [ -z "$VERSION" ]; then
  VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p')
  [ -n "$VERSION" ] || { echo "could not determine latest version" >&2; exit 1; }
fi
number=${VERSION#v}

if [ -z "$INSTALL_DIR" ]; then
  if [ -w /usr/local/bin ]; then INSTALL_DIR=/usr/local/bin; else INSTALL_DIR="$HOME/.local/bin"; fi
fi
mkdir -p "$INSTALL_DIR"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
url="https://github.com/$REPO/releases/download/$VERSION/apic_${number}_${os}_${arch}.tar.gz"
echo "downloading $url"
curl -fsSL "$url" | tar -xz -C "$tmp"
install -m 0755 "$tmp/apic" "$INSTALL_DIR/apic"
echo "installed apic $VERSION to $INSTALL_DIR/apic"
case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "add $INSTALL_DIR to your PATH" ;;
esac
