#!/bin/bash
set -e

# Fetch issue details and prepare context for ledit

REPO_OWNER=$(echo $GITHUB_REPOSITORY | cut -d'/' -f1)
REPO_NAME=$(echo $GITHUB_REPOSITORY | cut -d'/' -f2)
ISSUE_DATA_DIR="/tmp/ledit-issue-$ISSUE_NUMBER"

echo "Fetching issue #$ISSUE_NUMBER from $GITHUB_REPOSITORY..."

# Create temporary directory for issue data
rm -rf "$ISSUE_DATA_DIR"
mkdir -p "$ISSUE_DATA_DIR"

# Fetch issue details
gh api "/repos/$REPO_OWNER/$REPO_NAME/issues/$ISSUE_NUMBER" > "$ISSUE_DATA_DIR/issue.json"

# Extract issue information
ISSUE_TITLE=$(jq -r '.title' "$ISSUE_DATA_DIR/issue.json")
ISSUE_BODY=$(jq -r '.body // ""' "$ISSUE_DATA_DIR/issue.json")
ISSUE_STATE=$(jq -r '.state' "$ISSUE_DATA_DIR/issue.json")
ISSUE_LABELS=$(jq -r '.labels[].name' "$ISSUE_DATA_DIR/issue.json" | tr '\n' ',')

echo "Issue: $ISSUE_TITLE (state: $ISSUE_STATE)"

# Fetch all comments
echo "Fetching comments..."
gh api "/repos/$REPO_OWNER/$REPO_NAME/issues/$ISSUE_NUMBER/comments" --paginate > "$ISSUE_DATA_DIR/comments.json"

# Create issue context file
cat > "$ISSUE_DATA_DIR/context.md" << EOF
# GitHub Issue #$ISSUE_NUMBER: $ISSUE_TITLE

**Repository**: $GITHUB_REPOSITORY
**State**: $ISSUE_STATE
**Labels**: $ISSUE_LABELS
**URL**: https://github.com/$GITHUB_REPOSITORY/issues/$ISSUE_NUMBER

## Description

$ISSUE_BODY

## Comments
EOF

# Add comments to context
jq -r '.[] | "### Comment by @\(.user.login) on \(.created_at)\n\n\(.body)\n"' "$ISSUE_DATA_DIR/comments.json" >> "$ISSUE_DATA_DIR/context.md" || echo "(No comments)" >> "$ISSUE_DATA_DIR/context.md"

# Download images from issue body and comments
echo "Extracting and downloading images..."
mkdir -p "$ISSUE_DATA_DIR/images"

# Function to extract and download images
download_images() {
    local text="$1"
    local prefix="$2"
    local count=0
    
    # Extract markdown image links ![alt](url)
    echo "$text" | grep -oE '!\[([^\]]*)\]\(([^)]+)\)' | sed -E 's/!\[([^\]]*)\]\(([^)]+)\)/\2/' | while read -r url; do
        if [[ "$url" =~ ^https?:// ]]; then
            count=$((count + 1))
            ext="${url##*.}"
            ext="${ext%%\?*}" # Remove query params
            [[ "$ext" =~ ^(jpg|jpeg|png|gif|webp|svg)$ ]] || ext="png"
            filename="${prefix}_${count}.${ext}"
            echo "  Downloading: $url -> $filename"
            curl -sL "$url" -o "$ISSUE_DATA_DIR/images/$filename" || echo "  Failed to download: $url"
        fi
    done
    
    # Extract HTML img tags <img src="url">
    echo "$text" | grep -oE '<img[^>]+src="([^"]+)"' | sed -E 's/<img[^>]+src="([^"]+)"/\1/' | while read -r url; do
        if [[ "$url" =~ ^https?:// ]]; then
            count=$((count + 1))
            ext="${url##*.}"
            ext="${ext%%\?*}"
            [[ "$ext" =~ ^(jpg|jpeg|png|gif|webp|svg)$ ]] || ext="png"
            filename="${prefix}_${count}.${ext}"
            echo "  Downloading: $url -> $filename"
            curl -sL "$url" -o "$ISSUE_DATA_DIR/images/$filename" || echo "  Failed to download: $url"
        fi
    done
}

# Download images from issue body
download_images "$ISSUE_BODY" "issue"

# Download images from comments
jq -r '.[].body // ""' "$ISSUE_DATA_DIR/comments.json" 2>/dev/null | while IFS= read -r comment; do
    download_images "$comment" "comment"
done

# List downloaded images
IMAGE_COUNT=$(find "$ISSUE_DATA_DIR/images" -type f 2>/dev/null | wc -l)
echo "Downloaded $IMAGE_COUNT images"

# Export paths for other scripts
echo "ISSUE_CONTEXT_FILE=$ISSUE_DATA_DIR/context.md" >> $GITHUB_ENV
echo "ISSUE_IMAGES_DIR=$ISSUE_DATA_DIR/images" >> $GITHUB_ENV
echo "ISSUE_DATA_DIR=$ISSUE_DATA_DIR" >> $GITHUB_ENV

echo "Issue data prepared at: $ISSUE_DATA_DIR"