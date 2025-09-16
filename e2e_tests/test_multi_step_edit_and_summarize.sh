#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "Agent - Multi-Step Edit and Summarization Test"
}

# Function to run the test logic
run_test_logic() {
    local model_name=$1
    echo "--- TEST: Agent - Multi-Step Edit and Summarization Test ---"

    # Create test workspace
    workdir="multi_step_test"
    rm -rf "$workdir"
    mkdir -p "$workdir"
    cd "$workdir" || exit 1

    # Create a Go file that will be modified in multiple steps
    cat > calculator.go << 'EOF'
package main

import "fmt"

// Add returns the sum of two integers
func Add(a, b int) int {
    return a + b
}

func main() {
    fmt.Println("Calculator")
    fmt.Println("Add(5, 3) =", Add(5, 3))
}
EOF

    # Create a README
    cat > README.md << 'EOF'
# Test Calculator

A simple calculator for testing multi-step agent edits.
EOF

    # Step 1: Add a Subtract function
    output1=$(../../ledit agent "Add a Subtract function to calculator.go that takes two integers and returns their difference. Also add an example to main." --skip-prompt -m "$model_name" 2>&1)
    if ! echo "$output1" | grep -q "Simplified Agent completed successfully"; then
        echo "FAIL: Step 1 (Add Subtract) did not complete successfully"
        echo "Output: $output1"
        exit 1
    fi
    echo "PASS: Step 1 (Add Subtract) completed"

    # Step 2: Add a Multiply function
    output2=$(../../ledit agent "Add a Multiply function to calculator.go that takes two integers and returns their product. Also add an example to main." --skip-prompt -m "$model_name" 2>&1)
    if ! echo "$output2" | grep -q "Simplified Agent completed successfully"; then
        echo "FAIL: Step 2 (Add Multiply) did not complete successfully"
        echo "Output: $output2"
        exit 1
    fi
    echo "PASS: Step 2 (Add Multiply) completed"

    # Step 3: Add a Divide function with error handling
    output3=$(../../ledit agent "Add a Divide function to calculator.go. It should take two integers and return a float64 and an error. Handle division by zero. Add examples for both success and error cases to main." --skip-prompt -m "$model_name" 2>&1)
    if ! echo "$output3" | grep -q "Simplified Agent completed successfully"; then
        echo "FAIL: Step 3 (Add Divide) did not complete successfully"
        echo "Output: $output3"
        exit 1
    fi
    echo "PASS: Step 3 (Add Divide) completed"

    # Combine all outputs to check for summarization
    all_output="$output1\n$output2\n$output3"

    # Check if summarization was triggered
    if echo "$all_output" | grep -q "Summarizing analysis results"; then
        echo "PASS: Summarization was triggered during the multi-step edit"
    else
        echo "WARN: Summarization was not triggered. The test may not have generated enough context."
    fi

    # Final validation of the code
    exit_code=0
    if ! grep -q "func Subtract" calculator.go; then
        echo "FAIL: Subtract function not found"
        exit_code=1
    fi
    if ! grep -q "func Multiply" calculator.go; then
        echo "FAIL: Multiply function not found"
        exit_code=1
    fi
    if ! grep -q "func Divide" calculator.go; then
        echo "FAIL: Divide function not found"
        exit_code=1
    fi
    echo "PASS: All functions were added to calculator.go"

    # Check if the code compiles
    if go build calculator.go 2>/dev/null; then
        echo "PASS: Final code compiles successfully"
    else
        echo "FAIL: Final code does not compile"
        cat calculator.go
        exit_code=1
    fi

    # Clean up
    cd ../ || true
    rm -rf "$workdir"

    echo "----------------------------------------------------"
    echo
    return $exit_code
}
