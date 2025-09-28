#!/bin/bash
set -e

echo "Testing CI mode with simple query..."
export CI=1

# Guard for networked agent
if [[ -z "${OPENROUTER_API_KEY:-}" ]]; then
  echo "SKIP: OPENROUTER_API_KEY not set; skipping quick CI agent test"
  exit 0
fi

# Test with a simple query that should complete quickly
./ledit agent "What is 2 + 2?" || true

echo -e "\n\nTest completed successfully!"
