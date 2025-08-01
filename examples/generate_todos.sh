#!/bin/zsh

source ~/.zshrc

# Define the output file for the generated todos
TODO_FILE="todo.txt"

# Define the base prompt for ledit
BASE_PROMPT="Goal: Create a todo.txt with a robust set of todos to accomplish the following requirements. DO NOT COMPLETE the requirements, just outline the detailed steps needed to accomplish them. Use the current workspace information to help inform what needs to change to fulfil the requirements. There should be one todo per line and each line should be able to stand alone as a complete thought, providing all the information needed to accomplish the todo without relying on the context of other lines. REQUIREMENTS: "

# Check if requirements are provided as command-line arguments
if [ -z "$*" ]; then
    echo "Usage: $0 \"<requirements_for_todos>\""
    echo "Example: $0 \"Add user authentication and a database for user management\""
    echo "This script generates a todo.txt file based on the provided requirements using 'ledit'."
    exit 1
fi

# Capture all command-line arguments as the final requirements string
FINAL_REQUIREMENTS="$*"

# Combine the base prompt with the specific requirements
# The \n\n will be part of the string passed to ledit, which should interpret them as newlines.
FULL_LEDIT_PROMPT="${BASE_PROMPT}\n\n ${FINAL_REQUIREMENTS} #WS "

echo "Generating tasks for '${TODO_FILE}' based on requirements: '${FINAL_REQUIREMENTS}'"
echo "Calling ledit to generate content..."

# Execute ledit within a zsh shell to ensure ~/.zshrc is sourced.
# This is crucial if 'ledit' is a shell function or relies on zsh environment settings.
# The '--skip-prompt' flag tells ledit not to ask for initial confirmation.
# The 'echo "y" |' pipes 'y' to ledit's stdin, handling any potential interactive prompts.
# The output of ledit is redirected to the TODO_FILE.
ledit code \"${FULL_LEDIT_PROMPT}\" --skip-prompt -m gemini:gemini-2.5-pro

# Check the exit status of the ledit command
if [ $? -eq 0 ]; then
    echo "Tasks successfully generated and saved to '${TODO_FILE}'."
    # Optional: Verify if the generated file is not empty
    if [ ! -s "$TODO_FILE" ]; then
        echo "Warning: '${TODO_FILE}' was created but appears to be empty or contains only whitespace."
        echo "This might indicate an issue with the ledit generation or the prompt."
    fi
else
    echo "Error: Failed to generate tasks using ledit."
    echo "Please check if 'ledit' is installed and configured correctly in your zsh environment."
    exit 1
fi