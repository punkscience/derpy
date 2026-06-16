#!/usr/bin/env bash
# derpy one-liner install script.
# Detects the current platform and prints the appropriate install command,
# or runs it directly for Linux.
#
# Usage:
#   curl -fsSL https://punkscience.github.io/derpy/install.sh | bash
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
BOLD='\033[1m'
NC='\033[0m' # No Color

# ---------------------------------------------------------------------------
# Platform detection
# ---------------------------------------------------------------------------
detect_platform() {
    local os
    os="$(uname -s)"
    case "$os" in
        Linux)  echo "linux" ;;
        Darwin) echo "macos" ;;
        CYGWIN*|MINGW*|MSYS*) echo "windows" ;;
        *)
            echo "unsupported: $os" >&2
            exit 1
            ;;
    esac
}

# ---------------------------------------------------------------------------
# Linux: APT install (Debian/Ubuntu)
# ---------------------------------------------------------------------------
install_linux() {
    # Require root for package operations. Re-exec via sudo if not already root
    # and not running under sudo.
    if [ "$(id -u)" -ne 0 ] && [ -z "${SUDO_USER:-}" ]; then
        echo -e "${BOLD}📦 derpy installer — Linux (APT)${NC}"
        echo ""
        echo "  This will:"
        echo "    1. Install the derpy APT repository signing key"
        echo "    2. Add the derpy APT source"
        echo "    3. Run: apt install derpy"
        echo ""
        echo "  Root privileges are required for steps 1-3."
        echo ""
        exec sudo bash "$0"
    fi

    echo -e "${BOLD}📦 derpy installer — Linux (APT)${NC}"

    # 1. Install GPG signing key
    echo "  → Installing derpy APT signing key…"
    curl -fsSL https://punkscience.github.io/derpy/apt/derpy-archive-keyring.gpg \
        -o /usr/share/keyrings/derpy-archive-keyring.gpg

    # 2. Add apt source
    echo "  → Adding derpy APT source…"
    cat > /etc/apt/sources.list.d/derpy.list <<'SOURCELIST'
deb [signed-by=/usr/share/keyrings/derpy-archive-keyring.gpg] https://punkscience.github.io/derpy/apt/ stable main
SOURCELIST

    # 3. Update and install
    echo "  → Updating package lists…"
    apt-get update -qq

    echo "  → Installing derpy…"
    apt-get install -y derpy

    echo ""
    echo -e "${GREEN}✓ derpy installed. Run: derpy --help${NC}"
}

# ---------------------------------------------------------------------------
# macOS: Homebrew install
# ---------------------------------------------------------------------------
install_macos() {
    echo -e "${BOLD}📦 derpy installer — macOS (Homebrew)${NC}"
    echo ""
    echo "  Installing via Homebrew…"
    echo ""

    if ! command -v brew &>/dev/null; then
        echo -e "${RED}Homebrew is not installed.${NC}"
        echo "  Install it first: https://brew.sh"
        exit 1
    fi

    brew install punkscience/homebrew-derpy/derpy

    echo ""
    echo -e "${GREEN}✓ derpy installed. Run: derpy --help${NC}"
}

# ---------------------------------------------------------------------------
# Windows: Chocolatey install
# ---------------------------------------------------------------------------
install_windows() {
    echo -e "${BOLD}📦 derpy installer — Windows (Chocolatey)${NC}"
    echo ""
    echo "  Run the following in an Administrator PowerShell:"
    echo ""
    echo "    choco install derpy"
    echo ""
    echo "  If you don't have Chocolatey installed, get it at:"
    echo "    https://chocolatey.org/install"
    echo ""

    # Attempt automatic install if running under a shell that has choco.
    if command -v choco &>/dev/null; then
        echo "  choco found — installing now…"
        choco install derpy -y
        echo ""
        echo -e "${GREEN}✓ derpy installed. Run: derpy --help${NC}"
    fi
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
PLATFORM="$(detect_platform)"

case "$PLATFORM" in
    linux)   install_linux ;;
    macos)   install_macos ;;
    windows) install_windows ;;
esac
