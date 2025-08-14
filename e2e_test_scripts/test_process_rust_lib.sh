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
  "goal": "Create Rust lib crate utils with greet(name)->String",
  "description": "Single-agent Rust lib",
  "agents": [{
    "id": "rustacean",
    "name": "Rust Dev",
    "persona": "backend_developer",
    "description": "Implements small Rust libraries",
    "skills": ["rust"],
    "model": "",
    "priority": 1,
    "depends_on": [],
    "config": {"skip_prompt": "true"}
  }],
  "steps": [{
    "id": "init",
    "name": "Init crate",
    "description": "Create Cargo.toml and src/lib.rs with greet(name: &str) -> String",
    "agent_id": "rustacean",
    "input": {},
    "expected_output": "Cargo.toml and src/lib.rs exist",
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


