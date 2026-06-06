# sprout one-line install script for Windows
# Requires PowerShell 5.1 (default on Windows 10/11) or PowerShell 7+

param(
    [switch]$Uninstall,
    [switch]$Service,
    [switch]$NoService,
    [switch]$Version
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

# Determine install directory
function Get-InstallDir {
    if ($env:SPROUT_INSTALL_DIR) {
        return $env:SPROUT_INSTALL_DIR
    }

    # Check for existing sprout or legacy ledit binary on PATH to upgrade in place
    $existingSprout = Get-Command sprout -ErrorAction SilentlyContinue
    if (-not $existingSprout) {
        $existingSprout = Get-Command ledit -ErrorAction SilentlyContinue
    }
    if ($existingSprout) {
        return Split-Path $existingSprout.Source
    }

    # Default to LOCALAPPDATA\Programs\sprout
    $defaultDir = Join-Path $env:LOCALAPPDATA "Programs\sprout"
    
    # Check if we should use a directory on PATH
    $pathDirs = $env:PATH -split ";" | Where-Object { $_ -and (Test-Path $_) }
    
    # Prefer a writable directory on PATH
    foreach ($dir in $pathDirs) {
        try {
            $testFile = Join-Path $dir "test_write_$PID.$(Get-Random)"
            $null = [System.IO.File]::WriteAllText($testFile, "test")
            [System.IO.File]::Delete($testFile)
            return $dir
        } catch {
            continue
        }
    }
    
    # Fall back to LOCALAPPDATA
    return $defaultDir
}

# Get version from environment or fetch latest from GitHub
function Get-Version {
    if ($env:SPROUT_VERSION) {
        return $env:SPROUT_VERSION
    }
    
    try {
        $apiUrl = "https://api.github.com/repos/sprout-foundry/sprout/releases/latest"
        $response = Invoke-RestMethod -Uri $apiUrl -UseBasicParsing -ErrorAction Stop
        $version = $response.tag_name
        return $version
    } catch {
        Write-LogError "Failed to get version from GitHub API"
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
        Invoke-WebRequest -Uri $downloadUrl -OutFile "$script:TEMP_DIR\$filename" -UseBasicParsing -ErrorAction Stop
    } catch {
        Write-LogError "Failed to download release: $_"
        exit 1
    }
    
    return $downloadUrl
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
    $machinePath = [Environment]::GetEnvironmentVariable("PATH", "Machine")
    $currentPath = $env:PATH
    
    # Check if already in PATH
    if ($currentPath -split ";" | Where-Object { $_ -eq $InstallDir }) {
        Write-LogInfo "Install directory already in PATH"
        return
    }
    
    # Try to add to user PATH first (no admin required)
    try {
        $newPath = "$InstallDir;$userPath"
        [Environment]::SetEnvironmentVariable("PATH", $newPath, "User")
        Write-LogInfo "Added $InstallDir to user PATH"
        
        # Update current session PATH
        $env:PATH = [Environment]::GetEnvironmentVariable("PATH", "User") + ";" + [Environment]::GetEnvironmentVariable("PATH", "Machine")
    } catch {
        Write-LogWarn "Could not add to PATH: $_"
        Write-LogInfo "You can manually add $InstallDir to your PATH"
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
    
    # Install the binary
    $zipPath = Join-Path $script:TEMP_DIR "sprout-${os}-${arch}.zip"
    $binaryPath = Install-Binary -ZipPath $zipPath -InstallDir $installDir
    
    # Verify installation
    Verify-Installation -InstallDir $installDir

    # Clean up legacy ledit binary if present on PATH
    $legacyBinary = Get-Command ledit -ErrorAction SilentlyContinue
    if ($legacyBinary) {
        Write-LogInfo "Removing legacy 'ledit' binary..."
        Remove-Item $legacyBinary.Source -Force -ErrorAction SilentlyContinue
    }

    # Offer to install as system service
    if (-not $NoService.IsPresent) {
        if ($Service.IsPresent) {
            Write-LogInfo "Automatic service management is not yet available on Windows."
            Write-LogInfo "You can manually run: sprout agent -d to start the daemon."
        } else {
            # Interactive prompt
            $answer = Read-Host "[INFO] Install sprout as a background service? (auto-start on login) [Y/n]"
            if (-not $answer -or $answer -match '^[Yy]') {
                Write-LogInfo "Automatic service management is not yet available on Windows."
                Write-LogInfo "You can manually run: sprout agent -d to start the daemon."
            }
        }
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
