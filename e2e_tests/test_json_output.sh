#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "Verify JSON Output Format"
}

# Function to run the test logic
run_test_logic() {
    local model_name=$1 # Capture the model_name passed from test.sh
    echo "--- TEST: Verify JSON Output Format ---"
    # Create a simple Python file to analyze
    echo "def add(a, b):
        return a + b" > math.py

    # Run ledit to analyze the file and check the JSON output
    ../ledit agent "Analyze the 'math.py' file and provide the summary, exports, and references in JSON format. #WORKSPACE" --skip-prompt -m "$model_name"

    # Load the workspace file ./ledit/workspace.json
    json_output=$(cat .ledit/workspace.json)

    # Validate JSON format using jq
    if echo "$json_output" | jq empty 2>/dev/null; then
        echo "PASS: JSON output is valid."
    else
        echo "FAIL: JSON output is invalid."
        echo "Output: $json_output"
        exit 1
    fi

    # Check for required keys in the JSON output
    if echo "$json_output" | jq -e '.files."math.py".summary? and .files."math.py".exports? and .files."math.py".references?' >/dev/null; then
        echo "PASS: JSON output contains required keys."
    else
        echo "FAIL: JSON output missing required keys."
        echo "Output: $json_output"
        exit 1
    fi
    echo "----------------------------------------------------"
    echo
}