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
    "editing_model": "deepinfra:deepseek-ai/DeepSeek-V3-0324",
    "summary_model": "deepinfra:mistralai/Mistral-Small-3.2-24B-Instruct-2506",
    "workspace_model": "deepinfra:meta-llama/Llama-3.3-70B-Instruct-Turbo",
    "orchestration_model": "deepinfra:Qwen/Qwen3-Coder-480B-A35B-Instruct",
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
    echo "Running 'ledit agent' to analyze the workspace for security concerns..."
    ../ledit agent "Analyze the project for any sensitive information and update the workspace. #WORKSPACE" -m "$model_name" --skip-prompt

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
    if grep -q "Skipped LLM summarization for secrets.txt due to security concerns" .ledit/workspace.log; then
        echo "PASS: 'Skipped LLM summarization for secrets.txt due to security concerns' found in workspace.log."
    else
        echo "FAIL: 'Skipped LLM summarization for secrets.txt due to security concerns' NOT found in workspace.log."
        echo "Content of .ledit/workspace.log:"
        cat .ledit/workspace.log
        exit 1
    fi
    
    echo "Test passed: Security credentials were detected and logged correctly, and LLM summarization was skipped."
    echo "----------------------------------------------------"
    echo
}