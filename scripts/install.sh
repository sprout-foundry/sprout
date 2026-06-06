#!/bin/sh
# sprout one-line install script
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
    printf '%b[INFO]%b %s\n' "$BLUE" "$NC" "$1"
}

log_success() {
    printf '%b[SUCCESS]%b %s\n' "$GREEN" "$NC" "$1"
}

log_warn() {
    printf '%b[WARN]%b %s\n' "$YELLOW" "$NC" "$1"
}

log_error() {
    printf '%b[ERROR]%b %s\n' "$RED" "$NC" "$1" >&2
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

# Detect operating system.
#
# Termux (Android) reports `Linux` from uname but needs a different install
# prefix and skips system-service registration — it's handled via
# is_termux() at call sites, not as a separate "os" value, because the
# release pipeline cross-compiles linux-arm64 with CGO disabled, producing
# a static binary that runs unmodified on Bionic libc.
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
        CYGWIN*|MINGW*|MSYS*)
            log_error "Detected $os (Cygwin / MSYS / Git Bash)."
            log_error "On Windows use install.ps1 from PowerShell instead:"
            log_error "  irm https://raw.githubusercontent.com/sprout-foundry/sprout/main/scripts/install.ps1 | iex"
            exit 1
            ;;
        FreeBSD|OpenBSD|NetBSD|DragonFly)
            log_error "BSD platforms are not supported by the release pipeline."
            log_error "Build from source: https://github.com/sprout-foundry/sprout#from-source"
            exit 1
            ;;
        *)
            log_error "Unsupported operating system: $os"
            log_error "Supported: Linux, macOS (Darwin), Termux on Android"
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

