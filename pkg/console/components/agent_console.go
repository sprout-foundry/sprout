package components

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	commands "github.com/alantheprice/ledit/pkg/agent_commands"
	tools "github.com/alantheprice/ledit/pkg/agent_tools"
	"github.com/alantheprice/ledit/pkg/console"
	"golang.org/x/term"
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

	// UI Handler
	uiHandler *console.UIHandler

	// State
	sessionStartTime time.Time
	totalTokens      int
	totalCost        float64
	prompt           string
	historyFile      string

	// JSON formatter for structured output
	jsonFormatter *JSONFormatter

	// Interrupt handling
	interruptChan chan string
	outputMutex   sync.Mutex
	ctrlCCount    int
	lastCtrlC     time.Time
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

	ac := &AgentConsole{
		BaseComponent:    base,
		agent:            agent,
		commandRegistry:  commands.NewCommandRegistry(),
		input:            input,
		footer:           footer,
		sessionStartTime: time.Now(),
		prompt:           config.Prompt,
		historyFile:      config.HistoryFile,
		interruptChan:    make(chan string, 1),
		outputMutex:      sync.Mutex{},
		jsonFormatter:    NewJSONFormatter(),
	}

	// Set the interrupt channel and output mutex on the agent
	agent.SetInterruptChannel(ac.interruptChan)
	agent.SetOutputMutex(&ac.outputMutex)

	// Set up Ctrl+C handler
	input.SetOnCancel(func() {
		ac.handleCtrlC()
	})

	return ac
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

	// Create and initialize UI handler
	ac.uiHandler = console.NewUIHandler(deps.Terminal)
	if err := ac.uiHandler.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize UI handler: %w", err)
	}

	// Register components with UI handler
	ac.uiHandler.RegisterComponent("agent", ac)
	ac.uiHandler.RegisterComponent("input", ac.input)
	ac.uiHandler.RegisterComponent("footer", ac.footer)

	// Initialize sub-components
	if err := ac.footer.Init(ctx, deps); err != nil {
		return err
	}

	// Set up stats update callback for real-time pricing updates
	if ac.agent != nil {
		ac.agent.SetStatsUpdateCallback(func(totalTokens int, totalCost float64) {
			ac.totalTokens = totalTokens
			ac.totalCost = totalCost
			ac.updateFooter()
		})
	}

	// Load history
	if ac.historyFile != "" {
		if err := ac.input.LoadHistory(ac.historyFile); err != nil {
			// Non-fatal, just log
			fmt.Printf("Note: Could not load history: %v\n", err)
		}
	}

	// Set up command callbacks
	ac.input.SetOnSubmit(ac.handleCommand)
	ac.input.SetOnCancel(ac.handleCtrlC)
	ac.input.SetOnTab(ac.handleAutocomplete)

	// Set up terminal with scroll regions
	if err := ac.setupTerminal(); err != nil {
		return fmt.Errorf("failed to setup terminal: %w", err)
	}

	// Update footer with initial state
	ac.updateFooter()

	// Set up signal handling to intercept Ctrl+C
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Handle signals in background
	go func() {
		for range sigChan {
			// Signal received, but we handle Ctrl+C through input component
			// This prevents the default behavior of killing the program
		}
	}()

	return nil
}

