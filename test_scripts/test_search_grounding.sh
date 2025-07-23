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
    echo "" | ../ledit code "Update research.txt with information from a web search about latest AI trends. #SG \"latest AI trends\"" -f research.txt --skip-prompt --non-interactive -m "$model_name" > "$output_log" 2>&1

    echo
    echo "--- Verifying Test ---"
    # Check that the command output contains search-related messages
    if grep -q "Performing Jina AI search for query" "$output_log"; then
        echo "PASS: Search grounding was triggered."
    else
        echo "FAIL: Search grounding was not triggered."
        cat "$output_log"
        exit 1
    fi

    # Check that the research file was updated
    original_research_content="This is a research document about AI."
    new_research_content=$(cat research.txt)
    if [ "$original_research_content" == "$new_research_content" ]; then
        echo "FAIL: research.txt was not updated with search results."
        cat research.txt
        exit 1
    fi
    echo "PASS: research.txt was updated with search results."
    echo "--- Content of updated research.txt: ---"
    cat research.txt
    echo "----------------------------------------"
    echo "----------------------------------------------------"
    echo
}