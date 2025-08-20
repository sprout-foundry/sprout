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
    # Create a minimal process.json for orchestration instead of passing a freeform prompt
    cat > process.json << 'JSON'
{
  "version": "1.0",
  "goal": "Create a simple Go HTTP server application with /hello endpoint that returns 'Hello, World!'",
  "description": "Single-agent demo to validate orchestration pipeline end-to-end",
  "base_model": "",
  "agents": [
    {
      "id": "dev",
      "name": "Go Backend Developer",
      "persona": "backend_developer",
      "description": "Implements Go HTTP services and REST APIs",
      "skills": ["go", "http", "web_servers"],
      "model": "",
      "priority": 1,
      "depends_on": [],
      "config": {"skip_prompt": "true"},
      "budget": {
        "max_tokens": 80000,
        "max_cost": 4.0,
        "token_warning": 60000,
        "cost_warning": 3.0,
        "alert_on_limit": true,
        "stop_on_limit": false
      }
    }
  ],
  "steps": [
    {
      "id": "create_server",
      "name": "Create HTTP Server",
      "description": "Create go.mod and main.go with HTTP server that listens on port 8080 and responds to /hello with 'Hello, World!'",
      "agent_id": "dev",
      "input": {},
      "expected_output": "Working HTTP server with /hello endpoint",
      "status": "pending",
      "depends_on": [],
      "timeout": 120,
      "retries": 2
    }
  ],
  "validation": {
    "required": false,
    "build_command": "go build",
    "test_command": "go test ./...",
    "custom_checks": ["go vet ./..."]
  },
  "settings": {
    "max_retries": 2,
    "step_timeout": 300,
    "parallel_execution": false,
    "stop_on_failure": true,
    "log_level": "info"
  }
}
JSON

    echo "Running ledit process with process.json"
    ../../ledit process process.json --model "$model_name" --skip-prompt

    echo
    echo "--- Verifying Test ---"

    # Check that the state file was created
    if [ ! -f ".ledit/orchestration_state.json" ]; then
        echo "FAIL: .ledit/orchestration_state.json was not created."
        exit 1
    fi
    echo "PASS: .ledit/orchestration_state.json was created."

    # Check that files were created
    if [ ! -f "main.go" ] || [ ! -f "go.mod" ]; then
        echo "FAIL: Not all expected application files were created."
        ls -l
        exit 1
    fi
    echo "PASS: Application files (main.go, go.mod) were created."

    # Check that the step is marked as completed
    if grep -q '"status": "failed"' .ledit/orchestration_state.json; then
        echo "FAIL: One or more orchestration steps failed."
        exit 1
    fi
    if ! grep -q '"status": "completed"' .ledit/orchestration_state.json; then
        echo "FAIL: No steps were marked as completed."
        exit 1
    fi
    echo "PASS: Orchestration step completed successfully."

    cd ../
    echo "----------------------------------------------------"
    echo
    echo "Orchestration test completed successfully."
    end_time=$(date +%s)
    duration=$((end_time - start_time))
    echo "Test duration: $duration seconds"
    echo "----------------------------------------------------"
}

# run_test_logic deepinfra:Qwen/Qwen3-Coder-480B-A35B-Instruct # Pass the model name from the command line argument