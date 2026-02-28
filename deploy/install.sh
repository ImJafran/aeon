#!/bin/bash
set -e

# Aeon Installation Script
# Usage: curl -sSL https://raw.githubusercontent.com/ImJafran/aeon/main/deploy/install.sh | bash

REPO="ImJafran/aeon"
VERSION="${AEON_VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
AEON_HOME="${AEON_HOME:-$HOME/.aeon}"

echo ""
echo "  Aeon — Self-Evolving AI Agent"
echo "  =============================="
echo ""

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
    x86_64)  ARCH="amd64" ;;
    aarch64) ARCH="arm64" ;;
    arm64)   ARCH="arm64" ;;
    *)       echo "  Unsupported architecture: $ARCH"; exit 1 ;;
esac

case "$OS" in
    linux|darwin) ;;
    *)  echo "  Unsupported OS: $OS"; exit 1 ;;
esac

echo "  OS:   $OS/$ARCH"

# Try go install first (preferred)
if command -v go &> /dev/null; then
    GO_VER=$(go version | awk '{print $3}' | sed 's/go//')
    echo "  Go:   $GO_VER"
    echo ""
    echo "  Installing via 'go install'..."
    CGO_ENABLED=0 go install github.com/$REPO/cmd/aeon@latest

    # go install puts it in GOBIN or GOPATH/bin
    GOBIN=$(go env GOBIN)
    [ -z "$GOBIN" ] && GOBIN="$(go env GOPATH)/bin"

    if [ -f "$GOBIN/aeon" ]; then
        echo "  Installed to $GOBIN/aeon"
        # Optionally copy to INSTALL_DIR if it's different
        if [ "$GOBIN" != "$INSTALL_DIR" ] && [ "$INSTALL_DIR" = "/usr/local/bin" ]; then
            echo "  (also available at $GOBIN/aeon — add \$GOPATH/bin to your PATH)"
        fi
    fi
else
    echo "  Go:   not found"
    echo ""
    echo "  Building from source..."

    # Check for git
    if ! command -v git &> /dev/null; then
        echo "  Error: git is required. Install git and try again."
        exit 1
    fi

    TEMP_DIR=$(mktemp -d)
    trap "rm -rf $TEMP_DIR" EXIT

    git clone --depth 1 https://github.com/$REPO.git "$TEMP_DIR"
    cd "$TEMP_DIR"

    if ! command -v go &> /dev/null; then
        echo ""
        echo "  Error: Go 1.24+ is required to build Aeon."
        echo "  Install Go from https://go.dev/dl/ and try again."
        echo ""
        echo "  Or use 'go install':"
        echo "    go install github.com/$REPO/cmd/aeon@latest"
        exit 1
    fi

    CGO_ENABLED=0 go build -ldflags="-s -w" -o aeon ./cmd/aeon

    echo "  Installing to $INSTALL_DIR..."
    if [ -w "$INSTALL_DIR" ]; then
        cp aeon "$INSTALL_DIR/aeon"
    else
        sudo cp aeon "$INSTALL_DIR/aeon"
    fi
    chmod +x "$INSTALL_DIR/aeon"
fi

echo ""

# Run init if not already configured
if [ ! -f "$AEON_HOME/config.yaml" ]; then
    echo "  Running first-time setup..."
    echo ""
    aeon init
else
    echo "  Config already exists at $AEON_HOME/config.yaml"
fi

echo ""
echo "  Installation complete!"
echo ""
echo "  Next steps:"
echo "    aeon init     # first-time setup (if not done)"
echo "    aeon          # interactive CLI mode"
echo "    aeon serve    # daemon mode (Telegram bot)"
echo ""
echo "  Docs: https://github.com/$REPO"
echo ""
