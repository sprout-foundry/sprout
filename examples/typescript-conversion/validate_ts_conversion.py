import subprocess
import sys
import os
import shutil
import shlex # Added for robust shell command construction
import concurrent.futures # Added for parallel processing

def run_command(cmd, check=True, capture_output=True, text=True, **kwargs):
    """
    Helper to run shell commands.
    Raises subprocess.CalledProcessError if check is True and command returns non-zero.
    Exits if the command itself is not found (FileNotFoundError).
    """
    try:
        result = subprocess.run(
            cmd,
            check=check,
            capture_output=capture_output,
            text=text,
            **kwargs
        )
        return result.stdout.strip() if capture_output else None
    except subprocess.CalledProcessError as e:
        print(f"Error executing command: {' '.join(cmd)}", file=sys.stderr)
        if e.stdout:
            print(f"STDOUT: {e.stdout}", file=sys.stderr)
        if e.stderr:
            print(f"STDERR: {e.stderr}", file=sys.stderr)
        raise # Re-raise the exception for the caller to handle
    except FileNotFoundError:
        print(f"Error: Command not found: {cmd[0]}", file=sys.stderr)
        sys.exit(1) # Critical error, exit immediately

def get_report_filename(ts_file_relative_to_git_root):
    """
    Generates a unique filename for the conversion report based on the TS file path.
    Replaces slashes with underscores and appends '.txt'.
    Example: 'src/components/MyComponent.ts' -> 'src_components_MyComponent.ts.txt'
    """
    # Remove leading/trailing slashes and replace internal ones
    base_name = ts_file_relative_to_git_root.replace(os.sep, '_').replace('/', '_')
    return f"{base_name}.txt"

