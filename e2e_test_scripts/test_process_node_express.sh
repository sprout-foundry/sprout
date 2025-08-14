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

    PROMPT="Create a Node.js Express API with an endpoint GET /health returning {status:'ok'}. Include package.json, an entry server file, and a README with instructions to run. Use minimal dependencies."

    ../../ledit process "$PROMPT" --model "$model_name" --skip-prompt

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


