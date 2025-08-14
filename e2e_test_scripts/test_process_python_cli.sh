#!/bin/bash

get_test_name() {
    echo "Process - Python CLI App"
}

run_test_logic() {
    local model_name=$1
    echo "--- TEST: Process - Python CLI App ---"
    start_time=$(date +%s)

    workdir="process_python_cli"
    rm -rf "$workdir"
    mkdir -p "$workdir"
    cd "$workdir" || exit 1

    PROMPT="Create a small Python CLI tool named greet that accepts a --name argument and prints 'Hello, NAME!'. Use argparse and include a simple README. Provide a minimal project layout."

    ../../ledit process "$PROMPT" --model "$model_name" --skip-prompt

    echo
    echo "--- Verifying Test ---"
    # requirements.json should exist
    if [ ! -f ".ledit/requirements.json" ]; then
        echo "FAIL: .ledit/requirements.json was not created."
        exit 1
    fi
    echo "PASS: .ledit/requirements.json was created."

    # Ensure at least one step completed and none failed
    if grep -q '"status": "failed"' .ledit/requirements.json; then
        echo "FAIL: One or more steps failed according to requirements.json"
        exit 1
    fi
    if ! grep -q '"status": "completed"' .ledit/requirements.json; then
        echo "FAIL: No steps marked completed in requirements.json"
        exit 1
    fi
    echo "PASS: requirements.json indicates completed steps and no failures."

    # Check key files for a Python CLI project
    if [ ! -f "README.md" ]; then
        echo "FAIL: README.md not found."
        exit 1
    fi
    if ! ls *.py >/dev/null 2>&1; then
        echo "FAIL: No Python source file (*.py) found."
        ls -la
        exit 1
    fi
    # Basic sanity: should reference argparse
    if ! grep -R -q "argparse" .; then
        echo "FAIL: Did not detect argparse usage in generated files."
        exit 1
    fi
    echo "PASS: Python CLI sources generated with argparse."

    cd ../ || true
    end_time=$(date +%s)
    duration=$((end_time - start_time))
    echo "Test duration: $duration seconds"
}


