#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "Validation Context Generation"
}

# Function to run the test logic
run_test_logic() {
    local model_name=$1 # Capture the model_name passed from test.sh
    echo "--- TEST: Validation Context Generation ---"

    # Test 1: Go project validation
    echo "Testing Go project validation..."
    orig_dir=$(pwd)
    TEST_DIR=$(mktemp -d)
    cd "$TEST_DIR"
    cat > go.mod <<EOF
module testproject
go 1.21
EOF
    cat > main.go <<EOF
package main
func main() {}
EOF

    # Initialize ledit and check validation
    if "${orig_dir}/ledit" init --skip-prompt; then
        if [ -f ".ledit/validation_context.md" ]; then
            echo "✓ Validation context generated for Go project"
            grep -q "go build" .ledit/validation_context.md || { echo "Missing go build"; exit 1; }
            grep -q "go test" .ledit/validation_context.md || { echo "Missing go test"; exit 1; }
        else
            echo "✗ Validation context not generated"
            exit 1
        fi
    else
        echo "✗ Failed to initialize ledit"
        exit 1
    fi

    # Test 2: Node.js project validation
    echo -e "\nTesting Node.js project validation..."
    TEST_DIR2=$(mktemp -d)
    cd "$TEST_DIR2"
    cat > package.json <<EOF
{
  "name": "test-project",
  "version": "1.0.0",
  "scripts": {
    "build": "tsc",
    "test": "jest",
    "lint": "eslint src",
    "typecheck": "tsc --noEmit"
  }
}
EOF

    # Run validate command
    if "${orig_dir}/ledit" validate; then
        if [ -f ".ledit/validation_context.md" ]; then
            echo "✓ Validation context generated for Node.js project"
            grep -q "npm run build" .ledit/validation_context.md || { echo "Missing npm run build"; exit 1; }
            grep -q "npm run lint" .ledit/validation_context.md || { echo "Missing npm run lint"; exit 1; }
            grep -q "npm run typecheck" .ledit/validation_context.md || { echo "Missing npm run typecheck"; exit 1; }
            grep -q "npm run test" .ledit/validation_context.md || { echo "Missing npm run test"; exit 1; }
        else
            echo "✗ Validation context not generated"
            exit 1
        fi
    else
        echo "✗ Failed to run validate command"
        exit 1
    fi

    # Test 3: Python project validation
    echo -e "\nTesting Python project validation..."
    TEST_DIR3=$(mktemp -d)
    cd "$TEST_DIR3"
    touch requirements.txt
    touch ruff.toml
    touch mypy.ini

    if "${orig_dir}/ledit" validate; then
        if [ -f ".ledit/validation_context.md" ]; then
            echo "✓ Validation context generated for Python project"
            grep -q "pytest" .ledit/validation_context.md || { echo "Missing pytest"; exit 1; }
            grep -q "ruff check" .ledit/validation_context.md || { echo "Missing ruff check"; exit 1; }
            grep -q "mypy" .ledit/validation_context.md || { echo "Missing mypy"; exit 1; }
        else
            echo "✗ Validation context not generated"
            exit 1
        fi
    else
        echo "✗ Failed to run validate command"
        exit 1
    fi

    # Cleanup
    rm -rf "$TEST_DIR" "$TEST_DIR2" "$TEST_DIR3"
    cd "$orig_dir"

    echo "✓ Validation Context Generation test passed"
}