def generate_summary_report(conversion_results, report_dir):
    """
    Generates and prints a summary report of the conversion validation process.
    Also writes the summary to a file in the report directory.
    """
    print("\n" + "=" * 70)
    print("                 TypeScript Conversion Validation Summary Report")
    print("=" * 70)

    total_files = len(conversion_results)
    success_count = 0
    manual_check_required = []
    skipped_files = []
    ledit_failed_files = []
    other_issues = []

    for result in conversion_results:
        status = result['status'].lower()
        if "success" in status:
            success_count += 1
        elif "manual check required" in status or "no ledit output" in status or "parse error" in status:
            manual_check_required.append(result)
        elif "skipped" in status:
            skipped_files.append(result)
        elif "ledit command failed" in status:
            ledit_failed_files.append(result)
        else:
            other_issues.append(result)

    summary_lines = []
    summary_lines.append(f"Total Deleted JS/JSX Files Processed: {total_files}")
    summary_lines.append(f"Successfully Validated Conversions: {success_count}")
    summary_lines.append(f"Files Requiring Manual Check: {len(manual_check_required)}")
    summary_lines.append(f"Files Skipped (e.g., original content not found): {len(skipped_files)}")
    summary_lines.append(f"Files where Ledit Command Failed: {len(ledit_failed_files)}")
    if other_issues:
        summary_lines.append(f"Files with Other Uncategorized Issues: {len(other_issues)}")

    summary_lines.append("\nDetailed Breakdown:")

    if manual_check_required:
        summary_lines.append("\n--- Files Requiring Manual Check ---")
        for item in manual_check_required:
            summary_lines.append(f"  JS: {item['js_file']}")
            summary_lines.append(f"  TS: {item['ts_file']}")
            summary_lines.append(f"  Status: {item['status']}")
            summary_lines.append(f"  Reason: {item['reason']}")
            summary_lines.append(f"  Report: {item['report_path']}\n")

    if ledit_failed_files:
        summary_lines.append("\n--- Files Where Ledit Command Failed ---")
        for item in ledit_failed_files:
            summary_lines.append(f"  JS: {item['js_file']}")
            summary_lines.append(f"  TS: {item['ts_file']}")
            summary_lines.append(f"  Status: {item['status']}")
            summary_lines.append(f"  Reason: {item['reason']}")
            summary_lines.append(f"  Report: {item['report_path']}\n")

    if skipped_files:
        summary_lines.append("\n--- Skipped Files ---")
        for item in skipped_files:
            summary_lines.append(f"  JS: {item['js_file']}")
            summary_lines.append(f"  TS: {item['ts_file']}")
            summary_lines.append(f"  Status: {item['status']}")
            summary_lines.append(f"  Reason: {item['reason']}")
            summary_lines.append(f"  Report: {item['report_path']}\n")

    if other_issues:
        summary_lines.append("\n--- Files with Other Issues ---")
        for item in other_issues:
            summary_lines.append(f"  JS: {item['js_file']}")
            summary_lines.append(f"  TS: {item['ts_file']}")
            summary_lines.append(f"  Status: {item['status']}")
            summary_lines.append(f"  Reason: {item['reason']}")
            summary_lines.append(f"  Report: {item['report_path']}\n")

    summary_lines.append("\n" + "=" * 70)
    summary_lines.append("Next Steps:")
    summary_lines.append("1.  Review 'Files Requiring Manual Check': Examine the individual reports for these files in the 'conversion_report' directory. Address any identified issues in the corresponding TypeScript files.")
    summary_lines.append("2.  Investigate 'Files Where Ledit Command Failed': Check the reports for these files to understand why 'ledit' failed (e.g., environment issues, command syntax). Rerun the validation for these files if necessary after resolving the underlying problem.")
    summary_lines.append("3.  Verify Skipped Files: If any files were unexpectedly skipped, investigate the reasons provided in their reports.")
    summary_lines.append("4.  Commit Validated Conversions: Once you are confident that the TypeScript conversions are correct and complete for the successfully validated files, you can proceed with committing the new TypeScript files and the deletion of the original JavaScript files.")
    summary_lines.append("5.  Clean Up: You may delete the 'conversion_report' directory after reviewing all reports and taking necessary actions.")
    summary_lines.append("=" * 70)


    summary_output = "\n".join(summary_lines)
    print(summary_output)

    summary_filepath = os.path.join(report_dir, "conversion_summary_report.txt")
    try:
        with open(summary_filepath, "w") as f:
            f.write(summary_output)
        print(f"\nFull summary report saved to: {summary_filepath}")
    except IOError as e:
        print(f"Error: Could not write summary report to {summary_filepath}: {e}", file=sys.stderr)

    print("=" * 70)
    print("Validation process complete.")
    print("=" * 70)

