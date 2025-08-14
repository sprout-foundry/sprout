#!/bin/bash

get_test_name() {
    echo "Process - Static Website"
}

run_test_logic() {
    local model_name=$1
    echo "--- TEST: Process - Static Website ---"
    start_time=$(date +%s)

    workdir="process_static_site"
    rm -rf "$workdir"
    mkdir -p "$workdir"
    cd "$workdir" || exit 1

    cat > process.json << 'JSON'
{
  "version": "1.0",
  "goal": "Generate minimal static site",
  "description": "Index + CSS + JS",
  "agents": [{
    "id": "web",
    "name": "Web Dev",
    "persona": "frontend_developer",
    "description": "Creates static sites",
    "skills": ["html", "css", "js"],
    "model": "",
    "priority": 1,
    "depends_on": [],
    "config": {"skip_prompt": "true"}
  }],
  "steps": [{
    "id": "init",
    "name": "Init site",
    "description": "Create index.html with 'Hello, Web!', styles.css, script.js logging to console",
    "agent_id": "web",
    "input": {},
    "expected_output": "index.html, styles.css, script.js exist",
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

    # Core files
    for f in index.html styles.css script.js; do
        if [ ! -f "$f" ]; then
            echo "FAIL: Missing $f"
            exit 1
        fi
    done
    if ! grep -q "Hello, Web!" index.html; then
        echo "FAIL: index.html does not contain the expected text."
        exit 1
    fi
    echo "PASS: Static site files generated and contain expected content."

    cd ../ || true
    end_time=$(date +%s)
    duration=$((end_time - start_time))
    echo "Test duration: $duration seconds"
}


