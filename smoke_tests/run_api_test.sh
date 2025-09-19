#!/bin/bash

# Run API functionality smoke test

echo "=== Running API Functionality Smoke Test ==="
echo
echo "This test verifies the registry removal changes are working correctly."
echo "It will make real API calls if API keys are set."
echo

# Change to smoke_tests directory
cd "$(dirname "$0")"

# Run the test
go run test_api_functionality.go

# Capture exit code
EXIT_CODE=$?

echo
if [ $EXIT_CODE -eq 0 ]; then
    echo "✅ Smoke test completed successfully"
else
    echo "❌ Smoke test failed with exit code $EXIT_CODE"
fi

exit $EXIT_CODE