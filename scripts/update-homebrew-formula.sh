#!/bin/sh
# Stamp the Homebrew formula with a release's version + per-platform
# SHA256 sums.
#
# Reads:
#   $1            release tag (e.g. v0.16.3) — required
#   $2            path to SHA256SUMS file from the release  — required
#   $3            output path for the stamped formula        — default: Formula/sprout.rb
#
# Behavior:
#   - Strips the leading 'v' from the tag for the version field
#     (Homebrew's `version` is conventionally bare).
#   - Reads SHA256SUMS once and extracts the four placeholders we need
#     by filename match.
#   - Aborts if any of the four sums is missing.
#   - Writes the stamped formula in-place (or to a new path).
#
# Designed to be called from .github/workflows/release.yml after the
# SHA256SUMS step runs. Also runnable locally for testing — just point
# it at a downloaded copy of the release's SHA256SUMS.

set -eu

usage() {
    cat <<'EOF'
Usage: scripts/update-homebrew-formula.sh <tag> <sha256sums-path> [out-path]

Example:
  curl -sLO https://github.com/sprout-foundry/sprout/releases/download/v0.16.3/SHA256SUMS
  ./scripts/update-homebrew-formula.sh v0.16.3 ./SHA256SUMS Formula/sprout.rb
EOF
}

if [ "$#" -lt 2 ]; then
    usage >&2
    exit 1
fi

tag="$1"
sums_path="$2"
out_path="${3:-Formula/sprout.rb}"

# Normalize the version. Homebrew's `version` field is bare ("0.16.3"),
# while release tags carry a "v" prefix ("v0.16.3"). The URL substitution
# inside the formula re-adds the v.
version="${tag#v}"

if [ ! -f "$sums_path" ]; then
    echo "error: SHA256SUMS not found at $sums_path" >&2
    exit 1
fi

# Pull the four hashes we need. awk: print the first column when the
# second column (after stripping the sha256sum binary-mode '*' prefix)
# matches the requested filename.
extract_sum() {
    awk -v f="$1" '{
        name = $2
        sub(/^\*/, "", name)
        if (name == f) { print $1; exit }
    }' "$sums_path"
}

sha_darwin_arm64=$(extract_sum "sprout-darwin-arm64.tar.gz")
sha_darwin_amd64=$(extract_sum "sprout-darwin-amd64.tar.gz")
sha_linux_arm64=$(extract_sum "sprout-linux-arm64.tar.gz")
sha_linux_amd64=$(extract_sum "sprout-linux-amd64.tar.gz")

for pair in \
    "darwin-arm64:$sha_darwin_arm64" \
    "darwin-amd64:$sha_darwin_amd64" \
    "linux-arm64:$sha_linux_arm64" \
    "linux-amd64:$sha_linux_amd64"; do
    name="${pair%%:*}"
    sha="${pair##*:}"
    if [ -z "$sha" ]; then
        echo "error: SHA256SUMS missing entry for sprout-${name}.tar.gz" >&2
        exit 1
    fi
done

# Write a fresh copy from the template so re-running is idempotent. We
# use a temp file + atomic rename so a Ctrl-C mid-edit can't leave a
# half-written formula behind.
template_path="Formula/sprout.rb"
if [ ! -f "$template_path" ]; then
    echo "error: template formula not found at $template_path" >&2
    exit 1
fi

tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT INT TERM

# The substitution targets are deterministic: version "0.0.0" and four
# all-zero sha256 strings. The four sha256s are positionally distinct
# inside the formula (darwin-arm, darwin-intel, linux-arm, linux-intel,
# top to bottom), so we walk them in order with awk.
awk -v ver="$version" \
    -v s1="$sha_darwin_arm64" \
    -v s2="$sha_darwin_amd64" \
    -v s3="$sha_linux_arm64" \
    -v s4="$sha_linux_amd64" '
BEGIN { sha_idx = 0 }
{
    if ($0 ~ /^  version "0\.0\.0"$/) {
        sub(/"0\.0\.0"/, "\"" ver "\"")
    }
    if ($0 ~ /sha256 "0{64}"/) {
        sha_idx++
        if (sha_idx == 1) sub(/0{64}/, s1)
        else if (sha_idx == 2) sub(/0{64}/, s2)
        else if (sha_idx == 3) sub(/0{64}/, s3)
        else if (sha_idx == 4) sub(/0{64}/, s4)
    }
    print
}
END {
    if (sha_idx != 4) {
        printf("error: expected 4 sha256 placeholders, found %d\n", sha_idx) > "/dev/stderr"
        exit 1
    }
}' "$template_path" > "$tmp"

mv "$tmp" "$out_path"
trap - EXIT INT TERM

echo "Wrote $out_path"
echo "  version:           $version"
echo "  darwin-arm64 sha:  $sha_darwin_arm64"
echo "  darwin-amd64 sha:  $sha_darwin_amd64"
echo "  linux-arm64 sha:   $sha_linux_arm64"
echo "  linux-amd64 sha:   $sha_linux_amd64"
