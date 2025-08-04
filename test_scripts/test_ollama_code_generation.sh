#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "Ollama Model Code Generation Test"
}

# Function to run the test logic
run_test_logic() {
    local model_name="ollama:qwen2.5-coder" # explicitly set the model name for this test since we are testing ollama code generation
    echo "--- TEST: Ollama Model Code Generation Test ---"
    echo "This test verifies code generation functionality using an Ollama model."

    # Create a temporary directory for this test to avoid conflicts
    mkdir -p ollama_test_dir
    cd ollama_test_dir

    # 1. Create initial files
    echo "This is an initial python script." > script.py
    echo "This is file1.txt." > file1.txt
    echo "Initial content of script.py:"
    cat script.py
    echo "Initial content of file1.txt:"
    cat file1.txt
    echo "-----------------------------"

    # 2. Modify script.py. This should trigger re-analysis for this file only.
    echo "print('This is an updated python script that calculates something important')" > script.py
    echo "File 'script.py' has been modified."

    # Store the original content of the file we expect to be changed.
    original_file1_content=$(cat file1.txt)

    # 3. Run ledit again. It should use the cached info for other files but re-analyze script.py.
    # The model_name is passed from the test runner (e.g., test.sh)
    echo "Running 'ledit code' to update file1.txt based on script.py using model: $model_name"
    ../../ledit code "In file1.txt, write a comment describing what the python script does now. #WORKSPACE" -f file1.txt -m "$model_name" --non-interactive --skip-prompt

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