# Determine install directory, preferring the location of any existing sprout binary
get_install_dir() {
    if [ -n "${SPROUT_INSTALL_DIR:-}" ]; then
        echo "$SPROUT_INSTALL_DIR"
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

    # If sprout is already installed somewhere on PATH, upgrade in place.
    # Also check for legacy ledit binary to allow in-place rebrand upgrades.
    local existing
    existing=$(command -v sprout 2>/dev/null || true)
    if [ -z "$existing" ]; then
        existing=$(command -v ledit 2>/dev/null || true)
    fi
    if [ -n "$existing" ]; then
        local existing_dir
        existing_dir=$(dirname "$existing")
        # Resolve symlinks so we write to the real location
        if command -v realpath >/dev/null 2>&1; then
            existing_dir=$(dirname "$(realpath "$existing")")
        fi
        echo "$existing_dir"
        return
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

# Get version from environment or fetch latest from GitHub.
# GitHub's unauthenticated API limit is 60 req/hr per IP, which corporate
# NATs blow past easily — surface SPROUT_VERSION as the workaround in the
# error rather than letting users guess.
get_version() {
    if [ -n "${SPROUT_VERSION:-}" ]; then
        echo "$SPROUT_VERSION"
    else
        local api_url="https://api.github.com/repos/sprout-foundry/sprout/releases/latest"
        local version
        version=$(curl --fail --show-error --silent "$api_url" | awk -F'"' '/"tag_name":/ {print $4; exit}')
        if [ -z "$version" ]; then
            log_error "Failed to get version from GitHub API."
            log_error "If you're behind a proxy or hitting the 60 req/hr unauthenticated"
            log_error "rate limit, pin a version explicitly:"
            log_error "  SPROUT_VERSION=v0.14.0 curl -fsSL .../install.sh | sh"
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

    local filename="sprout-${os}-${arch}.tar.gz"
    local download_url="https://github.com/sprout-foundry/sprout/releases/download/${version}/${filename}"

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
            sudo cp "$TEMP_DIR/$extracted_binary" "${install_dir}/sprout"
            sudo chmod +x "${install_dir}/sprout"
        else
            log_error "Installation requires sudo but sudo is not available"
            exit 1
        fi
    else
        cp "$TEMP_DIR/$extracted_binary" "${install_dir}/sprout"
        chmod +x "${install_dir}/sprout"
    fi
    
    log_success "sprout installed to $install_dir/sprout"
}

# Verify installation
verify_installation() {
    local install_dir="$1"
    local binary_path="${install_dir}/sprout"

    if [ ! -f "$binary_path" ]; then
        log_error "sprout binary not found at $binary_path"
        exit 1
    fi

    if [ ! -x "$binary_path" ]; then
        log_error "sprout binary is not executable"
        exit 1
    fi

    # Actually exec the binary. Catches the only mode where a wrong-libc
    # binary can land cleanly on disk but blow up at runtime — most
    # commonly: Termux pulling a glibc-linked binary, or someone uname
    # spoofing. The release pipeline cross-compiles linux-arm64 with CGO
    # disabled so this should normally pass on Termux too.
    if ! "$binary_path" version >/dev/null 2>&1; then
        log_error "sprout was installed to $binary_path but failed to run."
        if is_termux; then
            log_error "On Termux this usually means the binary is dynamically linked"
            log_error "against glibc and won't load on Bionic libc."
            log_error "Workaround: build from source."
            log_error "  pkg install golang nodejs make git"
            log_error "  git clone https://github.com/sprout-foundry/sprout.git"
            log_error "  cd sprout && make deploy-ui && go install ."
        else
            log_error "Try running '$binary_path version' directly to see the error."
        fi
        exit 1
    fi

    log_success "sprout binary verified"
}

# Remove old versions
remove_old_versions() {
    local install_dir="$1"
    local binary_path="${install_dir}/sprout"

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
    log_info "To uninstall sprout:"
    echo ""
    echo "  # Remove the binary"
    if [ -w "$install_dir" ]; then
        echo "  rm -f \"$install_dir/sprout\""
    else
        echo "  sudo rm -f \"$install_dir/sprout\""
    fi
    echo ""
}

# Print success message
print_success() {
    local install_dir="$1"
    local version="$2"
    
    echo ""
    log_success "sprout $version installed successfully!"
    echo ""
    echo "  Binary location: $install_dir/sprout"
    echo ""
    echo "  Run 'sprout version' to verify the installation"
    echo ""
}

# Main function
main() {
    # Check for uninstall flag
    if [ "${1:-}" = "--uninstall" ] || [ "${1:-}" = "-u" ]; then
        log_info "Uninstalling sprout..."
        
        local install_dir
        if [ -n "${SPROUT_INSTALL_DIR:-}" ]; then
            install_dir="$SPROUT_INSTALL_DIR"
        else
            install_dir=$(get_install_dir)
        fi
        
        local binary_path="${install_dir}/sprout"

        # Stop and uninstall service before removing binary
        if command -v sprout >/dev/null 2>&1; then
            command sprout service uninstall 2>/dev/null || true
        fi

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
            log_success "sprout uninstalled successfully"
        else
            log_warn "sprout not found at $binary_path"
        fi

        # Clean up legacy ledit binary if present
        local legacy_binary
        legacy_binary=$(command -v ledit 2>/dev/null || true)
        if [ -n "$legacy_binary" ]; then
            log_info "Removing legacy 'ledit' binary..."
            rm -f "$legacy_binary" 2>/dev/null || sudo rm -f "$legacy_binary" 2>/dev/null || true
        fi

        # Clean up legacy ledit service files
        case "$(uname -s)" in
            Darwin)
                if [ -f "${HOME}/Library/LaunchAgents/com.ledit.daemon.plist" ]; then
                    launchctl unload "${HOME}/Library/LaunchAgents/com.ledit.daemon.plist" 2>/dev/null || true
                    rm -f "${HOME}/Library/LaunchAgents/com.ledit.daemon.plist"
                fi
                ;;
            Linux)
                if [ -f "${HOME}/.config/systemd/user/ledit.service" ]; then
                    systemctl --user stop ledit.service 2>/dev/null || true
                    systemctl --user disable ledit.service 2>/dev/null || true
                    rm -f "${HOME}/.config/systemd/user/ledit.service"
                    systemctl --user daemon-reload 2>/dev/null || true
                fi
                ;;
        esac

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
    log_info "Installing sprout version: $version"
    
    # Determine install directory
    local install_dir
    install_dir=$(get_install_dir)
    log_info "Installing to: $install_dir"

    # Detect whether this is an upgrade (existing binary on PATH)
    # Also check now (before removing the old binary) whether the service daemon
    # is registered — we'll re-install it automatically after the upgrade.
    local service_was_installed="false"
    local existing_binary
    existing_binary=$(command -v sprout 2>/dev/null || true)
    if [ -z "$existing_binary" ]; then
        # Also check for legacy ledit binary from before the rebrand
        existing_binary=$(command -v ledit 2>/dev/null || true)
    fi
    if [ -n "$existing_binary" ]; then
        local old_version
        old_version=$("$existing_binary" version 2>/dev/null | head -1 || echo "unknown")
        log_info "Upgrading from: $old_version"
    fi

    # Detect installed service files before removing the old binary.
    # Check both current sprout paths and legacy ledit paths.
    case "$(uname -s)" in
        Darwin)
            if [ -f "${HOME}/Library/LaunchAgents/com.sprout.daemon.plist" ] || \
               [ -f "${HOME}/Library/LaunchAgents/com.ledit.daemon.plist" ]; then
                service_was_installed="true"
            fi
            ;;
        Linux)
            if [ -f "${HOME}/.config/systemd/user/sprout.service" ] || \
               [ -f "${HOME}/.config/systemd/user/ledit.service" ]; then
                service_was_installed="true"
            fi
            ;;
    esac

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
    install_binary "$TEMP_DIR/sprout-${os}-${arch}.tar.gz" "$install_dir"
    
    # Verify installation
    verify_installation "$install_dir"

    # Clean up legacy ledit binary if present on PATH
    local legacy_binary
    legacy_binary=$(command -v ledit 2>/dev/null || true)
    if [ -n "$legacy_binary" ]; then
        log_info "Removing legacy 'ledit' binary..."
        if ! rm -f "$legacy_binary" 2>/dev/null; then
            if command -v sudo >/dev/null 2>&1; then
                sudo rm -f "$legacy_binary" 2>/dev/null || true
            fi
        fi
    fi

    # Clean up legacy ledit service files
    case "$(uname -s)" in
        Darwin)
            if [ -f "${HOME}/Library/LaunchAgents/com.ledit.daemon.plist" ]; then
                log_info "Removing legacy ledit service..."
                launchctl unload "${HOME}/Library/LaunchAgents/com.ledit.daemon.plist" 2>/dev/null || true
                rm -f "${HOME}/Library/LaunchAgents/com.ledit.daemon.plist"
            fi
            ;;
        Linux)
            if [ -f "${HOME}/.config/systemd/user/ledit.service" ]; then
                log_info "Removing legacy ledit service..."
                systemctl --user stop ledit.service 2>/dev/null || true
                systemctl --user disable ledit.service 2>/dev/null || true
                rm -f "${HOME}/.config/systemd/user/ledit.service"
                systemctl --user daemon-reload 2>/dev/null || true
            fi
            ;;
    esac

    # If the service was previously installed, reinstall it now so the service
    # unit/plist points at the newly installed binary. Skip on Termux — it
    # has neither systemd nor launchd, so 'sprout service install' would fail.
    if [ "$service_was_installed" = "true" ] && ! is_termux; then
        log_info "Reinstalling sprout service to point at the updated binary..."
        if "${install_dir}/sprout" service install; then
            log_success "Service reinstalled successfully."
        else
            log_warn "Service reinstall failed. Run 'sprout service install' manually."
        fi
    fi

    # Print success message
    print_success "$install_dir" "$version"
    
    # Print uninstall instructions
    print_uninstall_instructions "$install_dir"
    
    # Cleanup is handled by trap
}

main "$@"
