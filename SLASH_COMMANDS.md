# Interactive Agent Slash Commands

The Ledit interactive agent supports slash commands for quick non-prompt-based operations during interactive mode.

## Usage

### Interactive Mode
```bash
ledit agent  # Starts interactive mode
```

## Available Slash Commands

The following slash commands are available in interactive mode:

### Basic Commands
- `/help` - Show all available commands with examples
- `/quit`, `/q`, `/exit` - Quit the interactive agent

### Provider and Model Management
- `/models` - List available models for current provider
- `/models select` - Interactive model selection
- `/models <model_id>` - Set model directly
- `/provider` - Show current provider information
- `/provider list` - List all available providers
- `/provider select` - Interactive provider selection

### Utility Commands
- `/shell <description>` - Generate and optionally execute shell commands from natural language
- `/exec <command>` - Execute shell commands directly
- `/commit` - Generate conventional commit messages for staged changes
- `/info` - Show current agent and workspace information
- `/init` - Initialize ledit configuration in current workspace

## Examples

### Basic Usage
```
ledit agent
🤖 > /help                                    # Show all available commands
🤖 > /provider list                           # List all providers
🤖 > /provider select                         # Switch providers interactively
🤖 > /models                                  # Show available models
🤖 > /models select                           # Select model interactively
🤖 > /info                                    # Show current status
🤖 > Add error handling to main.go            # Regular agent request
🤖 > /shell "list all go files"               # Generate shell command
🤖 > /exec ls -la                             # Execute command directly
🤖 > /commit                                  # Generate commit message
🤖 > /quit                                    # Exit
```

### Provider and Model Management
```
🤖 > /provider list                           # Shows: DeepInfra, OpenRouter, Ollama, etc.
🤖 > /provider select                         # Interactive provider selection
🤖 > /models                                  # Lists all models for current provider
🤖 > /models select                           # Interactive model selection
🤖 > /models deepseek-ai/DeepSeek-V3.1        # Set model directly
```

### Shell Command Generation
```
🤖 > /shell "find all Python files modified in last 7 days"
✅ Generated command: find . -name "*.py" -mtime -7
⚠️  Do you want to execute this command? (y/N): y
✅ Executing command...
[command output]
```

## Features

- **Tab Completion**: Slash commands support tab completion with common model and provider names
- **Interactive Selection**: Provider and model commands offer interactive selection menus
- **Shell Integration**: Generate shell commands from natural language descriptions
- **Direct Execution**: Execute shell commands directly without generating them first
- **Git Integration**: Generate conventional commit messages for staged changes
- **Context Aware**: Commands work with current workspace and provider settings
- **Non-blocking**: Slash commands execute instantly without LLM calls
- **Error Handling**: Unknown commands show helpful error messages with suggestions

## Navigation Keys

- **Enter**: Execute command or slash command
- **Tab**: Auto-complete slash commands
- **Ctrl+C**: Quit immediately
- **ESC**: Exit during certain interactive prompts (when available)