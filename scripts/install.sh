#!/bin/sh
set -e

# ==============================================================================
# WebLimbAI Universal 1-Line Installer (macOS & Linux)
# Usage: curl -fsSL https://raw.githubusercontent.com/anishraj836/WebLimbAI/main/scripts/install.sh | sh
# ==============================================================================

REPO="anishraj836/WebLimbAI"
BINARY_NAME="weblimb"

# 1. OS & Architecture Detection
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m | tr '[:upper:]' '[:lower:]')"

case "$OS" in
  darwin)
    OS="darwin"
    ;;
  linux)
    OS="linux"
    ;;
  *)
    echo "Error: Unsupported Operating System: $OS" >&2
    echo "WebLimbAI currently supports macOS (Darwin) and Linux." >&2
    exit 1
    ;;
esac

case "$ARCH" in
  x86_64|amd64)
    ARCH="amd64"
    ;;
  arm64|aarch64)
    ARCH="arm64"
    ;;
  *)
    echo "Error: Unsupported Architecture: $ARCH" >&2
    echo "WebLimbAI supports amd64 and arm64." >&2
    exit 1
    ;;
esac

# 2. Downloader Tool Detection (curl or wget)
if command -v curl >/dev/null 2>&1; then
  DOWNLOAD_CMD="curl -fsSL"
  DOWNLOAD_FILE_CMD="curl -fsSL -o"
elif command -v wget >/dev/null 2>&1; then
  DOWNLOAD_CMD="wget -qO-"
  DOWNLOAD_FILE_CMD="wget -q -O"
else
  echo "Error: Neither curl nor wget was found on your system." >&2
  exit 1
fi

# 3. Setup Temporary Workspace with Auto-Cleanup Trap
TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t 'weblimb-install')"
trap 'rm -rf "$TMP_DIR"' EXIT INT TERM

TAR_NAME="weblimb_${OS}_${ARCH}.tar.gz"
TAR_PATH="$TMP_DIR/$TAR_NAME"
CHECKSUM_PATH="$TMP_DIR/checksums.txt"

RELEASE_URL="https://github.com/${REPO}/releases/latest/download"
DOWNLOAD_URL="${RELEASE_URL}/${TAR_NAME}"
CHECKSUM_URL="${RELEASE_URL}/checksums.txt"

echo "Downloading WebLimbAI for ${OS}/${ARCH}..."
if ! $DOWNLOAD_FILE_CMD "$TAR_PATH" "$DOWNLOAD_URL"; then
  TAR_NAME_FALLBACK="agentlimbs_${OS}_${ARCH}.tar.gz"
  DOWNLOAD_URL_FALLBACK="${RELEASE_URL}/${TAR_NAME_FALLBACK}"
  if ! $DOWNLOAD_FILE_CMD "$TAR_PATH" "$DOWNLOAD_URL_FALLBACK"; then
    echo "Error: Failed to download release archive from:" >&2
    echo "   $DOWNLOAD_URL" >&2
    echo "Please check your network connection or verify release availability." >&2
    exit 1
  fi
fi

# 4. Checksum Verification
if $DOWNLOAD_FILE_CMD "$CHECKSUM_PATH" "$CHECKSUM_URL" 2>/dev/null; then
  EXPECTED_SUM="$(grep "$TAR_NAME" "$CHECKSUM_PATH" 2>/dev/null | awk '{print $1}')"
  if [ -n "$EXPECTED_SUM" ]; then
    echo "Verifying SHA-256 checksum integrity..."
    if command -v sha256sum >/dev/null 2>&1; then
      ACTUAL_SUM="$(sha256sum "$TAR_PATH" | awk '{print $1}')"
    elif command -v shasum >/dev/null 2>&1; then
      ACTUAL_SUM="$(shasum -a 256 "$TAR_PATH" | awk '{print $1}')"
    elif command -v openssl >/dev/null 2>&1; then
      ACTUAL_SUM="$(openssl dgst -sha256 "$TAR_PATH" | awk '{print $NF}')"
    fi

    if [ -n "$ACTUAL_SUM" ]; then
      if [ "$ACTUAL_SUM" != "$EXPECTED_SUM" ]; then
        echo "Error: Checksum verification failed!" >&2
        echo "   Expected: $EXPECTED_SUM" >&2
        echo "   Actual:   $ACTUAL_SUM" >&2
        exit 1
      fi
      echo "Checksum verified: $ACTUAL_SUM"
    fi
  fi
