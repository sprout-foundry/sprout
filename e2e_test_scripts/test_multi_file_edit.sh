#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "Multi-file Edit & Selective Context"
}

# Function to run the test logic
run_test_logic() {
    local model_name=$1 # Capture the model_name passed from test.sh
    echo "--- TEST: Multi-file Edit & Selective Context ---"
    # This test creates two related files and asks for an edit that requires understanding both.
    echo "def get_greeting():
        return 'Hello'" > greeter.py
    echo "from greeter import get_greeting

    print(get_greeting() + ' from the main script!')" > main.py
    echo "Created greeter.py and main.py"

    # Store the original content of the files we expect to be changed.
    original_greeter_content=$(cat greeter.py)
    original_main_content=$(cat main.py)

    # Run ledit to analyze these new files and perform an edit that requires context from both.
    ../ledit code "Refactor the code. In greeter.py, rename 'get_greeting' to 'create_salutation' and make it accept a 'name' argument. In main.py, update the call to use the new function and pass in the name 'World'. #WORKSPACE" --skip-prompt -m "$model_name"

    echo
    echo "--- Verifying Test ---"
    # Check if greeter.py was modified
    new_greeter_content=$(cat greeter.py)
    if [ "$original_greeter_content" == "$new_greeter_content" ]; then
        echo "FAIL: greeter.py was not modified."
        cat greeter.py
        exit 1
    fi
    echo "PASS: greeter.py was modified."

    # Check if main.py was modified
    new_main_content=$(cat main.py)
    if [ "$original_main_content" == "$new_main_content" ]; then
        echo "FAIL: main.py was not modified."
        cat main.py
        exit 1
    fi
    echo "PASS: main.py was modified."

    # Run the new script to confirm it's valid Python and runs without errors.
    echo "--- Running the refactored python code: ---"
    if python3 main.py > /dev/null; then
        echo "PASS: The refactored python script ran successfully."
    else
        echo "FAIL: The refactored python script failed to run."
        exit 1
    fi
    echo "-------------------------------------------"

    # Check that the new files are in the workspace
    grep -q "greeter.py" .ledit/workspace.json && echo "PASS: greeter.py added to workspace.json" || (echo "FAIL: greeter.py not in workspace.json"; exit 1)
    grep -q "main.py" .ledit/workspace.json && echo "PASS: main.py added to workspace.json" || (echo "FAIL: main.py not in workspace.json"; exit 1)
    echo "----------------------------------------------------"
    echo
}