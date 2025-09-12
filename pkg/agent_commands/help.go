package commands

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/agent"
)

// HelpCommand implements the /help slash command
type HelpCommand struct {
	registry *CommandRegistry
}

// Name returns the command name
func (h *HelpCommand) Name() string {
	return "help"
}

// Description returns the command description
func (h *HelpCommand) Description() string {
	return "Show help information and available slash commands"
}

// Execute runs the help command
func (h *HelpCommand) Execute(args []string, chatAgent *agent.Agent) error {
	fmt.Print(`
🤖 Coder Agent

A command-line coding assistant using OpenAI's gpt-oss-120b model with 7 core tools:
- shell_command: Execute shell commands for exploration and testing  
- read_file: Read file contents
- write_file: Create new files
- edit_file: Modify existing files with precise string replacement
- add_todo: Create and track development tasks
- update_todo_status: Update progress on tracked tasks
- list_todos: View all current tasks and their status

USAGE:
  Interactive mode:     ./coder
  Non-interactive:      ./coder "your query here"
  Local inference:      ./coder --local "your query"
  Custom model:         ./coder --model=meta-llama/Meta-Llama-3.1-70B-Instruct "your query"
  Piped input:         echo "your query" | ./coder
  Help:                ./coder --help

INPUT FEATURES:
  - Arrow keys for navigation and command history
  - Backspace/Delete for editing
  - Tab for completion (where available)
  - Ctrl+C to exit

EXAMPLES:
  # Interactive mode
  ./coder
  > Create a simple Go HTTP server in server.go
  
  # Non-interactive mode
  ./coder "Create a simple Go HTTP server in server.go"
  
  # Multi-word prompts (use quotes)
  ./coder "Fix the bug in main.go and add unit tests"
  
  # Local inference
  ./coder --local "Create a Python calculator"
  
  # Use a different model
  ./coder --model=meta-llama/Meta-Llama-3.1-70B-Instruct "Create a Python calculator"
  
  # Piped input
  echo "Fix the bug in main.go where the variable is undefined" | ./coder

ENVIRONMENT:
  DEEPINFRA_API_KEY: API token for DeepInfra (if not set, uses local Ollama)

MODEL OPTIONS:
  🏠 Local (Ollama):    gpt-oss:20b - FREE, runs locally (14GB VRAM)
  ☁️  Remote (DeepInfra): Multiple models available:
     • openai/gpt-oss-120b (default) - Uses harmony syntax
     • meta-llama/Meta-Llama-3.1-70B-Instruct - Standard format
     • microsoft/WizardLM-2-8x22B - Standard format
     • And many others - check DeepInfra docs for full list

SETUP:
  Local:  ollama pull gpt-oss:20b
  Remote: export DEEPINFRA_API_KEY="your_api_key_here"

The agent follows a systematic exploration process and will autonomously:
- Explore your codebase using shell commands
- Read and understand relevant files
- Make precise modifications using the edit tool
- Create new files when needed
- Test and verify changes
- Continue iterating until the task is complete

Type 'help' during interactive mode for this help message.
Type 'exit' or 'quit' to end the session.

────────────────────────────────────────────────────────────────

🤖 SLASH COMMANDS:
  • Press TAB after typing '/' for tab completion
  • Type '/' and press ENTER for interactive command selection
`)

	// List all registered commands at the bottom for easy access
	commands := h.registry.ListCommands()
	for _, cmd := range commands {
		fmt.Printf("  /%s - %s\n", cmd.Name(), cmd.Description())
	}

	fmt.Println()

	return nil
}
