#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "Agent - Code Edit Workflow Test"
}

# Function to run the test logic
run_test_logic() {
    local model_name=$1 # Capture the model_name passed from test.sh
    echo "--- TEST: Agent - Code Edit Workflow Test ---"

    # Create test workspace with code to edit
    workdir="agent_code_edit_test"
    rm -rf "$workdir"
    mkdir -p "$workdir"
    cd "$workdir" || exit 1

    # Create a simple Go file that needs a basic fix
    cat > main.go << 'EOF'
package main

import "fmt"

func main() {
    // Simple greeting program
    message := "Hello"
    fmt.Println(message)
}
EOF

    # Create a README file
    cat > README.md << 'EOF'
# Test Application

This is a simple Go application for testing code editing.
EOF

    # Run agent to make a simple change
    output=$(../../ledit agent "Change the message in main.go from 'Hello' to 'Hello World'" --skip-prompt -m "$model_name" 2>&1)

    # Check if code editing completed successfully
    if echo "$output" | grep -q "Simplified Agent completed successfully"; then
        echo "PASS: Agent code edit workflow completed successfully"
        exit_code=0
    else
        echo "FAIL: Agent code edit workflow did not complete successfully"
        echo "Output: $output"
        exit_code=1
    fi

    # Check if the file was actually modified with the expected change
    if grep -q "Hello World" main.go; then
        echo "PASS: Code file was modified correctly"
    else
        echo "FAIL: Code file was not modified as expected"
        echo "----- main.go content -----"
        cat main.go || true
        echo "---------------------------"
        exit_code=1
    fi

    # Check if todos were created for code editing
    if echo "$output" | grep -q "Todos completed:"; then
        echo "PASS: Agent created and executed code editing todos"
    else
        echo "FAIL: Agent did not create or execute code editing todos"
        exit_code=1
    fi

    # Check if build validation passed
    if echo "$output" | grep -q "Build validation passed"; then
        echo "PASS: Build validation passed after code edits"
    else
        echo "WARN: Build validation status unclear"
    fi

    # Verify the fixed code compiles
    if go build main.go 2>/dev/null; then
        echo "PASS: Modified code compiles successfully"
    else
        echo "FAIL: Modified code does not compile"
        exit_code=1
    fi

    # Clean up
    cd ../ || true
    rm -rf "$workdir"

    echo "----------------------------------------------------"
    echo
    return $exit_code
}
