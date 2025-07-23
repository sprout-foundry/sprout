#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "File Deletion"
}

# Function to run the test logic
run_test_logic() {
    local model_name=$1 # Capture the model_name passed from test.sh
    echo "--- TEST: File Deletion ---"
    rm data.json
    echo "File 'data.json' has been deleted."

    # Run ledit again to trigger workspace update. The prompt is trivial.
    ../ledit code "Acknowledge that a file was deleted. #WORKSPACE" --skip-prompt -m "$model_name" --non-interactive

    echo
    echo "--- Verifying Test ---"
    # Check that data.json is no longer in the workspace file
    ! grep -q "\"data.json\":" .ledit/workspace.json && echo "PASS: data.json correctly removed from workspace.json" || (echo "FAIL: data.json still exists in workspace.json"; exit 1)
    echo "----------------------------------------------------"
    echo
}