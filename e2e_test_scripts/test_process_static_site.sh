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

    PROMPT="Generate a minimal static website with index.html, styles.css, and script.js. The page should show 'Hello, Web!' in the body and log a console message. Include a short README."

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