// Start starts the interactive loop
func (ac *AgentConsole) Start() error {
	// Note: We handle Ctrl+C in the input component instead of using signals
	// because the terminal is in raw mode
	ctx := context.Background()

	// Display welcome message
	ac.showWelcomeMessage()

	// Initial footer render
	ac.updateFooter()

	// Main input loop
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			// The input component handles cursor positioning
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

			// Update footer after processing
			ac.updateFooter()
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

	// Check if it's a shell command (common commands that users might type)
	if ac.isShellCommand(input) {
		fmt.Printf("\033[34m[shell]\033[0m Executing: %s\n", input)
		output, err := ac.executeShellCommand(input)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			fmt.Print(output)
			if !strings.HasSuffix(output, "\n") {
				fmt.Println()
			}
		}
		return nil
	}

	// Check for short/accidental input
	if len(input) <= 2 && !strings.Contains(input, "?") {
		fmt.Printf("Input too short. Did you mean to type something else? (Press Enter to continue or type your full query)\n")
		return nil
	}

	// Mark as processing
	// Lock output to prevent interleaving
	ac.outputMutex.Lock()

	// Clear the current input line and move to a new line for clean output
	fmt.Print("\r\033[K\n")

	// Show processing indicator
	fmt.Printf("🔄 Processing your request...\n")

	ac.outputMutex.Unlock()

	// Process synchronously to avoid formatting issues
	response, err := ac.agent.ProcessQueryWithContinuity(input)

	// Lock for output
	ac.outputMutex.Lock()
	defer ac.outputMutex.Unlock()

	if err != nil {
		fmt.Printf("\nError: %v\n", err)
	} else {
		// Update metrics
		ac.totalTokens = ac.agent.GetTotalTokens()
		ac.totalCost = ac.agent.GetTotalCost()

		// Display response with proper formatting
		if response != "" {
			// Clean up the response
			cleanResponse := strings.TrimSpace(response)
			if cleanResponse != "" {
				fmt.Printf("\n🎯 Agent Response:\n")

				// Check if this looks like JSON content
				if ac.jsonFormatter != nil && ac.jsonFormatter.DetectAndFormatJSON(cleanResponse) != cleanResponse {
					// Use JSON formatter for structured data
					formatted := ac.jsonFormatter.FormatModelResponse(cleanResponse)
					fmt.Println(formatted)
				} else {
					// For regular text/markdown, just print as-is to preserve formatting
					fmt.Println(cleanResponse)
				}
			}
		}

		// Print summary if we used tokens
		if ac.agent.GetTotalTokens() > 0 {
			ac.agent.PrintConciseSummary()
		}
	}

	// Update footer after processing
	ac.updateFooter()

	// Save history after each command
	if ac.historyFile != "" && ac.input != nil {
		if err := ac.input.SaveHistory(ac.historyFile); err != nil {
			// Don't show warning for every command, just log it silently
			_ = err
		}
	}

	// Add proper spacing before showing prompt again
	fmt.Printf("\n%s", ac.prompt)

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
		history := ac.input.GetHistory()
		fmt.Printf("History has %d items:\n", len(history))
		for i, line := range history {
			fmt.Printf("%3d: %s\n", i+1, line)
		}
		return nil

	case "debug":
		// Toggle debug mode for input
		fmt.Println("Debug mode: Press keys to see their codes, 'q' to exit debug")
		fd := int(os.Stdin.Fd())
		oldState, _ := term.MakeRaw(fd)
		defer term.Restore(fd, oldState)

		buf := make([]byte, 10)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				break
			}

			fmt.Printf("\r\nRead %d bytes: ", n)
			for i := 0; i < n; i++ {
				fmt.Printf("%02X ", buf[i])
			}

			// Check for 'q' to quit
			if n == 1 && buf[0] == 'q' {
				fmt.Println("\r\nExiting debug mode")
				break
			}
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

	// Get provider and model from agent
	provider := "unknown"
	model := "unknown"

	if ac.agent != nil {
		provider = ac.agent.GetProvider()
		model = ac.agent.GetModel()
	}

	// Update footer with current stats
	ac.footer.UpdateStats(model, provider, ac.totalTokens, ac.totalCost)

	// Trigger a render if needed
	if ac.footer.NeedsRedraw() {
		ac.footer.Render()
	}
}

func (ac *AgentConsole) showWelcomeMessage() {
	fmt.Printf(`Welcome to Ledit Agent! 🤖

I can help you with:
• Code analysis and generation
• File exploration and editing
• Shell command execution
• Project understanding

Type /help for available commands, or just start chatting!

`)
}

func (ac *AgentConsole) showHelp() {
	fmt.Println(`
Available Commands:
  /help, /?      - Show this help message
  /quit, /exit   - Exit the program
  /clear         - Clear the screen
  /history       - Show command history
  /stats         - Show session statistics
  /stop          - Stop current agent processing (during execution)
  
Agent Commands:`)

	if ac.commandRegistry != nil {
		for _, cmd := range ac.commandRegistry.ListCommands() {
			fmt.Printf("  /%-12s - %s\n", cmd.Name(), cmd.Description())
		}
	}

	fmt.Println(`
Tips:
• Common shell commands (ls, pwd, etc.) are executed directly
• Short inputs (1-2 chars) will prompt for confirmation
• Use /model to change the AI model
• Use /provider to switch between providers
• While agent is processing, you can:
  - Type additional instructions (will be queued)
  - Use /exit or /quit to exit immediately
  - Use /stop to stop current processing`)
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

func (ac *AgentConsole) setupTerminal() error {
	// Get terminal size
	_, height, err := ac.Terminal().GetSize()
	if err != nil {
		return err
	}

	// Clear screen and home cursor
	ac.Terminal().ClearScreen()

	// Initial footer render (before setting scroll region)
	if err := ac.footer.Render(); err != nil {
		return err
	}

	// Set up scroll region to leave room for footer (2 lines)
	// The content area is from line 1 to height-2
	if err := ac.Terminal().SetScrollRegion(1, height-2); err != nil {
		return err
	}

	// Move cursor to top of scroll region
	ac.Terminal().MoveCursor(1, 1)

	return nil
}

func (ac *AgentConsole) handleCtrlC() {
	// Recover from any panic
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "[PANIC] in handleCtrlC: %v\n", r)
		}
	}()

	now := time.Now()

	// If it's been more than 2 seconds since last Ctrl+C, reset counter
	if now.Sub(ac.lastCtrlC) > 2*time.Second {
		ac.ctrlCCount = 0
	}

	ac.ctrlCCount++
	ac.lastCtrlC = now

	if ac.ctrlCCount == 1 {
		// First Ctrl+C - show message on the same line
		fmt.Print("\r\033[K^C  💡 Press Ctrl+C again to exit\n")
		// Redraw prompt
		fmt.Print(ac.prompt)
	} else {
		// Second Ctrl+C - exit
		fmt.Println("🚪 Exiting...")

		// Restore terminal before exiting
		if ac.input != nil && ac.input.isRawMode {
			ac.input.restore()
		}

		ac.cleanup()
		os.Exit(0)
	}
}

