#!/bin/bash
set -e

echo "Setting up branch: $BRANCH_NAME"

# Configure git
git config user.name "github-actions[bot]"
git config user.email "github-actions[bot]@users.noreply.github.com"

# Fetch latest changes
git fetch origin

# Get default branch
DEFAULT_BRANCH=$(gh repo view --json defaultBranchRef -q '.defaultBranchRef.name')

# Check if branch already exists
if git ls-remote --heads origin "$BRANCH_NAME" | grep -q "$BRANCH_NAME"; then
    echo "Branch $BRANCH_NAME already exists, checking out..."
    git checkout -B "$BRANCH_NAME" "origin/$BRANCH_NAME"
    
    # Pull latest changes from default branch
    git pull origin "$DEFAULT_BRANCH" --no-edit || {
        echo "Merge conflicts detected, will let agent resolve them"
    }
else
    echo "Creating new branch $BRANCH_NAME from $DEFAULT_BRANCH..."
    git checkout -b "$BRANCH_NAME" "origin/$DEFAULT_BRANCH"
fi

# Output branch info
echo "branch-name=$BRANCH_NAME" >> $GITHUB_OUTPUT
echo "Branch setup complete: $(git branch --show-current)"