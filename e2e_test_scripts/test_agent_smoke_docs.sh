#!/bin/bash

get_test_name() {
    echo "Agent v2 - Docs smoke (README usage insertion)"
}

run_test_logic() {
    local model_name=$1
    echo "--- TEST: Agent v2 - Docs smoke (README usage insertion) ---"
    start_time=$(date +%s)

    workdir="agent_v2_docs_smoke"
    rm -rf "$workdir"
    mkdir -p "$workdir"
    cd "$workdir" || exit 1

    # Seed a minimal README without usage
    cat > README.md << 'EOF'
# Test Repo

This is a minimal README used for docs smoke testing.
It currently lacks usage for ledit agent.
EOF

    # Run the agent to add usage; keep prompt simple and deterministic
    ../../ledit agent --agent v2 -m "$model_name" --skip-prompt "Update README: add usage for ledit agent"

    echo
    echo "--- Verifying Test ---"
    if grep -qi 'ledit agent' README.md; then
        echo "PASS: README updated with usage"
        exit_code=0
    else
        echo "FAIL: README was not updated with usage"
        echo "----- README -----"
        cat README.md || true
        echo "------------------"
        exit_code=1
    fi

    cd ../ || true
    end_time=$(date +%s)
    duration=$((end_time - start_time))
    echo "Test duration: $duration seconds"
    echo "agent_v2_docs_smoke,$duration,$((exit_code==0?1:0)),$((exit_code!=0?1:0))" >> e2e_results.csv
    exit $exit_code
}
