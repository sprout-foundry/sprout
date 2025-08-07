#!/bin/zsh

# Source .zshrc to ensure ledit is available and configured,
# and any other necessary environment variables or functions.
source ~/.zshrc

# Global array to store meaningful tasks for the current iteration.
# Using 'typeset -ga' makes it a global array.
typeset -ga _current_tasks_to_process

# Function to check if todo.txt exists.
# Returns 0 if file exists, 1 otherwise.
check_todo_file_exists() {
    if [[ ! -f todo.txt ]]; then
        echo "Error: todo.txt not found in the current directory."
        echo "Please create a todo.txt file with one task per line."
        return 1
    fi
    return 0
}

# Function to load meaningful tasks from todo.txt into the global array _current_tasks_to_process.
# It clears the array, reads the file, and populates the array with non-empty/non-whitespace lines.
# Returns 0 if meaningful tasks are loaded, 1 if the file is empty or contains only whitespace.
load_meaningful_tasks() {
    _current_tasks_to_process=() # Clear the array for the new load
    local meaningful_line_found=false

    # Check if todo.txt is truly empty (0 bytes)
    if [[ ! -s todo.txt ]]; then
        echo "todo.txt is empty (0 bytes). No more tasks to process."
        return 1 # Indicate no meaningful tasks
    fi

    # Read todo.txt line by line
    # IFS= ensures leading/trailing whitespace is preserved
    # -r prevents backslash escapes from being interpreted
    while IFS= read -r line; do
        # If line is not empty or just any whitespace (correctly handles tabs, newlines, etc.)
        if [[ -n "${line//[[:space:]]/}" ]]; then
            _current_tasks_to_process+=("$line")
            meaningful_line_found=true
        fi
    done < todo.txt

    if [[ "$meaningful_line_found" = false ]]; then
        echo "todo.txt contains only empty or whitespace lines. No more tasks to process."
        return 1 # Indicate no meaningful tasks
    fi

    return 0 # Indicate success, _current_tasks_to_process is populated
}

# Main script logic starts here

# Check for an optional command-line argument
OPTIONAL_CHECK_COMMAND="$1"
if [[ -n "$OPTIONAL_CHECK_COMMAND" ]]; then
    echo "An optional check command was provided: '$OPTIONAL_CHECK_COMMAND'"
    echo "This command will be run after each 'ledit code' call."
    echo "If it fails, 'ledit fix' will be called to address the issue."
else
    echo "No optional check command provided. Script will proceed without post-change checks."
fi

# Perform initial check for todo.txt file existence
if ! check_todo_file_exists; then
    exit 1
fi

echo "Processing tasks from todo.txt..."

# Loop through the process up to three times, or until todo.txt is empty
for i in {1..3}; do
    echo "--- Processing Iteration $i of 3 ---"

    # Load tasks for the current iteration.
    # This ensures that each iteration operates on a fresh snapshot of todo.txt.
    if ! load_meaningful_tasks; then
        break # No meaningful tasks found, exit the loop
    fi

    # Process each task loaded in this iteration.
    # The loop iterates over the _current_tasks_to_process array, which is a snapshot
    # of the file at the beginning of this iteration.
    for task_line in "${_current_tasks_to_process[@]}"; do
        echo "Calling ledit for: \"$task_line\""
        # Call ledit with the content of the line
        # The --skip-prompt flag prevents ledit from asking for confirmation
        ledit code "Make the following change, or if it is already done, remove this request from the todo.txt file. Analyze dependencies and ensure the project remains in a working state after this change by updating dependent code Requested Change: '$task_line' #WS" --skip-prompt

        # If an optional check command was provided, run it
        if [[ -n "$OPTIONAL_CHECK_COMMAND" ]]; then
            echo "Running post-change check command: '$OPTIONAL_CHECK_COMMAND'"
            # Use eval to execute the command string
            eval "$OPTIONAL_CHECK_COMMAND"
            local check_status=$?

            if [[ "$check_status" -ne 0 ]]; then
                echo "Check command failed with exit status $check_status."
                echo "Calling ledit fix to address the issue caused by the previous change."
                ledit fix "$OPTIONAL_CHECK_COMMAND" --skip-prompt
            else
                echo "Check command succeeded."
            fi
        fi
    done

    echo "Iteration $i complete."
    # If ledit modifies todo.txt (e.g., removes a task), the next iteration
    # will call load_meaningful_tasks again and get the updated content.
done

echo "All tasks from todo.txt processed (or todo.txt became empty)."