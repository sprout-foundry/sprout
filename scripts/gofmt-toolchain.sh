#!/usr/bin/env bash
# Resolve the gofmt that matches the toolchain pinned in go.mod, so the local
# format gate agrees with CI.
#
# Why this exists: `make fmt-check` and the pre-commit hook used bare `gofmt`,
# which resolves via PATH. A developer whose installed Go is NEWER than the
# pinned toolchain keeps using their newer Go — Go only switches toolchains
# upward, never down — so their gofmt can format differently from CI's, and
# the gate reports false failures on files CI accepts (and vice versa).
#
# CI installs Go from the `go` directive in go.mod (currently the 1.26 line)
# and formats with that version's gofmt. This script resolves the same
# toolchain's gofmt from the module cache and prints its path. Falls back to
# PATH's gofmt when the pinned toolchain is not present (e.g. a fresh clone
# that has not yet downloaded it), so the gate still runs.
#
# Usage:  "$(scripts/gofmt-toolchain.sh)" -l pkg/ cmd/ internal/
set -u

tc="$(grep -m1 '^toolchain ' go.mod 2>/dev/null | awk '{print $2}')"
if [ -n "$tc" ]; then
  dir="$(go env GOMODCACHE)/golang.org/toolchain@v0.0.1-${tc}.$(go env GOOS)-$(go env GOARCH)"
  if [ -x "$dir/bin/gofmt" ]; then
    printf '%s\n' "$dir/bin/gofmt"
    exit 0
  fi
fi

command -v gofmt
