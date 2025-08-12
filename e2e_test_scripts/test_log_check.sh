#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "Check the log"
}

# Function to run the test logic
run_test_logic() {
    local model_name=$1 # Capture the model_name passed from test.sh
    echo "--- TEST: Check the log ---"
    ../ledit log
    echo "----------------------------------------------------"
    echo
}