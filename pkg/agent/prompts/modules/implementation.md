# Implementation Request Module

## Request Type: Coding & Building

**Indicators**: "add", "fix", "implement", "create", "build", "change", "update", "refactor"

**Strategy**: Discover → plan → implement → verify

## Execution Approach

### Step 1: Task Planning
- For complex tasks (3+ steps), use add_todos to break down work
- Mark todos as "in_progress" when starting, "completed" when done
- Track progress with list_todos

**Todo Usage**:
- Use when: 3+ steps, multiple files, keywords like "implement", "build", "refactor"
- Don't use for: Simple questions, single file operations, basic info requests

### Step 2: Discovery & Context
- Discover file structure and existing code patterns
- Locate relevant code and dependencies
- Batch read ALL needed files in ONE tool call array

**Discovery Pattern**:
```bash
# File structure
ls -la

# Find relevant files
find . -name "*.go" -type f | head -10

# Locate existing patterns
grep -r "similar_function" --include="*.go" .
```

### Step 3: Implementation
- Make changes using edit_file or write_file
- Follow existing code patterns and styles
- Test changes with shell_command after each modification

### Step 4: Completion Verification
- Run builds and tests to ensure success
- Mark all todos as completed
- Verify all requirements are met
- Provide completion summary

## Implementation Patterns

**File Operations**: Always batch read files in a single tool call array
**Code Changes**: Make targeted, tested changes
**Verification**: Build and test after changes
**Completion**: Clear summary when task is finished