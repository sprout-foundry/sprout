#!/bin/sh
# ledit one-line install script
set -eu

# Colors with fallback for non-tty
if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[0;33m'
    BLUE='\033[0;34m'
    NC='\033[0m' # No Color
else
    RED=''
    GREEN=''
    YELLOW=''
    BLUE=''
    NC=''
fi

log_info() {
    printf "${BLUE}[INFO]%s %s\n" "$NC" "$1"
}

log_success() {
    printf "${GREEN}[SUCCESS]%s %s\n" "$NC" "$1"
}

log_warn() {
    printf "${YELLOW}[WARN]%s %s\n" "$NC" "$1"
}

log_error() {
    printf "${RED}[ERROR]%s %s\n" "$NC" "$1" >&2
}

# Cleanup function
cleanup() {
    if [ -n "${TEMP_DIR:-}" ] && [ -d "$TEMP_DIR" ]; then
        rm -rf "$TEMP_DIR"
    fi
}

# Set up trap for cleanup
trap cleanup EXIT INT TERM

# Check for required dependencies
check_dependencies() {
    local missing=0
    for cmd in curl tar awk grep; do
        if ! command -v "$cmd" >/dev/null 2>&1; then
            log_error "$cmd is required but not installed"
            missing=1
        fi
    done
    if [ "$missing" -eq 1 ]; then
        log_error "Please install missing dependencies and try again"
        exit 1
    fi
}

# Detect operating system
detect_os() {
    local os
    os=$(uname -s)
    case "$os" in
        Darwin)
            echo "darwin"
            ;;
        Linux)
            echo "linux"
            ;;
        *)
            log_error "Unsupported operating system: $os"
            log_error "Supported: Linux, macOS (Darwin)"
            exit 1
            ;;
    esac
}

# Detect whether we are running inside Termux on Android.
is_termux() {
    [ -n "${TERMUX_VERSION:-}" ] || [ -d "/data/data/com.termux/files/usr" ]
}

# Detect architecture
detect_arch() {
    local arch
    arch=$(uname -m)
    case "$arch" in
        x86_64)
            echo "amd64"
            ;;
        aarch64)
            echo "arm64"
            ;;
        arm64)
            echo "arm64"
            ;;
        *)
            log_error "Unsupported architecture: $arch"
            log_error "Supported: x86_64 (amd64), aarch64/arm64 (arm64)"
            exit 1
            ;;
    esac
}

# Determine install directory
get_install_dir() {
    if [ -n "${LEDIT_INSTALL_DIR:-}" ]; then
        echo "$LEDIT_INSTALL_DIR"
        return
    fi

    # In Termux we must install inside the app prefix, not /usr/local/bin.
    if is_termux; then
        if [ -n "${PREFIX:-}" ] && [ -d "${PREFIX}/bin" ]; then
            echo "${PREFIX}/bin"
            return
        fi
        if [ -d "/data/data/com.termux/files/usr/bin" ]; then
            echo "/data/data/com.termux/files/usr/bin"
            return
        fi
    fi

    # Prefer /usr/local/bin (with sudo) if it's in PATH, since it's the standard location
    if echo ":${PATH}:" | grep -q ":/usr/local/bin:"; then
        echo "/usr/local/bin"
        return
    fi

    # Fall back to user-local bin if it exists on PATH
    if [ -d "$HOME/bin" ] && [ -w "$HOME/bin" ] && echo ":${PATH}:" | grep -q ":${HOME}/bin:"; then
        echo "$HOME/bin"
        return
    fi

    # Last resort: /usr/local/bin (will require sudo)
    echo "/usr/local/bin"
}

# Get version from environment or fetch latest from GitHub
get_version() {
    if [ -n "${LEDIT_VERSION:-}" ]; then
        echo "$LEDIT_VERSION"
    else
        local api_url="https://api.github.com/repos/alantheprice/ledit/releases/latest"
        local version
        version=$(curl --fail --show-error --silent "$api_url" | awk -F'"' '/"tag_name":/ {print $4; exit}')
        if [ -z "$version" ]; then
            log_error "Failed to get version from GitHub API"
            exit 1
        fi
        echo "$version"
    fi
}

# Download the release tarball
download_release() {
    local version="$1"
    local os="$2"
    local arch="$3"

    local filename="ledit-${os}-${arch}.tar.gz"
    local download_url="https://github.com/alantheprice/ledit/releases/download/${version}/${filename}"

    log_info "Downloading $filename" >&2

    curl --fail --show-error --location --progress-bar \
        -o "$TEMP_DIR/$filename" \
        "$download_url"

    # Only print the URL to stdout (captured by caller)
    echo "$download_url"
}

