# sprout one-line install script for Windows
# Requires PowerShell 5.1 (default on Windows 10/11) or PowerShell 7+

param(
    [switch]$Uninstall,
    [switch]$Service,
    [switch]$NoService,
    [switch]$Version,
    [switch]$KeepConfig
)

# Color output via Write-Host -ForegroundColor — scoped to a single line so
# we don't mutate [Console]::ForegroundColor and bleed colors into the host
# session for the rest of the user's terminal lifetime.

function Write-LogInfo {
    param([string]$Message)
    Write-Host "[INFO] " -ForegroundColor Blue -NoNewline
    Write-Host $Message
}

function Write-LogSuccess {
    param([string]$Message)
    Write-Host "[SUCCESS] " -ForegroundColor Green -NoNewline
    Write-Host $Message
}

function Write-LogWarn {
    param([string]$Message)
    Write-Host "[WARN] " -ForegroundColor Yellow -NoNewline
    Write-Host $Message
}

function Write-LogError {
    param([string]$Message)
    Write-Host "[ERROR] " -ForegroundColor Red -NoNewline
    Write-Host $Message -ForegroundColor Red
}

# Cleanup function
function Cleanup {
    if ($script:TEMP_DIR -and (Test-Path $script:TEMP_DIR)) {
        Remove-Item -Path $script:TEMP_DIR -Recurse -Force -ErrorAction SilentlyContinue
    }
}

# Verify PowerShell version. The script itself uses Invoke-RestMethod /
# Invoke-WebRequest (both >= PS 3.0) and System.IO.Compression, so we don't
# need any external binaries — checking the host's PS version is enough.
function Check-Dependencies {
    if ($PSVersionTable.PSVersion.Major -lt 5) {
        Write-LogError "PowerShell 5.0+ is required (found $($PSVersionTable.PSVersion))"
        Write-LogError "Install PowerShell 7: https://aka.ms/powershell"
        exit 1
    }
}

# Detect operating system.
#
# Note: $PSVersionTable.OS only exists on PowerShell 6+ (Core). The default
# Windows shell is Windows PowerShell 5.1, where $PSVersionTable.OS is null
# — the previous check ('$PSVersionTable.OS -match "Windows"') therefore
# made the script unrunnable on the stock shell. $env:OS is set to
# "Windows_NT" by the OS itself and works identically on 5.1 and 7+.
function Detect-OS {
    if ($env:OS -eq 'Windows_NT') {
        return "windows"
    }
    # PS 7 on Linux/macOS — not supported by this script.
    Write-LogError "Unsupported operating system (this script is Windows-only)"
    Write-LogError "On Linux/macOS use install.sh instead:"
    Write-LogError "  curl -fsSL https://raw.githubusercontent.com/sprout-foundry/sprout/main/scripts/install.sh | sh"
    exit 1
}

# Detect architecture
function Detect-Arch {
    if ($env:PROCESSOR_ARCHITECTURE -eq "AMD64" -or $env:PROCESSOR_ARCHITEW6432 -eq "AMD64") {
        return "amd64"
    } elseif ($env:PROCESSOR_ARCHITECTURE -eq "ARM64") {
        return "arm64"
    }
    
    Write-LogWarn "Unsupported architecture: $env:PROCESSOR_ARCHITECTURE (only amd64 and arm64 are available)"
    return "amd64"
}

