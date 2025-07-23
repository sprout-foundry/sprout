<#
.SYNOPSIS
    Installs the latest version of ledit for Windows.

.DESCRIPTION
    This script downloads the latest ledit executable from the GitHub releases,
    extracts it, moves it to a user-local installation directory, and adds
    that directory to the user's PATH environment variable.

.NOTES
    Requires PowerShell 5.1 or later.
    Run this script from an elevated PowerShell session if you want to install
    to a system-wide path (though the default is user-local).
#>

# --- Configuration ---
$GITHUB_REPO = "alantheprice/ledit"
$INSTALL_BASE_DIR = "$env:LOCALAPPDATA\ledit" # User-local installation directory
$INSTALL_BIN_DIR = Join-Path $INSTALL_BASE_DIR "bin"
$BINARY_NAME = "ledit.exe"
$RELEASES_API_URL = "https://api.github.com/repos/$GITHUB_REPO/releases/latest"

# --- Functions ---

function Get-LatestReleaseTag {
    Write-Host "Fetching latest release tag from GitHub..."
    try {
        $releaseInfo = Invoke-RestMethod -Uri $RELEASES_API_URL -Method Get
        $tagName = $releaseInfo.tag_name
        if ([string]::IsNullOrEmpty($tagName)) {
            Write-Error "Could not retrieve the latest release tag from GitHub. 'tag_name' was empty."
            exit 1
        }
        Write-Host "Latest ledit release: $tagName"
        return $tagName
    }
    catch {
        Write-Error "Error retrieving latest release tag: $($_.Exception.Message)"
        exit 1
    }
}

function Add-ToUserPath {
    param (
        [string]$PathToAdd
    )
    Write-Host "Adding '$PathToAdd' to user PATH environment variable..."
    try {
        $currentPath = [Environment]::GetEnvironmentVariable("Path", "User")
        if (-not ($currentPath -split ';' -contains $PathToAdd)) {
            [Environment]::SetEnvironmentVariable("Path", "$currentPath;$PathToAdd", "User")
            Write-Host "Successfully added to PATH. You may need to restart your terminal for changes to take effect."
        } else {
            Write-Host "Path '$PathToAdd' already exists in user PATH."
        }
    }
    catch {
        Write-Error "Error adding to PATH: $($_.Exception.Message)"
        exit 1
    }
}

# --- Main Script ---

Write-Host "Starting ledit installation..."

# 1. Detect OS and Architecture
$OS_NAME = "windows"
$ARCH_NAME = ""

switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture) {
    "X64" { $ARCH_NAME = "amd64" }
    "Arm64" { $ARCH_NAME = "arm64" }
    default {
        Write-Error "Unsupported architecture: $([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture)"
        exit 1
    }
}

Write-Host "Detected OS: $OS_NAME, Architecture: $ARCH_NAME"

# 2. Get the latest release tag
$LATEST_TAG = Get-LatestReleaseTag
if ([string]::IsNullOrEmpty($LATEST_TAG)) {
    Write-Error "Error: Could not retrieve the latest release tag."
    exit 1
}

# 3. Construct download URL
$DOWNLOAD_URL = "https://github.com/$GITHUB_REPO/releases/download/$LATEST_TAG/ledit_${OS_NAME}_${ARCH_NAME}.zip"
Write-Host "Downloading from: $DOWNLOAD_URL"

# 4. Create temporary directory and download the archive
$TEMP_DIR = Join-Path ([System.IO.Path]::GetTempPath()) "ledit_install_$(Get-Random)"
New-Item -ItemType Directory -Path $TEMP_DIR -Force | Out-Null
$ZIP_FILE = Join-Path $TEMP_DIR "ledit.zip"

