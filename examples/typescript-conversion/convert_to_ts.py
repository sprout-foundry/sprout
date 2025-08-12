import os
import subprocess
from pathlib import Path
from concurrent.futures import ThreadPoolExecutor, as_completed
import sys # Added for potential stdin piping

def find_js_jsx_files(directory):
    """
    Recursively finds all .js and .jsx files in the given directory,
    ignoring commonly excluded folders like 'node_modules', 'dist', 'build', etc.
    """
    file_list = []
    # Define a list of directories to ignore
    ignore_dirs = [
        'node_modules',
        'dist',
        'build',
        '.git',
        'coverage',
        'out',
        'public', # Often contains build artifacts or static assets
        'temp',
        'tmp'
    ]

    for root, dirs, files in os.walk(directory):
        # Exclude specified directories from traversal
        # Modifying dirs in-place tells os.walk not to recurse into them
        dirs[:] = [d for d in dirs if d not in ignore_dirs]
        
        for file in files:
            if file.endswith(('.js', '.jsx')):
                file_list.append(Path(root) / file)
    return file_list

def process_file_to_typescript(js_file_path):
    """
    Processes a single .js/.jsx file to convert it to .ts/.tsx using 'ledit code'.
    It reads the content of the original file, passes it to 'ledit code' via stdin
    along with a prompt, captures 'ledit's stdout, and writes it to a new .ts/.tsx file.
    After successful conversion, it deletes the original file.
    Returns True on successful conversion and deletion, False otherwise.
    """
    # Determine the target TypeScript file path
    # e.g., 'file.js' -> 'file.ts', 'component.jsx' -> 'component.tsx'
    ts_file_path = js_file_path.with_suffix('.ts' if js_file_path.suffix == '.js' else '.tsx')
    print(f"Attempting to convert: {js_file_path} -> {ts_file_path}")

    try:
        # Read the content of the original JavaScript/JSX file
        with open(js_file_path, 'r', encoding='utf-8') as f_in:
            original_code = f_in.read()

        # Define the prompt for the 'ledit code' command
        prompt = "Convert this JavaScript/JSX code to TypeScript/TSX, adding types where appropriate and preserving all existing functionality and structure. Do not change the logic or functionality of the code, just add TypeScript types and interfaces where necessary."

        # Run the 'ledit code' command.
        # Source ~/.zshrc first, then pipe the original code to ledit's stdin.
        # Capture ledit's stdout and stderr.
        command = f'source ~/.zshrc && ledit code "{prompt}" -f {js_file_path} -m deepinfra:Qwen/Qwen3-Coder-480B-A35B-Instruct --skip-prompt'
        
        process = subprocess.run(
            ['zsh', '-c', command],
            capture_output=True, # Capture stdout and stderr
            text=True,           # Decode stdout/stderr as text
            input=original_code, # Pass original code to ledit's stdin
            encoding='utf-8',    # Explicitly set encoding
            errors='replace'     # Replace characters that cannot be decoded
        )

        # Pipe ledit's stdout to the console directly
        if process.stdout:
            print(f"\n--- Ledit Output for {js_file_path} ---\n")
            print(process.stdout)
            print(f"\n--- End Ledit Output ---\n")
        
        if process.stderr:
            print(f"Ledit stderr for {js_file_path}:\n{process.stderr}")

        if process.returncode != 0:
            print(f"Error: 'ledit code' command failed with exit code {process.returncode}")
            return False
        
        # Remove the original file after successful conversion
        os.remove(js_file_path)
        print(f"Successfully converted and deleted: {js_file_path}")
        return True
    except FileNotFoundError:
        print(f"Error: 'zsh' or 'ledit' command not found. Please ensure they are installed and in your system's PATH.")
        return False
    except Exception as e:
        print(f"An unexpected error occurred while processing {js_file_path}: {e}")
        return False

