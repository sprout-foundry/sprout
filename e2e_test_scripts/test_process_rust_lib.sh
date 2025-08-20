#!/bin/bash

get_test_name() {
    echo "Process - Rust Library"
}

run_test_logic() {
    local model_name=$1
    echo "--- TEST: Process - Rust Library ---"
    start_time=$(date +%s)

    workdir="process_rust_lib"
    rm -rf "$workdir"
    mkdir -p "$workdir"
    cd "$workdir" || exit 1

    cat > process.json << 'JSON'
{
  "version": "1.0",
  "goal": "Create a Rust library crate named 'utils' with a greet function that takes a name parameter and returns a greeting string",
  "description": "Single-agent Rust library implementation with proper crate structure and documentation",
  "base_model": "",
  "agents": [{
    "id": "rustacean",
    "name": "Rust Developer",
    "persona": "backend_developer",
    "description": "Implements Rust libraries and crates with proper documentation and testing",
    "skills": ["rust", "library_development", "cargo"],
    "model": "",
    "priority": 1,
    "depends_on": [],
    "config": {"skip_prompt": "true"},
    "budget": {
      "max_tokens": 70000,
      "max_cost": 3.5,
      "token_warning": 50000,
      "cost_warning": 2.5,
      "alert_on_limit": true,
      "stop_on_limit": false
    }
  }],
  "steps": [{
    "id": "create_crate",
    "name": "Create Rust Library Crate",
    "description": "Create Cargo.toml and src/lib.rs with a public greet(name: &str) -> String function that returns a greeting",
    "agent_id": "rustacean",
    "input": {},
    "expected_output": "Working Rust library crate with greet function",
    "status": "pending",
    "depends_on": [],
    "timeout": 120,
    "retries": 2
  }],
  "validation": {
    "required": false,
    "build_command": "cargo check",
    "test_command": "cargo test",
    "custom_checks": ["cargo clippy -- -D warnings"]
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

    if [ ! -f "Cargo.toml" ] || [ ! -f "src/lib.rs" ]; then
        echo "FAIL: Expected Rust crate files not found."
        ls -la
        exit 1
    fi
    if ! grep -q "greet\(name: &str\)" src/lib.rs && ! grep -q "greet(.*&str" src/lib.rs; then
        echo "FAIL: greet function signature not found in src/lib.rs."
        cat src/lib.rs || true
        exit 1
    fi
    echo "PASS: Rust library files present with greet function."

    cd ../ || true
    end_time=$(date +%s)
    duration=$((end_time - start_time))
    echo "Test duration: $duration seconds"
}


