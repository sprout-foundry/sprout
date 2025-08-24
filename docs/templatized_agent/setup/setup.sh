#!/bin/bash

# Agent Template Setup Script (clean, buildable template generator)
# Generates a minimal agent template with a clean architecture, avoiding
# fragile copies from the current repo. Suitable as a base for many agents.

set -euo pipefail

TARGET_DIR="${1:-./agent-template}"
TEMPLATE_NAME="${2:-simple-agent}"
MODULE_NAME_RAW="${3:-}"

if [[ -z "${MODULE_NAME_RAW}" ]]; then
BASENAME="$(basename "${TARGET_DIR}")"
MODULE_NAME="${BASENAME//[^a-zA-Z0-9_.\-]/-}"
else
MODULE_NAME="${MODULE_NAME_RAW}"
fi

echo "🤖 Setting up Agent Template"
echo "Target: $TARGET_DIR"
echo "Template: $TEMPLATE_NAME"
echo "Module: $MODULE_NAME"
echo

echo "📁 Creating directory structure..."
mkdir -p "$TARGET_DIR"/{cmd,pkg/{agent,tools,config,utils},configs,examples,docs,scripts,tests}

echo "🧱 Generating minimal, buildable codebase..."

echo "  → go.mod"
cat > "$TARGET_DIR/go.mod" << EOF
module $MODULE_NAME

go 1.21

require (
github.com/spf13/cobra v1.8.0
github.com/spf13/viper v1.18.0
)