# Install the binary
install_binary() {
    local tarball="$1"
    local install_dir="$2"
    
    # Extract the tarball
    log_info "Extracting binary from $tarball"
    tar -xzf "$tarball" -C "$TEMP_DIR"
    
    # Determine the actual binary name in the tarball
    local extracted_binary
    extracted_binary=$(tar -tzf "$tarball" | grep -v '/$' | head -1)
    
    # Check if we need sudo
    if [ ! -w "$install_dir" ]; then
        log_warn "Installing to $install_dir requires elevated privileges"
        if command -v sudo >/dev/null 2>&1; then
            log_info "Using sudo for installation..."
            sudo mkdir -p "$install_dir"
            sudo cp "$TEMP_DIR/$extracted_binary" "${install_dir}/ledit"
            sudo chmod +x "${install_dir}/ledit"
        else
            log_error "Installation requires sudo but sudo is not available"
            exit 1
        fi
    else
        cp "$TEMP_DIR/$extracted_binary" "${install_dir}/ledit"
        chmod +x "${install_dir}/ledit"
    fi
    
    log_success "ledit installed to $install_dir/ledit"
}

# Verify installation
verify_installation() {
    local install_dir="$1"
    local binary_path="${install_dir}/ledit"
    
    if [ ! -f "$binary_path" ]; then
        log_error "ledit binary not found at $binary_path"
        exit 1
    fi
    
    if [ ! -x "$binary_path" ]; then
        log_error "ledit binary is not executable"
        exit 1
    fi
    
    log_success "ledit binary verified"
}

# Remove old versions
remove_old_versions() {
    local install_dir="$1"
    local binary_path="${install_dir}/ledit"

    if [ -f "$binary_path" ]; then
        local old_version
        old_version=$("$binary_path" version 2>/dev/null | head -1 || echo "unknown")
        log_info "Removing old version: $old_version"
        if ! rm -f "$binary_path" 2>/dev/null; then
            if command -v sudo >/dev/null 2>&1; then
                sudo rm -f "$binary_path"
            fi
        fi
    fi
}

# Print uninstall instructions
print_uninstall_instructions() {
    local install_dir="$1"
    echo ""
    log_info "To uninstall ledit:"
    echo ""
    echo "  # Remove the binary"
    if [ -w "$install_dir" ]; then
        echo "  rm -f \"$install_dir/ledit\""
    else
        echo "  sudo rm -f \"$install_dir/ledit\""
    fi
    echo ""
}

# Print success message
print_success() {
    local install_dir="$1"
    local version="$2"
    
    echo ""
    log_success "ledit $version installed successfully!"
    echo ""
    echo "  Binary location: $install_dir/ledit"
    echo ""
    echo "  Run 'ledit version' to verify the installation"
    echo ""
}

# Main function
main() {
    # Check for uninstall flag
    if [ "${1:-}" = "--uninstall" ] || [ "${1:-}" = "-u" ]; then
        log_info "Uninstalling ledit..."
        
        local install_dir
        if [ -n "${LEDIT_INSTALL_DIR:-}" ]; then
            install_dir="$LEDIT_INSTALL_DIR"
        else
            install_dir=$(get_install_dir)
        fi
        
        local binary_path="${install_dir}/ledit"
        
        if [ -f "$binary_path" ]; then
            local version
            version=$("$binary_path" version 2>/dev/null | head -1 || echo "unknown")
            log_info "Removing: $version"
            if ! rm -f "$binary_path" 2>/dev/null; then
                if command -v sudo >/dev/null 2>&1; then
                    sudo rm -f "$binary_path"
                else
                    log_error "Cannot remove $binary_path (permission denied, sudo not available)"
                    exit 1
                fi
            fi
            log_success "ledit uninstalled successfully"
        else
            log_warn "ledit not found at $binary_path"
        fi
        
        print_uninstall_instructions "$install_dir"
        exit 0
    fi
    
    # Check dependencies
    log_info "Checking dependencies..."
    check_dependencies
    
    # Create temporary directory
    TEMP_DIR=$(mktemp -d)
    
    # Detect OS and architecture
    log_info "Detecting operating system and architecture..."
    local os arch
    os=$(detect_os)
    arch=$(detect_arch)
    log_info "Detected: $os-$arch"
    
    # Get version
    local version
    version=$(get_version)
    log_info "Installing ledit version: $version"
    
    # Determine install directory
    local install_dir
    install_dir=$(get_install_dir)
    log_info "Installing to: $install_dir"

    if is_termux; then
        mkdir -p "$install_dir"
    fi
    
    # Remove old versions if they exist
    remove_old_versions "$install_dir"
    
    # Download the release
    local download_url
    download_url=$(download_release "$version" "$os" "$arch")
    log_info "Downloaded from: $download_url"
    
    # Install the binary
    install_binary "$TEMP_DIR/ledit-${os}-${arch}.tar.gz" "$install_dir"
    
    # Verify installation
    verify_installation "$install_dir"
    
    # Print success message
    print_success "$install_dir" "$version"
    
    # Print uninstall instructions
    print_uninstall_instructions "$install_dir"
    
    # Cleanup is handled by trap
}

main "$@"
