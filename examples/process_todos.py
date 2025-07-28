import os
import subprocess
import sys

def check_todo_file_exists(filename="todo.txt"):
    """
    Checks if the todo.txt file exists.
    Returns True if file exists, False otherwise.
    """
    if not os.path.exists(filename):
        print(f"Error: {filename} not found in the current directory.")
        print(f"Please create a {filename} file with one task per line.")
        return False
    return True

def load_meaningful_tasks(filename="todo.txt"):
    """
    Loads meaningful tasks from todo.txt into a list.
    Returns a list of tasks if meaningful tasks are loaded,
    or an empty list if the file is empty or contains only whitespace.
    """
    tasks = []
    meaningful_line_found = False

    # Check if todo.txt is truly empty (0 bytes)
    if not os.path.exists(filename) or os.path.getsize(filename) == 0:
        # This message is now handled by the main function for better context.
        return []

    try:
        with open(filename, 'r', encoding='utf-8') as f:
            for line in f:
                # Check if line contains any non-whitespace characters
                if line.strip():
                    # Append the line, stripping only the trailing newline character
                    # to preserve leading/trailing whitespace if it's part of the task.
                    tasks.append(line.rstrip('\n'))
                    meaningful_line_found = True
    except FileNotFoundError:
        # This case should ideally be caught by check_todo_file_exists,
        # but included for robustness.
        print(f"Error: {filename} not found.")
        return []
    except Exception as e:
        print(f"An error occurred while reading {filename}: {e}")
        return []

    if not meaningful_line_found:
        # This message is now handled by the main function for better context.
        return []

    return tasks

def generate_todos_from_prompt(output_todo_file):
    base_prompt="Goal: Create a todo.txt with a robust set of todos to accomplish the following requirements. Use the current workspace information to help inform what needs to change to fulfil the requirements. There should be one todo per line and each line should be able to give all the information needed to accomplish the todo in each line. REQUIREMENTS: "

    print("The base prompt for task generation is:")
    print("----------------------------------------------------")
    print(base_prompt)
    print("----------------------------------------------------")
    user_requirements = input("Please enter your specific requirements for the tasks (e.g., 'Add user authentication'): ").strip()

    if not user_requirements:
        raise ValueError("User requirements cannot be empty. Please provide specific requirements for task generation.")
    else:
        # Combine the base prompt with the user's specific requirements
        full_ledit_prompt = f"{base_prompt}\n\n {user_requirements}"

    print(f"\nCalling ledit to generate content for '{output_todo_file}'...")
    # print(f"Ledit prompt: \"{full_ledit_prompt}\"") # Uncomment for debugging the ledit prompt

    try:
        # Execute ledit with the combined prompt and capture its stdout
        command = [
            "ledit",
            "code",
            full_ledit_prompt,
            "--skip-prompt", 
            "-m", "gemini:gemini-2.5-pro" # Use the best model for task generation
        ]
        result = subprocess.run(command, capture_output=True, text=True, check=True)

        # Write ledit's stdout to the todo.txt file
        with open(output_todo_file, 'w', encoding='utf-8') as f:
            f.write(result.stdout)
        print(f"Tasks generated and saved to '{output_todo_file}'.")

    except FileNotFoundError:
        raise FileNotFoundError("\nError: 'ledit' command not found.")
    except subprocess.CalledProcessError as e:
        raise Exception(f"Error executing ledit: {e}\nStderr: {e.stderr}")
    except Exception as e:
        raise Exception(f"An unexpected error occurred during ledit execution: {e}")


def main():
    todo_filename = "todo.txt"

    # Determine if we need to generate tasks
    should_generate = False
    if not os.path.exists(todo_filename):
        print(f"\n'{todo_filename}' does not exist.")
        should_generate = True
    elif os.path.getsize(todo_filename) == 0:
        print(f"\n'{todo_filename}' is empty (0 bytes).")
        should_generate = True
    else:
        # File exists and is not 0 bytes, but check if it has meaningful content
        # This prevents processing a file that only contains whitespace/newlines
        meaningful_tasks = load_meaningful_tasks(todo_filename)
        if not meaningful_tasks:
            print(f"\n'{todo_filename}' contains only empty or whitespace lines.")
            should_generate = True

    if should_generate:
        print("Would you like to generate new tasks based on a prompt? (yes/no)")
        user_choice = input("Enter 'yes' to generate, or 'no' to exit: ").strip().lower()

        if user_choice == 'yes':
            try:
                generate_todos_from_prompt(todo_filename)
                # After generation, re-check if todo.txt now has content
                # If it's still empty or doesn't exist, something went wrong with generation
                if not os.path.exists(todo_filename) or not load_meaningful_tasks(todo_filename):
                    print("Task generation failed or resulted in an empty todo.txt. Exiting.")
                    sys.exit(1)
                print(f"\nProceeding with processing newly generated tasks from '{todo_filename}'.")
            except FileNotFoundError as e:
                print(f"Error: {e}")
                sys.exit(1)
            except Exception as e:
                print(f"An error occurred during task generation: {e}")
                sys.exit(1)
        else:
            print("Exiting without processing tasks.")
            sys.exit(0) # Exit gracefully if user chooses not to generate
    else:
        print(f"Processing existing tasks from '{todo_filename}'...")

    # The existing task processing loop starts here
    print(f"Processing tasks from {todo_filename}...")

    # Loop through the process up to three times, or until todo.txt is empty
    for i in range(1, 4): # range(1, 4) gives iterations 1, 2, 3
        print(f"--- Processing Iteration {i} of 3 ---")

        # Load tasks for the current iteration.
        # This ensures that each iteration operates on a fresh snapshot of todo.txt.
        current_tasks_to_process = load_meaningful_tasks(todo_filename)

        if not current_tasks_to_process:
            print("No meaningful tasks found for this iteration, breaking loop.")
            break # No meaningful tasks found, exit the loop

        # Process each task loaded in this iteration.
        for task_line in current_tasks_to_process:
            print(f"Calling ledit for: \"{task_line}\"")
            # Call ledit with the content of the line
            # The --skip-prompt flag prevents ledit from asking for confirmation
            # Note: 'ledit' needs to be an executable available in your system's PATH.
            # If 'ledit' is a shell function or alias defined in your .zshrc,
            # you might need to adjust this call (e.g., by running 'zsh -c "source ~/.zshrc && ledit..."').
            try:
                command = [
                    "ledit",
                    "code",
                    f"Make the following change, or if it is already done, remove this request from the todo.txt file. Requested Change: '{task_line}' #WS",
                    "--skip-prompt"
                ]
                # subprocess.run will execute the command. check=True raises an error if the command fails.
                subprocess.run(command, check=True)
            except FileNotFoundError:
                print("\nError: 'ledit' command not found.")
                print("Please ensure 'ledit' is installed and in your system's PATH.")
                print("If 'ledit' is a shell function, you might need to adjust how it's called in Python (e.g., via 'zsh -c').")
                sys.exit(1)
            except subprocess.CalledProcessError as e:
                print(f"Error executing ledit for task '{task_line}': {e}")
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