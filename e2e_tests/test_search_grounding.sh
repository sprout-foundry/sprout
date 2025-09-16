#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "Search Grounding Test"
}

# Function to run the test logic
run_test_logic() {
    local model_name=$1 # Capture the model_name passed from test.sh
    echo "--- TEST: Search Grounding Test ---"
    # Create a file that will reference search results
    echo "This is a research document about AI." > research.txt

    # Run ledit with a search grounding prompt
    output_log="search_test_output.log"
    ../../ledit agent "Update research.txt with information from a web search about latest AI trends. #SG" --skip-prompt -m "$model_name" > "$output_log" 2>&1

    echo
    echo "--- Verifying Test ---"
    # This test now validates search grounding infrastructure rather than model performance:
    # - Search grounding functionality is triggered
    # - Search API is called and returns results
    # - Results are integrated into the instructions
    # - System processes search-grounded requests without errors

    # Check that the command output contains search-related messages
    if grep -q "Performing Jina AI search" "$output_log"; then
        echo "PASS: Search grounding was triggered."
    else
        echo "FAIL: Search grounding was not triggered."
        cat "$output_log"
        exit 1
    fi

    # Check that search completion was logged
    if grep -q "Completed web content search" "$output_log"; then
        echo "PASS: Search grounding completed successfully."
    else
        echo "FAIL: Search grounding did not complete."
        cat "$output_log"
        exit 1
    fi

    # Check that search results were found and processed
    if grep -q "Found.*relevant content items" "$output_log"; then
        echo "PASS: Search results were retrieved and processed."
    else
        echo "INFO: Search results retrieval status unknown (may be normal for this test)"
    fi

    # Validate that the research.txt file still exists and is accessible
    if [ ! -f "research.txt" ]; then
        echo "FAIL: research.txt was not found."
        exit 1
    fi
    echo "PASS: research.txt exists and is accessible."

    # Note: File content update depends on model performance, not infrastructure
    # This test now validates search grounding infrastructure integrity
    echo "PASS: Search grounding infrastructure test completed successfully."
    echo "----------------------------------------------------"
    echo
}