# Determine install directory.
#
# Priority:
#   1. $env:SPROUT_INSTALL_DIR if set (explicit user choice).
#   2. Directory of an existing sprout / ledit binary on PATH (upgrade in place).
#   3. %LOCALAPPDATA%\Programs\sprout (standard per-user location).
#
# The previous implementation probe-wrote a test file into every directory on
# PATH and picked the first writable one — that frequently picked unexpected
# locations (random tool bin folders, dev SDK paths) and made upgrades hard
# to find. LOCALAPPDATA is the documented Windows per-user app prefix and
# the installer adds it to PATH itself, so we don't need to scan.
function Get-InstallDir {
    if ($env:SPROUT_INSTALL_DIR) {
        return $env:SPROUT_INSTALL_DIR
    }

    # Upgrade in place if sprout (or the legacy ledit binary) is on PATH.
    $existingSprout = Get-Command sprout -ErrorAction SilentlyContinue
    if (-not $existingSprout) {
        $existingSprout = Get-Command ledit -ErrorAction SilentlyContinue
    }
    if ($existingSprout) {
        return Split-Path $existingSprout.Source
    }

    return (Join-Path $env:LOCALAPPDATA "Programs\sprout")
}

# Retry wrapper for Invoke-WebRequest / Invoke-RestMethod with exponential
# backoff. PowerShell 6+ exposes -MaximumRetryCount on these cmdlets, but
# 5.1 (the default Windows shell) doesn't, so we roll our own to keep the
# script single-source. Honors $env:SPROUT_INSTALL_RETRIES (default 3).
function Invoke-WithRetries {
    param(
        [ScriptBlock]$Action,
        [string]$Description = "request"
    )

    $maxRetries = if ($env:SPROUT_INSTALL_RETRIES) {
        [int]$env:SPROUT_INSTALL_RETRIES
    } else {
        3
    }

    for ($attempt = 1; $attempt -le $maxRetries; $attempt++) {
        try {
            return & $Action
        } catch {
            $lastError = $_
            if ($attempt -lt $maxRetries) {
                $delay = [math]::Pow(2, $attempt - 1)
                Write-LogWarn "$Description failed (attempt $attempt/$maxRetries): $($_.Exception.Message). Retrying in ${delay}s..."
                Start-Sleep -Seconds $delay
            }
        }
    }

    # All retries exhausted — surface targeted advice for common failure
    # modes before re-throwing.
    $msg = $lastError.Exception.Message
    if ($msg -match "rate limit|403") {
        Write-LogError "GitHub API rate limit hit (60 req/hr per IP)."
        Write-LogError "Pin a version with `$env:SPROUT_VERSION='v0.14.0'` and re-run."
    } elseif ($msg -match "404|Not Found") {
        Write-LogError "Resource not found. Check the version tag exists."
    } elseif ($msg -match "name or service not known|DNS|could not be resolved|No such host") {
        Write-LogError "DNS lookup failed. Are you offline or behind a captive portal?"
    } elseif ($msg -match "proxy|407") {
        Write-LogError "Proxy required. Set `$env:HTTPS_PROXY` and re-run."
    } else {
        Write-LogError "$Description failed: $msg"
    }
    throw $lastError
}

