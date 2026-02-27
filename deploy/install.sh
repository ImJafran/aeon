#!/bin/bash
set -e

# Aeon Installation Script
# Usage: curl -sSL https://raw.githubusercontent.com/jafran/aeon/main/deploy/install.sh | bash

VERSION="${AEON_VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
AEON_HOME="${AEON_HOME:-$HOME/.aeon}"

echo "🌱 Installing Aeon..."
echo ""

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    arm64)   ARCH="arm64" ;;
    *)       echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

echo "  OS: $OS ($ARCH)"

# Download binary
if [ "$VERSION" = "latest" ]; then
    echo "  Downloading latest release..."
else
    echo "  Downloading version $VERSION..."
fi

# For now, build from source
if command -v go &> /dev/null; then
    echo "  Go found, building from source..."
    TEMP_DIR=$(mktemp -d)
    trap "rm -rf $TEMP_DIR" EXIT

    cd "$TEMP_DIR"
    git clone --depth 1 https://github.com/jafran/aeon.git .
    CGO_ENABLED=0 go build -ldflags="-s -w" -o aeon ./cmd/aeon

    echo "  Installing to $INSTALL_DIR..."
    if [ -w "$INSTALL_DIR" ]; then
        cp aeon "$INSTALL_DIR/aeon"
    else
        sudo cp aeon "$INSTALL_DIR/aeon"
    fi
    chmod +x "$INSTALL_DIR/aeon"
else
    echo "  Error: Go is required to build from source."
    echo "  Install Go from https://go.dev/dl/ and try again."
    exit 1
fi

echo ""
echo "✓ Aeon installed to $INSTALL_DIR/aeon"
echo ""

# Run init if not already configured
if [ ! -f "$AEON_HOME/config.yaml" ]; then
    echo "Running first-time setup..."
    echo ""
    aeon init
else
    echo "Config already exists at $AEON_HOME/config.yaml"
fi

echo ""
echo "🌱 Installation complete!"
echo ""
echo "  Run 'aeon' for interactive CLI mode."
echo "  Run 'aeon serve' for daemon mode (Telegram)."
echo ""
echo "  For systemd deployment, see deploy/aeon.service"
