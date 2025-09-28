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
    
    # Use the agent with OpenRouter to add a docstring
    echo "Running agent with OpenRouter streaming..."
    OUTPUT=$(./ledit agent --provider openrouter --model "deepseek/deepseek-chat:free" "Add a comment explaining what the hello function does in test_streaming.py" 2>&1)
    
    # Check if the output contains token information
    if echo "$OUTPUT" | grep -q "tokens used"; then
        echo "✓ Token counting is working in streaming mode"
        
        # Extract and display token info
        echo "$OUTPUT" | grep -E "(tokens used|Total cost)" || true
    else
        echo "✗ Token counting not found in output"
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