fi

# 5. Extract Binary
echo "Extracting archive..."
tar -xzf "$TAR_PATH" -C "$TMP_DIR"

if [ ! -f "$TMP_DIR/$BINARY_NAME" ]; then
  if [ -f "$TMP_DIR/lightlimbs" ]; then
    mv "$TMP_DIR/lightlimbs" "$TMP_DIR/$BINARY_NAME"
  elif [ -f "$TMP_DIR/weblimb" ]; then
    mv "$TMP_DIR/weblimb" "$TMP_DIR/$BINARY_NAME"
  elif [ -f "$TMP_DIR/agentlimbs" ]; then
    mv "$TMP_DIR/agentlimbs" "$TMP_DIR/$BINARY_NAME"
  elif [ -f "$TMP_DIR/agentlimbs-light" ]; then
    mv "$TMP_DIR/agentlimbs-light" "$TMP_DIR/$BINARY_NAME"
  else
    echo "Error: Extracted archive did not contain '$BINARY_NAME' binary." >&2
    exit 1
  fi
fi

chmod +x "$TMP_DIR/$BINARY_NAME"

# 6. Determine Installation Target Directory
if [ "$(id -u)" -eq 0 ]; then
  INSTALL_DIR="/usr/local/bin"
else
  INSTALL_DIR="$HOME/.local/bin"
fi

mkdir -p "$INSTALL_DIR"

echo "Installing $BINARY_NAME to $INSTALL_DIR..."
if [ -w "$INSTALL_DIR" ]; then
  mv "$TMP_DIR/$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"
else
  if command -v sudo >/dev/null 2>&1; then
    echo "Elevated permissions required to write to $INSTALL_DIR:"
    sudo mv "$TMP_DIR/$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"
  else
    INSTALL_DIR="$HOME/.local/bin"
    mkdir -p "$INSTALL_DIR"
    mv "$TMP_DIR/$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"
  fi
fi

if [ -w "$INSTALL_DIR" ]; then
  ln -sf "$INSTALL_DIR/$BINARY_NAME" "$INSTALL_DIR/agentlimbs" 2>/dev/null || true
  ln -sf "$INSTALL_DIR/$BINARY_NAME" "$INSTALL_DIR/lightlimbs" 2>/dev/null || true
fi

# 7. PATH Check & Guidance
PATH_CONFIGURED=0
case ":$PATH:" in
  *":$INSTALL_DIR:"*) PATH_CONFIGURED=1 ;;
esac

echo ""
echo "WebLimbAI installed successfully at: $INSTALL_DIR/$BINARY_NAME"

if [ "$PATH_CONFIGURED" -eq 0 ]; then
  echo ""
  echo "NOTE: '$INSTALL_DIR' is not currently in your \$PATH."
  echo "   Add it to your shell configuration file (e.g. ~/.zshrc or ~/.bashrc):"
  echo "     export PATH=\"$INSTALL_DIR:\$PATH\""
  echo ""
fi

echo "Next Steps:"
echo "   1. Auto-configure AI IDEs (Claude Desktop & Cursor):"
echo "      $INSTALL_DIR/$BINARY_NAME init-mcp"
echo ""
echo "   2. Scrape clean RAG Markdown directly in your terminal:"
echo "      $INSTALL_DIR/$BINARY_NAME scrape https://go.dev -j"
echo ""
echo "   3. Search documentation locally:"
echo "      $INSTALL_DIR/$BINARY_NAME search \"goroutine scheduler\""
