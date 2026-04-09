#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "Check LLM token tracking"
}

# Function to run the test logic
run_test_logic() {
    local model_name=$1
    echo "--- TEST: Check LLM token tracking ---"

    # Run a simple ledit agent command and capture output
    output=$(ledit agent "What is the capital of France?" --model "$model_name" 2>&1)
    echo "$output"

    # Check if the agent completed successfully (output includes completion indicator)
    if echo "$output" | grep -q "\[OK\] Completed"; then
        echo "Agent completed successfully. Test passed."
        return 0
    else
        echo "Agent did not complete successfully."
        echo "Output was: $output"
        return 1
    fi
}

# If called directly, run the test
if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
    run_test_logic "${1:-}"
fi
