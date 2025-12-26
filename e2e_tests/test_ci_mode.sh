#!/bin/bash
# Function to return test name
get_test_name() {
    echo "Testing CI Mode Behavior"
}

# Test script for CI/non-interactive mode

echo "=== Testing CI Mode Behavior ==="
echo ""

# Guard: require provider key to run agent in CI/non-interactive tests
if [[ -z "${OPENROUTER_API_KEY:-}" ]]; then
  echo "SKIP: OPENROUTER_API_KEY not set; skipping CI mode agent tests"
  exit 0
fi

echo ""
echo "1. Testing with CI environment variable set:"
echo "----------------------------------------"
CI=1 ./ledit agent "What is 2+2?" || true

echo ""
echo "2. Testing with GITHUB_ACTIONS environment variable set:"
echo "-------------------------------------------------------"
GITHUB_ACTIONS=true ./ledit agent "What is 3+3?" || true

echo ""
echo "3. Testing with output piped (non-TTY):"
echo "--------------------------------------"
./ledit agent "What is 4+4?" | cat || true

echo ""
echo "4. Testing without any arguments in CI mode:"
echo "------------------------------------------"
CI=1 ./ledit agent || true

echo ""
echo "5. Testing streaming in CI mode:"
echo "-------------------------------"
CI=1 ./ledit agent --no-stream=false "Write a hello world in Python" || true

echo ""
echo "=== CI Mode Tests Complete ==="
