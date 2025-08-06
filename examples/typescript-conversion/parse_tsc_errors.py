import sys
import re
import subprocess
import shlex
import concurrent.futures # Import for parallel execution

"""
prompt use to with the ledit command to initially generate the script, there were some additional modifications to add workspace context support
`ledit code "Generate a script that parses tsc output and groups errors by files. It should loop through each file of errors and call 'ledit code \"\{grouped_errors_for_file\} Only fix the typing errors, do not modify any other functionality note that any is a valid type in TypeScript and works as a fallback type. The fix might be to use any, or to unify existing overlapping types or create missing interfaces or types. It is critical to not remove any methods or method signatures, but just update type information without changing any other functionality. \" -f \{filename\} "`

"""

model = "deepinfra:Qwen/Qwen3-Coder-480B-A35B-Instruct" # Model to use for ledit, can be changed as needed
fallback_model = "lambda-ai:deepseek-v3-0324" # Fallback model if the main one fails


def parse_tsc_output(tsc_output):
    """
    Parses TypeScript compiler output and groups errors by file.
    Expected format: path/to/file.ts(line,column): error TSxxxx: Message
    """
    errors_by_file = {}
    # Regex to match tsc error lines: file(line,col): error TSxxxx: message
    # It also handles lines that might just be a file path followed by an error message
    # or just a message without line/col if tsc output format varies slightly.
    error_pattern = re.compile(r'^(.*?)\((\d+),(\d+)\):\s*(error|warning)\s*(TS\d+):\s*(.*)$')
    simple_error_pattern = re.compile(r'^(.*?):\s*(error|warning)\s*(TS\d+):\s*(.*)$')


    for line in tsc_output.splitlines():
        match = error_pattern.match(line)
        if match:
            filepath, line_num, col_num, level, error_code, message = match.groups()
            error_string = f"{filepath}({line_num},{col_num}): {level} {error_code}: {message}"
            errors_by_file.setdefault(filepath, []).append(error_string)
        else:
            # Try a simpler pattern if line/col are not present
            match = simple_error_pattern.match(line)
            if match:
                filepath, level, error_code, message = match.groups()
                error_string = f"{filepath}: {level} {error_code}: {message}"
                errors_by_file.setdefault(filepath, []).append(error_string)
            else:
                # If it doesn't match a known error pattern, it might be a summary or unrelated line.
                # We can optionally print it or ignore it. For now, we'll ignore it for error grouping.
                pass
    return errors_by_file

def call_ledit_for_file(filename, errors, base_prompt):
    """
    Function to encapsulate the ledit call for a single file.
    This will be run in parallel.
    """
    grouped_errors_for_file = "\n".join(errors)
    
    # Construct the full prompt for ledit
    full_ledit_prompt = f"{base_prompt}\n ERRORS: {grouped_errors_for_file}"
    
    # Construct the ledit command string to be run within zsh
    # shlex.quote is used to properly escape the prompt and filename for the shell command
    ledit_cmd_str = (
        # f"ledit code {shlex.quote(full_ledit_prompt)} -f {shlex.quote(filename)} -m lambda-ai:deepseek-v3-0324 --skip-prompt"
        f"ledit code {shlex.quote(full_ledit_prompt)} -f {shlex.quote(filename)} -m {model} --skip-prompt"
    )
    
    # Construct the full command to run via zsh, sourcing .zshrc first
    # This ensures that ledit (and any other tools it might depend on) is correctly found
    # and configured according to the user's zsh environment.
    command = [
        "zsh",
        "-c",
        f"source ~/.zshrc && {ledit_cmd_str}"
    ]
    
    print(f"--- Calling ledit for {filename} ---")
    print(f"Command: {' '.join(command)}")
    
    try:
        # Execute the command
        # For actual execution, you might want to remove check=True if ledit can exit with non-zero for valid reasons
        # or capture output/errors. For this use case, we assume ledit handles its own output.
        subprocess.run(command, check=True)
        print(f"--- ledit call for {filename} completed ---")
        return f"Successfully processed {filename}"
    except FileNotFoundError:
        return f"Error: 'zsh' or 'ledit' command not found for {filename}. Please ensure they are installed and in your PATH."
    except subprocess.CalledProcessError as e:
        return (
            f"Error calling ledit for {filename}: {e}\n"
            f"Stderr: {e.stderr.decode() if e.stderr else 'N/A'}\n"
            f"Stdout: {e.stdout.decode() if e.stdout else 'N/A'}"
        )
    except Exception as e:
        return f"An unexpected error occurred while processing {filename}: {e}"

def main():
    print("--- Running 'tsc --noEmit' to get TypeScript errors ---")
    try:
        # Execute 'tsc --noEmit' and capture its output
        # capture_output=True captures stdout and stderr
        # text=True decodes stdout/stderr as text using default encoding
        # check=False means we handle non-zero exit codes ourselves, as tsc exits with 1 on errors
        tsc_process = subprocess.run(
            [ "zsh",
            "-c",
            f"source ~/.zshrc && tsc --noEmit"],
            capture_output=True,
            text=True,
            check=False
        )
        tsc_output = tsc_process.stdout
        tsc_stderr = tsc_process.stderr

        if tsc_process.returncode != 0 and tsc_output == "":
            # If tsc exited with an error and produced no stdout, print stderr
            # This might happen if tsc itself is not found, or a configuration error
            print(f"Error running 'tsc --noEmit':")
            if tsc_stderr:
                print(tsc_stderr)
            else:
                print("Unknown error. 'tsc --noEmit' returned a non-zero exit code.")
            sys.exit(1)

    except FileNotFoundError:
        print("Error: 'tsc' command not found. Please ensure TypeScript is installed globally.")
        print("You can install it using: npm install -g typescript") # Added installation recommendation
        sys.exit(1)
    except Exception as e:
        print(f"An unexpected error occurred while trying to run 'tsc --noEmit': {e}")
        sys.exit(1)

    errors_by_file = parse_tsc_output(tsc_output)

    if not errors_by_file:
        print("No TypeScript errors found.")
        return

    base_prompt = (
        "Only fix the typing errors, do not modify any other functionality "
        "note that 'any' is a valid type in TypeScript and works as a fallback type. "
        "The fix might be to use 'any', or to unify existing overlapping types or "
        "create missing interfaces or types. It is critical to not remove any "
        "methods or method signatures, but just update type information without "
        "changing any other functionality. If you are not able to resolve the issue, "
        "add the typescript ignore comment above the relevant line. #WS"
    )

    # Use ThreadPoolExecutor to run ledit calls in parallel
    # Set max_workers to 20 for up to 20 parallel fixes
    MAX_PARALLEL_FIXES = 10
    print(f"\n--- Attempting to fix errors in up to {MAX_PARALLEL_FIXES} files in parallel ---")
    with concurrent.futures.ThreadPoolExecutor(max_workers=MAX_PARALLEL_FIXES) as executor:
        # Submit tasks to the executor
        future_to_filename = {
            executor.submit(call_ledit_for_file, filename, errors, base_prompt): filename
            for filename, errors in errors_by_file.items()
        }

        # Process results as they complete
        for future in concurrent.futures.as_completed(future_to_filename):
            filename = future_to_filename[future]
            try:
                result = future.result()
                print(f"Result for {filename}: {result}")
            except Exception as exc:
                print(f"{filename} generated an exception: {exc}")

    print("\n--- All ledit calls attempted ---")

if __name__ == "__main__":
    main()