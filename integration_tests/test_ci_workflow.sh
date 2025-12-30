#!/bin/bash
set -e

echo "============================================"
echo "Testing CI Workflow Integration"
echo "============================================"

# Optional model argument (default to test client)
MODEL_ARG=${1:-test:test}

# Store original directory for reliable cleanup
ORIGINAL_DIR=$(pwd)

# Create a test directory (use current directory for more portability)
TEST_DIR="./testing/ledit_ci_integration_$$"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR" || { echo "Failed to cd to test dir: $TEST_DIR"; exit 1; }

# Initialize a simple project
cat > test.go << 'EOF'
package main

import "fmt"

func main() {
    fmt.Println("Hello, CI!")
}
EOF

# Test 1: Basic CI execution
echo -e "\n=== Test 1: Basic CI Execution ==="
export CI=1
ledit agent -m "$MODEL_ARG" "What does this Go file do?" > output.txt 2>&1 || true

if grep -q "CI Progress" output.txt || grep -q "CI Summary" output.txt; then
    echo "✅ CI mode detected and working"
else
    echo "⚠️  CI indicators not found, checking output format..."
    if grep -q "Session:" output.txt && grep -q "tokens" output.txt; then
        echo "✅ Token tracking working"
    else
        echo "❌ Output format issue"
        cat output.txt
    fi
fi

# Test 2: Non-TTY execution
echo -e "\n=== Test 2: Non-TTY Execution ==="
echo "Explain this code" | ledit agent -m "$MODEL_ARG" > piped_output.txt 2>&1 || true

# Check for ANSI codes
if grep -q $'\033\[' piped_output.txt; then
    echo "⚠️  ANSI codes found in piped output"
else
    echo "✅ Clean output without ANSI codes"
fi

# Test 3: GitHub Actions environment
echo -e "\n=== Test 3: GitHub Actions Environment ==="
unset CI
export GITHUB_ACTIONS=true
ledit agent -m "$MODEL_ARG" "List files" > gh_output.txt 2>&1 || true

if grep -q "test.go" gh_output.txt; then
    echo "✅ GitHub Actions mode working"
else
    echo "❌ GitHub Actions mode issue"
fi

# Clean up
cd "$ORIGINAL_DIR" || { echo "Failed to return to original directory"; exit 1; }
rm -rf "$TEST_DIR"
unset CI
unset GITHUB_ACTIONS

echo -e "\n✅ CI workflow integration test completed"
