#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "Enhanced Context Test"
}

# Function to run the test logic
run_test_logic() {
    local model_name=$1 # Capture the model_name passed in
    echo "--- TEST: Enhanced Context Test ---"

    # Create a dummy file with content for the AI to process
    local test_file="test_file_for_enhanced_context.txt"
    local summary_file="features_summary.md"
    echo "This is a test file containing important information about the project's core features: AI-powered code generation, automated code review, and self-correction loops." > "$test_file"

    # Run ledit to summarize the content of the dummy file
    output_log="enhanced_context_test.log"
    ../ledit agent "Summarize the key features mentioned in $test_file into a new file called $summary_file" --skip-prompt -m "$model_name" > "$output_log" 2>&1

    # Check if the summary file was created
    if [ ! -f "$summary_file" ]; then
        echo "FAIL: Summary file '$summary_file' was not created."
        cat "$output_log"
        return 1
    fi

    # Check if the summary file contains evidence of understanding the original content
    if grep -q "AI-powered code generation" "$summary_file" && \
       grep -q "automated code review" "$summary_file" && \
       grep -q "self-correction loops" "$summary_file"; then
        echo "PASS: Enhanced context was handled correctly. Summary file contains expected features."
    else
        echo "FAIL: Summary file '$summary_file' does not contain the expected summary of features."
        echo "--- Content of $summary_file ---"
        cat "$summary_file"
        echo "--- End of $summary_file content ---"
        cat "$output_log"
        return 1
    fi

    # Clean up
    rm -f "$test_file" "$summary_file" "$output_log"

    echo "----------------------------------------------------"
    echo
    return 0
}
#!/bin/bash

# Function to return the test name
get_test_name() {
    echo "Enhanced Context Test"
}

# Function to run the test logic
run_test_logic() {
    local model_name=$1 # Capture the model_name passed in
    echo "--- TEST: Enhanced Context Test ---"

    # Create a dummy file with content for the AI to process
    echo "This is a dummy file with some important information about the project.
    It describes the main goal: To develop an AI-powered code editing and assistance tool.
    Key features include: code generation, automated code review, and Git integration.
    The technical vision is a microservices architecture with cloud-native deployment." > dummy_project_info.txt

    # Simulate opening the file and requesting AI assistance
    # The AI should demonstrate understanding of the full file content
    output_log="enhanced_context_test.log"
    ../ledit agent "Summarize the key features and technical vision from dummy_project_info.txt into a new file called project_summary.md" --skip-prompt -m "$model_name" > "$output_log" 2>&1

    # Assert that the AI's response (in project_summary.md) contains information derived from the full file content
    # We expect the AI to have read the file and extracted the key features and technical vision.
    if grep -q "code generation" project_summary.md && \
       grep -q "automated code review" project_summary.md && \
       grep -q "Git integration" project_summary.md && \
       grep -q "microservices architecture" project_summary.md && \
       grep -q "cloud-native deployment" project_summary.md; then
        echo "PASS: AI demonstrated understanding of enhanced minimal context."
    else
        echo "FAIL: AI did not fully understand the enhanced minimal context."
        echo "--- Output Log ---"
        cat "$output_log"
        echo "--- project_summary.md ---"
        cat project_summary.md
        exit 1
    fi

    echo "----------------------------------------------------"
    echo
}
