package components

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	api "github.com/alantheprice/ledit/pkg/agent_api"
	commands "github.com/alantheprice/ledit/pkg/agent_commands"
	"github.com/alantheprice/ledit/pkg/console"
)

// AgentConsole is the main console component for agent interactions
type AgentConsole struct {
	*console.BaseComponent

	// Core dependencies
	agent           *agent.Agent
	commandRegistry *commands.CommandRegistry

	// Sub-components
	input  *InputComponent
	footer *FooterComponent

	// State
	sessionStartTime time.Time
	totalTokens      int
	totalCost        float64
	prompt           string
	historyFile      string
}

// NewAgentConsole creates a new agent console
func NewAgentConsole(agent *agent.Agent, config *AgentConsoleConfig) *AgentConsole {
	if config == nil {
		config = DefaultAgentConsoleConfig()
	}

	base := console.NewBaseComponent("agent-console", "agent")

	// Create sub-components
	input := NewInputComponent("agent-input", config.Prompt)
	input.SetHistory(true).SetEcho(true)

	footer := NewFooterComponent()

	return &AgentConsole{
		BaseComponent:    base,
		agent:            agent,
		commandRegistry:  commands.NewCommandRegistry(),
		input:            input,
		footer:           footer,
		sessionStartTime: time.Now(),
		prompt:           config.Prompt,
		historyFile:      config.HistoryFile,
	}
}

// AgentConsoleConfig holds configuration
type AgentConsoleConfig struct {
	HistoryFile string
	Prompt      string
}

// DefaultAgentConsoleConfig returns default config
func DefaultAgentConsoleConfig() *AgentConsoleConfig {
	homeDir, _ := os.UserHomeDir()
	return &AgentConsoleConfig{
		HistoryFile: homeDir + "/.ledit_agent_history",
		Prompt:      "🤖 > ",
	}
}

// Init initializes the component
func (ac *AgentConsole) Init(ctx context.Context, deps console.Dependencies) error {
	if err := ac.BaseComponent.Init(ctx, deps); err != nil {
		return err
	}

	// Initialize sub-components
	if err := ac.input.Init(ctx, deps); err != nil {
		return err
	}

	if err := ac.footer.Init(ctx, deps); err != nil {
		return err
	}

	// Load history
	if ac.input.historyEnabled && ac.historyFile != "" {
		if history, err := loadHistory(ac.historyFile); err == nil {
			for _, line := range history {
				ac.input.AddToHistory(line)
			}
		}
	}

	// Set up command callbacks
	ac.input.SetOnSubmit(ac.handleCommand)
	ac.input.SetOnCancel(ac.handleCancel)
	ac.input.SetOnTab(ac.handleAutocomplete)

	// Update footer with initial state
	ac.updateFooter()

	return nil
}

// Start starts the interactive loop
func (ac *AgentConsole) Start() error {
	// Set up signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		<-sigChan
		cancel()
		ac.cleanup()
		os.Exit(0)
	}()

	// Main input loop
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			line, _, err := ac.input.ReadLine()
			if err != nil {
				if err.Error() == "EOF" {
					return nil
				}
				return err
			}

			// Process the line
			if err := ac.processInput(line); err != nil {
				fmt.Printf("Error: %v\n", err)
			}
		}
	}
}

func (ac *AgentConsole) processInput(input string) error {
	input = strings.TrimSpace(input)
	if input == "" {
		return nil
	}

	// Check for commands
	if strings.HasPrefix(input, "/") {
		return ac.handleCommand(input)
	}

	// Regular agent interaction using GenerateResponse
	messages := []api.Message{
		{Role: "user", Content: input},
	}

	response, err := ac.agent.GenerateResponse(messages)
	if err != nil {
		return err
	}

	// Update metrics (would need to track these separately)
	// For now, just update footer
	ac.updateFooter()

	// Display response
	fmt.Println(response)

	return nil
}

