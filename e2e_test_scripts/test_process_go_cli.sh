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
  "goal": "Create a Go CLI named echoer that uses command line flags to echo arguments",
  "description": "Single-agent Go CLI implementation with proper package structure",
  "base_model": "",
  "agents": [{
    "id": "godev",
    "name": "Go Developer",
    "persona": "backend_developer",
    "description": "Implements Go applications and command-line tools",
    "skills": ["go", "cli_development"],
    "model": "",
    "priority": 1,
    "depends_on": [],
    "config": {"skip_prompt": "true"},
    "budget": {
      "max_tokens": 100000,
      "max_cost": 5.0,
      "token_warning": 80000,
      "cost_warning": 4.0,
      "alert_on_limit": true,
      "stop_on_limit": false
    }
  }],
  "steps": [{
    "id": "init_cli",
    "name": "Initialize Go CLI Project",
    "description": "Create go.mod and main.go with proper package structure and flag parsing to echo command line arguments",
    "agent_id": "godev",
    "input": {},
    "expected_output": "Working Go CLI that accepts arguments and echoes them using flags",
    "status": "pending",
    "depends_on": [],
    "timeout": 120,
    "retries": 2
  }],
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

    ../../ledit process process.json --model "$model_name" --skip-prompt

    echo
    echo "--- Verifying Test ---"
    if [ ! -f ".ledit/orchestration_state.json" ]; then
        echo "FAIL: .ledit/orchestration_state.json was not created."
        exit 1
    fi
    echo "PASS: .ledit/orchestration_state.json was created."

    if grep -q '"status": "failed"' .ledit/orchestration_state.json; then
        echo "FAIL: One or more steps failed."
        exit 1
    fi
    if ! grep -q '"status": "completed"' .ledit/orchestration_state.json; then
        echo "FAIL: No steps marked completed."
        exit 1
    fi
    echo "PASS: Steps completed without failure."

    if [ ! -f "go.mod" ] || [ ! -f "main.go" ]; then
        echo "FAIL: Expected Go files (go.mod, main.go) not found."
        ls -la
        exit 1
    fi
    # Strict, concrete checks
    if ! grep -q "module \w\+" go.mod; then
        echo "FAIL: go.mod missing module declaration."
        cat go.mod
        exit 1
    fi
    if ! grep -q "package main" main.go; then
        echo "FAIL: main.go missing package main."
        cat main.go
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


