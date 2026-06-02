#!/bin/bash
# generate-provider-index.sh creates a manifest index.json file from per-provider config JSON files.
# Usage: bash scripts/generate-provider-index.sh [configs-dir]
# Defaults to ./pkg/agent_providers/configs if no directory is provided.
#
# Copies each provider config to ./providers/, injects schema_version and published_at
# metadata, then generates providers/index.json listing all providers.

set -euo pipefail

CONFIGS_DIR="${1:-pkg/agent_providers/configs}"
PROVIDERS_DIR="providers"
INDEX_FILE="${PROVIDERS_DIR}/index.json"

# Check if configs directory exists
if [[ ! -d "$CONFIGS_DIR" ]]; then
    echo "Error: provider configs directory '$CONFIGS_DIR' does not exist" >&2
    exit 1
fi

# Create providers output directory
mkdir -p "$PROVIDERS_DIR"

# Find all JSON files in the configs directory
mapfile -t JSON_FILES < <(find "$CONFIGS_DIR" -maxdepth 1 -name '*.json' ! -name 'index.json' -type f | sort)

if [[ ${#JSON_FILES[@]} -eq 0 ]]; then
    echo "Error: no JSON files found in '$CONFIGS_DIR'" >&2
    exit 1
fi

PUBLISHED_AT=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
PROVIDERS=""

for file in "${JSON_FILES[@]}"; do
    # Extract provider name from filename (e.g., openrouter.json -> openrouter)
    provider=$(basename "$file" .json)

    # Add to providers list
    if [[ -z "$PROVIDERS" ]]; then
        PROVIDERS="$provider"
    else
        PROVIDERS="$PROVIDERS,$provider"
    fi

    # Copy provider config to providers directory
    cp "$file" "${PROVIDERS_DIR}/${provider}.json"

    # Inject schema_version and published_at into the copied file
    if command -v jq >/dev/null 2>&1; then
        # Use jq if available (more robust)
        TMPL=$(mktemp "${PROVIDERS_DIR}/provider.XXXXXX")
        jq --arg sv "1" --arg pa "$PUBLISHED_AT" \
            '.schema_version = ($sv | tonumber) | .published_at = $pa' \
            "${PROVIDERS_DIR}/${provider}.json" > "$TMPL" && \
            mv "$TMPL" "${PROVIDERS_DIR}/${provider}.json"
    else
        # Fallback: use grep/sed to inject metadata after the opening brace
        if grep -q '"schema_version"' "${PROVIDERS_DIR}/${provider}.json" 2>/dev/null; then
            # Already has schema_version, update it
            sed -i "s/\"schema_version\"[[:space:]]*:[[:space:]]*[0-9]*/\"schema_version\": 1/" "${PROVIDERS_DIR}/${provider}.json"
            if grep -q '"published_at"' "${PROVIDERS_DIR}/${provider}.json" 2>/dev/null; then
                sed -i "s/\"published_at\"[[:space:]]*:[[:space:]]*\"[^\"]*\"/\"published_at\": \"${PUBLISHED_AT}\"/" "${PROVIDERS_DIR}/${provider}.json"
            else
                sed -i "s/{/{\n  \"schema_version\": 1,\n  \"published_at\": \"${PUBLISHED_AT}\",/" "${PROVIDERS_DIR}/${provider}.json"
            fi
        else
            # Inject after opening brace
            sed -i "s/{/{\n  \"schema_version\": 1,\n  \"published_at\": \"${PUBLISHED_AT}\",/" "${PROVIDERS_DIR}/${provider}.json"
        fi
    fi
done

# Generate providers/index.json
if command -v jq >/dev/null 2>&1; then
    # Parse comma-separated into sorted JSON array
    PROVIDERS_JSON=$(echo "$PROVIDERS" | tr ',' '\n' | sort | jq -R . | jq -s .)

    # Create index.json using jq
    echo "{}" | jq \
        --arg sv "1" \
        --arg published "$PUBLISHED_AT" \
        --argjson providers "$PROVIDERS_JSON" \
        '.schema_version = ($sv | tonumber) | .published_at = $published | .providers = $providers' \
        > "$INDEX_FILE"
else
    # Fallback: create manually
    {
        echo "{"
        echo "  \"schema_version\": 1,"
        echo "  \"published_at\": \"$PUBLISHED_AT\","
        echo "  \"providers\": ["
        IFS=',' read -ra SORTED_PROVIDERS <<< "$PROVIDERS"
        printf '    "%s"\n' "${SORTED_PROVIDERS[@]}" | sort | sed '$ s/,$//' | paste -sd ',' | sed 's/^/    /'
        echo ""
        echo "  ]"
        echo "}"
    } > "$INDEX_FILE"
fi

echo "Created $INDEX_FILE with ${#JSON_FILES[@]} providers"
