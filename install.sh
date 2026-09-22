#!/bin/sh
# install.sh — download and install the ml binary from GitHub releases.
#
# Usage:
#   curl -sfL https://raw.githubusercontent.com/Aliancn/mdlive/main/install.sh | sh
#   sh install.sh                      # latest release
#   sh install.sh v0.2.0               # a specific version
#   ML_INSTALL_DIR=~/bin sh install.sh # custom install directory
#
# Covers macOS and Linux (amd64/arm64). Windows: use Git Bash/WSL, or
# download mdlive_v*_windows_amd64.tar.gz from the releases page.
set -eu

REPO="Aliancn/mdlive"

if [ "$#" -gt 1 ]; then
	echo "usage: $0 [version]" >&2
	exit 1
fi
VERSION="${1:-${ML_VERSION:-}}"

case "$(uname -s)" in
Darwin) OS="darwin" ;;
Linux) OS="linux" ;;
MINGW* | MSYS* | CYGWIN*)
	echo "error: native Windows is not supported by this script." >&2
	echo "download mdlive_v*_windows_amd64.tar.gz from https://github.com/$REPO/releases" >&2
	exit 1
;;
*)
	echo "error: unsupported OS: $(uname -s)" >&2
	exit 1
;;
esac

case "$(uname -m)" in
arm64 | aarch64) ARCH="arm64" ;;
x86_64 | amd64) ARCH="amd64" ;;
*)
	echo "error: unsupported architecture: $(uname -m)" >&2
	exit 1
;;
esac

fetch() {
	# fetch <url> <output-file>
	if command -v curl > /dev/null 2>&1; then
		curl -fsSL "$1" -o "$2"
	elif command -v wget > /dev/null 2>&1; then
		wget -qO "$2" "$1"
	else
		echo "error: need curl or wget to download" >&2
		exit 1
	fi
}

if [ -z "$VERSION" ]; then
	VERSION=$(fetch "https://api.github.com/repos/$REPO/releases/latest" - | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p')
	if [ -z "$VERSION" ]; then
		echo "error: cannot determine the latest release" >&2
		exit 1
	fi
fi
case "$VERSION" in
v*) ;;
*) VERSION="v$VERSION" ;;
esac

EXT="tar.gz"
[ "$OS" = "darwin" ] && EXT="zip"
ASSET="mdlive_${VERSION}_${OS}_${ARCH}.${EXT}"
URL="https://github.com/$REPO/releases/download/${VERSION}/${ASSET}"
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

echo "downloading $URL"
fetch "$URL" "$TMP/$ASSET"
fetch "https://github.com/$REPO/releases/download/${VERSION}/checksums.txt" "$TMP/checksums.txt"

WANT=$(sed -n "s/^\([0-9a-f]\{64\}\)  $ASSET\$/\1/p" "$TMP/checksums.txt")
if [ -z "$WANT" ]; then
	echo "error: $ASSET not found in checksums.txt" >&2
	exit 1
fi
if command -v sha256sum > /dev/null 2>&1; then
	GOT=$(sha256sum "$TMP/$ASSET" | cut -d' ' -f1)
else
	GOT=$(shasum -a 256 "$TMP/$ASSET" | cut -d' ' -f1)
fi
if [ "$GOT" != "$WANT" ]; then
	echo "error: checksum mismatch: $GOT != $WANT" >&2
	exit 1
fi

case "$EXT" in
zip) unzip -q -o "$TMP/$ASSET" -d "$TMP" ;;
tar.gz) tar -xzf "$TMP/$ASSET" -C "$TMP" ;;
esac
if [ ! -f "$TMP/ml" ]; then
	echo "error: archive does not contain the ml binary" >&2
	exit 1
fi

if [ -n "${ML_INSTALL_DIR:-}" ]; then
	INSTALL_DIR="$ML_INSTALL_DIR"
elif [ -w /usr/local/bin ] || mkdir -p /usr/local/bin 2> /dev/null; then
	INSTALL_DIR="/usr/local/bin"
elif command -v sudo > /dev/null 2>&1; then
	sudo install -m 0755 "$TMP/ml" /usr/local/bin/ml
	echo "installed $VERSION to /usr/local/bin/ml"
	exit 0
else
	INSTALL_DIR="$HOME/.local/bin"
fi

mkdir -p "$INSTALL_DIR"
install -m 0755 "$TMP/ml" "$INSTALL_DIR/ml"
echo "installed $VERSION to $INSTALL_DIR/ml"
case ":$PATH:" in
*":$INSTALL_DIR:"*) ;;
*)
	echo "note: add $INSTALL_DIR to your PATH, e.g.:"
	echo "  echo 'export PATH=\"$INSTALL_DIR:\$PATH\"' >> ~/.zshrc"
;;
esac