# Get version from environment or fetch latest from GitHub
function Get-Version {
    if ($env:SPROUT_VERSION) {
        return $env:SPROUT_VERSION
    }

    try {
        $response = Invoke-WithRetries -Description "GitHub API version lookup" -Action {
            Invoke-RestMethod -Uri "https://api.github.com/repos/sprout-foundry/sprout/releases/latest" `
                -UseBasicParsing -ErrorAction Stop
        }
        return $response.tag_name
    } catch {
        exit 1
    }
}

# Download the release zip
function Download-Release {
    param(
        [string]$Version,
        [string]$OS,
        [string]$Arch
    )

    $filename = "sprout-${OS}-${Arch}.zip"
    $downloadUrl = "https://github.com/sprout-foundry/sprout/releases/download/${Version}/${filename}"

    Write-LogInfo "Downloading $filename"

    try {
        Invoke-WithRetries -Description "release download" -Action {
            Invoke-WebRequest -Uri $downloadUrl `
                -OutFile "$script:TEMP_DIR\$filename" `
                -UseBasicParsing -ErrorAction Stop
        } | Out-Null
    } catch {
        exit 1
    }

    return $downloadUrl
}

# Verify the downloaded zip against the release's SHA256SUMS manifest.
# Set $env:SPROUT_SKIP_CHECKSUM='1' to bypass (mirrors install.sh).
function Test-Checksum {
    param(
        [string]$ArchivePath,
        [string]$ArchiveName,
        [string]$Version
    )

    if ($env:SPROUT_SKIP_CHECKSUM -eq '1') {
        Write-LogWarn "SPROUT_SKIP_CHECKSUM=1 — skipping checksum verification"
        return
    }

    $sumsUrl = "https://github.com/sprout-foundry/sprout/releases/download/${Version}/SHA256SUMS"
    $sumsPath = Join-Path $script:TEMP_DIR "SHA256SUMS"

    Write-LogInfo "Verifying SHA256 checksum..."

    try {
        Invoke-WithRetries -Description "SHA256SUMS download" -Action {
            Invoke-WebRequest -Uri $sumsUrl -OutFile $sumsPath `
                -UseBasicParsing -ErrorAction Stop
        } | Out-Null
    } catch {
        Write-LogWarn "Could not download SHA256SUMS for $Version."
        Write-LogWarn "Older releases may not ship a manifest. Re-run with"
        Write-LogWarn "  `$env:SPROUT_SKIP_CHECKSUM='1'"
        Write-LogWarn "if you trust the source."
        throw "checksum manifest unavailable"
    }

    # SHA256SUMS uses `<hex>  <filename>` lines (two spaces). Match the
    # one for our archive specifically.
    $expected = $null
    foreach ($line in Get-Content -Path $sumsPath) {
        $fields = $line -split '\s+', 2
        if ($fields.Length -eq 2 -and $fields[1].Trim() -eq $ArchiveName) {
            $expected = $fields[0].Trim().ToLowerInvariant()
            break
        }
    }
    if (-not $expected) {
        Write-LogError "$ArchiveName not listed in SHA256SUMS for $Version"
        throw "checksum entry missing"
    }

    $actual = (Get-FileHash -Path $ArchivePath -Algorithm SHA256).Hash.ToLowerInvariant()
    if ($expected -ne $actual) {
        Write-LogError "Checksum mismatch for $ArchiveName"
        Write-LogError "  expected: $expected"
        Write-LogError "  actual:   $actual"
        Write-LogError "Refusing to install. The download may be corrupted or tampered with."
        throw "checksum mismatch"
    }

    Write-LogSuccess "Checksum verified ($expected)"
}

# Install the binary
function Install-Binary {
    param(
        [string]$ZipPath,
        [string]$InstallDir
    )
    
    Write-LogInfo "Extracting binary from $ZipPath"
    
    # Extract the zip file
    Add-Type -AssemblyName System.IO.Compression.FileSystem
    $zip = [System.IO.Compression.ZipFile]::OpenRead($ZipPath)
    
    # Find the exe file in the archive
    $exeEntry = $zip.Entries | Where-Object { $_.Name -match '\.exe$' } | Select-Object -First 1
    
    if (-not $exeEntry) {
        Write-LogError "No .exe file found in the archive"
        $zip.Dispose()
        exit 1
    }
    
    # Extract to temp directory
    $extractedPath = Join-Path $script:TEMP_DIR "sprout.exe"
    $exeEntry.ExtractToFile($extractedPath, $true)
    $zip.Dispose()
    
    # Create install directory if it doesn't exist
    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }
    
    # Copy the binary to install directory
    $installPath = Join-Path $InstallDir "sprout.exe"
    Copy-Item -Path $extractedPath -Destination $installPath -Force
    
    Write-LogSuccess "sprout installed to $installPath"
    
    return $installPath
}

