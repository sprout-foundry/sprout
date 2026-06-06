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

# Check for required dependencies.
#
# We need ONE of {sha256sum, shasum} for checksum verification — Linux ships
# sha256sum (coreutils), macOS ships shasum (Perl). Termux has sha256sum if
# the user installed coreutils. We don't fail at check_dependencies if both
# are missing because verify_checksum() handles that case with a clearer
# message that mentions SPROUT_SKIP_CHECKSUM.
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

# curl wrapper with retries + actionable error messages on common failures.
# Wraps the most distinctive curl exit codes (6 DNS, 7 connect, 22 HTTP 4xx/5xx)
# so users get a hint instead of a stack trace.
#
# Usage: curl_with_retries [extra curl args] <url> [-o output_path]
#
# Honors $SPROUT_INSTALL_RETRIES (default 3) so flaky CI / corporate proxies
# can dial it up via env without editing the script.
curl_with_retries() {
    local retries="${SPROUT_INSTALL_RETRIES:-3}"
    local out
    local exit_code=0

    # --retry retries idempotent transient failures (5xx, connection drops).
    # --retry-delay 2 + --retry-max-time 60 caps the wait so a fully-down
    # endpoint doesn't hang for minutes. --connect-timeout 15 fails fast
    # on DNS / unreachable hosts.
    if out=$(curl --fail --show-error --silent --location \
        --retry "$retries" \
        --retry-delay 2 \
        --retry-max-time 60 \
        --connect-timeout 15 \
        "$@" 2>&1); then
        printf '%s' "$out"
        return 0
    fi

    exit_code=$?
    case "$exit_code" in
        6)
            log_error "DNS lookup failed. Are you offline or behind a captive portal?" ;;
        7)
            log_error "Could not connect to host. Check firewall / proxy settings." ;;
        22)
            log_error "HTTP error from server: $out" ;;
        28)
            log_error "Network timed out. Retry, or set SPROUT_INSTALL_RETRIES=5." ;;
        *)
            log_error "curl failed (exit $exit_code): $out" ;;
    esac
    return "$exit_code"
}

