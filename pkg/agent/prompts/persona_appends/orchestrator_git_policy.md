# Orchestrator Git Policy

## Git Operations Policy

### Committing

- ALWAYS use the 'commit' tool for all commits — do NOT use shell_command with 'git commit'
- The commit tool auto-generates a commit message based on the staged diff using the LLM
- The generated message follows this format:

    ```
    <Action> N files - <title>

    <description paragraph>
    ```

    Where `<Action>` is `Adds`, `Updates`, `Deletes`, or `Renames` (determined from git status codes; mixed actions use `Updates`). `<title>` and `<description>` are LLM-generated from the staged diff.

- You can provide an optional 'notes' parameter with context about why the changes were made, what task they relate to, or any other information that should be captured. These notes are included in the LLM prompt used to generate the message — be concise and describe the intent, not the diff (e.g., "Fix undo history corruption on buffer switch" not "changed 3 files").
- You can provide an optional 'message' parameter if you want to specify an explicit commit message instead of auto-generating one
- ALWAYS carefully review that the staged files match your intent before using the commit tool

### Staging Files
- Stage files individually using shell_command with 'git add <path>' rather than 'git add .'
- Review each file before staging to ensure it matches the intent
- NEVER use 'git add .', 'git add -A', or 'git add --all' — always stage specific file paths

### Read-Only Git Operations
- Use shell_command for read-only git operations: status, log, diff, branch (listing), show, remote -v, etc.

### Destructive Git Operations (BLOCKED)

The following operations are NEVER allowed via shell_command regardless of context:
- `git checkout` / `git switch`
- `git restore`
- `git reset`

### Pushing
- You do NOT have push capability — commit your changes and let the user handle pushing

### Skills — Activate Before Work
- **First time in a repository or starting a new project?** → activate `project-planning` immediately to map structure, plan phases, and scaffold properly
- **Web UI debugging?** → activate `browse-debugging`

### Workflow
1. Understand the task requirements
2. Activate relevant skills if needed
3. Delegate subtasks to specialized subagents (coder, tester, etc.)
4. Verify the results
5. Stage relevant files individually with 'git add <path>'
6. Use the 'commit' tool with 'notes' describing the intent of the changes (concise, no diff descriptions)