def generate_html_summary_report(conversion_results, report_dir):
    """
    Generates an HTML summary report of the conversion validation process.
    """
    html_filepath = os.path.join(report_dir, "conversion_summary_report.html")

    # Prepare data similar to the text report
    total_files = len(conversion_results)
    success_count = 0
    manual_check_required = []
    skipped_files = []
    ledit_failed_files = []
    other_issues = []

    for result in conversion_results:
        status = result['status'].lower()
        if "success" in status:
            success_count += 1
        elif "manual check required" in status or "no ledit output" in status or "parse error" in status:
            manual_check_required.append(result)
        elif "skipped" in status:
            skipped_files.append(result)
        elif "ledit command failed" in status:
            ledit_failed_files.append(result)
        else:
            other_issues.append(result)

    html_content = []
    html_content.append("<!DOCTYPE html>")
    html_content.append("<html lang='en'>")
    html_content.append("<head>")
    html_content.append("    <meta charset='UTF-8'>")
    html_content.append("    <meta name='viewport' content='width=device-width, initial-scale=1.0'>")
    html_content.append("    <title>TypeScript Conversion Validation Summary</title>")
    html_content.append("    <style>")
    html_content.append("        body { font-family: Arial, sans-serif; line-height: 1.6; margin: 20px; background-color: #f4f4f4; color: #333; }")
    html_content.append("        .container { max-width: 960px; margin: auto; background: #fff; padding: 20px; border-radius: 8px; box-shadow: 0 0 10px rgba(0,0,0,0.1); }")
    html_content.append("        h1, h2, h3 { color: #0056b3; }")
    html_content.append("        .summary-box { background-color: #e9ecef; border: 1px solid #dee2e6; padding: 15px; margin-bottom: 20px; border-radius: 5px; }")
    html_content.append("        .summary-box p { margin: 5px 0; }")
    html_content.append("        .section { margin-bottom: 30px; padding: 15px; border: 1px solid #ddd; border-radius: 5px; background-color: #fff; }")
    html_content.append("        .file-item { border-bottom: 1px dashed #eee; padding-bottom: 10px; margin-bottom: 10px; }")
    html_content.append("        .file-item:last-child { border-bottom: none; margin-bottom: 0; padding-bottom: 0; }")
    html_content.append("        .status-success { color: green; font-weight: bold; }")
    html_content.append("        .status-manual { color: orange; font-weight: bold; }")
    html_content.append("        .status-failed { color: red; font-weight: bold; }")
    html_content.append("        .status-skipped { color: gray; font-weight: bold; }")
    html_content.append("        a { color: #007bff; text-decoration: none; }")
    html_content.append("        a:hover { text-decoration: underline; }")
    html_content.append("        .next-steps { background-color: #e6f7ff; border: 1px solid #91d5ff; padding: 15px; border-radius: 5px; margin-top: 30px; }")
    html_content.append("        .next-steps ol { margin-left: 20px; padding-left: 0; }")
    html_content.append("        .next-steps li { margin-bottom: 8px; }")
    html_content.append("    </style>")
    html_content.append("</head>")
    html_content.append("<body>")
    html_content.append("    <div class='container'>")
    html_content.append("        <h1>TypeScript Conversion Validation Summary Report</h1>")
    html_content.append("        <div class='summary-box'>")
    html_content.append(f"            <p><strong>Total Deleted JS/JSX Files Processed:</strong> {total_files}</p>")
    html_content.append(f"            <p><strong>Successfully Validated Conversions:</strong> <span class='status-success'>{success_count}</span></p>")
    html_content.append(f"            <p><strong>Files Requiring Manual Check:</strong> <span class='status-manual'>{len(manual_check_required)}</span></p>")
    html_content.append(f"            <p><strong>Files Skipped (e.g., original content not found):</strong> <span class='status-skipped'>{len(skipped_files)}</span></p>")
    html_content.append(f"            <p><strong>Files where Ledit Command Failed:</strong> <span class='status-failed'>{len(ledit_failed_files)}</span></p>")
    if other_issues:
        html_content.append(f"            <p><strong>Files with Other Uncategorized Issues:</strong> <span class='status-failed'>{len(other_issues)}</span></p>")
    html_content.append("        </div>")

    def add_file_section(title, file_list, status_class):
        if file_list:
            html_content.append(f"        <div class='section'>")
            html_content.append(f"            <h2>{title} ({len(file_list)} files)</h2>")
            for item in file_list:
                # Get relative path for the report link (assuming HTML and TXT reports are in the same directory)
                report_relative_path = os.path.basename(item['report_path'])
                html_content.append(f"            <div class='file-item'>")
                html_content.append(f"                <p><strong>JS:</strong> {item['js_file']}</p>")
                html_content.append(f"                <p><strong>TS:</strong> {item['ts_file']}</p>")
                html_content.append(f"                <p><strong>Status:</strong> <span class='{status_class}'>{item['status']}</span></p>")
                html_content.append(f"                <p><strong>Reason:</strong> {item['reason']}</p>")
                html_content.append(f"                <p><strong>Report:</strong> <a href='./{report_relative_path}'>{report_relative_path}</a></p>")
                html_content.append(f"            </div>")
            html_content.append(f"        </div>")

    add_file_section("Files Requiring Manual Check", manual_check_required, "status-manual")
    add_file_section("Files Where Ledit Command Failed", ledit_failed_files, "status-failed")
    add_file_section("Skipped Files", skipped_files, "status-skipped")
    add_file_section("Files with Other Issues", other_issues, "status-failed")

    html_content.append("        <div class='next-steps'>")
    html_content.append("            <h2>Next Steps</h2>")
    html_content.append("            <ol>")
    html_content.append("                <li><strong>Review 'Files Requiring Manual Check'</strong>: Examine the individual reports for these files in the <code>conversion_report</code> directory. Address any identified issues in the corresponding TypeScript files.</li>")
    html_content.append("                <li><strong>Investigate 'Files Where Ledit Command Failed'</strong>: Check the reports for these files to understand why <code>ledit</code> failed (e.g., environment issues, command syntax). Rerun the validation for these files if necessary after resolving the underlying problem.</li>")
    html_content.append("                <li><strong>Verify Skipped Files</strong>: If any files were unexpectedly skipped, investigate the reasons provided in their reports.</li>")
    html_content.append("                <li><strong>Commit Validated Conversions</strong>: Once you are confident that the TypeScript conversions are correct and complete for the successfully validated files, you can proceed with committing the new TypeScript files and the deletion of the original JavaScript files.</li>")
    html_content.append("                <li><strong>Clean Up</strong>: You may delete the <code>conversion_report</code> directory after reviewing all reports and taking necessary actions.</li>")
    html_content.append("            </ol>")
    html_content.append("        </div>")

    html_content.append("    </div>")
    html_content.append("</body>")
    html_content.append("</html>")

    try:
        with open(html_filepath, "w", encoding="utf-8") as f:
            f.write("\n".join(html_content))
        print(f"\nHTML summary report saved to: {html_filepath}")
    except IOError as e:
        print(f"Error: Could not write HTML summary report to {html_filepath}: {e}", file=sys.stderr)