# Add directory to PATH if not already present
function Add-To-Path {
    param([string]$InstallDir)

    $userPath = [Environment]::GetEnvironmentVariable("PATH", "User")
    $currentPath = $env:PATH

    # Check if already in PATH
    if ($currentPath -split ";" | Where-Object { $_ -eq $InstallDir }) {
        Write-LogInfo "Install directory already in PATH"
        return
    }

    # Try to add to user PATH first (no admin required). Setting via
    # [Environment]::SetEnvironmentVariable broadcasts WM_SETTINGCHANGE
    # automatically since .NET 4.5, so new terminals pick it up.
    try {
        $newPath = "$InstallDir;$userPath"
        [Environment]::SetEnvironmentVariable("PATH", $newPath, "User")
        Write-LogInfo "Added $InstallDir to user PATH"

        # Update current session PATH so the user can run `sprout` without
        # opening a new terminal.
        $env:PATH = [Environment]::GetEnvironmentVariable("PATH", "User") + ";" + [Environment]::GetEnvironmentVariable("PATH", "Machine")
    } catch {
        Write-LogWarn "Could not add to PATH: $_"
        Write-LogInfo "You can manually add $InstallDir to your PATH"
    }
}

# Symmetric counterpart to Add-To-Path: remove $InstallDir from the user
# PATH if present. Only touches the User scope — never Machine — because
# the installer never writes to Machine PATH.
function Remove-FromPath {
    param([string]$InstallDir)

    $userPath = [Environment]::GetEnvironmentVariable("PATH", "User")
    if (-not $userPath) {
        return
    }

    $segments = $userPath -split ";" | Where-Object {
        $_ -and ($_ -ne $InstallDir)
    }
    $newPath = $segments -join ";"

    if ($newPath -eq $userPath) {
        return
    }

    try {
        [Environment]::SetEnvironmentVariable("PATH", $newPath, "User")
        Write-LogInfo "Removed $InstallDir from user PATH"
    } catch {
        Write-LogWarn "Could not update PATH: $_"
        Write-LogInfo "You can manually remove $InstallDir from your PATH"
    }
}

# Verify installation
function Verify-Installation {
    param([string]$InstallDir)
    
    $binaryPath = Join-Path $InstallDir "sprout.exe"
    
    if (-not (Test-Path $binaryPath)) {
        Write-LogError "sprout binary not found at $binaryPath"
        exit 1
    }
    
    # Try to run the binary to verify it works
    try {
        $versionOutput = & $binaryPath version 2>&1
        if ($LASTEXITCODE -ne 0 -and $versionOutput -notmatch "sprout") {
            Write-LogError "sprout binary verification failed"
            exit 1
        }
    } catch {
        Write-LogError "Failed to verify sprout binary: $_"
        exit 1
    }
    
    Write-LogSuccess "sprout binary verified"
}

# Remove old versions
function Remove-Old-Versions {
    param([string]$InstallDir)
    
    $binaryPath = Join-Path $InstallDir "sprout.exe"
    
    if (Test-Path $binaryPath) {
        try {
            $oldVersion = & $binaryPath version 2>&1 | Select-Object -First 1
            Write-LogInfo "Removing old version: $oldVersion"
            Remove-Item -Path $binaryPath -Force
        } catch {
            Write-LogWarn "Could not remove old version: $_"
        }
    }
}

# Print uninstall instructions
function Print-UninstallInstructions {
    param([string]$InstallDir)

    Write-Host ""
    Write-LogInfo "To uninstall sprout:"
    Write-Host ""
    Write-Host "  # Remove the binary"
    Write-Host "  Remove-Item -Path '$InstallDir\sprout.exe'"
    Write-Host ""
}

# Resolve the active sprout config directory using the same rules the
# binary itself does. On Windows os.UserHomeDir() returns $env:USERPROFILE,
# so the default config dir is $env:USERPROFILE\.config\sprout (mirrors
# the Linux/macOS layout — sprout is XDG-style on every platform).
function Resolve-ConfigDir {
    if ($env:SPROUT_CONFIG)     { return $env:SPROUT_CONFIG }
    if ($env:LEDIT_CONFIG)      { return $env:LEDIT_CONFIG }
    if ($env:XDG_CONFIG_HOME)   { return (Join-Path $env:XDG_CONFIG_HOME "sprout") }
    if ($env:USERPROFILE)       { return (Join-Path $env:USERPROFILE ".config\sprout") }
    return $null
}

