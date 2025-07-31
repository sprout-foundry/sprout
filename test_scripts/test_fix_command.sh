#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "Fix Command"
}

# Function to run the test logic
run_test_logic() {
    local model_name=$1 # Capture the model_name passed from test.sh
    echo "--- TEST: Fix Command ---"

    # 1. Create a file with an error
    echo "Creating a Python script with a syntax error..."
    cat <<EOF > fixable_script.py
def main()
    print("Hello, World!")

if __name__ == "__main__":
    main()
EOF
    echo "Content of fixable_script.py:"
    cat fixable_script.py
    echo "-----------------------------"

    # 2. Run the command and confirm it fails
    echo "Running the broken script to confirm it fails..."
    python3 fixable_script.py > /dev/null 2>&1
    if [ $? -eq 0 ]; then
        echo "FAIL: The broken script ran successfully, which is unexpected."
        exit 1
    fi
    echo "PASS: The broken script failed as expected."

    # 3. Run ledit fix
    echo "Running 'ledit fix' to attempt to correct the script..."
    ../ledit fix "python3 fixable_script.py" --skip-prompt -m "$model_name"

    echo
    echo "--- Verifying Test ---"

    # 4. Check if the script was modified
    if ! grep -q "def main():" fixable_script.py; then
        echo "FAIL: The script was not fixed correctly. Missing colon."
        echo "--- Content of fixable_script.py after fix attempt: ---"
        cat fixable_script.py
        echo "--------------------------------------------------------"
        exit 1
    fi
    echo "PASS: The script appears to have been fixed (colon added)."

    # 5. Run the command again to confirm it passes
    echo "Running the script again to confirm it works..."
    output=$(python3 fixable_script.py)
    if [ $? -ne 0 ]; then
        echo "FAIL: The script still fails after the fix attempt."
        exit 1
    fi
    echo "PASS: The fixed script ran successfully."

    # 6. Check the output
    expected_output="Hello, World!"
    if [ "$output" != "$expected_output" ]; then
        echo "FAIL: The script output is not what was expected."
        echo "Expected: '$expected_output'"
        echo "Got: '$output'"
        exit 1
    fi
    echo "PASS: The script produced the correct output."

    echo "----------------------------------------------------"
    echo
}