def process_single_file(js_file_relative_to_git_root, git_root_dir, conversion_report_dir):
    """
    Processes a single deleted JS/JSX file to validate its TypeScript conversion.
    Returns a dictionary containing the result for this file.
    """
    print(f"  Processing deleted file: {js_file_relative_to_git_root}")

    # Initialize result dictionary for this file
    file_result = {
        'js_file': js_file_relative_to_git_root,
        'ts_file': 'N/A',
        'status': 'Unknown',
        'reason': 'Processing not completed.',
        'report_path': 'N/A'
    }

    ts_file_relative_to_git_root = ""
    if js_file_relative_to_git_root.endswith(".js"):
        ts_file_relative_to_git_root = js_file_relative_to_git_root[:-3] + ".ts"
    elif js_file_relative_to_git_root.endswith(".jsx"):
        ts_file_relative_to_git_root = js_file_relative_to_git_root[:-4] + ".tsx"
    else:
        file_result.update({
            'status': 'Skipped (Unsupported Extension)',
            'reason': f"File has unexpected extension: {os.path.splitext(js_file_relative_to_git_root)[1]}",
            'report_path': 'N/A'
        })
        print(f"  Warning: {file_result['reason']} for '{js_file_relative_to_git_root}'.", file=sys.stderr)
        return file_result

    report_filename = get_report_filename(ts_file_relative_to_git_root)
    report_filepath = os.path.join(conversion_report_dir, report_filename)
    file_result['report_path'] = report_filepath
    file_result['ts_file'] = ts_file_relative_to_git_root

    # Recovery support: Check if file already processed
    if os.path.exists(report_filepath):
        print(f"  Skipping '{js_file_relative_to_git_root}': Report already exists at '{report_filepath}'.")
        existing_status = "Unknown (Existing Report)"
        existing_reason = "Report already exists, status not re-evaluated."
        try:
            with open(report_filepath, 'r') as f:
                for line in f:
                    if line.startswith("Status:"):
                        existing_status = line[len("Status:"):].strip()
                    elif line.startswith("Reason:"):
                        existing_reason = line[len("Reason:"):].strip()
        except Exception:
            pass # Ignore errors reading existing report

        file_result.update({
            'status': existing_status,
            'reason': existing_reason
        })
        return file_result

    # Get the content of the original JS/JSX file from HEAD.
    js_file_contents = None
    try:
        git_show_result = subprocess.run(
            ["git", "show", f"HEAD:{js_file_relative_to_git_root}"],
            capture_output=True,
            text=True,
            check=False # Do not raise CalledProcessError here, handle return code
        )
        if git_show_result.returncode != 0:
            file_result.update({
                'status': "Skipped",
                'reason': (
                    f"Could not retrieve content for '{js_file_relative_to_git_root}' from HEAD. "
                    f"This might happen if the file was deleted in an earlier commit not covered by 'HEAD' or was never tracked. "
                    f"Git stderr: {git_show_result.stderr.strip()}"
                )
            })
            print(f"  Error: {file_result['reason']}", file=sys.stderr)
            # Write report for skipped file
            with open(report_filepath, "w") as f:
                f.write(f"Original JS/JSX File: {js_file_relative_to_git_root}\n")
                f.write(f"Corresponding TS/TSX File: {ts_file_relative_to_git_root}\n")
                f.write(f"Status: {file_result['status']}\n")
                f.write(f"Reason: {file_result['reason']}\n")
            return file_result
        js_file_contents = git_show_result.stdout.strip()

    except FileNotFoundError:
        # This case should ideally be caught by the initial git check in main,
        # but as a fallback, mark this file as failed.
        file_result.update({
            'status': 'Failed (Git Command Missing)',
            'reason': "'git' command not found during content retrieval. Please ensure it's installed and in your PATH."
        })
        print(f"  Critical Error: {file_result['reason']}", file=sys.stderr)
        return file_result
    except Exception as e:
        file_result.update({
            'status': 'Failed (Unexpected Error)',
            'reason': f"An unexpected error occurred while retrieving JS content: {e}"
        })
        print(f"  Critical Error: {file_result['reason']}", file=sys.stderr)
        return file_result


    # Construct absolute path for os.path.exists check and ledit command
    ts_file_absolute_path = os.path.join(git_root_dir, ts_file_relative_to_git_root)

    # Check if the corresponding TypeScript file exists
    if not os.path.exists(ts_file_absolute_path):
        file_result.update({
            'status': "Manual Check Required",
            'reason': (
                f"Corresponding TypeScript file '{ts_file_relative_to_git_root}' (absolute: {ts_file_absolute_path}) not found. "
                f"This indicates a potential issue with the conversion."
            )
        })
        print(f"  Critical Error: {file_result['reason']}", file=sys.stderr)

        # Write the critical error to the specific file's report
        try:
            with open(report_filepath, "w") as f:
                f.write(f"Original JS/JSX File: {js_file_relative_to_git_root}\n")
                f.write(f"Corresponding TS/TSX File: {ts_file_relative_to_git_root}\n")
                f.write(f"Status: {file_result['status']}\n")
                f.write(f"Reason: {file_result['reason']}\n")
                f.write("\nOriginal JS/JSX Content:\n")
                f.write(f"```javascript\n{js_file_contents}\n```\n")
        except IOError as e:
            print(f"  Error: Could not write to {report_filepath}: {e}", file=sys.stderr)

        return file_result

    print(f"  Corresponding TypeScript file found: {ts_file_relative_to_git_root} (absolute: {ts_file_absolute_path})")
    print("  Running ledit for comparison...")

    # Construct the ledit command arguments
    ledit_instruction_prompt = (
        f"Comparing the original, now deleted, file '{js_file_relative_to_git_root}' to the new TypeScript file '{ts_file_relative_to_git_root}'.\n"
        f"Was any functionality lost or are there any other issues?\n"
        f"Please provide the validation result in the following exact format (copy-paste and modify):\n"
        f"  Status: Success\n"
        f"  Reason: No issues found during manual comparison.\n"
        f"OR\n"
        f"  Status: Manual Check Required\n"
        f"  Reason: <Describe the issue here, e.g., 'Functionality X is missing', 'Type Y is incorrect'>\n\n"
        f"  Recommendation: <Describe the recommended fix or workaround here>\n"
        f"Your input will be saved directly to the report file for this conversion.\n\n"
        f"Original JS file content:\n```javascript\n{js_file_contents}\n```"
        f"Updated File: #{ts_file_relative_to_git_root}\n"
    )

    quoted_instruction_prompt = shlex.quote(ledit_instruction_prompt)
    quoted_ts_file_for_ledit = shlex.quote(ts_file_absolute_path)

    ledit_command_str = f"ledit code {quoted_instruction_prompt} -f {quoted_ts_file_for_ledit} --skip-prompt -m lambda-ai:deepseek-v3-0324"
    full_zsh_command = f"source ~/.zshrc && {ledit_command_str}"

    ledit_result = subprocess.run(
        ['zsh', '-c', full_zsh_command],
        capture_output=True,
        encoding='utf-8',
        check=False
    )

    ledit_output = ledit_result.stdout.strip()
    ledit_error = ledit_result.stderr.strip()

    status_from_ledit = "Unknown"
    reason_from_ledit = "No specific reason provided by ledit output."
    recommendation_from_ledit = "No recommendation provided."


    found_status = False
    found_reason = False
    found_recommendation = False

    if ledit_result.returncode != 0:
        status_from_ledit = "Ledit Command Failed"
        reason_from_ledit = (
            f"Ledit exited with code {ledit_result.returncode}. "
            f"Stderr: {ledit_error if ledit_error else 'None'}."
        )
    elif not ledit_output.strip():
        status_from_ledit = "Manual Check Required (No Ledit Output)"
        reason_from_ledit = "Ledit returned empty output. User might have exited without providing input."
    else:
        # Parse ledit_output for Status and Reason
        lines = ledit_output.splitlines()
        for line in lines:
            if line.strip().startswith("Status:"):
                status_from_ledit = line.strip()[len("Status:"):].strip()
                found_status = True
            elif line.strip().startswith("Reason:"):
                reason_from_ledit = line.strip()[len("Reason:"):].strip()
                found_reason = True
            elif line.strip().startswith("Recommendation:"):
                recommendation_from_ledit = line.strip()[len("Recommendation:"):].strip()
                found_recommendation = True
        # If status/reason not found, default to a generic "Manual Check Required"
        if not found_status or not found_reason:
            status_from_ledit = "Manual Check Required (Ledit Output Parse Error)"
            reason_from_ledit = f"Could not parse status/reason from ledit output. Raw output: {ledit_output}"


    # Write the ledit result to the specific file's report
    try:
        with open(report_filepath, "w") as f:
            f.write(f"Original JS/JSX File: {js_file_relative_to_git_root}\n")
            f.write(f"Corresponding TS/TSX File: {ts_file_relative_to_git_root}\n")
            f.write(f"Status: {status_from_ledit}\n")
            f.write(f"Reason: {reason_from_ledit}\n")
            if found_recommendation:
                f.write(f"Recommendation: {recommendation_from_ledit}\n")
            f.write("\nOriginal JS/JSX Content:\n")
            f.write(f"```javascript\n{js_file_contents}\n```\n")
            if ledit_error:
                f.write("\nLedit Stderr:\n")
                f.write(f"```\n{ledit_error}\n```\n")
    except IOError as e:
        print(f"  Error: Could not write to {report_filepath}: {e}", file=sys.stderr)

    print(f"  Finished processing: {js_file_relative_to_git_root}. Report saved to {report_filepath}")

    file_result.update({
        'status': status_from_ledit,
        'reason': reason_from_ledit,
        'recommendation': recommendation_from_ledit if 'recommendation_from_ledit' in locals() else "N/A"
    })
    return file_result


