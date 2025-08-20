#!/bin/bash

get_test_name() {
    echo "Agent v2 - Mediocre prompt still edits"
}

run_test_logic() {
    local model_name=$1
    echo "--- TEST: Agent v2 - Mediocre prompt still edits ---"
    start_time=$(date +%s)

    workdir="agent_v2_mediocre"
    rm -rf "$workdir"
    mkdir -p "$workdir"
    cd "$workdir" || exit 1

    cat > greet.txt << 'EOF'
hello
EOF

    # A vague/mediocre prompt asking for more enthusiasm
    ../../ledit agent --skip-prompt "Make the first line of $(pwd)/greet.txt more enthusiastic"

    echo
    echo "--- Verifying Test ---"
    # Expect the first non-empty line to end with '!'
    head -n 1 greet.txt | grep -q '!$'
    if [ $? -ne 0 ]; then
        echo "FAIL: Expected the file to become more enthusiastic (end with '!')"
        exit_code=1
    else
        echo "PASS: Mediocre prompt led to an edit"
        exit_code=0
    fi

    cd ../ || true
    end_time=$(date +%s)
    duration=$((end_time - start_time))
    echo "Test duration: $duration seconds"
    echo "agent_v2_mediocre,$duration,$((exit_code==0?1:0)),$((exit_code!=0?1:0))" >> e2e_results.csv
    exit $exit_code
}


