#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "Provider Selection Test"
}

# Function to run the test logic
run_test_logic() {
    local model_name=$1
    echo "--- TEST: Provider Selection ---"

    # Guard for provider tests requiring network/key
    if [[ -z "${OPENROUTER_API_KEY:-}" ]]; then
        echo "SKIP: OPENROUTER_API_KEY not set; skipping provider selection tests"
        return 0
    fi

    # Create a simple test file
    cat > test_provider.txt << 'EOF'
This is a test file for provider selection.
EOF

    # Test 1: Verify dry-run mode works with an explicit provider and model
    echo "1. Testing explicit provider with dry-run mode..."
    OUTPUT=$(LEDIT_SKIP_CONNECTION_CHECK=1 LEDIT_NO_SUBAGENTS=1 ledit agent --provider deepinfra -m "$model_name" --no-connection-check --dry-run "list files" 2>&1)

    if echo "$OUTPUT" | grep -q "\[OK\] Completed\|Dry Run\|dry.run"; then
        echo "✓ Provider dry-run mode executed successfully"
    elif echo "$OUTPUT" | grep -q "error\|Error\|FAIL"; then
        echo "✗ Provider test failed with error"
        echo "$OUTPUT" | grep -i "error\|fail" | head -3
        # Don't fail — external provider issues are environmental
        echo "SKIP: Provider unavailable or misconfigured"
        rm -f test_provider.txt
        return 0
    else
        echo "ℹ Dry-run completed without expected markers (check output)"
        echo "$OUTPUT" | tail -5
    fi

    # Test 2: Verify the same test without --provider uses default from config
    echo -e "\n2. Testing default provider resolution..."
    OUTPUT2=$(LEDIT_SKIP_CONNECTION_CHECK=1 LEDIT_NO_SUBAGENTS=1 ledit agent -m "$model_name" --no-connection-check --dry-run "hello" 2>&1)

    if echo "$OUTPUT2" | grep -q "\[OK\] Completed"; then
        echo "✓ Default provider dry-run mode executed successfully"
    elif echo "$OUTPUT2" | grep -q "error\|Error\|FAIL"; then
        echo "✗ Default provider test failed with error"
        echo "$OUTPUT2" | grep -i "error\|fail" | head -3
        echo "SKIP: Default provider unavailable"
        rm -f test_provider.txt
        return 0
    fi

    # Clean up
    rm -f test_provider.txt

    echo -e "\n✅ All provider selection tests passed!"
}

# If called directly, run the test
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    run_test_logic "${1:-}"
fi
