#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "Empty Search Grounding Query Test"
}

# Function to run the test logic
run_test_logic() {
    local model_name=$1 # Capture the model_name passed from test.sh
    echo "--- TEST: Empty Search Grounding Query Test ---"

    # Run a code generation command that should trigger a web search
    output=$(../../ledit code "How do I implement a bubble sort in Go?" --skip-prompt 2>&1)

    # Check if web content search was initiated
    if grep -q "Starting web content search for query: " "$output_log"; then
        echo "PASS: Web content search initiation was logged."
    else
        echo "FAIL: Web content search initiation was NOT logged."
        cat "$output_log"
        exit 1
    fi

    echo "----------------------------------------------------"
    echo
}