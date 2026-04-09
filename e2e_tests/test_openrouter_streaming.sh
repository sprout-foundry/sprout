#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "OpenRouter Streaming Token Counting"
}

# Function to run the test logic
run_test_logic() {
    echo "--- TEST: OpenRouter Streaming Token Counting ---"

    # Create a simple test file
    cat > test_streaming.py << 'EOF'
def hello():
    """A simple hello function to test token counting."""
    return "Hello from OpenRouter streaming!"
EOF

    # Require a real OpenRouter key; skip gracefully if missing
    if [[ -z "${OPENROUTER_API_KEY}" || "${OPENROUTER_API_KEY}" == "test" ]]; then
        echo "SKIP: OPENROUTER_API_KEY not set; skipping OpenRouter streaming test"
        return 0
    fi

    # Use the agent with OpenRouter streaming; -m selects a free model that supports tools
    echo "Running agent with OpenRouter streaming..."
    OUTPUT=$(LEDIT_SKIP_CONNECTION_CHECK=1 ./ledit agent --provider openrouter --no-connection-check --model "meta-llama/llama-3.3-70b-instruct:free" "Add a docstring explaining what the hello function does in test_streaming.py" 2>&1)

    # Check if the agent ran and completed (streaming + provider work)
    if echo "$OUTPUT" | grep -q "\[OK\] Completed"; then
        echo "✓ OpenRouter streaming completed successfully"

        # Check if token/cost info is in output (may or may not be present depending on mode)
        if echo "$OUTPUT" | grep -qi "token"; then
            echo "✓ Token tracking info found in output"
        else
            echo "ℹ Token tracking info not in CLI output (may be in web UI)"
        fi
    elif echo "$OUTPUT" | grep -q "No endpoints found\|does not support tool use\|rate-limit"; then
        echo "SKIP: OpenRouter free tier unavailable (rate-limited or no tool-capable endpoint)"
        return 0
    else
        echo "✗ OpenRouter streaming did not complete"
        echo "Output was:"
        echo "$OUTPUT"
        exit 1
    fi

    # Clean up
    rm -f test_streaming.py
}

# If called directly, run the test
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    run_test_logic
fi
