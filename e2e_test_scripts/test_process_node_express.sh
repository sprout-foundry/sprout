#!/bin/bash

get_test_name() {
    echo "Process - Node Express API"
}

run_test_logic() {
    local model_name=$1
    echo "--- TEST: Process - Node Express API ---"
    start_time=$(date +%s)

    workdir="process_node_express"
    rm -rf "$workdir"
    mkdir -p "$workdir"
    cd "$workdir" || exit 1

    cat > process.json << 'JSON'
{
  "version": "1.0",
  "goal": "Create a Node.js Express API with a /health endpoint that returns JSON status",
  "description": "Single-agent Node.js Express API implementation with proper project structure",
  "base_model": "",
  "agents": [{
    "id": "noder",
    "name": "Node.js Backend Developer",
    "persona": "backend_developer",
    "description": "Implements Node.js Express APIs and RESTful services",
    "skills": ["node", "express", "javascript", "rest_api"],
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
  }],
  "steps": [{
    "id": "create_api",
    "name": "Create Express API Project",
    "description": "Create package.json and server.js with Express server that has GET /health endpoint returning {status: 'ok'}",
    "agent_id": "noder",
    "input": {},
    "expected_output": "Working Express API with /health endpoint",
    "status": "pending",
    "depends_on": [],
    "timeout": 120,
    "retries": 2
  }],
  "validation": {
    "required": false,
    "build_command": "npm install",
    "test_command": "npm test",
    "custom_checks": ["node --check server.js"]
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

    # Check key files
    if [ ! -f "package.json" ]; then
        echo "FAIL: package.json not found."
        exit 1
    fi
    if ! grep -q "express" package.json; then
        echo "FAIL: package.json does not reference express."
        exit 1
    fi
    if ! ls *.js >/dev/null 2>&1; then
        echo "FAIL: No JavaScript source files found."
        ls -la
        exit 1
    fi
    echo "PASS: Node/Express project generated."

    cd ../ || true
    end_time=$(date +%s)
    duration=$((end_time - start_time))
    echo "Test duration: $duration seconds"
}


