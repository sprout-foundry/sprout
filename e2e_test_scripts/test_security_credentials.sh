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
API_KEY=sk-live-ghsidofnaregaisdohswq18r9r83r8wioshd
DB_PASSWORD=gionsdfigwebnx!
AWS_SECRET_ACCESS_KEY=QIGSDIONDINGIEFN
GITHUB_TOKEN=ghp_ghsiroegisodhaeq223902713rinqefiy3hr039r
EOF
    echo "Content of secrets.txt:"
    cat secrets.txt
    echo "-----------------------------"

    # 2. Run ledit to analyze the workspace, which should detect the credentials
    echo "Running 'ledit code' to analyze the workspace for security concerns..."
    ../ledit code "Analyze the project for any sensitive information and update the workspace. #WORKSPACE" -m "$model_name" --skip-prompt

    echo
    echo "--- Verifying Test ---"

    # 3. Check .ledit/workspace.log for security concerns related to secrets.txt
    if [ ! -f ".ledit/workspace.log" ]; then
        echo "FAIL: .ledit/workspace.log was not created."
        exit 1
    fi

    # check the workspace.log for "API Key Exposure"
    if grep -q "API Key Exposure" .ledit/workspace.log; then
        echo "PASS: 'API Key Exposure' found in workspace.log."
    else
        echo "FAIL: 'API Key Exposure' NOT found in workspace.log."
        echo "Content of .ledit/workspace.log:"
        cat .ledit/workspace.log
        exit 1
    fi

    # Check for the specific message about skipping LLM summarization due to security concerns
    # This message is defined in pkg/prompts/messages.go: SkippingLLMSummarizationDueToSecurity
    if grep -q "Skipping LLM summarization for 'secrets.txt' due to detected security concerns and lack of confirmation." .ledit/workspace.log; then
        echo "PASS: 'Skipping LLM summarization for 'secrets.txt' due to detected security concerns and lack of confirmation.' found in workspace.log."
    else
        echo "FAIL: 'Skipping LLM summarization for 'secrets.txt' due to detected security concerns and lack of confirmation.' NOT found in workspace.log."
        echo "Content of .ledit/workspace.log:"
        cat .ledit/workspace.log
        exit 1
    fi
    
    echo "Test passed: Security credentials were detected and logged correctly, and LLM summarization was skipped."
    echo "----------------------------------------------------"
    echo
}