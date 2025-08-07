import subprocess
import argparse

def get_prompt(user_input: str) -> str:
    instructions = (
        "Goal: Create a todo.txt with a robust set of todos to accomplish the following requirements. "
        "DO NOT COMPLETE the requirements, just outline the DETAILED steps needed to accomplish them. "
        "Use the current workspace information to help inform what needs to change to fulfil the requirements. "
        "There should be one todo per line and each line should be able to stand alone as a complete thought, "
        "providing all the information needed to accomplish the todo without relying on the context of other lines. " 
        "Note that the todos should be written in a way that they can be completed in a single commit without needing the context of the image to be understood. "
        "Make sure to consider all aspects of the front-end update, including layout, styling, responsiveness, and any necessary adjustments to existing components and existing dependent code. "
        "Do not recommend using any new libraries or frameworks unless absolutely necessary and don't recommend changes to any existing functionality unless it is required to meet the requirements. "
        "REQUIREMENTS: "
        "Update the look and feel of the front end to match the layout and styling shown in the provided image. "
    )

    if user_input:
        return  f"{instructions} {user_input} #WS"
    else:
        return   f"{instructions} #WS"

if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Generate a ledit command for front-end updates based on an image and user input.")
    parser.add_argument("image_path", type=str, help="Path to the image file showing the desired layout.")
    parser.add_argument("--user_input", type=str, default="", help="Additional user input or considerations for the front-end update.")
    
    args = parser.parse_args()

    image_path = args.image_path
    user_input = args.user_input

    prompt = get_prompt(user_input)
    ledit_command_str = f'ledit code "{prompt}" --image "{image_path}" --skip-prompt -m deepinfra:google/gemini-2.5-pro'

    print(f"ledit command for front-end update:\n{ledit_command_str}")

    full_zsh_command = f"source ~/.zshrc && {ledit_command_str}"

    # Note: This part of the script is for demonstration of the command.
    # To actually run it, you might need to execute the printed command manually
    # or ensure your environment is set up correctly for subprocess.run with zsh.
    print("\n--- Attempting to run the command via subprocess (may require manual execution) ---")
    print("To run this command, make sure you have a vision-capable model configured (e.g., gemini:gemini-2.5-flash).")
    print(f"Also, ensure '{image_path}' exists and contains the desired layout image.")
    print("\nExample usage:")
    print(f"python {__file__} path/to/your/image.png --user_input 'Add responsive design considerations for mobile devices.'")
    print(f"python {__file__} path/to/another/image.jpg")
    print("----------------------------------------------------------------------------------")

    try:
        ledit_result = subprocess.run(
            ['zsh', '-c', full_zsh_command],
            capture_output=True,
            encoding='utf-8',
            check=False,
            text=True # Ensure text mode for stdout/stderr
        )

        ledit_output = ledit_result.stdout.strip()
        ledit_error = ledit_result.stderr.strip()

        if ledit_output:
            print("\nLedit Output:")
            print(ledit_output)
        if ledit_error:
            print("\nLedit Error:")
            print(ledit_error)
        if ledit_result.returncode != 0:
            print(f"\nCommand exited with non-zero status: {ledit_result.returncode}")

    except FileNotFoundError:
        print("\nError: 'zsh' command not found. Make sure zsh is installed and in your PATH.")
    except Exception as e:
        print(f"\nAn unexpected error occurred: {e}")
