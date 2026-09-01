#!/bin/sh
set -e

REPO="Baranigsiz/kurisu"
BINARY="kurisugate"

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

case "$OS" in
  linux|darwin) ;;
  *) echo "Unsupported OS: $OS"; exit 1 ;;
esac

echo "⚡ Installing KurisuGate for $OS/$ARCH..."

LATEST_RELEASE=$(curl -s "https://api.github.com/repos/$REPO/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
if [ -z "$LATEST_RELEASE" ]; then
  LATEST_RELEASE="v1.0.0"
fi

URL="https://github.com/$REPO/releases/download/$LATEST_RELEASE/${BINARY}_${OS}_${ARCH}.tar.gz"

DEST="/usr/local/bin"
if [ ! -w "$DEST" ]; then
  DEST="$HOME/.local/bin"
  mkdir -p "$DEST"
fi

curl -sL "$URL" | tar xz -C "$DEST" "$BINARY"
chmod +x "$DEST/$BINARY"

echo "✔ KurisuGate successfully installed to $DEST/$BINARY!"
echo "🚀 Run '$BINARY start' to get started."