# Verify the downloaded archive against the release SHA256SUMS manifest.
#
# Set SPROUT_SKIP_CHECKSUM=1 to skip (escape hatch — e.g. if a release is
# missing the manifest because of a pipeline incident). We still log it as
# a warning so users know they're trusting unverified bytes.
verify_checksum() {
    local archive_path="$1"
    local archive_name="$2"
    local version="$3"

    if [ "${SPROUT_SKIP_CHECKSUM:-0}" = "1" ]; then
        log_warn "SPROUT_SKIP_CHECKSUM=1 — skipping checksum verification"
        return 0
    fi

    # Pick the available hashing tool. macOS ships shasum (Perl); most Linux
    # distros ship sha256sum (coreutils). Both emit the same `<hash>  <file>`
    # format so callers below can grep identically.
    local sha_cmd
    if command -v sha256sum >/dev/null 2>&1; then
        sha_cmd="sha256sum"
    elif command -v shasum >/dev/null 2>&1; then
        sha_cmd="shasum -a 256"
    else
        log_warn "Neither sha256sum nor shasum is available — cannot verify checksum."
        log_warn "Install coreutils, or re-run with SPROUT_SKIP_CHECKSUM=1 to bypass."
        return 1
    fi

    local sums_url="https://github.com/sprout-foundry/sprout/releases/download/${version}/SHA256SUMS"
    local sums_path="${TEMP_DIR}/SHA256SUMS"

    log_info "Verifying SHA256 checksum..."
    if ! curl_with_retries -o "$sums_path" "$sums_url" >/dev/null; then
        log_warn "Could not download SHA256SUMS for $version."
        log_warn "Older releases may not ship a manifest — re-run with"
        log_warn "SPROUT_SKIP_CHECKSUM=1 if you trust the source."
        return 1
    fi

    local expected
    expected=$(awk -v f="$archive_name" '$2 == f {print $1; exit}' "$sums_path")
    if [ -z "$expected" ]; then
        log_error "$archive_name not listed in SHA256SUMS for $version"
        return 1
    fi

    local actual
    # shellcheck disable=SC2086  # $sha_cmd is intentionally word-split
    actual=$($sha_cmd "$archive_path" | awk '{print $1}')
    if [ "$expected" != "$actual" ]; then
        log_error "Checksum mismatch for $archive_name"
        log_error "  expected: $expected"
        log_error "  actual:   $actual"
        log_error "Refusing to install. The download may be corrupted or tampered with."
        return 1
    fi

    log_success "Checksum verified ($expected)"
    return 0
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
# error rather than letting users guess. Uses curl_with_retries so a flaky
# network can self-heal across attempts.
get_version() {
    if [ -n "${SPROUT_VERSION:-}" ]; then
        echo "$SPROUT_VERSION"
    else
        local api_url="https://api.github.com/repos/sprout-foundry/sprout/releases/latest"
        local response
        if ! response=$(curl_with_retries "$api_url" 2>&1); then
            log_error "Failed to get version from GitHub API."
            log_error "If you're behind a proxy or hitting the 60 req/hr unauthenticated"
            log_error "rate limit, pin a version explicitly:"
            log_error "  SPROUT_VERSION=v0.14.0 curl -fsSL .../install.sh | sh"
            exit 1
        fi
        local version
        version=$(echo "$response" | awk -F'"' '/"tag_name":/ {print $4; exit}')
        if [ -z "$version" ]; then
            log_error "GitHub API returned an unexpected payload (no tag_name)."
            log_error "Pin a version with SPROUT_VERSION=vX.Y.Z and re-run."
            exit 1
        fi
        echo "$version"
    fi
}

# Download the release tarball with retries (via curl_with_retries).
# Uses --progress-bar via a separate curl invocation rather than the wrapper
# because the wrapper is for short headless fetches (manifest, version JSON);
# the tarball download benefits from a visible progress indicator.
download_release() {
    local version="$1"
    local os="$2"
    local arch="$3"

    local filename="sprout-${os}-${arch}.tar.gz"
    local download_url="https://github.com/sprout-foundry/sprout/releases/download/${version}/${filename}"
    local retries="${SPROUT_INSTALL_RETRIES:-3}"

    log_info "Downloading $filename" >&2

    if ! curl --fail --show-error --location --progress-bar \
        --retry "$retries" \
        --retry-delay 2 \
        --retry-max-time 120 \
        --connect-timeout 15 \
        -o "$TEMP_DIR/$filename" \
        "$download_url"; then
        local code=$?
        log_error "Failed to download $download_url (curl exit $code)" >&2
        log_error "If GitHub Releases is unreachable from your network, you can" >&2
        log_error "download the tarball manually and run:" >&2
        log_error "  SPROUT_VERSION=$version sh install.sh" >&2
        log_error "after placing it in your current directory." >&2
        return "$code"
    fi

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
    # commonly: Termux pulling a glibc-linked binary, an older glibc host
    # running a binary built against newer glibc, or someone uname spoofing.
    # The release pipeline cross-compiles linux-arm64 with CGO disabled so
    # this should normally pass on Termux too.
    local run_output run_status
    run_output=$("$binary_path" version 2>&1)
    run_status=$?
    if [ "$run_status" -ne 0 ]; then
        log_error "sprout was installed to $binary_path but failed to run."

        # Detect the most common Linux failure: host glibc is too old for
        # the binary the release pipeline built. The runtime loader prints
        # a very specific message we can pattern-match:
        #   /lib/x86_64-linux-gnu/libc.so.6: version `GLIBC_2.34' not found
        if printf '%s' "$run_output" | grep -qE "GLIBC_[0-9]+\.[0-9]+.+not found"; then
            local needed
            needed=$(printf '%s' "$run_output" | grep -oE "GLIBC_[0-9]+\.[0-9]+" | sort -u | tail -1)
            log_error "Your host glibc is too old. The binary needs $needed but"
            log_error "your system provides an older version. Run 'ldd --version'"
            log_error "to see what you have."
            log_error ""
            log_error "Workarounds:"
            log_error "  1. Upgrade your distro (RHEL 7 / Ubuntu 18.04 era are the"
            log_error "     common culprits)."
            log_error "  2. Pin to an older sprout release built against older glibc:"
            log_error "     SPROUT_VERSION=v0.13.0 curl -fsSL .../install.sh | sh"
            log_error "  3. Build from source (recent Go and Node required)."
        elif is_termux; then
            log_error "On Termux this usually means the binary is dynamically linked"
            log_error "against glibc and won't load on Bionic libc."
            log_error "Workaround: build from source."
            log_error "  pkg install golang nodejs make git"
            log_error "  git clone https://github.com/sprout-foundry/sprout.git"
            log_error "  cd sprout && make deploy-ui && go install ."
        else
            log_error "Output from '$binary_path version':"
            printf '%s\n' "$run_output" | sed 's/^/  /' >&2
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

# Print --help text. Mirrors what install.ps1 -? would show on Windows so
# users have the same surface area on both installers.
print_help() {
    cat <<'EOF'
sprout one-line install script

USAGE:
  curl -fsSL .../install.sh | sh                  install latest release
  sh install.sh [FLAGS]                           run a downloaded copy
  curl -fsSL .../install.sh | sh -s -- [FLAGS]    pass flags through curl

FLAGS:
  -h, --help          show this help and exit
  -v, --version       show the version that would be installed and exit
  -u, --uninstall     remove sprout + service files
      --keep-config   used with --uninstall: keep config + session state.
                      Default is to remove ~/.config/sprout/ and
                      ~/.sprout/ alongside the binary.
      --dry-run       print what would happen (URL, install dir, sudo
                      prompts, PATH changes, paths removed) without
                      touching the filesystem. Useful for reviewing the
                      script before piping curl | sh, and for CI.

ENV VARS:
  SPROUT_VERSION         pin a specific release tag (e.g. v0.14.0).
                         Skips the GitHub API call.
  SPROUT_INSTALL_DIR     install destination. Defaults to /usr/local/bin,
                         existing sprout dir on PATH, or $PREFIX/bin on Termux.
  SPROUT_INSTALL_RETRIES network retry count for curl (default 3).
  SPROUT_SKIP_CHECKSUM   if "1", skip SHA256 verification of the download.
                         Use only when a release is missing the manifest.
  SPROUT_CONFIG          config dir override (see also LEDIT_CONFIG for
                         pre-rebrand back-compat). Honored on uninstall.
  XDG_CONFIG_HOME        XDG override; uninstall reads it before falling
                         back to ~/.config/sprout/.

EXAMPLES:
  SPROUT_VERSION=v0.14.0 curl -fsSL .../install.sh | sh
  SPROUT_INSTALL_DIR=~/.local/bin curl -fsSL .../install.sh | sh
  curl -fsSL .../install.sh | sh -s -- --uninstall
  curl -fsSL .../install.sh | sh -s -- --uninstall --keep-config
EOF
}

# Resolve the active sprout config directory using the same rules the
# binary itself does (see pkg/envutil/env.go:GetConfigDir):
#   1. $SPROUT_CONFIG / $LEDIT_CONFIG
#   2. $XDG_CONFIG_HOME/sprout
#   3. $HOME/.config/sprout
# Prints the path; never creates it.
resolve_config_dir() {
    if [ -n "${SPROUT_CONFIG:-}" ]; then
        echo "$SPROUT_CONFIG"
    elif [ -n "${LEDIT_CONFIG:-}" ]; then
        echo "$LEDIT_CONFIG"
    elif [ -n "${XDG_CONFIG_HOME:-}" ]; then
        echo "${XDG_CONFIG_HOME}/sprout"
    elif [ -n "${HOME:-}" ]; then
        echo "${HOME}/.config/sprout"
    fi
}

# Resolve the conversation state dir (~/.sprout/, holds sessions/).
# Not configurable in the binary today — pkg/agent/persistence.go hardcodes
# $HOME/.sprout/sessions. Keep this in sync if that ever changes.
resolve_state_dir() {
    if [ -n "${HOME:-}" ]; then
        echo "${HOME}/.sprout"
    fi
}

# Remove sprout's config + state dirs unless --keep-config was passed.
# Refuses to delete anything that doesn't look like a sprout-owned dir
# (e.g. if SPROUT_CONFIG points at /) — sanity-checks the path before rm.
remove_config_dirs() {
    local keep_config="$1"

    if [ "$keep_config" = "true" ]; then
        log_info "Keeping config and session state per --keep-config"
        return 0
    fi

    local config_dir
    config_dir=$(resolve_config_dir)
    if [ -n "$config_dir" ] && [ -d "$config_dir" ]; then
        # Refuse to nuke obviously-wrong targets. The check covers $HOME
        # itself, /, and bare $HOME/.config (which holds OTHER apps' data).
        case "$config_dir" in
            "/"|"$HOME"|"${HOME}/"|"${HOME}/.config"|"${HOME}/.config/")
                log_warn "Refusing to remove suspicious config path: $config_dir"
                ;;
            *)
                log_info "Removing config dir: $config_dir"
                rm -rf "$config_dir"
                ;;
        esac
    fi

    local state_dir
    state_dir=$(resolve_state_dir)
    if [ -n "$state_dir" ] && [ -d "$state_dir" ]; then
        case "$state_dir" in
            "/"|"$HOME"|"${HOME}/")
                log_warn "Refusing to remove suspicious state path: $state_dir"
                ;;
            *)
                log_info "Removing session state: $state_dir"
                rm -rf "$state_dir"
                ;;
        esac
    fi
}