# Conversation state dir — $USERPROFILE\.sprout, mirrors the unix layout
# in pkg/agent/persistence.go.
function Resolve-StateDir {
    if ($env:USERPROFILE)       { return (Join-Path $env:USERPROFILE ".sprout") }
    return $null
}

# Remove sprout's config + state dirs unless -KeepConfig was passed.
# Refuses to delete anything that doesn't look like a sprout-owned dir.
function Remove-ConfigDirs {
    param([bool]$KeepConfig)

    if ($KeepConfig) {
        Write-LogInfo "Keeping config and session state per -KeepConfig"
        return
    }

    $suspicious = @(
        '/', '\',
        $env:USERPROFILE,
        (Join-Path $env:USERPROFILE '.config')
    ) | Where-Object { $_ }

    $configDir = Resolve-ConfigDir
    if ($configDir -and (Test-Path $configDir)) {
        $normalized = ($configDir.TrimEnd('\','/'))
        if ($suspicious -contains $normalized) {
            Write-LogWarn "Refusing to remove suspicious config path: $configDir"
        } else {
            Write-LogInfo "Removing config dir: $configDir"
            Remove-Item -Path $configDir -Recurse -Force -ErrorAction SilentlyContinue
        }
    }

    $stateDir = Resolve-StateDir
    if ($stateDir -and (Test-Path $stateDir)) {
        $normalized = ($stateDir.TrimEnd('\','/'))
        if ($suspicious -contains $normalized) {
            Write-LogWarn "Refusing to remove suspicious state path: $stateDir"
        } else {
            Write-LogInfo "Removing session state: $stateDir"
            Remove-Item -Path $stateDir -Recurse -Force -ErrorAction SilentlyContinue
        }
    }
}

# Print success message
function Print-Success {
    param([string]$InstallDir, [string]$Version)
    
    Write-Host ""
    Write-LogSuccess "sprout $Version installed successfully!"
    Write-Host ""
    Write-Host "  Binary location: $InstallDir\sprout.exe"
    Write-Host ""
    Write-Host "  Run 'sprout version' to verify the installation"
    Write-Host ""

    if (-not $NoService.IsPresent) {
        Write-Host "  Run 'sprout service install' to set up auto-start"
        Write-Host ""
    }
}

# Show version info
function Show-Version {
    if ($env:SPROUT_VERSION) {
        Write-Host "sprout version $env:SPROUT_VERSION (requested)"
    } else {
        try {
            $apiUrl = "https://api.github.com/repos/sprout-foundry/sprout/releases/latest"
            $response = Invoke-RestMethod -Uri $apiUrl -UseBasicParsing -ErrorAction Stop
            $version = $response.tag_name -replace '^v', ''
            Write-Host "sprout version $version (latest)"
        } catch {
            Write-LogError "Failed to get version: $_"
            exit 1
        }
    }
}

# Main function
function Main {
    # Show version if requested
    if ($Version.IsPresent) {
        Show-Version
        exit 0
    }

    # Validate mutual exclusion of service flags
    if ($Service.IsPresent -and $NoService.IsPresent) {
        Write-LogError "--Service and --NoService are mutually exclusive"
        exit 1
    }

    # Handle uninstall
    if ($Uninstall.IsPresent) {
        Write-LogInfo "Uninstalling sprout..."

        $installDir = Get-InstallDir
        $binaryPath = Join-Path $installDir "sprout.exe"

        if (Test-Path $binaryPath) {
            try {
                $version = & $binaryPath version 2>&1 | Select-Object -First 1
                Write-LogInfo "Removing: $version"
            } catch {
                $version = "unknown"
            }

            # Try to uninstall service first
            try {
                & $binaryPath service uninstall 2>$null
            } catch {
                # Service uninstall not available, ignore
            }
        }
        
        if (Test-Path $binaryPath) {
            try {
                Remove-Item -Path $binaryPath -Force
                Write-LogSuccess "sprout uninstalled successfully"
            } catch {
                Write-LogError "Cannot remove $binaryPath: $_"
                exit 1
            }
        } else {
            Write-LogWarn "sprout not found at $binaryPath"
        }

        # Strip the install dir from the user PATH if we own it (i.e. we
        # only added a per-user entry — never touch Machine PATH). The
        # installer adds it via Add-To-Path on install, so the symmetric
        # cleanup belongs here. If the user added the dir to PATH some
        # other way, this still cleans it because we match exactly.
        Remove-FromPath -InstallDir $installDir

        # Config + state cleanup (skip with -KeepConfig).
        Remove-ConfigDirs -KeepConfig $KeepConfig.IsPresent

        Print-UninstallInstructions $installDir
        exit 0
    }
    
    # Check dependencies
    Write-LogInfo "Checking dependencies..."
    Check-Dependencies
    
    # Create temporary directory
    $script:TEMP_DIR = (New-Item -ItemType Directory -Path (Join-Path $env:TEMP "sprout-$PID") -Force).FullName
    
    # Detect OS and architecture
    Write-LogInfo "Detecting operating system and architecture..."
    $os = Detect-OS
    $arch = Detect-Arch
    Write-LogInfo "Detected: $os-$arch"
    
    # Get version
    $version = Get-Version
    Write-LogInfo "Installing sprout version: $version"
    
    # Determine install directory
    $installDir = Get-InstallDir
    Write-LogInfo "Installing to: $installDir"
    
    # Remove old versions if they exist
    Remove-Old-Versions $installDir
    
    # Download the release
    $downloadUrl = Download-Release -Version $version -OS $os -Arch $arch
    Write-LogInfo "Downloaded from: $downloadUrl"

    # Verify the downloaded archive against the release's SHA256SUMS manifest
    # BEFORE we extract / copy it. Failure aborts.
    $archiveName = "sprout-${os}-${arch}.zip"
    $zipPath = Join-Path $script:TEMP_DIR $archiveName
    try {
        Test-Checksum -ArchivePath $zipPath -ArchiveName $archiveName -Version $version
    } catch {
        Write-LogError "Refusing to install an unverified binary."
        exit 1
    }

    # Install the binary
    $binaryPath = Install-Binary -ZipPath $zipPath -InstallDir $installDir

    # Verify installation
    Verify-Installation -InstallDir $installDir

    # Clean up legacy ledit binary if present on PATH
    $legacyBinary = Get-Command ledit -ErrorAction SilentlyContinue
    if ($legacyBinary) {
        Write-LogInfo "Removing legacy 'ledit' binary..."
        Remove-Item $legacyBinary.Source -Force -ErrorAction SilentlyContinue
    }

    # Note about service management. The previous build had an interactive
    # prompt that always replied "not yet available" regardless of the
    # answer — removed as user-hostile noise. When real service management
    # ships on Windows, restore it here gated by $Service / $NoService.
    if ($Service.IsPresent) {
        Write-LogInfo "Automatic service management is not yet available on Windows."
        Write-LogInfo "Run 'sprout agent -d' to start the daemon manually."
    }

    # Add to PATH if needed
    Add-To-Path -InstallDir $installDir
    
    # Print success message
    Print-Success -InstallDir $installDir -Version $version
    
    # Print uninstall instructions
    Print-UninstallInstructions -InstallDir $installDir
    
    # Cleanup is handled by trap
}

# Run main function
try {
    Main
} catch {
    Write-LogError "Unexpected error: $_"
    exit 1
} finally {
    Cleanup
}
