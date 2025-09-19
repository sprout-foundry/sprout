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
  Interactive mode:     ./ledit
  Non-interactive:      ./ledit "your query here"
  Local inference:      ./ledit --local "your query"
  Custom model:         ./ledit --model=meta-llama/Meta-Llama-3.1-70B-Instruct "your query"
  Piped input:         echo "your query" | ./ledit
  Help:                ./ledit --help

INPUT FEATURES:
  - Arrow keys for navigation and command history
  - Backspace/Delete for editing
  - Tab for completion (where available)
  - Ctrl+C to exit

EXAMPLES:
  # Interactive mode
  ./ledit
  > Create a simple Go HTTP server in server.go
  
  # Non-interactive mode
  ./ledit "Create a simple Go HTTP server in server.go"
  
  # Multi-word prompts (use quotes)
  ./ledit "Fix the bug in main.go and add unit tests"
  
  # Local inference
  ./ledit --local "Create a Python calculator"
  
  # Use a different model
  ./ledit --model=meta-llama/Meta-Llama-3.1-70B-Instruct "Create a Python calculator"
  
  # Piped input
  echo "Fix the bug in main.go where the variable is undefined" | ./ledit

ENVIRONMENT:
  DEEPINFRA_API_KEY: API token for DeepInfra models
  OLLAMA_API_KEY: API token for Ollama Turbo models (optional, enables remote acceleration)
  LEDIT_DEBUG_HANG: Enable hang detection and monitoring (true/false)
  LEDIT_HANG_TIMEOUT: Timeout for hang detection (e.g., "5m", "300s")

MODEL OPTIONS:
  🏠 Local (Ollama):    Various models - FREE, runs locally
  ⚡ Turbo (Ollama):    Remote acceleration with datacenter-grade hardware
     • gpt-oss:20b - 128k context
     • gpt-oss:120b - 256k context  
     • deepseek-v3.1:671b - 128k context
  ☁️  Remote (DeepInfra): Multiple models available:
     • openai/gpt-oss-120b (default) - Uses harmony syntax
     • meta-llama/Meta-Llama-3.1-70B-Instruct - Standard format
     • microsoft/WizardLM-2-8x22B - Standard format
     • And many others - check DeepInfra docs for full list

SETUP:
  Local:  ollama pull <model-name>
  Turbo:  export OLLAMA_API_KEY="your_api_key_here" (get from https://ollama.com/settings/keys)
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

DEBUGGING HANGS:
  If ledit appears to hang or freeze:
  • Run: ./debug_hanging.sh (to analyze current state)
  • Enable hang detection: ./debug_hang_mode.sh
  • Check logs: less .ledit/workspace.log
  • Monitor progress: tail -f .ledit/runlogs/run-*.jsonl

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
