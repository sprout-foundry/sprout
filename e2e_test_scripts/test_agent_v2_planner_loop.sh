#!/bin/bash

get_test_name() {
    echo "Agent v2 - Planner→Executor→Evaluator loop"
}

run_test_logic() {
    local model_name=$1
    echo "--- TEST: Agent v2 - Planner→Executor→Evaluator loop ---"
    start_time=$(date +%s)

    workdir="agent_v2_loop"
    rm -rf "$workdir"
    mkdir -p "$workdir"
    cd "$workdir" || exit 1

    # Create a file and ask for a tiny change without header keywords to force the loop
    cat > data.txt << 'EOF'
alpha
beta
gamma
EOF

    ../../ledit agent --agent v2 --skip-prompt "Append the word delta to the end of $(pwd)/data.txt"

    echo
    echo "--- Verifying Test ---"
    tail -n 1 data.txt | grep -q 'delta'
    if [ $? -ne 0 ]; then
        echo "FAIL: Expected planner/executor to append 'delta'"
        exit_code=1
    else
        echo "PASS: Planner loop executed an edit successfully"
        exit_code=0
    fi

    cd ../ || true
    end_time=$(date +%s)
    duration=$((end_time - start_time))
    echo "Test duration: $duration seconds"
    echo "agent_v2_loop,$duration,$((exit_code==0?1:0)),$((exit_code!=0?1:0))" >> e2e_results.csv
    exit $exit_code
}


