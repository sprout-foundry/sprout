# PowerShell twin of prepare-grammars.sh for Windows hosts without bash.
# Copies the tree-sitter grammar blobs pkg/ast embeds from the gotreesitter
# module cache into pkg/ast/grammars/bin/. Keep BLOBS in sync with the
# //go:embed directive in pkg/ast/grammars_embed.go.

$ErrorActionPreference = 'Stop'

$ProjectRoot = Split-Path -Parent $PSScriptRoot
$Dst = Join-Path $ProjectRoot 'pkg\ast\grammars\bin'

$Blobs = @(
    'go.bin', 'typescript.bin', 'tsx.bin', 'javascript.bin', 'python.bin',
    'c.bin', 'cpp.bin', 'c_sharp.bin', 'java.bin', 'rust.bin', 'ruby.bin',
    'php.bin', 'swift.bin', 'kotlin.bin', 'dart.bin', 'lua.bin',
    'elixir.bin', 'haskell.bin', 'bash.bin'
)

Push-Location $ProjectRoot
try {
    $Version = (go list -m -f '{{.Version}}' github.com/odvcencio/gotreesitter)
    if (-not $Version) {
        throw '[prepare-grammars] could not resolve gotreesitter version - is go.mod present?'
    }
    $ModCache = (go env GOMODCACHE)
} finally {
    Pop-Location
}

$Src = Join-Path $ModCache "github.com\odvcencio\gotreesitter@$Version\grammars\grammar_blobs"
if (-not (Test-Path $Src -PathType Container)) {
    throw "[prepare-grammars] grammar blob directory missing: $Src (try 'go mod download' first)"
}

New-Item -ItemType Directory -Force -Path $Dst | Out-Null

foreach ($blob in $Blobs) {
    $from = Join-Path $Src $blob
    if (-not (Test-Path $from -PathType Leaf)) {
        throw "[prepare-grammars] missing upstream blob: $from (gotreesitter $Version may have renamed or removed it)"
    }
    $to = Join-Path $Dst $blob
    # Module cache files are read-only; a copied read-only destination would
    # make the next run's overwrite fail, so clear the attribute each time.
    Copy-Item -Path $from -Destination $to -Force
    Set-ItemProperty -Path $to -Name IsReadOnly -Value $false
}

Write-Host "[prepare-grammars] copied $($Blobs.Count) grammar blobs from gotreesitter $Version"