Write-Host "Downloading ledit to $ZIP_FILE..."
try {
    Invoke-WebRequest -Uri $DOWNLOAD_URL -OutFile $ZIP_FILE -UseBasicParsing
}
catch {
    Write-Error "Error: Failed to download ledit from $DOWNLOAD_URL. $($_.Exception.Message)"
    Remove-Item -Path $TEMP_DIR -Recurse -Force -ErrorAction SilentlyContinue
    exit 1
}

Write-Host "Download complete. Extracting..."

# 5. Extract the binary
try {
    Expand-Archive -Path $ZIP_FILE -DestinationPath $TEMP_DIR -Force
}
catch {
    Write-Error "Error: Failed to extract ledit.zip. $($_.Exception.Message)"
    Remove-Item -Path $TEMP_DIR -Recurse -Force -ErrorAction SilentlyContinue
    exit 1
}

# Check if the binary exists in the extracted location
$EXTRACTED_BINARY_PATH = Join-Path $TEMP_DIR $BINARY_NAME
if (-not (Test-Path $EXTRACTED_BINARY_PATH)) {
    Write-Error "Error: Expected binary '$BINARY_NAME' not found in extracted archive at '$EXTRACTED_BINARY_PATH'."
    Remove-Item -Path $TEMP_DIR -Recurse -Force -ErrorAction SilentlyContinue
    exit 1
}

Write-Host "Extracted ledit binary."

# 6. Install the binary
Write-Host "Installing ledit to $INSTALL_BIN_DIR..."

# Create installation directory if it doesn't exist
if (-not (Test-Path $INSTALL_BIN_DIR)) {
    Write-Host "Creating installation directory: $INSTALL_BIN_DIR"
    try {
        New-Item -ItemType Directory -Path $INSTALL_BIN_DIR -Force | Out-Null
    }
    catch {
        Write-Error "Error: Failed to create $INSTALL_BIN_DIR. Please ensure you have appropriate permissions. $($_.Exception.Message)"
        Remove-Item -Path $TEMP_DIR -Recurse -Force -ErrorAction SilentlyContinue
        exit 1
    }
}

# Move the binary
try {
    Move-Item -Path $EXTRACTED_BINARY_PATH -Destination $INSTALL_BIN_DIR -Force
}
catch {
    Write-Error "Error: Failed to move ledit binary to $INSTALL_BIN_DIR. Please check permissions. $($_.Exception.Message)"
    Remove-Item -Path $TEMP_DIR -Recurse -Force -ErrorAction SilentlyContinue
    exit 1
}

Write-Host "ledit installed successfully to $INSTALL_BIN_DIR"

# 7. Add to PATH
Add-ToUserPath -PathToAdd $INSTALL_BIN_DIR

# 8. Cleanup
Write-Host "Cleaning up temporary files..."
Remove-Item -Path $TEMP_DIR -Recurse -Force -ErrorAction SilentlyContinue
Write-Host "Temporary files cleaned up."

# 9. Verify installation
Write-Host ""
Write-Host "Verifying installation..."
try {
    # Refresh environment variables for the current session
    $env:Path = [Environment]::GetEnvironmentVariable("Path", "User") + ";" + [Environment]::GetEnvironmentVariable("Path", "Machine")

    # Attempt to run ledit --version
    $leditVersionOutput = & ledit --version 2>&1
    if ($LASTEXITCODE -eq 0) {
        Write-Host "ledit is in your PATH and executable."
        Write-Host "ledit version:"
        Write-Host $leditVersionOutput
    } else {
        Write-Warning "ledit is not directly in your PATH or could not be executed. You may need to restart your terminal."
        Write-Host "You can try running it with: '$INSTALL_BIN_DIR\$BINARY_NAME'"
        Write-Host "Error output from ledit --version: $leditVersionOutput"
    }
}
catch {
    Write-Warning "Could not verify ledit installation via 'ledit --version'. $($_.Exception.Message)"
    Write-Host "You can try running it with: '$INSTALL_BIN_DIR\$BINARY_NAME'"
}

Write-Host ""
Write-Host "Installation complete!"
Write-Host "To get started, run: ledit init"
