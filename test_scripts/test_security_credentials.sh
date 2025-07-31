#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "Security Credentials Detection"
}

# Function to run the test logic
run_test_logic() {
    local model_name=$1 # Capture the model_name passed from test.sh
    echo "--- TEST: Security Credentials Detection ---"

    mkdir -p .ledit
    cat <<EOF > .ledit/config.json
{
    "editing_model": "lambda-ai:llama3.3-70b-instruct-fp8",
    "summary_model": "lambda-ai:hermes3-8b",
    "workspace_model": "lambda-ai:llama3.3-70b-instruct-fp8",
    "orchestration_model": "lambda-ai:llama3.3-70b-instruct-fp8",
    "local_model": "qwen2.5-coder:32b",
    "enable_security_checks": true,
    "track_with_git": false
}
EOF

    # 1. Create a file with simulated security credentials
    echo "Creating a file with dummy credentials..."
    cat <<EOF > secrets.txt
# This file contains sensitive information
API_KEY=sk-live-abcdef1234567890abcdef1234567890
DB_PASSWORD=superSecurePassword123!
AWS_SECRET_ACCESS_KEY=AKIAIOSFODNN7EXAMPLE
GITHUB_TOKEN=ghp_abcdefghijklmnopqrstuvwxyz0123456789ABCDEF
EOF
    echo "Content of secrets.txt:"
    cat secrets.txt
    echo "-----------------------------"

    # 2. Run ledit to analyze the workspace, which should detect the credentials
    echo "Running 'ledit code' to analyze the workspace for security concerns..."
    ../ledit code "Analyze the project for any sensitive information and update the workspace. #WORKSPACE" -m "$model_name" --skip-prompt

    echo
    echo "--- Verifying Test ---"

    # 3. Check .ledit/workspace.json for security concerns related to secrets.txt
    # We expect the workspace.json to contain a 'security_concerns' field for secrets.txt
    # This test assumes 'jq' is available in the test environment, as seen in other tests.

    if [ ! -f ".ledit/workspace.json" ]; then
        echo "FAIL: .ledit/workspace.json was not created."
        exit 1
    fi

    # Attempt to parse the JSON and check if 'secrets.txt' has a non-empty 'security_concerns' array
    if jq -e '.files."secrets.txt".security_concerns | length > 0' .ledit/workspace.json >/dev/null 2>&1; then
        echo "PASS: 'secrets.txt' has 'security_concerns' detected in workspace.json."
        
        # Check if exactly 4 security concerns were detected, as we expect 4 types of credentials.
        # We no longer check for specific types, as the LLM might not label them consistently.
        if jq -e '.files."secrets.txt".security_concerns | length == 4' .ledit/workspace.json >/dev/null 2>&1; then
            echo "PASS: Exactly 4 security concerns were detected for secrets.txt."
        else
            echo "FAIL: Expected 4 security concerns for secrets.txt, but a different number was detected or structure is unexpected."
            echo "Content of .ledit/workspace.json for secrets.txt:"
            jq '.files."secrets.txt"' .ledit/workspace.json
            exit 1
        fi
    else
        echo "FAIL: 'secrets.txt' did not show any 'security_concerns' in workspace.json."
        echo "Content of .ledit/workspace.json for secrets.txt:"
        jq '.files."secrets.txt"' .ledit/workspace.json
        exit 1
    fi

    # 4. Check the workspace log for the specific message indicating skipping summarization
    echo "Checking .ledit/workspace.log for security concern message..."
    if [ ! -f ".ledit/workspace.log" ]; then
        echo "FAIL: .ledit/workspace.log was not created."
        exit 1
    fi

    # The expected log message comes from prompts.SkippingLLMSummarizationDueToSecurity
    # which is "File %s contains confirmed security concerns. Skipping LLM summarization."
    if grep -q "File secrets.txt contains confirmed security concerns. Skipping LLM summarization." .ledit/workspace.log; then
        echo "PASS: Log message 'File secrets.txt contains confirmed security concerns. Skipping LLM summarization.' found in workspace.log."
    else
        echo "FAIL: Log message 'File secrets.txt contains confirmed security concerns. Skipping LLM summarization.' NOT found in workspace.log."
        echo "Content of .ledit/workspace.log:"
        cat .ledit/workspace.log
        exit 1
    fi

    echo "----------------------------------------------------"
    echo
}