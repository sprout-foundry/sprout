#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "Orchestration Feature"
}

# Function to run the test logic
run_test_logic() {
    local model_name=$1 # Capture the model_name passed from test.sh
    echo "--- TEST: Orchestration Feature ---"
    # start a timer to measure the duration of the test
    start_time=$(date +%s)
    # Create initial files for the orchestration test
    mkdir -p orchestration_test
    cd orchestration_test
    # Define the orchestration prompt
    ORCHESTRATION_PROMPT="Create a simple Go HTTP server application. The package name should be hello and it should have a /hello endpoint that returns a localized greeting."

    echo "Running ledit orchestrate with prompt: \"$ORCHESTRATION_PROMPT\""

    # Run ledit orchestrate. Pipe 'y' to confirm the plan execution.
    orchestrate_output_log="orchestrate_output.log"
    ../../ledit process "$ORCHESTRATION_PROMPT" --model "$model_name" --skip-prompt

    echo
    echo "--- Verifying Test ---"

    # Check that the requirements.json file was created
    if [ ! -f ".ledit/requirements.json" ]; then
        echo "FAIL: .ledit/requirements.json was not created."
        exit 1
    fi
    echo "PASS: .ledit/requirements.json was created."

    # Check that files were created
    if [ ! -f "main.go" ] || [ ! -f "go.mod" ] || ! ls *_test.go >/dev/null 2>&1; then
        echo "FAIL: Not all expected application files were created."
        ls -l
        exit 1
    fi
    echo "PASS: Application files (main.go, go.mod, *_test.go) were created."

    # Check that all steps are marked as completed
    if grep -q '"status": "failed"' .ledit/requirements.json; then
        echo "FAIL: One or more orchestration steps failed."
        exit 1
    fi
    if ! grep -q '"status": "completed"' .ledit/requirements.json; then
        echo "FAIL: No steps were marked as completed."
        exit 1
    fi
    echo "PASS: All orchestration steps completed successfully."

    cd ../
    echo "----------------------------------------------------"
    echo
    echo "Orchestration test completed successfully."
    end_time=$(date +%s)
    duration=$((end_time - start_time))
    echo "Test duration: $duration seconds"
    echo "----------------------------------------------------"
}

# run_test_logic lambda-ai:qwen25-coder-32b-instruct # Pass the model name from the command line argument