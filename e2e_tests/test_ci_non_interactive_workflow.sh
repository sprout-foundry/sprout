#!/bin/bash

# Function to return test name
get_test_name() {
    echo "Testing CI/Non-Interactive Workflows"
}

# Function to run the test logic
run_test_logic() {
    local model_name=$1
    echo "============================================"
    echo "Testing CI/Non-Interactive Workflows"
    echo "============================================"

    # Set up test directory inside the test workspace (not $HOME)
    TEST_DIR="ci_workflow_test_$$"
    mkdir -p "$TEST_DIR"
    cd "$TEST_DIR"

    # Initialize a git repo for testing
    git init
    echo "# Test Project" > README.md
    git add README.md
    git commit -m "Initial commit"

    # Create some test files
    cat > main.go << 'GOEOF'
package main

import "fmt"

func main() {
    fmt.Println("Hello, World!")
}
GOEOF

    git add .
    git commit -m "Add main.go"

    # Test 1: Non-interactive execution
    echo -e "\n=== Test 1: CI Agent Execution ==="
    export CI=1

    echo -e "\n--- Testing agent command in CI ---"
    if ledit agent "What files are in this directory?" > ci_output.txt 2>&1; then
        if grep -q "Completed\|completed\|\[OK\]" ci_output.txt; then
            echo "✅ Agent completed successfully in CI mode"
        elif grep -q "\[>>\]" ci_output.txt; then
            echo "✅ Agent processed the query in CI mode"
        else
            echo "❌ Unexpected output format"
            cat ci_output.txt
        fi
    else
        echo "❌ Agent command failed"
        cat ci_output.txt
    fi

    # Test 2: Streaming output captured
    echo -e "\n=== Test 2: Streaming Output ==="
    if ledit agent "Say hello" > streaming_output.txt 2>&1 || true; then
        if [ -s streaming_output.txt ]; then
            echo "✅ Output captured"
        else
            echo "❌ No output captured"
        fi
    fi

    # Summary
    echo -e "\n============================================"
    echo "CI/Non-Interactive Workflow Test Summary"
    echo "============================================"

    # Clean up test dir (stay within workspace)
    cd ..
    rm -rf "$TEST_DIR"

    echo -e "\nTest completed."
    return 0
}
