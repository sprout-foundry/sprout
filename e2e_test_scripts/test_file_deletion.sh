#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "File Deletion"
}

# Function to run the test logic
run_test_logic() {
    local model_name=$1 # Capture the model_name passed from test.sh
    echo "--- TEST: File Deletion ---"
    # Ensure data.json exists before attempting to delete it
    echo "test content" > data.json
    echo "Created data.json for deletion test."

    # Run the agent to delete the file
    output=$(../../ledit code "Delete the data.json file" --skip-prompt 2>&1)
    echo "$output"

    echo
    echo "--- Verifying Test ---"
    # Check that data.json is no longer in the workspace file
    ! grep -q "\"data.json\":" .ledit/workspace.json && echo "PASS: data.json correctly removed from workspace.json" || (echo "FAIL: data.json still exists in workspace.json"; exit 1)
    
    # Also check if the file actually exists on disk
    if [ ! -f "data.json" ]; then
        echo "PASS: data.json was successfully deleted from disk."
    else
        echo "FAIL: data.json still exists on disk."
        exit 1
    fi

    echo "----------------------------------------------------"
    echo
}