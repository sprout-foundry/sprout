#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "Empty Search Grounding Query Test"
}

# Function to run the test logic
run_test_logic() {
    local model_name=$1 # Capture the model_name passed from test.sh
    echo "--- TEST: Empty Search Grounding Query Test ---"

    # Run ledit with an empty search grounding prompt
    output_log="empty_search_test_output.log"
    # Using --non-interactive to prevent any prompts from blocking the test
    echo "" | ../ledit code "Create a hello world app using echo and huma v2 in go. #SG" --skip-prompt --non-interactive -m "$model_name" > "$output_log" 2>&1

    # echo "Output of ledit command:"
    # echo "$(cat "$output_log")" > ../../output.log

    echo
    echo "--- Verifying Test ---"

    # Check that the command output indicates a search was initiated (even if empty)
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