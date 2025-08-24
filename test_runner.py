#!/usr/bin/env python3

import argparse
import subprocess
import os
import shutil
import time
import sys
from collections import OrderedDict
from pathlib import Path
import logging # Import logging module
import json

"""
This script performs a robust test of the `ledit` workspace functionality.
It can run all tests in parallel to speed up validation or run a single test interactively.

Features:
- Parallel test execution with timeout handling for robust testing.
- Interactive single test selection mode for focused debugging.
- Detailed failure reporting with extracted reasons for quick diagnosis.
- Clean output formatting with ANSI color codes for readability.
- Test numbering is based on the current run's discovered test order.
- Each test runs in its own isolated subdirectory within the 'testing' folder.

This script is a Python replacement for the original test.sh script,
providing more robust state management, clearer output, and enhanced error handling.
"""

# Configure logging
# Set level to logging.INFO for general flow messages.
# Change to logging.DEBUG for more verbose internal state tracking.
logging.basicConfig(level=logging.INFO, format='%(levelname)s: %(message)s')

# ANSI color codes for pretty printing in the terminal
GREEN = '\033[32m'
RED = '\033[31m'
RESET = '\033[0m'

DEFAULT_MODEL = 'deepinfra:Qwen/Qwen2.5-Coder-32B-Instruct'

def extract_failure_reason(stdout, stderr):
    """Extract a short, concise failure reason from test output.
    
    This function attempts to parse common error messages from stdout and stderr
    to provide a more human-readable reason for test failures.
    
    Args:
        stdout (str): Standard output from the test process.
        stderr (str): Standard error from the test process.
    
    Returns:
        str: A concise failure reason, truncated to 1000 characters,
             or "Unknown failure reason" if no specific pattern is found.
    """
    # Prioritize specific error keywords in stderr
    if "error:" in stderr.lower():
        return stderr.split("error:")[-1].strip()[:1000]
    if "failed:" in stdout.lower():
        return stdout.split("failed:")[-1].strip()[:1000]
    if "assertion failed" in stderr.lower():
        return stderr.split("assertion failed")[-1].strip()[:1000]
    # Fallback to general stderr or stdout if specific errors aren't found
    if stderr.strip():
        return stderr.strip()[:1000]
    if stdout.strip():
        return stdout.strip()[:1000]
    return "Unknown failure reason"

