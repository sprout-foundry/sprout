#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "Agent - Question Workflow Test"
}

# Function to run the test logic
run_test_logic() {
    local model_name=$1 # Capture the model_name passed from test.sh
    echo "--- TEST: Agent - Question Workflow Test ---"

    # Create test workspace with some code to ask questions about
    workdir="agent_question_test"
    rm -rf "$workdir"
    mkdir -p "$workdir"
    cd "$workdir" || exit 1

    # Create a Go file to ask questions about
    cat > calculator.go << 'EOF'
package calculator

// Add returns the sum of two numbers
func Add(a, b int) int {
    return a + b
}

// Subtract returns the difference of two numbers
func Subtract(a, b int) int {
    return a - b
}

// Multiply returns the product of two numbers
func Multiply(a, b int) int {
    return a * b
}

// Divide returns the quotient of two numbers
func Divide(a, b int) (int, error) {
    if b == 0 {
        return 0, fmt.Errorf("division by zero")
    }
    return a / b, nil
}
EOF

    # Create a README file
    cat > README.md << 'EOF'
# Calculator Package

This Go package provides basic arithmetic operations.
Functions include Add, Subtract, Multiply, and Divide.
EOF

    # Run agent with a question about the code
    output=$(../../ledit agent "Explain how the calculator package works and provide examples of how to use each function" --skip-prompt -m "$model_name" 2>&1)

    # Check if question was answered successfully
    if echo "$output" | grep -q "🤖 Answer:"; then
        echo "PASS: Agent question workflow provided an answer"
        exit_code=0
    else
        echo "FAIL: Agent question workflow did not provide an answer"
        echo "Output: $output"
        exit_code=1
    fi

    # Check if token usage was tracked
    if echo "$output" | grep -q "Total tokens used:"; then
        echo "PASS: Token usage was tracked in question workflow"
    else
        echo "FAIL: Token usage was not tracked in question workflow"
        exit_code=1
    fi

    # Check if cost was calculated
    if echo "$output" | grep -q "Total cost:"; then
        echo "PASS: Cost was calculated in question workflow"
    else
        echo "FAIL: Cost was not calculated in question workflow"
        exit_code=1
    fi

    # Verify the answer shows the agent is working (either direct answer or using tools)
    if echo "$output" | grep -q -E "(🤖 Answer:|tool_calls|calculator)"; then
        echo "PASS: Agent is responding appropriately to the question"
    else
        echo "FAIL: Agent did not respond appropriately to the question"
        echo "Actual agent output (last 500 chars):"
        echo "$output" | tail -c 500
        exit_code=1
    fi

    # Clean up
    cd ../ || true
    rm -rf "$workdir"

    echo "----------------------------------------------------"
    echo
    return $exit_code
}
