#!/bin/bash

get_test_name() {
    echo "Process - Go CLI Tool"
}

run_test_logic() {
    local model_name=$1
    echo "--- TEST: Process - Go CLI Tool ---"
    start_time=$(date +%s)

    workdir="process_go_cli"
    rm -rf "$workdir"
    mkdir -p "$workdir"
    cd "$workdir" || exit 1

    cat > process.json << 'JSON'
{
  "version": "1.0",
  "goal": "Create a Go CLI named echoer",
  "description": "Single-agent Go CLI",
  "agents": [{
    "id": "godev",
    "name": "Go Dev",
    "persona": "backend_developer",
    "description": "Implements small Go CLIs",
    "skills": ["go"],
    "model": "",
    "priority": 1,
    "depends_on": [],
    "config": {"skip_prompt": "true"}
  }],
  "steps": [{
    "id": "init",
    "name": "Init CLI",
    "description": "Create go.mod and main.go in package main using flag to echo args",
    "agent_id": "godev",
    "input": {},
    "expected_output": "go.mod and main.go exist",
    "status": "pending",
    "depends_on": [],
    "timeout": 60,
    "retries": 0
  }],
  "validation": {"required": false},
  "settings": {"max_retries": 0, "step_timeout": 120, "parallel_execution": false, "stop_on_failure": true, "log_level": "info"}
}
JSON

    ../../ledit process process.json --model "$model_name" --skip-prompt

    echo
    echo "--- Verifying Test ---"
    if [ ! -f ".ledit/requirements.json" ]; then
        echo "FAIL: .ledit/requirements.json was not created."
        exit 1
    fi
    echo "PASS: .ledit/requirements.json was created."

    if grep -q '"status": "failed"' .ledit/requirements.json; then
        echo "FAIL: One or more steps failed."
        exit 1
    fi
    if ! grep -q '"status": "completed"' .ledit/requirements.json; then
        echo "FAIL: No steps marked completed."
        exit 1
    fi
    echo "PASS: Steps completed without failure."

    if [ ! -f "go.mod" ] || [ ! -f "main.go" ]; then
        echo "FAIL: Expected Go files (go.mod, main.go) not found."
        ls -la
        exit 1
    fi
    if ! grep -q "package main" main.go; then
        echo "FAIL: main.go missing package main."
        exit 1
    fi
    echo "PASS: Go CLI sources present."

    cd ../ || true
    end_time=$(date +%s)
    duration=$((end_time - start_time))
    echo "Test duration: $duration seconds"
}


