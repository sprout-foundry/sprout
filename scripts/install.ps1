# ledit one-line install script for Windows
# Requires PowerShell 5.1+ or PowerShell 7+

param(
    [switch]$Uninstall,
    [switch]$Version
)

# Enable colored output with fallback for non-color terminals
$ColorEnabled = $Host.UI.RawUI.SupportsColor

$Colors = @{
    Red = if ($ColorEnabled) { [ConsoleColor]::Red } else { $null }
    Green = if ($ColorEnabled) { [ConsoleColor]::Green } else { $null }
    Yellow = if ($ColorEnabled) { [ConsoleColor]::Yellow } else { $null }
    Blue = if ($ColorEnabled) { [ConsoleColor]::Blue } else { $null }
    Normal = if ($ColorEnabled) { $null } else { $null }
}

function Write-LogInfo {
    param([string]$Message)
    $color = $Colors.Blue
    if ($ColorEnabled) { [Console]::ForegroundColor = $color }
    Write-Host "[INFO] $Message"
    if ($ColorEnabled) { [Console]::ForegroundColor = [Console]::White }
}

function Write-LogSuccess {
    param([string]$Message)
    $color = $Colors.Green
    if ($ColorEnabled) { [Console]::ForegroundColor = $color }
    Write-Host "[SUCCESS] $Message"
    if ($ColorEnabled) { [Console]::ForegroundColor = [Console]::White }
}

function Write-LogWarn {
    param([string]$Message)
    $color = $Colors.Yellow
    if ($ColorEnabled) { [Console]::ForegroundColor = $color }
    Write-Host "[WARN] $Message"
    if ($ColorEnabled) { [Console]::ForegroundColor = [Console]::White }
}

function Write-LogError {
    param([string]$Message)
    $color = $Colors.Red
    if ($ColorEnabled) { [Console]::ForegroundColor = $color }
    Write-Host "[ERROR] $Message"
    if ($ColorEnabled) { [Console]::ForegroundColor = [Console]::White }
}

# Cleanup function
function Cleanup {
    if ($script:TEMP_DIR -and (Test-Path $script:TEMP_DIR)) {
        Remove-Item -Path $script:TEMP_DIR -Recurse -Force -ErrorAction SilentlyContinue
    }
}

# Check for curl dependency
function Check-Dependencies {
    $requiredCmds = @("curl")
    foreach ($cmd in $requiredCmds) {
        $cmdPath = Get-Command $cmd -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Source -ErrorAction SilentlyContinue
        if (-not $cmdPath) {
            Write-LogError "$cmd is required but not installed"
            exit 1
        }
    }
}

