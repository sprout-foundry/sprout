#!/usr/bin/env python3

import argparse
import subprocess
import os
import sys
import json
import time
import re

# ANSI escape codes for colors
RED = "\033[91m"
GREEN = "\033[92m"
YELLOW = "\033[93m"
BLUE = "\033[94m"
MAGENTA = "\033[95m"
CYAN = "\033[96m"
RESET = "\033[0m"
BOLD = "\033[1m"
DIM = "\033[2m"

def print_color(text, color):
    print(f"{color}{text}{RESET}")

def run_command(command, cwd=None, shell=False, capture_output=True, text=True, check=True, timeout=None):
    """
    Runs a shell command and returns its output.
    """
    print_color(f"Running command: {command}", BLUE)
    try:
        result = subprocess.run(
            command,
            cwd=cwd,
            shell=shell,
            capture_output=capture_output,
            text=text,
            check=check,
            timeout=timeout
        )
        if capture_output:
            if result.stdout:
                print_color("STDOUT:", DIM)
                print(result.stdout)
            if result.stderr:
                print_color("STDERR:", RED)
                print(result.stderr)
        return result
    except subprocess.CalledProcessError as e:
        print_color(f"Command failed with exit code {e.returncode}", RED)
        if e.stdout:
            print_color("STDOUT:", DIM)
            print(e.stdout)
        if e.stderr:
            print_color("STDERR:", RED)
            print(e.stderr)
        raise
    except subprocess.TimeoutExpired as e:
        print_color(f"Command timed out after {timeout} seconds", RED)
        if e.stdout:
            print_color("STDOUT:", DIM)
            print(e.stdout)
        if e.stderr:
            print_color("STDERR:", RED)
            print(e.stderr)
        raise

def setup_test_environment(test_dir):
    """
    Sets up a temporary test environment.
    """
    print_color(f"Setting up test environment in {test_dir}...", YELLOW)
    os.makedirs(test_dir, exist_ok=True)
    os.chdir(test_dir)
    run_command(["git", "init"], check=False)
    run_command(["git", "config", "user.name", "Test User"])
    run_command(["git", "config", "user.email", "test@example.com"])
    # Create a dummy file and commit it to have a clean state
    with open("dummy.txt", "w") as f:
        f.write("initial content")
    run_command(["git", "add", "dummy.txt"])
    run_command(["git", "commit", "-m", "Initial commit"])
    print_color("Test environment setup complete.", GREEN)

def cleanup_test_environment(original_cwd):
    """
    Cleans up the temporary test environment.
    """
    print_color("Cleaning up test environment...", YELLOW)
    os.chdir(original_cwd)
    # Remove the test directory
    try:
        subprocess.run(["rm", "-rf", "test_env"], check=True)
        print_color("Test environment cleaned up.", GREEN)
    except subprocess.CalledProcessError as e:
        print_color(f"Error during cleanup: {e}", RED)

def run_test(name, test_func, *args, **kwargs):
    """
    Runs a single test function and reports its status.
    """
    print_color(f"\n--- Running Test: {name} ---", CYAN)
    start_time = time.time()
    try:
        test_func(*args, **kwargs)
        end_time = time.time()
        print_color(f"--- Test '{name}' PASSED in {end_time - start_time:.2f} seconds ---", GREEN)
        return True
    except Exception as e:
        end_time = time.time()
        print_color(f"--- Test '{name}' FAILED in {end_time - start_time:.2f} seconds ---", RED)
        print_color(f"Error: {e}", RED)
        return False

