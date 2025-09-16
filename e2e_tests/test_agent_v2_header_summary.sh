#!/bin/bash

get_test_name() {
    echo "Agent v2 - LLM header summary"
}

run_test_logic() {
    local model_name=$1
    echo "--- TEST: Agent v2 - LLM header summary ---"
    start_time=$(date +%s)

    workdir="agent_v2_header"
    rm -rf "$workdir"
    mkdir -p "$workdir"
    cd "$workdir" || exit 1

    # Create a simple file
    cat > sample.ts << 'EOF'
export function add(a: number, b: number): number {
  return a + b;
}
EOF

    # Request a file header summary
    ../../ledit agent --skip-prompt "Add a file header summary to $(pwd)/sample.ts"

    echo
    echo "--- Verifying Test ---"
    # Check first line now begins with a comment style for TS (// or /** */ block)
    head -n 1 sample.ts | grep -E '^(//|/\*)' >/dev/null 2>&1
    if [ $? -ne 0 ]; then
        echo "FAIL: Expected a header comment to be inserted at the top of sample.ts"
        exit_code=1
    else
        echo "PASS: Header comment inserted"
        exit_code=0
    fi

    cd ../ || true
    end_time=$(date +%s)
    duration=$((end_time - start_time))
    echo "Test duration: $duration seconds"
    echo "agent_v2_header,$duration,$((exit_code==0?1:0)),$((exit_code!=0?1:0))" >> e2e_results.csv
    exit $exit_code
}