# Detect operating system
function Detect-OS {
    $os = $PSVersionTable.OS
    if ($os -match "Windows") {
        return "windows"
    }
    Write-LogError "Unsupported operating system: $os"
    Write-LogError "Only Windows is supported"
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
    if ($env:LEDIT_INSTALL_DIR) {
        return $env:LEDIT_INSTALL_DIR
    }
    
    # Default to LOCALAPPDATA\Programs\ledit
    $defaultDir = Join-Path $env:LOCALAPPDATA "Programs\ledit"
    
    # Check if we should use a directory on PATH
    $pathDirs = $env:PATH -split ";" | Where-Object { $_ -and (Test-Path $_) }
    
    # Prefer a writable directory on PATH
    foreach ($dir in $pathDirs) {
        try {
            $testFile = Join-Path $dir "test_write_$$.$(Get-Random)"
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
    if ($env:LEDIT_VERSION) {
        return $env:LEDIT_VERSION
    }
    
    try {
        $apiUrl = "https://api.github.com/repos/alantheprice/ledit/releases/latest"
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
    
    $filename = "ledit-${OS}-${Arch}.zip"
    $downloadUrl = "https://github.com/alantheprice/ledit/releases/download/${Version}/${filename}"
    
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
    $extractedPath = Join-Path $script:TEMP_DIR "ledit.exe"
    $exeEntry.ExtractToFile($extractedPath, $true)
    $zip.Dispose()
    
    # Create install directory if it doesn't exist
    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }
    
    # Copy the binary to install directory
    $installPath = Join-Path $InstallDir "ledit.exe"
    Copy-Item -Path $extractedPath -Destination $installPath -Force
    
    Write-LogSuccess "ledit installed to $installPath"
    
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
    
    $binaryPath = Join-Path $InstallDir "ledit.exe"
    
    if (-not (Test-Path $binaryPath)) {
        Write-LogError "ledit binary not found at $binaryPath"
        exit 1
    }
    
    # Try to run the binary to verify it works
    try {
        $versionOutput = & $binaryPath version 2>&1
        if ($LASTEXITCODE -ne 0 -and $versionOutput -notmatch "ledit") {
            Write-LogError "ledit binary verification failed"
            exit 1
        }
    } catch {
        Write-LogError "Failed to verify ledit binary: $_"
        exit 1
    }
    
    Write-LogSuccess "ledit binary verified"
}

# Remove old versions
function Remove-Old-Versions {
    param([string]$InstallDir)
    
    $binaryPath = Join-Path $InstallDir "ledit.exe"
    
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
    Write-LogInfo "To uninstall ledit:"
    Write-Host ""
    Write-Host "  # Remove the binary"
    Write-Host "  Remove-Item -Path '$InstallDir\ledit.exe'"
    Write-Host ""
}

# Print success message
function Print-Success {
    param([string]$InstallDir, [string]$Version)
    
    Write-Host ""
    Write-LogSuccess "ledit $Version installed successfully!"
    Write-Host ""
    Write-Host "  Binary location: $InstallDir\ledit.exe"
    Write-Host ""
    Write-Host "  Run 'ledit version' to verify the installation"
    Write-Host ""
}

# Show version info
function Show-Version {
    if ($env:LEDIT_VERSION) {
        Write-Host "ledit version $env:LEDIT_VERSION (requested)"
    } else {
        try {
            $apiUrl = "https://api.github.com/repos/alantheprice/ledit/releases/latest"
            $response = Invoke-RestMethod -Uri $apiUrl -UseBasicParsing -ErrorAction Stop
            $version = $response.tag_name -replace '^v', ''
            Write-Host "ledit version $version (latest)"
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
    
    # Handle uninstall
    if ($Uninstall.IsPresent) {
        Write-LogInfo "Uninstalling ledit..."
        
        $installDir = Get-InstallDir
        $binaryPath = Join-Path $installDir "ledit.exe"
        
        if (Test-Path $binaryPath) {
            try {
                $version = & $binaryPath version 2>&1 | Select-Object -First 1
                Write-LogInfo "Removing: $version"
            } catch {
                $version = "unknown"
            }
        }
        
        if (Test-Path $binaryPath) {
            try {
                Remove-Item -Path $binaryPath -Force
                Write-LogSuccess "ledit uninstalled successfully"
            } catch {
                Write-LogError "Cannot remove $binaryPath: $_"
                exit 1
            }
        } else {
            Write-LogWarn "ledit not found at $binaryPath"
        }
        
        Print-UninstallInstructions $installDir
        exit 0
    }
    
    # Check dependencies
    Write-LogInfo "Checking dependencies..."
    Check-Dependencies
    
    # Create temporary directory
    $script:TEMP_DIR = (New-Item -ItemType Directory -Path (Join-Path $env:TEMP "ledit-$PID") -Force).FullName
    
    # Detect OS and architecture
    Write-LogInfo "Detecting operating system and architecture..."
    $os = Detect-OS
    $arch = Detect-Arch
    Write-LogInfo "Detected: $os-$arch"
    
    # Get version
    $version = Get-Version
    Write-LogInfo "Installing ledit version: $version"
    
    # Determine install directory
    $installDir = Get-InstallDir
    Write-LogInfo "Installing to: $installDir"
    
    # Remove old versions if they exist
    Remove-Old-Versions $installDir
    
    # Download the release
    $downloadUrl = Download-Release -Version $version -OS $os -Arch $arch
    Write-LogInfo "Downloaded from: $downloadUrl"
    
    # Install the binary
    $zipPath = Join-Path $script:TEMP_DIR "ledit-${os}-${arch}.zip"
    $binaryPath = Install-Binary -ZipPath $zipPath -InstallDir $installDir
    
    # Verify installation
    Verify-Installation -InstallDir $installDir
    
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