def main():
    current_directory = Path('.')
    initial_files = find_js_jsx_files(current_directory)

    if not initial_files:
        print(f"No .js or .jsx files found in '{current_directory}' or its subdirectories.")
        return

    print(f"Found {len(initial_files)} .js/.jsx files to process.")

    files_to_process = list(initial_files) # Files that still need to be processed
    successful_conversions = []
    failed_conversions = []
    batch_size = 30
    batch_num = 0

    # Loop until no files are left to process
    while files_to_process:
        batch_num += 1
        print(f"\n--- Processing batch {batch_num} (remaining files: {len(files_to_process)}) ---")

        # Take a batch from the current files_to_process
        current_batch = files_to_process[:batch_size]
        files_to_process = files_to_process[batch_size:] # Remove processed files from the list

        if not current_batch:
            break # No more files to process

        batch_successful_count = 0
        
        # Use ThreadPoolExecutor for parallel processing
        with ThreadPoolExecutor(max_workers=batch_size) as executor: # max_workers set to 10
            # Submit tasks and store futures mapped to their original file paths
            future_to_file = {executor.submit(process_file_to_typescript, file_path): file_path for file_path in current_batch}
            
            for future in as_completed(future_to_file):
                file_path = future_to_file[future]
                try:
                    if future.result():
                        successful_conversions.append(file_path)
                        batch_successful_count += 1
                    else:
                        failed_conversions.append(file_path)
                        files_to_process.append(file_path) # Add back to the list if it failed
                except Exception as exc:
                    print(f'{file_path} generated an exception: {exc}')
                    failed_conversions.append(file_path)
                    files_to_process.append(file_path) # Add back to the list if an exception occurred

        # Run 'tsc' and call parse_tsc_errors.py in a loop until errors are gone or max attempts reached
        print(f"\n--- Running 'tsc' after batch {batch_num} ---")
        max_fix_attempts = 2 # Define a limit for fix attempts
        fix_attempt_count = 0
        build_successful = False

        while not build_successful and fix_attempt_count < max_fix_attempts:
            print(f"--- Build/Fix attempt {fix_attempt_count + 1}/{max_fix_attempts} ---")
            try:
                build_process = subprocess.run(
                    ['tsc'], # Changed from ['npm', 'run', 'build'] to ['tsc']
                    capture_output=True,
                    text=True,
                    encoding='utf-8',    # Explicitly set encoding
                    errors='replace',    # Replace characters that cannot be decoded
                    cwd=current_directory # Run tsc from the current directory
                )
            except FileNotFoundError:
                print(f"Error: 'tsc' command not found. Please ensure TypeScript is installed globally by running 'npm install -g typescript'.")
                build_successful = False # Mark as failed so it doesn't proceed as if successful
                break # Exit the build/fix loop if tsc is not found

            if build_process.stdout:
                print(f"tsc stdout:\n{build_process.stdout}")
            
            # If 'tsc' had any error output (stderr is not empty or return code is non-zero)
            if build_process.returncode != 0 or build_process.stderr:
                print(f"tsc stderr:\n{build_process.stderr}")
                print(f"\n--- 'tsc' had errors. Calling 'parse_tsc_errors.py' ---")
                
                # Combine stdout and stderr for parse_tsc_errors.py
                tsc_output = build_process.stdout + build_process.stderr

                # Get the directory where this script is located
                script_dir = Path(__file__).parent
                parse_script_path = script_dir / 'parse_tsc_errors.py'

                parse_errors_process = subprocess.run(
                    [sys.executable, str(parse_script_path)], # Use the full path to parse_tsc_errors.py
                    capture_output=True,
                    text=True,
                    input=tsc_output, # Pass tsc output to stdin of parse_tsc_errors.py
                    encoding='utf-8',    # Explicitly set encoding
                    errors='replace',    # Replace characters that cannot be decoded
                    cwd=current_directory
                )
                
                if parse_errors_process.stdout:
                    print(f"parse_tsc_errors.py stdout:\n{parse_errors_process.stdout}")
                if parse_errors_process.stderr:
                    print(f"parse_tsc_errors.py stderr:\n{parse_errors_process.stderr}")
                
                if parse_errors_process.returncode != 0:
                    print(f"Error: 'parse_tsc_errors.py' command failed with exit code {parse_errors_process.returncode}. This might indicate a persistent issue.")
                    break # Break the fix loop if parse_tsc_errors.py itself fails
                
                fix_attempt_count += 1
            else:
                print(f"'tsc' completed successfully.")
                build_successful = True
        
        if not build_successful:
            print(f"Warning: 'tsc' failed after {max_fix_attempts} attempts. Continuing with next batch or finishing.")

        # Break condition: if no files were successfully processed in the last batch,
        # and there are still files to process, it might indicate a stuck state.
        if len(current_batch) > 0 and batch_successful_count == 0 and len(files_to_process) > 0:
            print("No progress made in this batch (no successful conversions). Breaking to prevent potential infinite loop.")
            break


    print("\n--- Conversion Summary ---")
    print(f"Total files initially found: {len(initial_files)}")
    print(f"Successfully converted: {len(successful_conversions)}")
    for f in successful_conversions:
        print(f"  - {f}")
    print(f"Failed conversions: {len(failed_conversions)}")
    for f in failed_conversions:
        print(f"  - {f}")

   # Call validate_ts_conversion.py after all files have been processed
    print("\n--- Validating TypeScript Conversion ---")
    try:
        # Get the directory where this script is located
        script_dir = Path(__file__).parent
        validate_script_path = script_dir / 'validate_ts_conversion.py'

        print(f"Run validation script with the command: python3 {validate_script_path}")
    except FileNotFoundError:
        print(f"Error: 'validate_ts_conversion.py' not found. Please ensure it is in the current directory.")
    except Exception as e:
        print(f"An unexpected error occurred during validation: {e}")

if __name__ == "__main__":
    main()