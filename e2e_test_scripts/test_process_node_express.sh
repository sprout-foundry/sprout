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
  "goal": "Create a Node Express API with /health",
  "description": "Single-agent Node API",
  "agents": [{
    "id": "noder",
    "name": "Node Dev",
    "persona": "backend_developer",
    "description": "Implements small Express APIs",
    "skills": ["node", "express"],
    "model": "",
    "priority": 1,
    "depends_on": [],
    "config": {"skip_prompt": "true"}
  }],
  "steps": [{
    "id": "init",
    "name": "Init API",
    "description": "Create package.json and server.js with GET /health => {status:'ok'}",
    "agent_id": "noder",
    "input": {},
    "expected_output": "package.json and server.js exist",
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


