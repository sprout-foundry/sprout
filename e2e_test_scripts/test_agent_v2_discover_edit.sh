#!/bin/bash

get_test_name() {
    echo "Agent v2 - Discover file and edit via workspace search"
}

run_test_logic() {
    local model_name=$1
    echo "--- TEST: Agent v2 - Discover file and edit via workspace search ---"
    start_time=$(date +%s)

    workdir="agent_v2_discover"
    rm -rf "$workdir"
    mkdir -p "$workdir"
    cd "$workdir" || exit 1

    cat > main.go << 'EOF'
package main

func main() {
    greet()
}

func greet() {
    println("hi")
}
EOF

    # Do not mention file name; ask to modify greet function
    ../../ledit agent --agent v2 -m "$model_name" --skip-prompt "At the beginning of the greet function, add a log line println(\"started\")."

    echo
    echo "--- Verifying Test ---"
    # Expect the greet function to include the new log line before the existing println("hi")
    awk '/func greet\(\)/{flag=1; next} /\}/{flag=0} flag {print}' main.go | grep -q 'println("started")'
    if [ $? -ne 0 ]; then
        echo "FAIL: Expected println(\"started\") in greet function"
        exit_code=1
    else
        echo "PASS: Agent discovered file and edited function"
        exit_code=0
    fi

    cd ../ || true
    end_time=$(date +%s)
    duration=$((end_time - start_time))
    echo "Test duration: $duration seconds"
    echo "agent_v2_discover,$duration,$((exit_code==0?1:0)),$((exit_code!=0?1:0))" >> e2e_results.csv
    exit $exit_code
}


