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
  "goal": "Create a Python CLI application using argparse with command-line argument handling",
  "description": "Single-agent Python CLI implementation with proper argparse usage and documentation",
  "base_model": "",
  "agents": [{
    "id": "pydev",
    "name": "Python Developer",
    "persona": "backend_developer",
    "description": "Implements Python command-line applications and scripts",
    "skills": ["python", "argparse", "cli_development"],
    "model": "",
    "priority": 1,
    "depends_on": [],
    "config": {"skip_prompt": "true"},
    "budget": {
      "max_tokens": 60000,
      "max_cost": 3.0,
      "token_warning": 45000,
      "cost_warning": 2.5,
      "alert_on_limit": true,
      "stop_on_limit": false
    }
  }],
  "steps": [{
    "id": "create_cli",
    "name": "Create Python CLI Application",
    "description": "Create greet.py using argparse with --name flag and README.md with usage instructions",
    "agent_id": "pydev",
    "input": {},
    "expected_output": "Working Python CLI with argparse and documentation",
    "status": "pending",
    "depends_on": [],
    "timeout": 120,
    "retries": 2
  }],
  "validation": {
    "required": false,
    "test_command": "python -m py_compile greet.py",
    "custom_checks": ["python -c \"import ast; ast.parse(open('greet.py').read())\""]
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


