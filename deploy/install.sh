#!/bin/bash
set -e

# Aeon Installation Script
# Usage: curl -sSL https://raw.githubusercontent.com/ImJafran/aeon/main/deploy/install.sh | bash

REPO="ImJafran/aeon"
GO_VERSION="1.24.1"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"
AEON_HOME="${AEON_HOME:-$HOME/.aeon}"

echo ""
echo "  Aeon — Self-Evolving AI Agent"
echo "  =============================="
echo ""

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

case "$ARCH" in
    x86_64)  GOARCH="amd64" ;;
    aarch64) GOARCH="arm64" ;;
    arm64)   GOARCH="arm64" ;;
    *)       echo "  Unsupported architecture: $ARCH"; exit 1 ;;
esac

case "$OS" in
    linux|darwin) ;;
    *)  echo "  Unsupported OS: $OS"; exit 1 ;;
esac

echo "  OS:   $OS/$GOARCH"

# Install Go if not present
install_go() {
    echo "  Go:   not found — installing Go $GO_VERSION..."
    GO_TAR="go${GO_VERSION}.${OS}-${GOARCH}.tar.gz"
    GO_URL="https://go.dev/dl/$GO_TAR"

    TEMP_DIR=$(mktemp -d)
    trap "rm -rf $TEMP_DIR" EXIT

    curl -sSL "$GO_URL" -o "$TEMP_DIR/$GO_TAR"

    if [ -w "/usr/local" ]; then
        tar -C /usr/local -xzf "$TEMP_DIR/$GO_TAR"
    else
        sudo tar -C /usr/local -xzf "$TEMP_DIR/$GO_TAR"
    fi

    export PATH="/usr/local/go/bin:$HOME/go/bin:$PATH"

    # Add to shell profile if not already there
    SHELL_RC="$HOME/.bashrc"
    [ -f "$HOME/.zshrc" ] && SHELL_RC="$HOME/.zshrc"

    if ! grep -q '/usr/local/go/bin' "$SHELL_RC" 2>/dev/null; then
        echo 'export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin' >> "$SHELL_RC"
        echo "  Added Go to PATH in $SHELL_RC"
    fi

    echo "  Go:   $(go version | awk '{print $3}') installed"
}

if command -v go &> /dev/null; then
    echo "  Go:   $(go version | awk '{print $3}')"
else
    install_go
fi

# Ensure INSTALL_DIR exists and is in PATH
mkdir -p "$INSTALL_DIR"
if ! echo "$PATH" | grep -q "$INSTALL_DIR"; then
    SHELL_RC="$HOME/.bashrc"
    [ -f "$HOME/.zshrc" ] && SHELL_RC="$HOME/.zshrc"
    if ! grep -q "$INSTALL_DIR" "$SHELL_RC" 2>/dev/null; then
        echo "export PATH=\"\$PATH:$INSTALL_DIR\"" >> "$SHELL_RC"
        echo "  Added $INSTALL_DIR to PATH in $SHELL_RC"
    fi
    export PATH="$PATH:$INSTALL_DIR"
fi

echo ""
echo "  Installing Aeon..."
CGO_ENABLED=0 go install -ldflags="-s -w" github.com/$REPO/cmd/aeon@latest

# Copy from GOBIN to INSTALL_DIR
GOBIN=$(go env GOBIN)
[ -z "$GOBIN" ] && GOBIN="$(go env GOPATH)/bin"

if [ -f "$GOBIN/aeon" ]; then
    cp "$GOBIN/aeon" "$INSTALL_DIR/aeon"
    chmod +x "$INSTALL_DIR/aeon"
    echo "  Installed to $INSTALL_DIR/aeon"

    # Also install to /usr/local/bin so 'aeon' works globally
    if [ "$INSTALL_DIR" != "/usr/local/bin" ]; then
        if [ -w "/usr/local/bin" ]; then
            cp "$INSTALL_DIR/aeon" /usr/local/bin/aeon
            chmod +x /usr/local/bin/aeon
        else
            sudo cp "$INSTALL_DIR/aeon" /usr/local/bin/aeon
            sudo chmod +x /usr/local/bin/aeon
        fi
        echo "  Linked to /usr/local/bin/aeon"
    fi
fi

echo ""

# Run init if not already configured
if [ ! -f "$AEON_HOME/config.json" ]; then
    echo "  Running first-time setup..."
    echo ""
    "$INSTALL_DIR/aeon" init
else
    echo "  Config already exists at $AEON_HOME/config.json"
fi

echo ""
echo "  Installation complete!"
echo ""
echo "  Commands:"
echo "    aeon            # interactive CLI"
echo "    aeon serve      # daemon mode (Telegram bot)"
echo "    aeon init       # re-run setup"
echo "    aeon uninstall  # remove everything"
echo ""
echo "  Docs: https://github.com/$REPO"
echo ""
