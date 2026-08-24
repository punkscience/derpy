#!/usr/bin/env bash
# derpy one-liner install script.
# Detects the current platform and prints the appropriate install command,
# or runs it directly for Linux.
#
# Usage:
#   curl -fsSL https://punk-science-studios-inc.github.io/derpy/install.sh | bash
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
        # When piped via curl|bash or bash <(curl), $0 is /dev/fd/N which
        # sudo cannot read. Re-fetch from the canonical URL in that case.
        if [ ! -f "$0" ] || [ "${0#/dev/fd/}" != "$0" ]; then
            SCRIPT_URL="https://punk-science-studios-inc.github.io/derpy/install.sh"
            TMP_SCRIPT="$(mktemp /tmp/derpy-install.XXXXXX)"
            curl -fsSL "$SCRIPT_URL" -o "$TMP_SCRIPT"
            exec sudo bash "$TMP_SCRIPT"
        fi
        exec sudo bash "$0"
    fi

    echo -e "${BOLD}📦 derpy installer — Linux (APT)${NC}"

    # 1. Install GPG signing key (binary format, ready for apt)
    echo "  → Installing derpy APT signing key…"
    curl -fsSL https://punk-science-studios-inc.github.io/derpy/apt/derpy-archive-keyring.gpg \
        -o /usr/share/keyrings/derpy-archive-keyring.gpg

    # 2. Add apt source
    echo "  → Adding derpy APT source…"
    cat > /etc/apt/sources.list.d/derpy.list <<'SOURCELIST'
deb [arch=amd64,arm64 signed-by=/usr/share/keyrings/derpy-archive-keyring.gpg] https://punk-science-studios-inc.github.io/derpy/apt/ stable main
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
# Windows: install via direct download or Chocolatey
# ---------------------------------------------------------------------------
install_windows() {
    echo -e "${BOLD}📦 derpy installer — Windows${NC}"
    echo ""

    local arch="amd64"
    local tag="v1.0.0"
    local version="1.0.0"

    # Detect ARM64
    if [ "$(uname -m)" = "aarch64" ] || [ "$PROCESSOR_ARCHITECTURE" = "ARM64" ]; then
        arch="arm64"
    fi

    local zip="derpy_${version}_windows_${arch}.zip"
    local url="https://github.com/Punk-Science-Studios-Inc/derpy/releases/download/${tag}/${zip}"
    local install_dir="$HOME/.local/bin"

    mkdir -p "$install_dir"

    echo "  → Downloading derpy ${tag} (${arch})…"
    curl -fsSL "$url" -o "$TEMP/$zip"

    echo "  → Extracting to $install_dir …"
    unzip -o "$TEMP/$zip" -d "$install_dir"
    rm -f "$TEMP/$zip"

    echo ""
    echo -e "${GREEN}✓ derpy installed to $install_dir/derpy.exe${NC}"
    echo ""
    echo "  Make sure $install_dir is on your PATH, or run:"
    echo "    & \"$install_dir\\derpy.exe\" --help"
    echo ""
    echo "  Coming soon: choco install derpy"
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
