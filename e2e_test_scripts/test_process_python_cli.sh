#!/bin/bash

get_test_name() {
    echo "Process - Python CLI App"
}

run_test_logic() {
    local model_name=$1
    echo "--- TEST: Process - Python CLI App ---"
    start_time=$(date +%s)

    workdir="process_python_cli"
    rm -rf "$workdir"
    mkdir -p "$workdir"
    cd "$workdir" || exit 1

    # Create a minimal single-step process file
    cat > process.json << 'JSON'
{
  "version": "1.0",
  "goal": "Create a Python CLI using argparse",
  "description": "Single-agent small Python CLI",
  "agents": [{
    "id": "pydev",
    "name": "Python Dev",
    "persona": "backend_developer",
    "description": "Implements small Python CLIs",
    "skills": ["python", "argparse"],
    "model": "",
    "priority": 1,
    "depends_on": [],
    "config": {"skip_prompt": "true"}
  }],
  "steps": [{
    "id": "init",
    "name": "Init CLI",
    "description": "Create greet.py using argparse with --name and a README",
    "agent_id": "pydev",
    "input": {},
    "expected_output": "greet.py and README.md exist",
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
    # requirements.json should exist
    if [ ! -f ".ledit/orchestration_state.json" ]; then
        echo "FAIL: .ledit/orchestration_state.json was not created."
        exit 1
    fi
    echo "PASS: .ledit/orchestration_state.json was created."

    # Ensure at least one step completed and none failed
    if grep -q '"status": "failed"' .ledit/orchestration_state.json; then
        echo "FAIL: One or more steps failed according to requirements.json"
        exit 1
    fi
    if ! grep -q '"status": "completed"' .ledit/orchestration_state.json; then
        echo "FAIL: No steps marked completed in requirements.json"
        exit 1
    fi
    echo "PASS: orchestration_state.json indicates completed steps and no failures."

    # Check key files for a Python CLI project
    if [ ! -f "README.md" ]; then
        echo "FAIL: README.md not found."
        exit 1
    fi
    if ! ls *.py >/dev/null 2>&1; then
        echo "FAIL: No Python source file (*.py) found."
        ls -la
        exit 1
    fi
    # Basic sanity: should reference argparse
    if ! grep -R -q "argparse" .; then
        echo "FAIL: Did not detect argparse usage in generated files."
        exit 1
    fi
    echo "PASS: Python CLI sources generated with argparse."

    cd ../ || true
    end_time=$(date +%s)
    duration=$((end_time - start_time))
    echo "Test duration: $duration seconds"
}


