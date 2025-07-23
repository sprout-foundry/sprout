#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "Cached Workspace & Modifying a File"
}

# Function to run the test logic
run_test_logic() {
    local model_name=$1 # Capture the model_name passed from test.sh
    echo "--- TEST: Cached Workspace & Modifying a File ---"
    # Modify script.py. This should trigger re-analysis for this file only.
    echo "print('This is an updated python script that calculates something important')" > script.py
    echo "File 'script.py' has been modified."

    # Store the original content of the file we expect to be changed.
    original_file1_content=$(cat file1.txt)

    # Run ledit again. It should use the cached info for other files but re-analyze script.py.
    ../ledit code "In file1.txt, write a comment describing what the python script does now. #WORKSPACE" --skip-prompt -m "$model_name"

    echo
    echo "--- Verifying Test ---"
    # The main verification is that file1.txt was modified, implying the LLM had the updated context.
    new_file1_content=$(cat file1.txt)
    if [ "$original_file1_content" == "$new_file1_content" ]; then
        echo "FAIL: file1.txt was not modified by the LLM."
        cat file1.txt
        exit 1
    fi
    echo "PASS: file1.txt was modified, indicating updated context was used."
    echo "--- Content of updated file1.txt: ---"
    cat file1.txt
    echo "---------------------------------------"
    echo "----------------------------------------------------"
    echo
}