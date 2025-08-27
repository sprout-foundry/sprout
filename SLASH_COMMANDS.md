# Interactive Agent Slash Commands

The Ledit interactive agent now supports slash commands for quick non-prompt-based operations.

## Usage

Start the interactive agent:
```bash
ledit agent --ui
```

Then type any slash command in the input box.

## Available Slash Commands

### Basic Commands
- `/help`, `/h` - Show all available commands with examples
- `/quit`, `/q`, `/exit` - Quit the interactive agent
- `/clear`, `/c` - Clear the logs display

### Information Commands  
- `/status`, `/s` - Show current agent status (model, tokens, cost, logs)
- `/history`, `/hist` - Show recent command history (last 10 commands)
- `/workspace`, `/ws` - Show workspace information (directory, git status, config)
- `/config` - Show current configuration settings

### Toggle Commands
- `/logs`, `/l` - Toggle logs collapse/expand
- `/show`, `/showlogs` - Show/expand logs (force visible)
- `/hide`, `/hidelogs` - Hide/collapse logs (force hidden)
- `/progress`, `/p` - Toggle progress collapse/expand  
- `/model [name]` - Show current model or set new model for next execution

## Examples

```
/help                                    # Show help
/status                                  # Check current status
/show                                    # Expand logs to see output
/clear                                   # Clear logs
Add error handling to main.go            # Regular agent command
/hide                                    # Hide logs for more space
/history                                 # See previous commands
/model deepinfra:deepseek-ai/DeepSeek-V3.1   # Change model
Fix the authentication bug               # Another agent command
/quit                                    # Exit
```

## Features

- **Command History**: Tracks last 50 commands for easy reference
- **Auto-completion**: Commands support short aliases (e.g., `/h` for `/help`)
- **Context Aware**: Commands show relevant information about current workspace
- **Non-blocking**: Slash commands execute instantly without LLM calls
- **Error Handling**: Unknown commands show helpful error messages

## Navigation Keys

- **Enter**: Execute command or slash command
- **ESC**: Unfocus input (ESC again to quit)  
- **Ctrl+C**: Quit immediately
- **i**: Focus input when unfocused
- **l**: Toggle logs (keyboard shortcut)
- **p**: Toggle progress (keyboard shortcut)