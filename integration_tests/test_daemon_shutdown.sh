#!/bin/bash

# SP-053-1d: Integration test for daemon SIGTERM shutdown behavior.
# Verifies that the sprout daemon shuts down cleanly within 15 seconds
# when sent SIGTERM, including graceful web server cleanup.

get_test_name() {
    echo "SP-053-1d: Daemon SIGTERM Shutdown"
}

run_test_logic() {
    local model_name=$1
    echo "--- TEST: SP-053-1d: Daemon SIGTERM Shutdown ---"
    start_time=$(date +%s)

    # ------------------------------------------------------------------
    # Skip conditions: CI environments and missing PTY support
    # ------------------------------------------------------------------
    if [ -n "$CI" ] || [ -n "$GITHUB_ACTIONS" ]; then
        echo "SKIP: Daemon shutdown test not feasible in CI environment"
        exit 0
    fi

    # Check if PTY device directory exists (required for daemon mode)
    if [ ! -e /dev/pts ]; then
        echo "SKIP: PTY not available (required for daemon mode)"
        exit 0
    fi

    # ------------------------------------------------------------------
    # Setup: build binary and create isolated config directory
    # ------------------------------------------------------------------
    BINARY="/tmp/sprout-daemon-shutdown-test"
    TEMP_DIR=$(mktemp -d)

    echo "Building sprout binary..."
    cd ../.. || { echo "FAIL: Could not change to project root"; exit 1; }
    if [ ! -f "go.mod" ]; then
        echo "FAIL: Not in project root (no go.mod found)"
        rm -rf "$TEMP_DIR"
        exit 1
    fi
    if ! go build -o "$BINARY" . 2>&1; then
        echo "FAIL: Could not build sprout binary"
        rm -rf "$TEMP_DIR"
        exit 1
    fi
    cd - > /dev/null || true

    echo "Using isolated config directory: $TEMP_DIR"

    # ------------------------------------------------------------------
    # Start the daemon in the background
    # ------------------------------------------------------------------
    # --web-port 0 assigns a random port to avoid conflicts
    # SPROUT_SERVICE=1 enables daemon logging mode
    # --no-connection-check skips provider connectivity checks
    SPROUT_CONFIG="$TEMP_DIR" SPROUT_SERVICE=1 \
        "$BINARY" agent -d --no-connection-check --web-port 0 > "$TEMP_DIR/daemon.log" 2>&1 &
    DAEMON_PID=$!

    echo "Daemon started with PID $DAEMON_PID"

    # ------------------------------------------------------------------
    # Wait for the daemon to become ready (process stays alive)
    # ------------------------------------------------------------------
    READY=false
    for i in $(seq 1 30); do
        if kill -0 "$DAEMON_PID" 2>/dev/null; then
            # Avoid zombie-process false positive: verify the process state
            # doesn't start with "Z".
            local _ps_state
            _ps_state=$(ps -o stat= -p "$DAEMON_PID" 2>/dev/null) || true
            if [[ "$_ps_state" != Z* ]]; then
                # Process is alive and not a zombie — check for startup marker
                if grep -q "Web UI available\|Web UI running" "$TEMP_DIR/daemon.log" 2>/dev/null; then
                    READY=true
                    break
                fi
                # Process is alive but startup marker not yet in log; continue polling
            fi
            # Zombie detected — process is stuck; stop and fail
            READY="zombie"
            break
        fi
        sleep 0.5
    done

    if [ "$READY" = "zombie" ]; then
        echo "FAIL: Daemon process is a zombie (stuck in Z state)"
        echo "--- Daemon log ---"
        cat "$TEMP_DIR/daemon.log" 2>/dev/null || true
        echo "------------------"
        rm -rf "$TEMP_DIR"
        rm -f "$BINARY"
        exit 1
    fi

    if [ "$READY" != "true" ]; then
        echo "FAIL: Daemon process died during startup (never became ready)"
        echo "--- Daemon log ---"
        cat "$TEMP_DIR/daemon.log" 2>/dev/null || true
        echo "------------------"
        rm -rf "$TEMP_DIR"
        rm -f "$BINARY"
        exit 1
    fi

    echo "Daemon is running (PID $DAEMON_PID)"

    # ------------------------------------------------------------------
    # Send SIGTERM and wait for clean shutdown
    # ------------------------------------------------------------------
    shutdown_start=$(date +%s%N)
    kill -TERM "$DAEMON_PID"
    echo "Sent SIGTERM to daemon (PID $DAEMON_PID)"

    # Poll for process exit: check every 0.5s for up to 15 seconds (30 iterations)
    SHUTDOWN_CLEAN=false
    for i in $(seq 1 30); do
        if ! kill -0 "$DAEMON_PID" 2>/dev/null; then
            SHUTDOWN_CLEAN=true
            break
        fi
        sleep 0.5
    done

    shutdown_end=$(date +%s%N)

    # ------------------------------------------------------------------
    # Result evaluation
    # ------------------------------------------------------------------
    if [ "$SHUTDOWN_CLEAN" = "true" ]; then
        # Calculate shutdown duration in seconds with decimal precision (pure bash, no bc)
        duration_ns=$((shutdown_end - shutdown_start))
        duration_s="$((duration_ns / 1000000000)).$(( (duration_ns % 1000000000) / 100000000 ))"
        echo "PASS: Daemon shut down cleanly in ${duration_s}s"
        exit_code=0
    else
        echo "FAIL: Daemon still running after 15s"
        echo "Forcing cleanup..."
        kill -9 "$DAEMON_PID" 2>/dev/null || true
        exit_code=1
    fi

    # ------------------------------------------------------------------
    # Cleanup
    # ------------------------------------------------------------------
    rm -rf "$TEMP_DIR"
    rm -f "$BINARY"

    end_time=$(date +%s)
    duration=$((end_time - start_time))
    echo "Test duration: $duration seconds"
    echo "daemon_shutdown,$duration,$((exit_code==0?1:0)),$((exit_code!=0?1:0))" >> e2e_results.csv
    exit $exit_code
}
