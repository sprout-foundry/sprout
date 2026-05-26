#!/usr/bin/env bash
# Copy the tree-sitter grammar blobs we actually use from the gotreesitter
# module cache into pkg/ast/grammars/bin/, where pkg/ast/grammars_embed.go
# picks them up via //go:embed.
#
# Five blobs are copied: go, typescript, tsx, javascript, python — the set
# enumerated in pkg/ast.SupportedLanguages. Bumping the gotreesitter version
# in go.mod automatically pulls fresh blobs on the next run.
#
# This script is idempotent: re-running it just overwrites the destination
# files. It exits non-zero if `go mod download` hasn't been run (no module
# cache entry) or if the upstream layout has changed.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DST="$PROJECT_ROOT/pkg/ast/grammars/bin"

# Blobs required by pkg/ast.SupportedLanguages. Keep in sync with the
# //go:embed directive in pkg/ast/grammars_embed.go.
BLOBS=(
    "go.bin"
    "typescript.bin"
    "tsx.bin"
    "javascript.bin"
    "python.bin"
)

VERSION="$(go list -m -f '{{.Version}}' github.com/odvcencio/gotreesitter)"
if [ -z "$VERSION" ]; then
    echo "[prepare-grammars] could not resolve gotreesitter version — is go.mod present?" >&2
    exit 1
fi

MODCACHE="$(go env GOMODCACHE)"
SRC="${MODCACHE}/github.com/odvcencio/gotreesitter@${VERSION}/grammars/grammar_blobs"

if [ ! -d "$SRC" ]; then
    echo "[prepare-grammars] grammar blob directory missing: $SRC" >&2
    echo "[prepare-grammars] try running 'go mod download' first" >&2
    exit 1
fi

mkdir -p "$DST"

for blob in "${BLOBS[@]}"; do
    if [ ! -f "$SRC/$blob" ]; then
        echo "[prepare-grammars] missing upstream blob: $SRC/$blob" >&2
        echo "[prepare-grammars] gotreesitter $VERSION may have renamed or removed it" >&2
        exit 1
    fi
    # `install -m 644` writes a fresh copy with explicit permissions — module
    # cache files are read-only (0444), and a plain `cp` over an existing
    # read-only destination fails with EACCES on re-runs.
    install -m 644 "$SRC/$blob" "$DST/$blob"
done

echo "[prepare-grammars] copied ${#BLOBS[@]} grammar blobs from gotreesitter $VERSION"
