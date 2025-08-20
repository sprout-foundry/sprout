#!/bin/bash

get_test_name() {
    echo "Agent v2 - Code smoke (minimal hunk replacement)"
}

run_test_logic() {
    local model_name=$1
    echo "--- TEST: Agent v2 - Code smoke (minimal hunk replacement) ---"
    start_time=$(date +%s)

    workdir="agent_v2_code_smoke"
    rm -rf "$workdir"
    mkdir -p "$workdir"
    cd "$workdir" || exit 1

    # Seed file with deterministic content
    cat > sample.txt << 'EOF'
This is a code-edit smoke test file.
Target: AAA_MARKER
Keep this line unchanged.
EOF

    # Run agent to replace marker via minimal change
    ../../ledit agent -m "$model_name" --skip-prompt "In $(pwd)/sample.txt, replace 'AAA_MARKER' with 'BBB_MARKER'"

    echo
    echo "--- Verifying Test ---"
    if grep -q 'BBB_MARKER' sample.txt; then
        echo "PASS: File content updated to BBB_MARKER"
        exit_code=0
    else
        echo "FAIL: Expected replacement did not occur"
        echo "----- sample.txt -----"
        cat sample.txt || true
        echo "----------------------"
        exit_code=1
    fi

    cd ../ || true
    end_time=$(date +%s)
    duration=$((end_time - start_time))
    echo "Test duration: $duration seconds"
    echo "agent_v2_code_smoke,$duration,$((exit_code==0?1:0)),$((exit_code!=0?1:0))" >> e2e_results.csv
    exit $exit_code
}
