#!/bin/bash
# Function to return test name
get_test_name() {
    echo "Testing non-interactive mode"
}
set -e

echo "Testing non-interactive mode (piped output)..."

# Guard for networked agent
if [[ -z "${OPENROUTER_API_KEY:-}" ]]; then
  echo "SKIP: OPENROUTER_API_KEY not set; skipping non-interactive agent tests"
  exit 0
fi

# Test with piped output (non-interactive but not CI)
echo "What is 2 + 2?" | ./ledit agent || true

echo -e "\n\nTesting CI mode..."
export CI=1

# Test with CI mode
./ledit agent "List the files in this directory" || true

echo -e "\n\nTest completed!"
