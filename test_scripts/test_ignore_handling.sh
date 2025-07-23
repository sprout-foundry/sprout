#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "Advanced Ignore Handling (.gitignore & .leditignore)"
}

# Function to run the test logic
run_test_logic() {
    local model_name=$1 # Capture the model_name passed from test.sh
    echo "--- TEST: Advanced Ignore Handling (.gitignore & .leditignore) ---"
    # --- Part 1: Simple file and extension ignores ---
    echo "--- Part 1: Simple file and extension ignores ---"
    # Create a file to be ignored via .leditignore
    echo "This is temporary data" > temp_data.tmp
    # Create a file to be ignored via .gitignore (add to existing .gitignore)
    echo "This is a build artifact" > build_artifact.o
    echo "*.o" >> .gitignore

    echo "Created temp_data.tmp and build_artifact.o"
    echo "Using 'ledit ignore' to ignore temp_data.tmp"

    # Use the new 'ignore' command
    ../ledit ignore "temp_data.tmp"

    echo
    echo "--- Verifying .ledit/leditignore content for simple ignore ---"
    if [ ! -f ".ledit/leditignore" ]; then
        echo "FAIL: .ledit/leditignore was not created."
        exit 1
    fi
    if grep -q "^temp_data.tmp$" ".ledit/leditignore"; then
        echo "PASS: temp_data.tmp found in .ledit/leditignore"
    else
        echo "FAIL: temp_data.tmp not found in .ledit/leditignore"
        cat .ledit/leditignore
        exit 1
    fi
    echo "----------------------------------------------------------"

    # --- Part 2: Glob and directory ignores ---
    echo
    echo "--- Part 2: Glob and directory ignores ---"
    mkdir -p a/b/ignore_this_dir
    mkdir -p docs
    echo "deep log data" > a/b/deep_file.log
    echo "file in ignored dir" > a/b/ignore_this_dir/some_file.txt
    echo "api documentation" > docs/api.md
    echo "this file is important" > a/b/important_file.txt
    echo "Created nested files and directories for glob testing."

    echo "**/*.log" >> .gitignore
    echo "docs/" >> .gitignore
    ../ledit ignore "**/ignore_this_dir/"
    echo "Added glob patterns to .gitignore and .leditignore"
    echo "--- .gitignore content: ---"
    cat .gitignore
    echo "---------------------------"
    echo "--- .ledit/leditignore content: ---"
    cat .ledit/leditignore
    echo "-----------------------------------"

    # Run ledit again to trigger workspace update with all new rules.
    ../ledit code "Acknowledge new ignore rules. #WORKSPACE" --skip-prompt -m "$model_name"

    echo
    echo "--- Verifying Test ---"
    # Check simple ignores from Part 1
    ! grep -q "\"temp_data.tmp\":" .ledit/workspace.json && echo "PASS: temp_data.tmp correctly ignored via .leditignore" || (echo "FAIL: temp_data.tmp still exists in workspace.json"; exit 1)
    ! grep -q "\"build_artifact.o\":" .ledit/workspace.json && echo "PASS: build_artifact.o correctly ignored via .gitignore" || (echo "FAIL: build_artifact.o still exists in workspace.json"; exit 1)

    # Check glob and directory ignores from Part 2
    ! grep -q "\"a/b/deep_file.log\":" .ledit/workspace.json && echo "PASS: a/b/deep_file.log correctly ignored via .gitignore glob (**/*.log)" || (echo "FAIL: a/b/deep_file.log still exists in workspace.json"; exit 1)
    ! grep -q "\"docs/api.md\":" .ledit/workspace.json && echo "PASS: docs/api.md correctly ignored via .gitignore directory rule (docs/)" || (echo "FAIL: docs/api.md still exists in workspace.json"; exit 1)
    ! grep -q "\"a/b/ignore_this_dir/some_file.txt\":" .ledit/workspace.json && echo "PASS: a/b/ignore_this_dir/some_file.txt correctly ignored via .leditignore glob (**/ignore_this_dir/)" || (echo "FAIL: a/b/ignore_this_dir/some_file.txt still exists in workspace.json"; exit 1)

    # Check that the important file was NOT ignored
    grep -q "\"a/b/important_file.txt\":" .ledit/workspace.json && echo "PASS: a/b/important_file.txt correctly included in workspace.json" || (echo "FAIL: a/b/important_file.txt was not found in workspace.json"; exit 1)

    echo "----------------------------------------------------"
    echo
}