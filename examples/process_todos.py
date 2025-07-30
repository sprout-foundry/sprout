import os
import subprocess
import sys

def load_meaningful_tasks(filename="todo.txt"):
    """
    Loads meaningful tasks from todo.txt into a list.
    Returns a list of tasks if meaningful tasks are loaded,
    or an empty list if the file is empty or contains only whitespace.
    """
    tasks = []

    # Check if todo.txt is truly empty (0 bytes)
    if not os.path.exists(filename) or os.path.getsize(filename) == 0:
        return []

    try:
        with open(filename, 'r', encoding='utf-8') as f:
            for line in f:
                # Check if line contains any non-whitespace characters
                if line.strip():
                    # Append the line, stripping only the trailing newline character
                    # to preserve leading/trailing whitespace if it's part of the task.
                    tasks.append(line.rstrip('\n'))
    except FileNotFoundError:
        # This case should ideally be caught before calling, but included for robustness.
        return []
    except Exception as e:
        print(f"An error occurred while reading {filename}: {e}")
        return []

    return tasks

def main():
    todo_filename = "todo.txt"
    MAX_ITERATIONS = 3

    print(f"Processing tasks from {todo_filename}...")

    # Loop through the process up to three times, or until todo.txt is empty
    for i in range(1, MAX_ITERATIONS + 1): # range(1, 4) gives iterations 1, 2, 3
        print(f"--- Processing Iteration {i} of {MAX_ITERATIONS} ---")

        # Load tasks for the current iteration.
        # This ensures that each iteration operates on a fresh snapshot of todo.txt.
        current_tasks_to_process = load_meaningful_tasks(todo_filename)

        if not current_tasks_to_process:
            print(f"No meaningful tasks found in {todo_filename} for this iteration, breaking loop.")
            break # No meaningful tasks found, exit the loop

        # Process each task loaded in this iteration.
        for task_line in current_tasks_to_process:
            print(f"Calling ledit for: \"{task_line}\"")
            try:
                command = [
                    "zsh",
                    "-c",
                    f"source ~/.zshrc && ledit code \"Make the following change, or if it is already done, remove this task from the todo.txt file. Requested Change: '{task_line}' #WS\" --skip-prompt"
                ]
                # subprocess.run will execute the command. check=True raises an error if the command fails.
                # Added capture_output=True, text=True, and input='y\n' to automatically respond 'y'
                result_task = subprocess.run(command, capture_output=True, text=True, check=True, input='y\n')

                # Print ledit's output to console
                if result_task.stdout:
                    print("\n--- Ledit Output (stdout) ---")
                    print(result_task.stdout)
                if result_task.stderr:
                    print("\n--- Ledit Output (stderr) ---")
                    print(result_task.stderr)
                print("-----------------------------\n")

            except FileNotFoundError:
                print("\nError: 'ledit' command not found.")
                print("Please ensure 'ledit' is installed and in your system's PATH.")
                print("If 'ledit' is a shell function, you might need to adjust how it's called in Python (e.g., via 'zsh -c').")
                sys.exit(1)
            except subprocess.CalledProcessError as e:
                print(f"Error executing ledit for task '{task_line}': {e}")
                if e.stdout:
                    print(f"Ledit stdout: {e.stdout}")
                if e.stderr:
                    print(f"Ledit stderr: {e.stderr}")
                # Decide if you want to exit or continue on ledit error.
                # For now, we'll continue to the next task.
            except Exception as e:
                print(f"An unexpected error occurred while calling ledit for task '{task_line}': {e}")

        print(f"Iteration {i} complete.")
        # If ledit modifies todo.txt (e.g., removes a task), the next iteration
        # will call load_meaningful_tasks again and get the updated content.

    print(f"All tasks from {todo_filename} processed (or {todo_filename} became empty).")

if __name__ == "__main__":
    main()