#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "Missing File Context Test"
}

# Function to run the test logic
run_test_logic() {
    local model_name=$1 # Capture the model_name passed in
    echo "--- TEST: Missing File Context Test ---"

    # Create a file that has some information for testing purposes
    echo "This is a reference file that will be used to test the llm requesting access to a file that was not provided, but is referenced in the text." > reference.txt

    # Run ledit to test handling of missing file context
    output_log="missing_file_test.log"
    ../ledit code "Summarize reference.txt into a new file called reference_summary.md" --skip-prompt -m "$model_name" > "$output_log" 2>&1

    # Check here that the command output contains the contents of the reference file
    if grep -q "This is a reference file that will be used to test the llm requesting access to a file that was not provided, but is referenced in the text." "$output_log"; then
        echo "FAIL: The command should have requested the file contents, but it did not."
        cat "$output_log"
    fi

    echo "PASS: Missing file context was handled correctly."

    echo "----------------------------------------------------"
    echo
}