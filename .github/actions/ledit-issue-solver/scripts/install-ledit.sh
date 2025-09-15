#!/bin/bash
set -e

LEDIT_VERSION="$1"

echo "Installing ledit version: $LEDIT_VERSION"

# Ensure Go environment is set up
export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin

# Install ledit
if [ "$LEDIT_VERSION" == "latest" ]; then
    echo "Installing latest version of ledit..."
    go install github.com/alantheprice/ledit@latest
else
    echo "Installing ledit version $LEDIT_VERSION..."
    go install github.com/alantheprice/ledit@$LEDIT_VERSION
fi

# Verify installation
if ! command -v ledit &> /dev/null; then
    echo "ERROR: ledit installation failed"
    exit 1
fi

INSTALLED_VERSION=$(ledit --version 2>/dev/null || echo "unknown")
echo "Ledit installed successfully: $INSTALLED_VERSION"
echo "Installation path: $(which ledit)"

# Make sure ledit is accessible
echo "PATH=$PATH" >> $GITHUB_ENV