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

"""
This script performs a robust test of the `ledit` workspace functionality.
It can run all tests in parallel to speed up validation or run a single test interactively.

Features:
- Parallel test execution with timeout handling for robust testing.
- Interactive single test selection mode for focused debugging.
- Detailed failure reporting with extracted reasons for quick diagnosis.
- Clean output formatting with ANSI color codes for readability.
- Test numbering is based on the current run's discovered test order.

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
        epilog=f"""\
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
"""
    )
    parser.add_argument(
        '-m', '--model',
        default='lambda-ai:qwen25-coder-32b-instruct',
        help='Specify the model name to use for tests (default: lambda-ai:qwen25-coder-32b-instruct).'
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
    args = parser.parse_args()
    
    # Log parsed arguments for debugging
    logging.debug(f"Parsed arguments: single={args.single}, test_number={args.test_number}, list_tests={args.list_tests}")

    model_name = args.model
    # If -t is provided, implicitly enable single_mode.
    single_mode = args.single or (args.test_number is not None)
    test_number_arg = args.test_number
    list_tests_only = args.list_tests

    logging.debug(f"Calculated modes: single_mode={single_mode}, test_number_arg={test_number_arg}, list_tests_only={list_tests_only}")

    # --- 0. SETUP ---
    print("--- 0. SETUP: Cleaning up and building the tool ---")
    
    # Get the root directory of the project (where this script is located)
    project_root = Path(__file__).parent.resolve()
    
    testing_dir = project_root / 'testing'
    test_scripts_dir = project_root / 'test_scripts'

    # Clean up previous testing artifacts if they exist
    if testing_dir.exists():
        print(f"Removing existing '{testing_dir}' directory...")
        shutil.rmtree(testing_dir)
    
    # Run go build in project root to compile the 'ledit' tool
    print("Building the 'ledit' tool...")
    try:
        # Capture output for better error reporting
        build_result = subprocess.run(
            ['go', 'build'], 
            check=True, 
            cwd=project_root, 
            capture_output=True, 
            text=True
        )
        print(f"Go build successful:\n{build_result.stdout.strip()}")
    except subprocess.CalledProcessError as e:
        logging.error(f"{RED}Error: 'go build' failed.{RESET}")
        logging.error(f"STDOUT:\n{e.stdout}")
        logging.error(f"STDERR:\n{e.stderr}")
        sys.exit(1)
    except FileNotFoundError:
        logging.error(f"{RED}Error: 'go' command not found. Is Go installed and in your PATH?{RESET}")
        sys.exit(1)

    # Create the testing directory and change into it for test execution
    testing_dir.mkdir()
    os.chdir(testing_dir)
    
    # Ensure the test_scripts directory exists, though it should if scripts are present
    test_scripts_dir.mkdir(exist_ok=True)
    print("----------------------------------------------------")
    print()

    # --- Test Discovery ---
    print("--- Discovering tests ---")
    tests = [] # List to store discovered test dictionaries: {'name': '...', 'path': Path(...)}
    # Find all test scripts following the 'test_*.sh' pattern and sort them alphabetically
    test_script_paths = sorted(test_scripts_dir.glob('test_*.sh'))

    if not test_script_paths:
        logging.error(f"{RED}Error: No test scripts found in '{test_scripts_dir}'. Ensure your test scripts are named 'test_*.sh'.{RESET}")
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
    results = OrderedDict() # Stores final results: test_name -> 'PASS'/'FAIL'/'FAIL (Timeout)'
    failure_reasons = {}    # Stores detailed reasons for failed tests
    processes = {}          # Dictionary to track active subprocesses: pid -> {process, name, start_time, stdout_file, stderr_file}
    
    # Start all selected tests as subprocesses
    for test in selected_tests_for_execution:
        test_name = test['name']
        print(f"--- Starting test: {test_name} ---")
        
        # Construct the command to run the test logic within the shell script
        # The 'run_test_logic' function is expected to be defined in each test_*.sh script.
        cmd = f". {test['path'].resolve()} && run_test_logic '{model_name}'"
        
        # Redirect stdout/stderr to temporary files to prevent blocking pipes
        # and allow reading output after process completion.
        stdout_file = open(f"{test_name}.stdout", "w")
        stderr_file = open(f"{test_name}.stderr", "w")

        process = subprocess.Popen(
            cmd,
            shell=True,
            executable='/bin/bash', # Ensure bash is used for sourcing
            stdout=stdout_file,
            stderr=stderr_file,
        )
        
        processes[process.pid] = {
            'process': process,
            'name': test_name,
            'start_time': time.time(),
            'stdout_file': stdout_file,
            'stderr_file': stderr_file,
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
                    with open(f"{info['name']}.stdout", "r") as f:
                        stdout = f.read()
                    with open(f"{info['name']}.stderr", "r") as f:
                        stderr = f.read()
                except FileNotFoundError:
                    logging.warning(f"Output files for {info['name']} not found. Could not retrieve detailed failure reason.")

                # Extract and store a concise failure reason
                if results[info['name']] != 'FAIL (Timeout)': # Don't overwrite timeout reason
                    failure_reasons[info['name']] = extract_failure_reason(stdout, stderr)
                
                # Print full output for failed tests
                if stdout or stderr:
                    print(f"--- Output for failed test: {info['name']} ---")
                    if stdout:
                        print("--- STDOUT ---")
                        print(stdout)
                    if stderr:
                        print("--- STDERR ---")
                        print(stderr)
                    print("------------------------------------------")

            # Clean up temporary output files
            try:
                os.remove(f"{info['name']}.stdout")
                os.remove(f"{info['name']}.stderr")
            except OSError as e:
                logging.warning(f"Could not remove temporary output files for {info['name']}: {e}")
            
            del processes[pid] # Remove from active processes

        # Wait before checking processes again, only if there are still active processes
        if processes:
            time.sleep(10)

    print("--------------------------------")

    # --- CLEANUP ---
    print("--- CLEANUP: Returning to parent directory ---")
    # Change back to the project root directory
    os.chdir(project_root)
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
    
    all_passed = True
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
            all_passed = False
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