#!/bin/bash

# --- Configuration ---
GITHUB_REPO="alantheprice/ledit"
INSTALL_DIR="/usr/local/bin" # System-wide installation directory

# --- Functions ---
get_latest_release_tag() {
    curl -s "https://api.github.com/repos/$GITHUB_REPO/releases/latest" | \
    grep "tag_name" | \
    head -n 1 | \
    cut -d : -f 2 | \
    tr -d \"\, | \
    tr -d ' '
}

# --- Main Script ---

echo "Starting ledit installation..."

# 1. Detect OS and Architecture
OS=$(uname -s)
ARCH=$(uname -m)

case "$OS" in
    Linux*)
        OS_NAME="linux"
        ;;
    Darwin*)
        OS_NAME="darwin"
        ;;
    *)
        echo "Unsupported operating system: $OS"
        exit 1
        ;;
esac

case "$ARCH" in
    x86_64)
        ARCH_NAME="amd64"
        ;;
    arm64|aarch64)
        ARCH_NAME="arm64"
        ;;
    *)
        echo "Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

echo "Detected OS: $OS_NAME, Architecture: $ARCH_NAME"

# 2. Get the latest release tag
LATEST_TAG=$(get_latest_release_tag)
if [ -z "$LATEST_TAG" ]; then
    echo "Error: Could not retrieve the latest release tag from GitHub."
    exit 1
fi
echo "Latest ledit release: $LATEST_TAG"

# 3. Construct download URL
DOWNLOAD_URL="https://github.com/$GITHUB_REPO/releases/download/$LATEST_TAG/ledit_${OS_NAME}_${ARCH_NAME}.tar.gz"
BINARY_NAME="ledit" # The name of the executable inside the tar.gz

echo "Downloading from: $DOWNLOAD_URL"

# 4. Download the archive
TEMP_DIR=$(mktemp -d)
TAR_FILE="$TEMP_DIR/ledit.tar.gz"

if ! curl -L -o "$TAR_FILE" "$DOWNLOAD_URL"; then
    echo "Error: Failed to download ledit from $DOWNLOAD_URL"
    rm -rf "$TEMP_DIR"
    exit 1
fi

echo "Download complete. Extracting..."

# 5. Extract the binary
if ! tar -xzf "$TAR_FILE" -C "$TEMP_DIR"; then
    echo "Error: Failed to extract ledit.tar.gz"
    rm -rf "$TEMP_DIR"
    exit 1
fi

# Check if the binary exists in the extracted location
if [ ! -f "$TEMP_DIR/$BINARY_NAME" ]; then
    echo "Error: Expected binary '$BINARY_NAME' not found in extracted archive."
    rm -rf "$TEMP_DIR"
    exit 1
fi

echo "Extracted ledit binary."

# 6. Install the binary
echo "Installing ledit to $INSTALL_DIR..."
if [ ! -d "$INSTALL_DIR" ]; then
    echo "Warning: Installation directory $INSTALL_DIR does not exist. Attempting to create it."
    if ! sudo mkdir -p "$INSTALL_DIR"; then
        echo "Error: Failed to create $INSTALL_DIR. Please ensure you have appropriate permissions."
        rm -rf "$TEMP_DIR"
        exit 1
    fi
fi

if ! sudo mv "$TEMP_DIR/$BINARY_NAME" "$INSTALL_DIR/$BINARY_NAME"; then
    echo "Error: Failed to move ledit binary to $INSTALL_DIR. Please check permissions."
    rm -rf "$TEMP_DIR"
    exit 1
fi

# 7. Set executable permissions
if ! sudo chmod +x "$INSTALL_DIR/$BINARY_NAME"; then
    echo "Error: Failed to set executable permissions for ledit."
    rm -rf "$TEMP_DIR"
    exit 1
fi

echo "ledit installed successfully to $INSTALL_DIR"

# 8. Cleanup
rm -rf "$TEMP_DIR"
echo "Temporary files cleaned up."

# 9. Verify installation
echo ""
echo "Verifying installation..."
if command -v ledit &> /dev/null; then
    echo "ledit is in your PATH."
    echo "ledit version:"
    ledit --version # Assuming ledit has a --version flag
else
    echo "ledit is not directly in your PATH. You may need to restart your terminal or add $INSTALL_DIR to your PATH."
    echo "You can try running it with: $INSTALL_DIR/ledit"
fi

echo ""
echo "Installation complete!"
echo "To get started, run: ledit init"
