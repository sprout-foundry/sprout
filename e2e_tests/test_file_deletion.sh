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
    echo '{"key": "value"}' > data.json
    echo "Created data.json for deletion test."

    # Run the agent to delete the file
    output=$(../../ledit agent "Delete the file data.json using rm command" --skip-prompt 2>&1)
    echo "$output"

    echo
    echo "--- Verifying Test ---"
    # Ensure workspace file exists before checking contents
    if [ -f .ledit/workspace.json ]; then
        if grep -q '"data.json":' .ledit/workspace.json; then
            echo "FAIL: data.json still exists in workspace.json"
            exit 1
        else
            echo "PASS: data.json correctly removed from workspace.json"
        fi
    else
        echo "INFO: .ledit/workspace.json not found (agent likely failed to start). Skipping workspace check."
    fi

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
