#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "Check LLM token tracking"
}

# Function to run the test logic
run_test_logic() {
    local model_name=$1 # Capture the model_name passed from test.sh
    echo "--- TEST: Check LLM token tracking ---"
    # Execute a simple ledit question command and check for token usage in output
    ../ledit question "What is the capital of France?" | grep -q "Tokens used:"
    if [ $? -eq 0 ]; then
        echo "Token usage information found in output."
    else
        echo "ERROR: Token usage information NOT found in output."
        exit 1
    fi
    echo "----------------------------------------------------"
}
#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "Check LLM token tracking"
}

# Function to run the test logic
run_test_logic() {
    local model_name=$1 # Capture the model_name passed from test.sh
    echo "--- TEST: Check LLM token tracking ---"

    # Run a simple ledit question and capture output
    output=$(../ledit question "What is the capital of France?" --model "$model_name" 2>&1)
    echo "$output"

    # Check if token usage information is present in the output
    if echo "$output" | grep -q "Tokens used:"; then
        echo "Token usage information found. Test passed."
        return 0
    else
        echo "Token usage information NOT found. Test failed."
        return 1
    fi
    echo "----------------------------------------------------"
    echo
}
