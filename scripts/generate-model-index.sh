#!/bin/bash
# generate-model-index.sh creates a manifest index.json file from per-provider model JSON files.
# Usage: bash scripts/generate-model-index.sh [models-dir]
# Defaults to ./models if no directory is provided.

set -euo pipefail

MODELS_DIR="${1:-models}"
INDEX_FILE="${MODELS_DIR}/index.json"

# Check if models directory exists
if [[ ! -d "$MODELS_DIR" ]]; then
    echo "Error: models directory '$MODELS_DIR' does not exist" >&2
    exit 1
fi

# Find all JSON files (excluding index.json)
mapfile -t JSON_FILES < <(find "$MODELS_DIR" -maxdepth 1 -name '*.json' ! -name 'index.json' -type f | sort)

if [[ ${#JSON_FILES[@]} -eq 0 ]]; then
    echo "Error: no JSON files found in '$MODELS_DIR'" >&2
    exit 1
fi

# Determine the most recent updated_at from any provider file
MOST_RECENT_DATE=""
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
    
    # Extract updated_at from this file
    if command -v jq >/dev/null 2>&1; then
        # Use jq if available (more robust)
        date=$(jq -r '.updated_at // empty' "$file" 2>/dev/null || true)
    else
        # Fallback to grep/sed
        date=$(grep -o '"updated_at"[[:space:]]*:[[:space:]]*"[^"]*"' "$file" 2>/dev/null | sed 's/.*"updated_at"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/' || true)
    fi
    
    if [[ -n "$date" && "$date" != "null" ]]; then
        # Compare dates (ISO 8601 format allows string comparison)
        if [[ -z "$MOST_RECENT_DATE" ]] || [[ "$date" > "$MOST_RECENT_DATE" ]]; then
            MOST_RECENT_DATE="$date"
        fi
    fi
done

# If no updated_at found in any file, use current time
if [[ -z "$MOST_RECENT_DATE" ]]; then
    MOST_RECENT_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
fi

# Sort providers alphabetically (they should already be sorted by filename)
# Use jq to create proper JSON array if available, otherwise simple comma-separated
if command -v jq >/dev/null 2>&1; then
    # Parse comma-separated into sorted JSON array
    PROVIDERS_JSON=$(echo "$PROVIDERS" | tr ',' '\n' | sort | jq -R . | jq -s .)
    
    # Create index.json using jq
    echo "{}" | jq \
        --arg updated "$MOST_RECENT_DATE" \
        --argjson providers "$PROVIDERS_JSON" \
        '.updated_at = $updated | .providers = $providers' > "$INDEX_FILE"
else
    # Fallback: create manually
    {
        echo "{"
        echo "  \"updated_at\": \"$MOST_RECENT_DATE\","
        echo "  \"providers\": ["
        IFS=',' read -ra SORTED_PROVIDERS <<< "$PROVIDERS"
        printf '    "%s"\n' "${SORTED_PROVIDERS[@]}" | sort | sed '$ s/,$//' | paste -sd ',' | sed 's/^/    /'
        echo ""
        echo "  ]"
        echo "}"
    } > "$INDEX_FILE"
fi

echo "Created $INDEX_FILE with ${#JSON_FILES[@]} providers"