def main():
    """Main function to orchestrate the test workflow.
    
    This function handles argument parsing, project setup, test discovery,
    execution (parallel or single), monitoring, and final reporting.
    """
    # 1. Argument Parsing
    parser = argparse.ArgumentParser(
        description="Run workspace functionality tests for ledit.",
        formatter_class=argparse.RawTextHelpFormatter,
        epilog=f"""
Examples:
  Run all tests:
    {sys.argv[0]}

  Run with a specific model:
    {sys.argv[0]} -m my-custom-model

  Run in interactive single test mode (prompts for test number):
    {sys.argv[0]} --single
    # When using --single without -t, the script will prompt you to enter a test number.
    # Ensure you are running in an interactive terminal for this to work.

  Run a specific test by number (e.g., test #2):
    {sys.argv[0]} -t 2

  Run a specific test by number in explicit single mode:
    {sys.argv[0]} --single -t 2

  View available tests and their numbers:
    {sys.argv[0]} --list-tests

  Run tests and keep the 'testing' directory for inspection (e.g., after failures):
    {sys.argv[0]} --keep-testing-dir

  Print a custom message and exit (skips tests):
    {sys.argv[0]} --message "Hello, Ledit!"
"""
    )
    parser.add_argument(
        '-m', '--model',
        default='',
        help='Specify the model name to use for tests. If omitted, uses `.ledit/config.json` orchestration_model (or editing_model) when available.'
    )
    parser.add_argument(
        '--single',
        action='store_true',
        help="Enable single test mode. If -t is not provided, it will prompt for a test number."
    )
    parser.add_argument(
        '-t', '--test-number',
        type=str,
        help="Specify a single test number to run. This implicitly enables single test mode."
    )
    parser.add_argument(
        '--list-tests',
        action='store_true',
        help="List all discovered tests and their assigned numbers, then exit."
    )
    parser.add_argument(
        '--keep-testing-dir',
        action='store_true',
        help="Do not remove the 'testing' directory after tests complete. Useful for debugging failures."
    )
    parser.add_argument(
        '--message',
        type=str,
        help='A custom message to print and then exit. If provided, no tests will be run.'
    )
    args = parser.parse_args()
    
    # Log parsed arguments for debugging
    logging.debug(f"Parsed arguments: single={args.single}, test_number={args.test_number}, list_tests={args.list_tests}, keep_testing_dir={args.keep_testing_dir}, message={args.message}")

    # If --message is provided, print it and exit immediately.
    if args.message:
        print(args.message)
        sys.exit(0)

    # model_name will be resolved after locating project_root so we can read .ledit/config.json
    # If -t is provided, implicitly enable single_mode.
    single_mode = args.single or (args.test_number is not None)
    test_number_arg = args.test_number
    list_tests_only = args.list_tests
    keep_testing_dir = args.keep_testing_dir # Initial value based on command-line arg

    logging.debug(f"Calculated modes: single_mode={single_mode}, test_number_arg={test_number_arg}, list_tests_only={list_tests_only}, keep_testing_dir={keep_testing_dir}")

    # --- 0. SETUP ---
    print("--- 0. SETUP: Cleaning up and building the tool ---")
    
    # Get the root directory of the project (where this script is located)
    project_root = Path(__file__).parent.resolve()
    
    testing_dir = project_root / 'testing'
    e2e_test_scripts_dir = project_root / 'e2e_test_scripts'

    # Resolve model name: prefer CLI arg; otherwise read orchestration model from .ledit/config.json
    def resolve_default_model() -> str:
        candidates = [project_root / '.ledit' / 'config.json', Path.home() / '.ledit' / 'config.json']
        for p in candidates:
            try:
                if p.exists():
                    with open(p, 'r') as f:
                        cfg = json.load(f)
                    # Prefer orchestration_model; fallback to editing_model; final fallback to legacy keys
                    if isinstance(cfg, dict):
                        m = (
                            cfg.get('orchestration_model')
                            or cfg.get('editing_model')
                            or cfg.get('SummaryModel')  # unlikely casing variants
                        )
                        if m and isinstance(m, str) and m.strip():
                            return m.strip()
            except Exception:
                # Ignore and continue to next candidate
                pass
        # Fallback to previous hardcoded default if nothing found
        return DEFAULT_MODEL

    model_name = args.model if args.model else resolve_default_model()

    # Clean up previous testing artifacts if they exist
    if testing_dir.exists():
        print(f"Removing existing '{testing_dir}' directory...")
        shutil.rmtree(testing_dir)
    # Create the main testing directory. Individual test directories will be created inside it.
    testing_dir.mkdir()

    # Run go build in project root to compile the 'ledit' tool
    print("Building the 'ledit' tool...")
    try:
        # Capture output for better error reporting
        build_result = subprocess.run(
            ['go', 'build'], 
            check=True, 
            cwd=project_root, # Build in the project root
            capture_output=True, 
            text=True
        )
        print(f"Go build successful:\n{build_result.stdout.strip()}")
        subprocess.run(['cp', 'ledit', 'testing/'], check=True, cwd=project_root) # Ensure go.mod is tidy
    except subprocess.CalledProcessError as e:
        logging.error(f"{RED}Error: 'go build' failed.{RESET}")
        logging.error(f"STDOUT:\n{e.stdout}")
        logging.error(f"STDERR:\n{e.stderr}")
        sys.exit(1)
    except FileNotFoundError:
        logging.error(f"{RED}Error: 'go' command not found. Is Go installed and in your PATH?{RESET}")
        sys.exit(1)


    # IMPORTANT: We no longer change into testing_dir here.
    # Each test will run in its own subdirectory within testing_dir.
    
    # Ensure the e2e_test_scripts directory exists, though it should if scripts are present
    e2e_test_scripts_dir.mkdir(exist_ok=True)
    print("----------------------------------------------------")
    print()

    # --- Test Discovery ---
    print("--- Discovering tests ---")
    tests = [] # List to store discovered test dictionaries: {'name': '...', 'path': Path(...)}
    # Find all test scripts following the 'test_*.sh' pattern and sort them alphabetically
    test_script_paths = sorted(e2e_test_scripts_dir.glob('test_*.sh'))

    if not test_script_paths:
        logging.error(f"{RED}Error: No test scripts found in '{e2e_test_scripts_dir}'. Ensure your test scripts are named 'test_*.sh'.{RESET}")
        sys.exit(1)

    for script_path in test_script_paths:
        try:
            # Execute 'get_test_name' function from each script to retrieve its logical name
            # This is done in a subshell to avoid affecting the current script's environment.
            cmd = f". {script_path.resolve()} && get_test_name"
            result = subprocess.run(
                cmd,
                shell=True,
                executable='/bin/bash', # Explicitly use bash for sourcing
                capture_output=True,
                text=True,
                check=True # Raise CalledProcessError if the command returns a non-zero exit code
            )
            test_name = result.stdout.strip()
            if not test_name:
                logging.warning(f"'get_test_name' in {script_path.name} returned an empty name. Skipping this test.")
                continue
            # Keep orchestration test; include new agent v2 tests as well
            tests.append({'name': test_name, 'path': script_path})
            print(f"Discovered Test: {test_name}")
        except subprocess.CalledProcessError as e:
            logging.error(f"{RED}Error discovering test name from {script_path.name}: {e.stderr.strip()}. Skipping.{RESET}")
        except FileNotFoundError:
            logging.error(f"{RED}Error: Bash not found. Ensure /bin/bash is available.{RESET}")
            sys.exit(1)
    
    if not tests:
        logging.error(f"{RED}Error: No valid tests discovered after processing all scripts.{RESET}")
        sys.exit(1)
    
    # Create tests_mapping for consistent test numbering within this run
    # Test numbers are 1-indexed strings for user-friendliness
    tests_mapping = {str(i+1): test['name'] for i, test in enumerate(tests)}
    
    print("-------------------------")
    print()

    # --- Test Listing Mode ---
    if list_tests_only:
        print("--- Available Tests and Numbers ---")
        for num, test_name in tests_mapping.items():
            print(f"{num}: {test_name}")
        print("-----------------------------------")
        sys.exit(0) # Exit after listing tests

    # --- Test Selection for Execution ---
    selected_tests_for_execution = []
    if single_mode:
        logging.info("--- Single Test Mode Activated ---")
        print("Available tests:")
        for num, test_name in tests_mapping.items():
            print(f"{num}: {test_name}")
        
        selected_test_number = None
        if test_number_arg:
            # Use specified test number if provided via -t
            selected_test_number = test_number_arg
            logging.info(f"Using test number '{selected_test_number}' from command-line argument (-t).")
        else:
            # Otherwise, prompt the user for input
            logging.info("No test number provided via -t. Prompting user for input.")
            try:
                selected_test_number = input("Enter the number of the test to run: ").strip()
            except EOFError:
                logging.error(f"{RED}Error: Input stream closed (EOF). Cannot prompt for test number in non-interactive mode. Please use -t <test_number> when running in non-interactive environments.{RESET}")
                sys.exit(1)
            except Exception as e:
                logging.error(f"{RED}An unexpected error occurred while reading input: {e}{RESET}")
                sys.exit(1)
        
        if selected_test_number:
            selected_test_name = tests_mapping.get(selected_test_number)
            if not selected_test_name:
                logging.error(f"{RED}Error: Invalid test number '{selected_test_number}'. Please choose from the list above.{RESET}")
                sys.exit(1)
            # Filter the 'tests' list to include only the selected test
            selected_tests_for_execution = [test for test in tests if test['name'] == selected_test_name]
            if not selected_tests_for_execution:
                logging.error(f"{RED}Error: Test '{selected_test_name}' (number {selected_test_number}) was found in mapping but not in discovered scripts. This should not happen.{RESET}")
                sys.exit(1)
            logging.info(f"Selected test for execution: '{selected_test_name}' (Number: {selected_test_number})")
        else:
            logging.error(f"{RED}Error: No test number entered or selected. Exiting single test mode. To run a specific test without prompting, use -t <test_number>.{RESET}")
            sys.exit(1)
    else:
        logging.info("--- Running all discovered tests (Parallel Mode) ---")
        selected_tests_for_execution = tests # Run all discovered tests

    # --- Test Execution & Monitoring ---
    # Prepare CSV to track performance and results
    results_csv_path = project_root / 'e2e_results.csv'
    try:
        with open(results_csv_path, 'w') as fcsv:
            fcsv.write('test_name,duration_seconds,passed,failed\n')
    except Exception as e:
        logging.warning(f"Could not initialize e2e_results.csv: {e}")
    results = OrderedDict() # Stores final results: test_name -> 'PASS'/'FAIL'/'FAIL (Timeout)'
    failure_reasons = {}    # Stores detailed reasons for failed tests
    processes = {}          # Dictionary to track active subprocesses: pid -> {process, name, sanitized_name, start_time, stdout_file, stderr_file, stdout_file_path, stderr_file_path, test_workspace_path}
    
    # Helper: select model per test when no explicit model was provided
    def select_model_for_test(test_name: str) -> str:
        # Agent and Orchestration process tests use DeepSeek V3
        if test_name.startswith('Agent v2') or test_name.startswith('Process -') or test_name == 'Orchestration Feature':
            return 'deepinfra:Qwen3-235B-A22B-Thinking-2507'
        # Ollama test uses Ollama model
        if 'Ollama' in test_name:
            return 'ollama:qwen2.5-coder'
        # Everything else uses the previous default
        return DEFAULT_MODEL

    # Start all selected tests as subprocesses
    for test in selected_tests_for_execution:
        test_name = test['name']
        sanitized_test_name = test_name.replace(' ', '_') # Sanitize name for file paths
        print(f"--- Starting test: {test_name} ---")
        
        # Create a unique directory for this test to ensure isolation
        current_test_workspace = testing_dir / sanitized_test_name
        current_test_workspace.mkdir(parents=True, exist_ok=True)
        logging.debug(f"Created test workspace: {current_test_workspace}")

        # Construct the command to run the test logic within the shell script
        # The 'run_test_logic' function is expected to be defined in each test_*.sh script.
        # We don't pass the ledit path explicitly to the script, but modify PATH for the subprocess.
        env = os.environ.copy()
        env['PATH'] = str(project_root) + os.pathsep + env.get('PATH', '')
        logging.debug(f"PATH for {test_name}: {env['PATH']}")

        # Choose model per test if not explicitly provided via CLI
        chosen_model = model_name if model_name else select_model_for_test(test_name)
        cmd = f". {test['path'].resolve()} && run_test_logic '{chosen_model}'"
        
        # Redirect stdout/stderr to temporary files within the test's workspace
        stdout_file_path = current_test_workspace / f"{sanitized_test_name}.stdout"
        stderr_file_path = current_test_workspace / f"{sanitized_test_name}.stderr"

        stdout_file = open(stdout_file_path, "w")
        stderr_file = open(stderr_file_path, "w")

        process = subprocess.Popen(
            cmd,
            shell=True,
            executable='/bin/bash', # Ensure bash is used for sourcing
            stdout=stdout_file,
            stderr=stderr_file,
            cwd=current_test_workspace, # Run the test script within its dedicated directory
            env=env, # Pass the modified environment
        )
        
        processes[process.pid] = {
            'process': process,
            'name': test_name,
            'sanitized_name': sanitized_test_name, # Store sanitized name for file operations
            'start_time': time.time(),
            'stdout_file': stdout_file,
            'stderr_file': stderr_file,
            'stdout_file_path': stdout_file_path, # Store path for reading later
            'stderr_file_path': stderr_file_path, # Store path for reading later
            'test_workspace_path': current_test_workspace, # Store for debugging/info
        }
        results[test_name] = 'RUNNING' # Initial status

    print("\n--- Monitoring running tests ---")
    timeout = 210 # 3.5 minutes timeout for each individual test

    # Loop while there are active processes to monitor
    while processes:
        print(f"Currently running tests: ({len(processes)} remaining)")
        
        pids_to_remove = [] # List to collect PIDs of finished or timed-out processes
        for pid, info in list(processes.items()): # Iterate over a copy to allow modification
            process = info['process']
            test_name = info['name']
            start_time = info['start_time']
            
            if process.poll() is None: # Process is still running
                elapsed_time = time.time() - start_time
                if elapsed_time > timeout:
                    logging.warning(f"{RED}Test '{test_name}' (PID {pid}) exceeded {timeout}s timeout and will be terminated.{RESET}")
                    process.kill() # Terminate the process
                    results[test_name] = 'FAIL (Timeout)'
                    failure_reasons[test_name] = f"Test timed out after {timeout} seconds"
                    pids_to_remove.append(pid)
                else:
                    print(f"- {test_name} ({int(elapsed_time)}s elapsed)")
            else: # Process has finished
                exit_code = process.poll()
                result = 'PASS' if exit_code == 0 else 'FAIL'
                print(f"Test '{test_name}' (PID {pid}) finished with result: {result} (exit code: {exit_code}).")
                
                results[test_name] = result
                pids_to_remove.append(pid)

        # Process finished/timed-out tests
        for pid in pids_to_remove:
            info = processes[pid]
            # Close file handles before reading to ensure all data is flushed
            info['stdout_file'].close()
            info['stderr_file'].close()
            
            # If the test failed or timed out, read its output for debugging
            if results[info['name']] != 'PASS':
                stdout = ""
                stderr = ""
                try:
                    with open(info['stdout_file_path'], "r") as f:
                        stdout = f.read()
                    with open(info['stderr_file_path'], "r") as f:
                        stderr = f.read()
                except FileNotFoundError:
                    logging.warning(f"Output files for {info['name']} not found at {info['stdout_file_path']}. Could not retrieve detailed failure reason.")

                # Extract and store a concise failure reason
                if results[info['name']] != 'FAIL (Timeout)': # Don't overwrite timeout reason
                    failure_reasons[info['name']] = extract_failure_reason(stdout, stderr)
                
                # Log full output for failed tests
                if stdout or stderr:
                    logging.info(f"--- Output for failed test: {info['name']} (in {info['test_workspace_path']}) ---")
                    if stdout:
                        logging.info("--- STDOUT ---")
                        logging.info(stdout)
                    if stderr:
                        logging.info("--- STDERR ---")
                        logging.info(stderr)
                    logging.info("------------------------------------------")

                # NEW: Write full context of failed test to a file
                sanitized_name_for_log = info['sanitized_name']
                failure_log_filename = f"test_failure_{sanitized_name_for_log}.log"
                failure_log_path = info['test_workspace_path'] / failure_log_filename
                
                # Ensure the reason is available, defaulting if somehow not set
                reason_for_log = failure_reasons.get(info['name'], "Reason not available")

                with open(failure_log_path, "w") as f:
                    f.write(f"--- Test Failure Log for: {info['name']} ---\n")
                    f.write(f"Result: {results[info['name']]}\n")
                    f.write(f"Reason: {reason_for_log}\n\n")
                    f.write(f"--- Full STDOUT ---\n")
                    f.write(stdout if stdout else "[No STDOUT]\n")
                    f.write("\n--- Full STDERR ---\n")
                    f.write(stderr if stderr else "[No STDERR]\n")
                    f.write("\n--- End of Log ---\n")
                logging.info(f"Full failure context saved to: {failure_log_path}")

            # Clean up temporary output files (stdout/stderr files)
            try:
                os.remove(info['stdout_file_path'])
                os.remove(info['stderr_file_path'])
            except OSError as e:
                logging.warning(f"Could not remove temporary output files for {info['name']}: {e}")
            
            del processes[pid] # Remove from active processes

        # Wait before checking processes again, only if there are still active processes
        if processes:
            time.sleep(10)

    print("--------------------------------")

    # Determine overall test pass/fail status after all tests have completed
    all_passed = True
    for test_name, status in results.items():
        if 'FAIL' in status: # Check for 'FAIL' or 'FAIL (Timeout)'
            all_passed = False
            break # Found a failure, no need to check further

    # --- CLEANUP ---
    print("--- CLEANUP: Removing testing artifacts ---")
    # The script remains in project_root throughout execution.
    # Clean up the main testing directory which contains all sub-test directories.
    
    # Determine if testing directory should be kept based on failures
    if not all_passed:
        logging.info(f"{RED}One or more tests failed. Keeping '{testing_dir}' directory for inspection.{RESET}")
        keep_testing_dir = True # Override to keep directory if tests failed

    if not keep_testing_dir:
        if testing_dir.exists():
            print(f"Removing '{testing_dir}' directory...")
            shutil.rmtree(testing_dir)
        else:
            print(f"'{testing_dir}' directory not found, no cleanup needed.")
    else:
        print(f"Skipping cleanup of '{testing_dir}' directory as --keep-testing-dir flag was set or tests failed.")
    print("----------------------------------------------------")
    print()

    # --- Failure Reasons Summary ---
    if failure_reasons:
        print("--- Test Failure Reasons Summary ---")
        for test_name, reason in failure_reasons.items(): 
            print(f"{RED}{test_name}:{RESET} {reason}")
        print("------------------------------------")
        print()

    # --- Final Reporting ---
    print("--- Test Results Summary ---")
    print(f"{'Test Name':<44} {'Result'}")
    print("-" * 51)
    
    # all_passed is already determined above, no need to re-initialize or re-calculate here
    # Iterate through the original discovered test order for consistent reporting
    for test in tests:
        name = test['name']
        # Get the result, defaulting to "NOT RUN" if a test was skipped or not processed
        result = results.get(name, "NOT RUN")
        
        # Truncate test name for clean formatting
        truncated_name = (name[:42] + '..') if len(name) > 44 else name
        
        if result == 'PASS':
            print(f"{GREEN}{truncated_name:<44} {result}{RESET}")
        elif 'FAIL' in result: # Catches 'FAIL' and 'FAIL (Timeout)'
            print(f"{RED}{truncated_name:<44} {result}{RESET}")
            # all_passed is already set to False if any test failed, no need to update here
        else: # For "NOT RUN" or any other unexpected status
            print(f"{truncated_name:<44} {result}")

    print("-" * 51)
    print(" ledit TESTING COMPLETE ---")

    # Exit with a non-zero status code if any test failed
    if not all_passed:
        sys.exit(1)

if __name__ == "__main__":
    try:
        main()
    except KeyboardInterrupt:
        # Handle Ctrl+C gracefully
        logging.info("\nTest execution interrupted by user. Exiting.")
        sys.exit(1)
    except Exception as e:
        # Catch any unexpected exceptions
        logging.critical(f"{RED}An unexpected error occurred: {e}{RESET}", exc_info=True) # exc_info=True to print traceback
        sys.exit(1)
