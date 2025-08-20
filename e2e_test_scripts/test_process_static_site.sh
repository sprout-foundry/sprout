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
  "goal": "Create a minimal static website with HTML, CSS, and JavaScript components",
  "description": "Single-agent static site generation with responsive design and interactive elements",
  "base_model": "",
  "agents": [{
    "id": "webdev",
    "name": "Frontend Web Developer",
    "persona": "frontend_developer",
    "description": "Creates modern static websites with HTML, CSS, and JavaScript",
    "skills": ["html", "css", "javascript", "web_development"],
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
    "id": "create_site",
    "name": "Create Static Website",
    "description": "Generate index.html with 'Hello, Web!' content, styles.css for styling, and script.js with console logging functionality",
    "agent_id": "webdev",
    "input": {},
    "expected_output": "Complete static website with all required files",
    "status": "pending",
    "depends_on": [],
    "timeout": 120,
    "retries": 2
  }],
  "validation": {
    "required": false,
    "custom_checks": ["node -e \"console.log('HTML validation passed')\""]
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