def test_code_command(model_name):
    """
    Tests the 'ledit code' command.
    """
    file_name = "test_code.go"
    initial_content = """package main

import "fmt"

func main() {
    fmt.Println("Hello, Ledit!")
}
"""
    with open(file_name, "w") as f:
        f.write(initial_content)

    # Use the 'code' command to add a new function
    instructions = "Add a function called 'greet' that takes a name (string) and prints 'Hello, [name]!'."
    run_command([
        "../ledit", "code", instructions,
        "-f", file_name,
        "-m", model_name,
        "--skip-prompt"
    ])

    # Verify the file content
    with open(file_name, "r") as f:
        content = f.read()
        assert "func greet(name string)" in content, "greet function not added"
        assert "fmt.Println(\"Hello, \" + name + \"!\")" in content, "greet function content incorrect"
        print_color(f"File '{file_name}' updated successfully with new function.", GREEN)

    # Verify git status
    git_status = run_command(["git", "status", "--porcelain"]).stdout
    assert " M " in git_status or "A  " in git_status, "Changes not staged or committed by ledit"
    print_color("Git status shows changes.", GREEN)

    # Verify commit message
    git_log = run_command(["git", "log", "-1", "--pretty=%B"]).stdout
    assert "Add test_code.go - Add greet function" in git_log or "Update test_code.go - Add greet function" in git_log, "Commit message not as expected"
    print_color("Commit message verified.", GREEN)

def test_fix_command(model_name):
    """
    Tests the 'ledit fix' command.
    """
    file_name = "test_fix.py"
    initial_content = """def divide(a, b):
    return a / b

print(divide(10, 0)) # This will cause a ZeroDivisionError
"""
    with open(file_name, "w") as f:
        f.write(initial_content)

    # Run the 'fix' command on the Python script
    # We expect ledit to fix the ZeroDivisionError
    run_command([
        "../ledit", "fix",
        "-f", file_name,
        "-m", model_name,
        "--skip-prompt",
        "--", "python3", file_name # Command to run and fix
    ])

    # Verify the file content
    with open(file_name, "r") as f:
        content = f.read()
        assert "try" in content and "except ZeroDivisionError" in content, "Error handling not added"
        print_color(f"File '{file_name}' fixed successfully with error handling.", GREEN)

def test_commit_command():
    """
    Tests the 'ledit commit' command.
    """
    file_name = "test_commit.txt"
    with open(file_name, "w") as f:
        f.write("This is a new file for commit test.")
    run_command(["git", "add", file_name])

    # Use the 'commit' command to generate a commit message
    run_command(["../ledit", "commit", "--skip-prompt"])

    # Verify the commit message
    git_log = run_command(["git", "log", "-1", "--pretty=%B"]).stdout
    assert "Add test_commit.txt" in git_log, "Commit message not generated or incorrect"
    print_color("Commit command verified.", GREEN)

def test_log_command():
    """
    Tests the 'ledit log' command.
    """
    # Ensure there's at least one ledit-generated change
    file_name = "test_log.txt"
    with open(file_name, "w") as f:
        f.write("Content for log test.")
    run_command(["../ledit", "code", "Add content to test_log.txt", "-f", file_name, "--skip-prompt"])

    # Run the 'log' command
    result = run_command(["../ledit", "log", "--buffer"])
    assert "Revision ID:" in result.stdout, "Log command did not show revision history"
    assert "File Changes (1):" in result.stdout, "Log command did not show file changes"
    print_color("Log command verified.", GREEN)

def test_process_command(model_name):
    """
    Tests the 'ledit process' command for orchestration.
    """
    # Create a dummy workspace.json for the test
    workspace_content = {
        "build_command": "echo 'Build successful!'",
        "files": {}
    }
    os.makedirs(".ledit", exist_ok=True)
    with open(".ledit/workspace.json", "w") as f:
        json.dump(workspace_content, f)

    # Define a simple process instruction
    process_instruction = "Create a new file named 'orchestrated.txt' with the content 'This file was created by ledit process command.'"

    # Run the 'process' command
    run_command([
        "../ledit", "process", process_instruction,
        "-m", model_name,
        "--skip-prompt"
    ])

    # Verify the new file was created
    assert os.path.exists("orchestrated.txt"), "orchestrated.txt was not created by process command"
    with open("orchestrated.txt", "r") as f:
        content = f.read()
        assert "This file was created by ledit process command." in content, "orchestrated.txt content is incorrect"
    print_color("Process command verified: file created and content checked.", GREEN)

