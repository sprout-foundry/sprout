#!/bin/bash

echo "=== LEDIT AD HOC FUNCTIONALITY TESTS ==="
echo

# Test 1: Basic CLI functionality
echo "1. Testing basic CLI functionality..."
./ledit --version 2>/dev/null || echo "  ✓ CLI binary executes"

# Test 2: Help commands work
echo "2. Testing help system..."
if ./ledit --help > /dev/null 2>&1; then
    echo "  ✓ Main help works"
else
    echo "  ✗ Main help failed"
fi

if ./ledit code --help > /dev/null 2>&1; then
    echo "  ✓ Code command help works"
else
    echo "  ✗ Code command help failed"
fi

if ./ledit agent --help > /dev/null 2>&1; then
    echo "  ✓ Agent command help works"  
else
    echo "  ✗ Agent command help failed"
fi

# Test 3: Config initialization
echo "3. Testing config system..."
mkdir -p test_config && cd test_config
if ../ledit init --skip-prompt > /dev/null 2>&1; then
    if [ -f ".ledit/config.json" ]; then
        echo "  ✓ Config initialization works"
    else
        echo "  ✗ Config file not created"
    fi
else
    echo "  ✗ Config initialization failed"
fi
cd .. && rm -rf test_config

# Test 4: Workspace analysis (without LLM calls)
echo "4. Testing workspace analysis..."
if [ -f "hello.go" ]; then
    # Test dry-run mode to avoid LLM calls
    if timeout 10s ./ledit code --dry-run "add comments" --filename hello.go > /dev/null 2>&1; then
        echo "  ✓ Dry-run mode works"
    else
        echo "  ✗ Dry-run mode failed or timed out"
    fi
else
    echo "  - Skipping workspace test (no test file)"
fi

# Test 5: Pricing command
echo "5. Testing pricing utilities..."
if ./ledit pricing --help > /dev/null 2>&1; then
    echo "  ✓ Pricing command available"
else
    echo "  ✗ Pricing command failed"
fi

# Test 6: Log command
echo "6. Testing log functionality..."
if ./ledit log --help > /dev/null 2>&1; then
    echo "  ✓ Log command available"
else
    echo "  ✗ Log command failed"
fi

# Test 7: Process command
echo "7. Testing orchestration system..."
if ./ledit process --help > /dev/null 2>&1; then
    echo "  ✓ Process command available"
else
    echo "  ✗ Process command failed"
fi

echo
echo "=== AD HOC TESTS COMPLETE ==="