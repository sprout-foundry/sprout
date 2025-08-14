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

    PROMPT="Create a Rust library crate named utils with a function greet(name: &str) -> String returning 'Hello, {name}!'. Include Cargo.toml, src/lib.rs, and a README."

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