def test_review_staged_command(model_name):
    """
    Tests the 'ledit review-staged' command.
    """
    file_name = "test_review_staged.go"
    initial_content = """package main

import "fmt"

func main() {
    fmt.Println("Hello, Ledit!")
}
"""
    with open(file_name, "w") as f:
        f.write(initial_content)
    run_command(["git", "add", file_name])
    run_command(["git", "commit", "-m", "Add initial test_review_staged.go"])

    # Modify the file and stage the changes
    modified_content = """package main

import "fmt"

func main() {
    fmt.Println("Hello, Ledit! This is an update.")
    // A new comment
}
"""
    with open(file_name, "w") as f:
        f.write(modified_content)
    run_command(["git", "add", file_name])

    # Run the 'review-staged' command
    result = run_command([
        "../ledit", "review-staged",
        "-m", model_name
    ])

    # Verify the output contains review information
    assert "--- LLM Code Review Result ---" in result.stdout, "Review result header not found"
    assert "Status:" in result.stdout, "Review status not found"
    assert "Feedback:" in result.stdout, "Review feedback not found"
    print_color("Review-staged command verified: review output found.", GREEN)


def main():
    parser = argparse.ArgumentParser(
        description="Run ledit integration tests.",
        formatter_class=argparse.RawTextHelpFormatter,
        epilog="""Examples:
  Run all tests with a specific model:
    python3 test.py -m openai:gpt-4o

  Run only the 'code' test:
    python3 test.py --test code -m ollama:llama3

  Run tests without cleaning up the environment (for debugging):
    python3 test.py --no-cleanup -m openai:gpt-4o
    """
    )
    parser.add_argument(
        '-m', '--model',
        default='deepinfra:Qwen/Qwen2.5-Coder-32B-Instruct',
        help='Specify the model name to use for tests (default: deepinfra:Qwen/Qwen2.5-Coder-32B-Instruct).'
    )
    parser.add_argument(
        '--test',
        help='Run only a specific test (e.g., "code", "fix", "commit", "log", "process", "review_staged").'
    )
    parser.add_argument(
        '--no-cleanup',
        action='store_true',
        help='Do not clean up the test environment after running tests.'
    )
    args = parser.parse_args()

    original_cwd = os.getcwd()
    test_dir = os.path.join(original_cwd, "test_env")

    # Build the ledit binary
    print_color("Building ledit binary...", YELLOW)
    try:
        run_command(["go", "build", "-o", "ledit", "../main.go"], cwd=original_cwd)
        print_color("Ledit binary built successfully.", GREEN)
    except Exception as e:
        print_color(f"Failed to build ledit: {e}", RED)
        sys.exit(1)

    setup_test_environment(test_dir)

    tests = {
        "code": test_code_command,
        "fix": test_fix_command,
        "commit": test_commit_command,
        "log": test_log_command,
        "process": test_process_command,
        "review_staged": test_review_staged_command,
    }

    results = {}
    if args.test:
        if args.test in tests:
            results[args.test] = run_test(args.test, tests[args.test], args.model)
        else:
            print_color(f"Error: Test '{args.test}' not found.", RED)
            sys.exit(1)
    else:
        for name, func in tests.items():
            # Pass model_name only to tests that require it
            if name in ["code", "fix", "process", "review_staged"]:
                results[name] = run_test(name, func, args.model)
            else:
                results[name] = run_test(name, func)

    print_color("\n--- Test Summary ---", MAGENTA)
    all_passed = True
    for test_name, passed in results.items():
        if passed:
            print_color(f"Test '{test_name}': PASSED", GREEN)
        else:
            print_color(f"Test '{test_name}': FAILED", RED)
            all_passed = False

    if not args.no_cleanup:
        cleanup_test_environment(original_cwd)
    else:
        print_color(f"\nSkipping cleanup. Test environment remains at: {test_dir}", YELLOW)

    if all_passed:
        print_color("\nAll selected tests PASSED!", GREEN + BOLD)
        sys.exit(0)
    else:
        print_color("\nSome tests FAILED!", RED + BOLD)
        sys.exit(1)

if __name__ == "__main__":
    main()