# Dry-run preview helpers. Print what *would* happen without touching
# the filesystem, the network, or the system PATH. Used by both the
# install and uninstall flows so users can review before piping curl to
# sh (and so CI smoke tests can validate the script's choices end to end
# without actually installing anything).

preview_install() {
    local version="$1"
    local os="$2"
    local arch="$3"
    local install_dir="$4"

    local archive="sprout-${os}-${arch}.tar.gz"
    local archive_url="https://github.com/sprout-foundry/sprout/releases/download/${version}/${archive}"
    local sums_url="https://github.com/sprout-foundry/sprout/releases/download/${version}/SHA256SUMS"

    echo
    log_info "DRY RUN — install preview (no files will be touched)"
    echo
    printf '  %-22s %s\n' "Version:" "$version"
    printf '  %-22s %s\n' "Platform:" "${os}-${arch}"
    printf '  %-22s %s\n' "Archive URL:" "$archive_url"
    printf '  %-22s %s\n' "Checksum URL:" "$sums_url"
    printf '  %-22s %s\n' "Install dir:" "$install_dir"

    if [ ! -w "$install_dir" ] && [ -d "$install_dir" ]; then
        printf '  %-22s %s\n' "Privilege:" "sudo required (install dir is not writable)"
    elif [ ! -d "$install_dir" ]; then
        local parent
        parent=$(dirname "$install_dir")
        if [ ! -w "$parent" ]; then
            printf '  %-22s %s\n' "Privilege:" "sudo required (would create $install_dir)"
        else
            printf '  %-22s %s\n' "Privilege:" "none (parent dir is writable)"
        fi
    else
        printf '  %-22s %s\n' "Privilege:" "none (install dir is writable)"
    fi

    if is_termux; then
        printf '  %-22s %s\n' "Termux mode:" "yes (service step skipped)"
    fi

    if [ "${SPROUT_SKIP_CHECKSUM:-0}" = "1" ]; then
        printf '  %-22s %s\n' "Checksum:" "SKIPPED (SPROUT_SKIP_CHECKSUM=1)"
    else
        printf '  %-22s %s\n' "Checksum:" "verified against SHA256SUMS"
    fi
    echo
    log_info "Re-run without --dry-run to install."
}

