#!/bin/bash

get_test_name() {
    echo "Agent v2 - Full file edit (deterministic block)"
}

run_test_logic() {
    local model_name=$1
    echo "--- TEST: Agent v2 - Full file edit (deterministic block) ---"
    start_time=$(date +%s)

    workdir="agent_v2_full_edit"
    rm -rf "$workdir"
    mkdir -p "$workdir"
    cd "$workdir" || exit 1

    # Seed a text file
    printf "old content\n" > data.txt

    # Deterministic instruction for full replacement
    # We pass an explicit target file and direct replacement content
    expected=$'alpha\nbeta\ngamma\n'
    ../../ledit agent --agent v2 --skip-prompt "In $(pwd)/data.txt change 'old content' to 'alpha\nbeta\ngamma'"

    echo
    echo "--- Verifying Test ---"
    if diff -u <(cat data.txt) <(printf "%s" "$expected"); then
        echo "PASS: File content replaced exactly"
        exit_code=0
    else
        echo "FAIL: File content does not match expected"
        echo "----- actual -----"
        cat data.txt || true
        echo "------------------"
        exit_code=1
    fi

    cd ../ || true
    end_time=$(date +%s)
    duration=$((end_time - start_time))
    echo "Test duration: $duration seconds"
    echo "agent_v2_full_edit,$duration,$((exit_code==0?1:0)),$((exit_code!=0?1:0))" >> e2e_results.csv
    exit $exit_code
}


