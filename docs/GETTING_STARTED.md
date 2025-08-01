# Getting Started with ledit

## Basic Usage

### Create or modify files

```bash
# Create a file or set of files with a command -- Note that that first command will initialize your environment and help you setup your configuration.
ledit code "Create Python factorial function"

# Update a specific file
ledit code "Add error handling" --filename math.py

# Update a file with context from a different file (note that filename can be specified as `--filename` or `-f`)
ledit code "Update this logic with this requirements: #requirements.txt" -f math.py

# Leverage workspace context to reference and update the correct files automatically
ledit code "Update the math logic based on the new requirements text file. #WS"
```

### Fix code issues

```bash
# Automatically fix common code issues (e.g., linting, formatting)
ledit fix "Fix all linting errors in src/"
```

### View changes

```bash
ledit log
```

### Interrogate the code

```bash

# This command will start a chat based on the code in your workspace (current directory)
ledit question
```

## Key Features

- **Smart Context**: Automatically includes relevant files using `#WORKSPACE`
- **Search Grounding**: Augment with web results using `#SG "query"`
- **Multi-Model Support**: Switch between LLM providers
- **Change Tracking**: Built-in version history

## Advanced Usage

### Orchestration

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

1. Start with small changes
2. Review diffs before applying
3. Use search grounding for current info
4. Leverage workspace context for better results