preview_uninstall() {
    local binary_path="$1"
    local keep_config="$2"

    echo
    printf '  %-22s %s\n' "Binary:" "$binary_path"
    if [ -f "$binary_path" ]; then
        printf '  %-22s %s\n' "Binary status:" "present (would remove)"
        if [ ! -w "$binary_path" ]; then
            printf '  %-22s %s\n' "Privilege:" "sudo required (binary not writable)"
        fi
    else
        printf '  %-22s %s\n' "Binary status:" "not found (nothing to remove)"
    fi

    if [ "$keep_config" = "true" ]; then
        printf '  %-22s %s\n' "Config + state:" "KEPT (--keep-config)"
    else
        local config_dir state_dir
        config_dir=$(resolve_config_dir)
        state_dir=$(resolve_state_dir)
        if [ -n "$config_dir" ] && [ -d "$config_dir" ]; then
            printf '  %-22s %s\n' "Config dir:" "$config_dir (would remove)"
        elif [ -n "$config_dir" ]; then
            printf '  %-22s %s\n' "Config dir:" "$config_dir (not present)"
        fi
        if [ -n "$state_dir" ] && [ -d "$state_dir" ]; then
            printf '  %-22s %s\n' "State dir:" "$state_dir (would remove)"
        elif [ -n "$state_dir" ]; then
            printf '  %-22s %s\n' "State dir:" "$state_dir (not present)"
        fi
    fi
    echo
    log_info "Re-run without --dry-run to uninstall."
}

