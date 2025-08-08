# Getting Started with ledit

## Basic Usage

### Create or modify files

```bash
# Create a file or set of files
ledit code "Create Python factorial function"

# Update a specific file
ledit code "Add error handling" --filename math.py

# Update a file with context from a different file (note that filename can be specified as `--filename` or `-f`)
ledit code "Update this logic with this requirements: #requirements.txt" -f math.py

# Leverage workspace context to reference and update the correct files automatically
ledit code "Update the math logic based on the new requirements text file. #WS"

# Use a different editing model
ledit code "Generate an accurate readme #WS" -m gemini:gemini-2.5-flash
```

### Fix code issues

```bash
# Attempt to fix a problem in your code by running a command and letting ledit attempt to fix the error
ledit fix "go build"
```

### View changes

```bash
# View the history of changes made by ledit and revert changes by prompt
ledit log
```

### Interrogate the code

```bash
# This command will start a chat based on the code in your workspace (current directory)
ledit question

# Ask a specific question directly
ledit question "Explain the main function in main.go"
```

### Commit staged changes

```bash
# Generate a conventional commit message and commit staged changes
ledit commit
```

### Review staged changes

```bash
# Perform an AI-powered code review on your currently staged Git changes
ledit review
```

## Key Features

-   **Smart Context**: Automatically builds and maintains an index of your workspace. An LLM selects the most relevant files to include as context for any given task using `#WORKSPACE` or `#WS`.
-   **Search Grounding**: Augment prompts with fresh information from the web using `#SG "query"`.
-   **Multi-Model Support**: Switch between LLM providers (e.g., OpenAI, Groq, Gemini, Ollama).
-   **Change Tracking**: Built-in version history and the ability to revert changes via `ledit log`.

## Advanced Usage

### Orchestration

**NOTE**: Currently the orchestration process should be considered in an alpha state and not ready for production use.

```bash
ledit process "Implement REST API with authentication"
```

### Multi-file Operations using workspace selection

```bash
ledit code "Update all API endpoints to v2 #WS"
```

### Model Comparison

```bash
ledit code "Explain this" --model openai:gpt-4o
ledit code "Explain this" --model groq:llama-3.3-70b
```

## Best Practices

1.  Start with small changes
2.  Review diffs before applying
3.  Use search grounding for current info
4.  Leverage workspace context for better results