func (ac *AgentConsole) handleCommand(input string) error {
	// Parse command
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return nil
	}

	cmd := strings.TrimPrefix(parts[0], "/")
	args := parts[1:]

	// Handle built-in commands
	switch cmd {
	case "help", "?":
		ac.showHelp()
		return nil
	case "quit", "exit", "q":
		ac.cleanup()
		os.Exit(0)
	case "clear":
		fmt.Print("\033[2J\033[H")
		return nil
	case "history":
		for _, line := range ac.input.GetHistory() {
			fmt.Println(line)
		}
		return nil
	case "stats":
		ac.showStats()
		return nil
	}

	// Check command registry
	if ac.commandRegistry != nil {
		if cmdHandler, exists := ac.commandRegistry.GetCommand(cmd); exists {
			return cmdHandler.Execute(args, ac.agent)
		}
	}

	return fmt.Errorf("unknown command: /%s", cmd)
}

func (ac *AgentConsole) handleCancel() {
	// Ctrl+C handling - already prints ^C in input component
}

func (ac *AgentConsole) handleAutocomplete(text string, pos int) []string {
	if strings.HasPrefix(text, "/") {
		// Command completion
		prefix := strings.TrimPrefix(text, "/")
		var suggestions []string

		// Built-in commands
		builtins := []string{"help", "quit", "exit", "clear", "history", "stats"}
		for _, cmd := range builtins {
			if strings.HasPrefix(cmd, prefix) {
				suggestions = append(suggestions, "/"+cmd)
			}
		}

		// Registry commands
		if ac.commandRegistry != nil {
			for _, cmd := range ac.commandRegistry.ListCommands() {
				if strings.HasPrefix(cmd.Name(), prefix) {
					suggestions = append(suggestions, "/"+cmd.Name())
				}
			}
		}

		return suggestions
	}

	return nil
}

func (ac *AgentConsole) updateFooter() {
	if ac.footer == nil {
		return
	}

	// Calculate session duration
	duration := time.Since(ac.sessionStartTime)

	// Update footer content via direct method calls
	// In a real implementation, this would use the state manager
	// For now, we'll just format the duration
	_ = formatDuration(duration)

	// The footer component would subscribe to state changes
	// and update itself accordingly
}

func (ac *AgentConsole) showHelp() {
	fmt.Println(`
Available Commands:
  /help, /?      - Show this help message
  /quit, /exit   - Exit the program
  /clear         - Clear the screen
  /history       - Show command history
  /stats         - Show session statistics
  
Agent Commands:`)

	if ac.commandRegistry != nil {
		for _, cmd := range ac.commandRegistry.ListCommands() {
			fmt.Printf("  /%-12s - %s\n", cmd.Name(), cmd.Description())
		}
	}
}

func (ac *AgentConsole) showStats() {
	duration := time.Since(ac.sessionStartTime)

	// Get provider and model from agent config
	provider := "unknown"
	model := "unknown"

	// These would come from the agent's configuration
	// For now, just display what we have

	fmt.Printf(`
Session Statistics:
  Duration: %s
  Tokens:   %d
  Cost:     $%.4f
  Provider: %s
  Model:    %s
`, formatDuration(duration), ac.totalTokens, ac.totalCost, provider, model)
}

func (ac *AgentConsole) cleanup() {
	// Save history
	if ac.historyFile != "" && ac.input != nil {
		history := ac.input.GetHistory()
		saveHistory(ac.historyFile, history)
	}

	// Clean up components
	if ac.input != nil {
		ac.input.Cleanup()
	}
	if ac.footer != nil {
		ac.footer.Cleanup()
	}
}

func (ac *AgentConsole) Cleanup() error {
	ac.cleanup()
	return ac.BaseComponent.Cleanup()
}

// Helper functions

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

func loadHistory(filename string) ([]string, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	var history []string
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			history = append(history, line)
		}
	}

	return history, nil
}

func saveHistory(filename string, history []string) error {
	data := strings.Join(history, "\n")
	return os.WriteFile(filename, []byte(data), 0600)
}