# Main function
main() {
    case "${1:-}" in
        -h|--help)
            print_help
            exit 0
            ;;
        -v|--version)
            local version
            version=$(get_version)
            echo "sprout $version"
            exit 0
            ;;
    esac

    # Check for flags. Accept any combination/order — `sh -s -- --uninstall
    # --keep-config --dry-run` should preview a full cleanup.
    local uninstall=false
    local keep_config=false
    local dry_run=false
    for arg in "$@"; do
        case "$arg" in
            --uninstall|-u) uninstall=true ;;
            --keep-config)  keep_config=true ;;
            --dry-run)      dry_run=true ;;
            -h|--help|-v|--version) ;; # already handled above
            *)
                log_warn "Unknown argument: $arg"
                ;;
        esac
    done

    if [ "$uninstall" = "true" ]; then
        if [ "$dry_run" = "true" ]; then
            log_info "DRY RUN — uninstall preview (no files will be touched)"
        else
            log_info "Uninstalling sprout..."
        fi

        local install_dir
        if [ -n "${SPROUT_INSTALL_DIR:-}" ]; then
            install_dir="$SPROUT_INSTALL_DIR"
        else
            install_dir=$(get_install_dir)
        fi

        local binary_path="${install_dir}/sprout"

        if [ "$dry_run" = "true" ]; then
            preview_uninstall "$binary_path" "$keep_config"
            exit 0
        fi

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

        # Config + state cleanup (skip with --keep-config).
        remove_config_dirs "$keep_config"

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

    # Dry-run short-circuits before any network or filesystem ops. We've
    # done enough to give a useful preview (version resolved, OS/arch
    # detected, install dir picked, Termux/sudo status known) — printing
    # the plan + exiting is more informative than continuing in some
    # "fake" mode would be.
    if [ "$dry_run" = "true" ]; then
        preview_install "$version" "$os" "$arch" "$install_dir"
        exit 0
    fi

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

    # Verify the downloaded archive against the release's SHA256SUMS manifest.
    # Failure here aborts before we chmod+x — that's the whole point.
    local archive_name="sprout-${os}-${arch}.tar.gz"
    if ! verify_checksum "$TEMP_DIR/$archive_name" "$archive_name" "$version"; then
        log_error "Refusing to install an unverified binary."
        exit 1
    fi

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