def main():
    print("Starting TypeScript conversion validation script...")

    # --- Determine Git root and CWD for robust path handling ---
    try:
        git_root_dir = run_command(["git", "rev-parse", "--show-toplevel"])
        current_working_dir = os.getcwd()
        print(f"Git Root Directory: {git_root_dir}")
        print(f"Current Working Directory: {current_working_dir}")

        # Ensure the script is run from within the Git repository
        if not current_working_dir.startswith(git_root_dir):
            print("Error: Current working directory is not within the Git repository root.", file=sys.stderr)
            print("Please run this script from within the Git repository.", file=sys.stderr)
            sys.exit(1)

    except subprocess.CalledProcessError:
        print("Error: Could not determine Git root directory. Is this a Git repository?", file=sys.stderr)
        sys.exit(1)
    except FileNotFoundError:
        print("Error: 'git' command not found. Please ensure it's installed and in your PATH.", file=sys.stderr)
        sys.exit(1)

    print("Looking for deleted .js or .jsx files in the current non-committed changeset...")

    # Get deleted .js and .jsx files from the current non-committed changeset.
    # Paths from 'git diff' are relative to the git repository root.
    try:
        git_diff_output = run_command(["git", "diff", "--name-status", "HEAD"])
    except subprocess.CalledProcessError:
        sys.exit(1)
    except FileNotFoundError:
        print("Error: 'git' command not found. Please ensure it's installed and in your PATH.", file=sys.stderr)
        sys.exit(1)

    deleted_files = []
    for line in git_diff_output.splitlines():
        if line.startswith("D\t"):
            filename = line[2:].strip()
            if filename.endswith(".js") or filename.endswith(".jsx"):
                deleted_files.append(filename) # These paths are relative to the git root

    if not deleted_files:
        print("No deleted .js or .jsx files found in the current non-committed changeset.")
        print("Script finished.")
        sys.exit(0)

    # Create conversion_report folder
    conversion_report_dir = os.path.join(current_working_dir, "conversion_report")
    print(f"Creating conversion_report directory: {conversion_report_dir}")
    try:
        os.makedirs(conversion_report_dir, exist_ok=True)
    except OSError as e:
        print(f"Error: Could not create conversion_report directory: {e}", file=sys.stderr)
        sys.exit(1)

    print("Found deleted JavaScript/JSX files. Processing them:")

    # List to store results for the final summary report
    conversion_results = []

    # Process files in parallel using ThreadPoolExecutor
    # The batch_size is now more about how many files to submit at once,
    # rather than how many to process sequentially.
    batch_size = 20 # Number of files to submit to the executor in one go
    max_workers = os.cpu_count() or 4 # Use CPU count or a reasonable default for threads

    # Using ThreadPoolExecutor for I/O-bound tasks (subprocess calls)
    with concurrent.futures.ThreadPoolExecutor(max_workers=max_workers) as executor:
        futures = []
        total_batches = len(deleted_files) // batch_size + (1 if len(deleted_files) % batch_size > 0 else 0)

        for i in range(0, len(deleted_files), batch_size):
            batch = deleted_files[i:i+batch_size]
            print(f"\n--- Submitting batch {i//batch_size + 1} of {total_batches} for processing ({len(batch)} files) ---")

            for js_file_relative_to_git_root in batch:
                # Submit each file to the executor
                future = executor.submit(process_single_file, js_file_relative_to_git_root, git_root_dir, conversion_report_dir)
                futures.append(future)

        # Collect results as they complete
        print("\n--- Collecting results from processed files ---")
        for future in concurrent.futures.as_completed(futures):
            try:
                result = future.result() # This will re-raise any exception from the worker thread
                conversion_results.append(result)
                # Print a concise update for completed files
                print(f"  Collected result for {result['js_file']} (Status: {result['status']})")
            except Exception as exc:
                # This catches exceptions that might have occurred during the execution of process_single_file
                # and were not handled within process_single_file itself.
                print(f"  An unexpected error occurred while processing a file: {exc}", file=sys.stderr)
                # It's good practice to add a generic error result if an unhandled exception occurs
                conversion_results.append({
                    'js_file': 'Unknown (Error in Thread)',
                    'ts_file': 'N/A',
                    'status': 'Failed (Unhandled Exception)',
                    'reason': f"An unhandled exception occurred: {exc}",
                    'report_path': 'N/A'
                })

    print("-" * 50)
    print("Script finished processing all deleted JavaScript/JSX files.")
    print(f"Please check the '{conversion_report_dir}' folder for detailed reports on each file.")

    # Generate and print the final summary report (text)
    generate_summary_report(conversion_results, conversion_report_dir)

    # Generate the HTML summary report
    generate_html_summary_report(conversion_results, conversion_report_dir)


if __name__ == "__main__":
    main()