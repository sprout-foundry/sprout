#!/bin/bash
set -e

echo "============================================"
echo "Testing CI/Non-Interactive Workflows"
echo "============================================"

# Set up test directory
TEST_DIR="/tmp/ledit_ci_test_$$"
mkdir -p "$TEST_DIR"
cd "$TEST_DIR"

# Initialize a git repo for testing
git init
echo "# Test Project" > README.md
git add README.md
git commit -m "Initial commit"

# Create some test files
cat > main.go << 'EOF'
package main

import "fmt"

func main() {
    fmt.Println("Hello, World!")
}

func add(a, b int) int {
    return a + b
}
EOF

cat > main_test.go << 'EOF'
package main

import "testing"

func TestAdd(t *testing.T) {
    result := add(2, 3)
    if result != 5 {
        t.Errorf("Expected 5, got %d", result)
    }
}
EOF

git add .
git commit -m "Add main.go and tests"

# Test 1: CI environment detection and non-interactive execution
echo -e "\n=== Test 1: CI Environment Detection ==="
export CI=1
export GITHUB_ACTIONS=1

# Test simple agent command in CI mode
echo -e "\n--- Testing simple agent command in CI ---"
ledit agent "What files are in this directory?" > ci_output.txt 2>&1

# Check for CI progress indicators
if grep -q "\[CI Progress\]" ci_output.txt; then
    echo "✅ CI progress indicators found"
else
    echo "❌ CI progress indicators missing"
    cat ci_output.txt
fi

# Check for token/cost output
if grep -q "Session:" ci_output.txt && grep -q "total" ci_output.txt && grep -q "\$" ci_output.txt; then
    echo "✅ Token and cost information displayed"
else
    echo "❌ Token and cost information missing"
    cat ci_output.txt
fi

# Test 2: Long-running operation with progress updates
echo -e "\n=== Test 2: Long-Running Operation Progress ==="
cat > complex_task.txt << 'EOF'
Please analyze this codebase and:
1. List all functions
2. Check for potential issues
3. Suggest improvements
4. Generate a summary report
EOF

# Run a more complex task that should trigger multiple progress updates
ledit agent "$(cat complex_task.txt)" > ci_long_output.txt 2>&1

# Count progress updates (should have multiple in a longer operation)
PROGRESS_COUNT=$(grep -c "\[CI Progress\]" ci_long_output.txt || true)
echo "Progress updates found: $PROGRESS_COUNT"

if [ "$PROGRESS_COUNT" -ge 1 ]; then
    echo "✅ Progress updates working in CI mode"
else
    echo "⚠️  Limited progress updates (may be due to fast execution)"
fi

# Test 3: Non-interactive mode without CI env vars
echo -e "\n=== Test 3: Non-Interactive Mode (No CI vars) ==="
unset CI
unset GITHUB_ACTIONS

# Test with piped input (non-interactive)
echo "List all Go files in this directory" | ledit agent > non_interactive_output.txt 2>&1

# Check output format
if grep -q "Agent Response:" non_interactive_output.txt || grep -q "main.go" non_interactive_output.txt; then
    echo "✅ Non-interactive mode working correctly"
else
    echo "❌ Non-interactive mode output issue"
    cat non_interactive_output.txt
fi

# Test 4: Output formatting in non-TTY environment
echo -e "\n=== Test 4: Non-TTY Output Formatting ==="
# Simulate non-TTY by using script command or redirecting all streams
ledit agent "Show me the contents of main.go" < /dev/null > non_tty_output.txt 2>&1

# Check that output doesn't contain ANSI escape codes or terminal control sequences
if grep -q $'\033\[' non_tty_output.txt; then
    echo "⚠️  ANSI escape codes found in non-TTY output"
    # Show first few instances
    grep -o $'\033\[[0-9;]*[mGKH]' non_tty_output.txt | head -5
else
    echo "✅ Clean output without terminal control sequences"
fi

# Test 5: Token and cost tracking accuracy
echo -e "\n=== Test 5: Token and Cost Tracking ==="
export CI=1

# Run multiple commands and check token accumulation
ledit agent "Count to 3" > count1.txt 2>&1
ledit agent "What is 2+2?" > count2.txt 2>&1
ledit agent "List files" > count3.txt 2>&1

# Extract token counts
for file in count*.txt; do
    echo "--- $file ---"
    grep -E "(Session:|total|tokens|\$)" "$file" || echo "No token info found"
done

# Test 6: Streaming output in CI mode
echo -e "\n=== Test 6: Streaming Output in CI ==="
# Test that streaming works properly and content appears
ledit agent "Write a haiku about coding" > streaming_output.txt 2>&1

if [ -s streaming_output.txt ]; then
    echo "✅ Streaming output captured"
    # Check for reasonable output
    LINE_COUNT=$(wc -l < streaming_output.txt)
    if [ "$LINE_COUNT" -ge 3 ]; then
        echo "✅ Multi-line output preserved"
    else
        echo "⚠️  Output seems truncated"
    fi
else
    echo "❌ No streaming output captured"
fi

# Test 7: Error handling in CI mode
echo -e "\n=== Test 7: Error Handling in CI ==="
# Test with invalid command or scenario that should produce an error
ledit agent "/nonexistentcommand" > error_output.txt 2>&1 || true

if grep -q -E "(Error|error|failed)" error_output.txt; then
    echo "✅ Error messages displayed properly"
else
    echo "⚠️  Error handling may need improvement"
fi

# Test 8: JSON output mode (if supported)
echo -e "\n=== Test 8: Structured Output Testing ==="
export LEDIT_OUTPUT_FORMAT=json
ledit agent "What is the current directory?" > json_output.txt 2>&1 || true
unset LEDIT_OUTPUT_FORMAT

# Check if JSON mode affected output
if grep -q "{" json_output.txt && grep -q "}" json_output.txt; then
    echo "✅ Structured output mode detected"
else
    echo "ℹ️  Standard output mode used"
fi

# Test 9: Concurrent execution safety
echo -e "\n=== Test 9: Concurrent Execution ==="
# Run multiple instances simultaneously
(ledit agent "Task 1" > concurrent1.txt 2>&1) &
PID1=$!
(ledit agent "Task 2" > concurrent2.txt 2>&1) &
PID2=$!
(ledit agent "Task 3" > concurrent3.txt 2>&1) &
PID3=$!

# Wait for all to complete
wait $PID1 $PID2 $PID3

# Check all completed successfully
for i in 1 2 3; do
    if [ -s "concurrent$i.txt" ]; then
        echo "✅ Concurrent task $i completed"
    else
        echo "❌ Concurrent task $i failed"
    fi
done

# Test 10: Signal handling
echo -e "\n=== Test 10: Signal Handling ==="
# Start a long-running task and send SIGTERM
(
    ledit agent "Count from 1 to 1000 slowly" > signal_output.txt 2>&1 &
    AGENT_PID=$!
    sleep 2
    kill -TERM $AGENT_PID 2>/dev/null || true
    wait $AGENT_PID 2>/dev/null || true
)

if [ -s signal_output.txt ]; then
    echo "✅ Graceful shutdown on SIGTERM"
else
    echo "⚠️  Signal handling needs verification"
fi

# Summary
echo -e "\n============================================"
echo "CI/Non-Interactive Workflow Test Summary"
echo "============================================"

# Clean up
cd /
rm -rf "$TEST_DIR"

echo -e "\nTest completed. Review output above for any issues."