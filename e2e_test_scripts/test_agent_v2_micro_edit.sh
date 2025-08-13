#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "Agent v2 - Deterministic micro_edit"
}

# Function to run the test logic
run_test_logic() {
    local model_name=$1 # may be unused for deterministic path
    echo "--- TEST: Agent v2 - Deterministic micro_edit ---"
    start_time=$(date +%s)

    # Setup
    workdir="agent_v2_micro_edit"
    rm -rf "$workdir"
    mkdir -p "$workdir"
    cd "$workdir" || exit 1

    # Create a small Go file (language does not matter for simple replace)
    cat > file.go << 'EOF'
package tmp

func Hello() {
    println("foo")
}
EOF

    # Run agent v2 deterministic micro edit
    ../../ledit agent --agent v2 --skip-prompt "In $(pwd)/file.go change \"foo\" to \"bar\""

    echo
    echo "--- Verifying Test ---"
    if ! grep -q 'bar' file.go; then
        echo "FAIL: Expected replacement did not occur"
        exit_code=1
    else
        echo "PASS: Replacement applied"
        exit_code=0
    fi

    cd ../ || true
    end_time=$(date +%s)
    duration=$((end_time - start_time))
    echo "Test duration: $duration seconds"

    # Track results
    echo "agent_v2_micro_edit,$duration,$((exit_code==0?1:0)),$((exit_code!=0?1:0))" >> e2e_results.csv
    exit $exit_code
}


