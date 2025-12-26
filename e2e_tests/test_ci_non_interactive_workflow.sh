#!/bin/bash
# Function to return test name
get_test_name() {
    echo "Testing CI/Non-Interactive Workflows"
}
set -e

echo "============================================"
echo "Testing CI/Non-Interactive Workflows"
echo "============================================"

# Set up test directory
TEST_DIR="$HOME/ledit_ci_worklow_test_$$"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"

# Initialize a git repo for testing
git init
echo "# Test Project" > README.md
git add README.md
git commit -m "Initial commit"

# Create some test files
cat > main.go << 'EOF'
package main

import "fmt"

func main() {
    fmt.Println("Hello, World!")
}

func add(a, b int) int {
    return a + b
}
EOF

cat > main_test.go << 'EOF'
package main

import "testing"

func TestAdd(t *testing.T) {
    result := add(2, 3)
    if result != 5 {
        t.Errorf("Expected 5, got %d", result)
    }
}
EOF

git add .
git commit -m "Add main.go and tests"

# Test 1: CI environment detection and non-interactive execution
echo -e "\n=== Test 1: CI Environment Detection ==="
export CI=1
export GITHUB_ACTIONS=1

# Test simple agent command in CI mode
echo -e "\n--- Testing simple agent command in CI ---"
ledit agent "What files are in this directory?" > ci_output.txt 2>&1

# Check for CI progress indicators
if grep -q "progress" ci_output.txt; then
    echo "✅ CI progress indicators found"
else
    echo "❌ CI progress indicators missing"
    cat ci_output.txt
fi

# Check for token/cost output
if grep -q "Session:" ci_output.txt; then
    echo "✅ Token and cost information displayed"
else
    echo "❌ Token and cost information missing"
    cat ci_output.txt
fi

# Test 2: Streaming output in CI mode
echo -e "\n=== Test 2: Streaming Output ==="
ledit agent "Write a simple hello world" > streaming_output.txt 2>&1 || true

if [ -s streaming_output.txt ]; then
    echo "✅ Streaming output captured"
else
    echo "❌ No streaming output captured"
fi

# Summary
echo -e "\n============================================"
echo "CI/Non-Interactive Workflow Test Summary"
echo "============================================"

# Clean up
cd /
rm -rf "$TEST_DIR"

echo -e "\nTest completed. Review output above for any issues."