func (ac *AgentConsole) cleanup() {
	// Reset scroll region
	ac.Terminal().ResetScrollRegion()

	// Save history
	if ac.historyFile != "" && ac.input != nil {
		if err := ac.input.SaveHistory(ac.historyFile); err != nil {
			fmt.Printf("Warning: Could not save history: %v\n", err)
		}
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

// isShellCommand checks if the input is likely a shell command
func (ac *AgentConsole) isShellCommand(input string) bool {
	// Split input into command and args
	parts := strings.Fields(input)
	if len(parts) == 0 {
		return false
	}

	cmd := parts[0]

	// Common shell commands that users might type
	shellCommands := []string{
		"ls", "ll", "la", "dir", "pwd", "cd", "cat", "less", "more", "head", "tail",
		"grep", "find", "echo", "clear", "history", "man", "which", "whoami",
		"ps", "top", "htop", "df", "du", "free", "uptime", "date", "cal",
		"cp", "mv", "rm", "mkdir", "rmdir", "touch", "chmod", "chown",
		"git", "go", "npm", "yarn", "python", "python3", "pip", "pip3",
		"docker", "kubectl", "make", "cargo", "rustc", "node", "deno",
		"vim", "vi", "nano", "emacs", "code", "subl", "tree", "wc",
		"curl", "wget", "ping", "netstat", "ss", "ip", "ifconfig",
		"brew", "apt", "apt-get", "yum", "snap", "flatpak",
		"sed", "awk", "cut", "sort", "uniq", "tr", "tee", "xargs",
		"kill", "killall", "pkill", "jobs", "fg", "bg", "nohup",
		"export", "source", "alias", "unalias", "type", "env",
		"tar", "gzip", "gunzip", "zip", "unzip", "bzip2", "bunzip2",
		"sha1sum", "sha256sum", "md5sum", "openssl", "base64",
		"systemctl", "service", "journalctl", "dmesg", "lsof", "strace",
		"diff", "comm", "paste", "join", "split", "csplit",
		"test", "true", "false", "yes", "seq", "expr", "bc",
		"screen", "tmux", "watch", "time", "timeout", "sleep",
		"mount", "umount", "fdisk", "mkfs", "fsck", "blkid",
		"id", "groups", "users", "who", "w", "last", "su", "sudo",
		"ssh", "scp", "rsync", "ftp", "sftp", "telnet", "nc", "nmap",
		"iptables", "ufw", "firewall-cmd", "tcpdump", "wireshark",
		"locate", "updatedb", "whereis", "file", "stat", "ln",
		"crontab", "at", "batch", "nohup", "nice", "renice",
		"patch", "diff", "git", "svn", "hg", "cvs",
		"gcc", "g++", "clang", "javac", "rustc", "go",
		"mysql", "psql", "sqlite3", "redis-cli", "mongo",
		"jq", "yq", "xmllint", "tig", "ag", "rg", "fd",
		"lspci", "lsusb", "lsblk", "lscpu", "lshw", "dmidecode",
		"modprobe", "lsmod", "rmmod", "insmod", "depmod",
		"hostnamectl", "timedatectl", "localectl", "loginctl",
	}

	// Check if it's a known shell command
	for _, shellCmd := range shellCommands {
		if cmd == shellCmd {
			return true
		}
	}

	// Check for paths (absolute or relative)
	if strings.HasPrefix(input, "/") || strings.HasPrefix(input, "./") || strings.HasPrefix(input, "../") {
		return true
	}

	// Check for command with common shell operators
	if strings.ContainsAny(input, "|&<>$") {
		return true
	}

	return false
}

// executeShellCommand executes a shell command and returns the output
func (ac *AgentConsole) executeShellCommand(command string) (string, error) {
	// Use the tools package to execute shell commands
	return tools.ExecuteShellCommand(command)
}

// Helper functions

// OnResize handles terminal resize events
func (ac *AgentConsole) OnResize(width, height int) {
	// Update scroll region
	ac.Terminal().SetScrollRegion(1, height-2)
}

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
