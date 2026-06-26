#!/bin/bash
# generate-provider-index.sh — Generate providers/index.json from provider config files.
# Usage: bash scripts/generate-provider-index.sh
#
# Reads JSON files from two source directories:
#   - pkg/agent_providers/configs/           (embedded built-ins)
#   - pkg/agent_providers/community-configs/ (remote-only, not embedded)
# extracts provider names, sorts them alphabetically, and writes
# providers/index.json. The community-configs/ directory is the path
# for adding a provider to GitHub Pages without growing the binary.
#
# Fails loudly if the same provider id appears in both source dirs —
# that's an unresolved conflict, not a precedence question.
#
# Does NOT copy, modify, or touch any individual provider files.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

CONFIGS_DIR="$REPO_ROOT/pkg/agent_providers/configs"
COMMUNITY_CONFIGS_DIR="$REPO_ROOT/pkg/agent_providers/community-configs"
PROVIDERS_DIR="$REPO_ROOT/providers"
INDEX_FILE="${PROVIDERS_DIR}/index.json"

# --- Validate ---

if [[ ! -d "$CONFIGS_DIR" ]]; then
    echo "Error: provider configs directory '$CONFIGS_DIR' does not exist" >&2
    exit 1
fi

shopt -s nullglob
JSON_FILES=("$CONFIGS_DIR"/*.json)
COMMUNITY_JSON_FILES=()
if [[ -d "$COMMUNITY_CONFIGS_DIR" ]]; then
    COMMUNITY_JSON_FILES=("$COMMUNITY_CONFIGS_DIR"/*.json)
fi
shopt -u nullglob

if [[ ${#JSON_FILES[@]} -eq 0 && ${#COMMUNITY_JSON_FILES[@]} -eq 0 ]]; then
    echo "Error: no JSON files found in '$CONFIGS_DIR' or '$COMMUNITY_CONFIGS_DIR'" >&2
    exit 1
fi

# --- Extract provider names + detect cross-dir collisions ---
# Avoid `declare -A` to keep this runnable on macOS bash 3.2.
#
# Local-only providers (non-HTTPS endpoint, e.g. lmstudio at
# http://127.0.0.1) are excluded from the registry index: they ship
# embedded in the binary and are meaningless to remote fetchers, which
# would reject them anyway — the registry requires HTTPS (SSRF guard).
# The publish workflow's copy step applies the same filter so the index
# lists exactly the providers that got published.

# is_https_endpoint reads .endpoint from a config file and returns 0
# (true) if it starts with https://. A missing/empty endpoint is
# treated as non-publishable. Case-insensitive on the scheme to match
# the workflow's jq check. Bash 3.2-safe — no ${var,,}.
is_https_endpoint() {
    local file="$1" ep=""
    if command -v jq >/dev/null 2>&1; then
        ep="$(jq -r '.endpoint // ""' "$file" 2>/dev/null || true)"
    else
        ep="$(grep -o '"endpoint"[[:space:]]*:[[:space:]]*"[^"]*"' "$file" 2>/dev/null \
            | sed 's/.*"endpoint"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/' || true)"
    fi
    [[ "$ep" == [hH][tT][tT][pP][sS]://* ]]
}

PROVIDERS=()
EMBEDDED_NAMES=""

# Safe expansion on empty arrays under `set -u`. See comment above
# for the bash-3.2 idiom.
for file in ${JSON_FILES[@]+"${JSON_FILES[@]}"}; do
    provider="$(basename "$file" .json)"
    if ! is_https_endpoint "$file"; then
        echo "skip $provider (non-HTTPS endpoint, local-only)" >&2
        continue
    fi
    EMBEDDED_NAMES+="$provider"$'\n'
    PROVIDERS+=("$provider")
done

# ${ARR[@]+"${ARR[@]}"} is the bash-3.2-safe expansion of a possibly-empty
# array under `set -u`; bare "${ARR[@]}" trips "unbound variable" there.
for file in ${COMMUNITY_JSON_FILES[@]+"${COMMUNITY_JSON_FILES[@]}"}; do
    provider="$(basename "$file" .json)"
    if ! is_https_endpoint "$file"; then
        echo "skip $provider (non-HTTPS endpoint, local-only)" >&2
        continue
    fi
    if printf '%s' "$EMBEDDED_NAMES" | grep -qxF -- "$provider"; then
        echo "Error: provider '$provider' exists in both configs/ and community-configs/ — remove one" >&2
        exit 1
    fi
    PROVIDERS+=("$provider")
done

# If every provider was filtered out as local-only (non-HTTPS), fail
# loudly rather than publishing an empty index that would wipe the
# registry. A genuinely empty source dir is already caught above; this
# guards the case where files exist but none are publishable.
if [[ ${#PROVIDERS[@]} -eq 0 ]]; then
    echo "Error: no publishable providers found — all have non-HTTPS endpoints (local-only)" >&2
    exit 1
fi

# Sort alphabetically
IFS=$'\n' SORTED=($(printf '%s\n' "${PROVIDERS[@]}" | sort)); unset IFS

# --- Timestamp ---

PUBLISHED_AT="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"

# --- Generate index.json ---

mkdir -p "$PROVIDERS_DIR"

if command -v jq >/dev/null 2>&1; then
    # Build JSON array from sorted names
    PROVIDERS_JSON=$(printf '%s\n' "${SORTED[@]}" | jq -R . | jq -s .)

    jq -n \
        --argjson sv 1 \
        --arg published "$PUBLISHED_AT" \
        --argjson providers "$PROVIDERS_JSON" \
        '{schema_version: $sv, published_at: $published, providers: $providers}' \
        > "$INDEX_FILE"
else
    # Fallback: manually construct JSON without jq
    {
        echo '{'
        echo '  "schema_version": 1,'
        echo "  \"published_at\": \"${PUBLISHED_AT}\","
        echo '  "providers": ['
        local_count=0
        for p in "${SORTED[@]}"; do
            local_count=$((local_count + 1))
            if [[ $local_count -eq ${#SORTED[@]} ]]; then
                echo "    \"${p}\""
            else
                echo "    \"${p}\","
            fi
        done
        echo '  ]'
        echo '}'
    } > "$INDEX_FILE"
fi

echo "Created $INDEX_FILE with ${#SORTED[@]} providers"
