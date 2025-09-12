package interactive

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	api "github.com/alantheprice/ledit/pkg/agent_api"
	commands "github.com/alantheprice/ledit/pkg/agent_commands"
	tools "github.com/alantheprice/ledit/pkg/agent_tools"
	"github.com/chzyer/readline"
)

// AgentInput provides an interactive input interface for the agent
type AgentInput struct {
	agent           *agent.Agent
	commandRegistry *commands.CommandRegistry
	readline        *readline.Instance
	historyFile     string
}

// Config holds configuration for the agent input
type Config struct {
	HistoryFile string
	Prompt      string
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	homeDir, _ := os.UserHomeDir()
	return &Config{
		HistoryFile: homeDir + "/.ledit_agent_history",
		Prompt:      "🤖 > ",
	}
}

// InterruptHandler is a custom interrupt handler for readline
type InterruptHandler struct{}

func (ih *InterruptHandler) OnInterrupt() bool {
	// Return true to continue, false to exit
	fmt.Print("\n💭 Use Ctrl+D or type 'exit' to quit. Press Ctrl+C again to force quit.\n")
	return true
}

// New creates a new AgentInput instance
func New(chatAgent *agent.Agent, config *Config) (*AgentInput, error) {
	if config == nil {
		config = DefaultConfig()
	}

	// Create command registry
	commandRegistry := commands.NewCommandRegistry()

	// Set up readline with history and tab completion
	rl, err := readline.NewEx(&readline.Config{
		Prompt:          config.Prompt,
		HistoryFile:     config.HistoryFile,
		HistoryLimit:    1000,
		AutoComplete:    createSlashCompleter(),
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to initialize readline: %w", err)
	}

	return &AgentInput{
		agent:           chatAgent,
		commandRegistry: commandRegistry,
		readline:        rl,
		historyFile:     config.HistoryFile,
	}, nil
}

// Close cleans up resources
func (ai *AgentInput) Close() {
	if ai.readline != nil {
		ai.readline.Close()
	}
}

// Run starts the interactive input loop
func (ai *AgentInput) Run() error {
	defer ai.Close()

	// Initially disable escape monitoring during normal input
	ai.agent.DisableEscMonitoring()

	// Show welcome message
	ai.showWelcomeMessage()

	// Set up signal handling for graceful shutdown and window resize
	interruptChannel := make(chan os.Signal, 1)
	signal.Notify(interruptChannel, syscall.SIGINT, syscall.SIGTERM)

	// Goroutine to handle graceful shutdown
	go func() {
		<-interruptChannel
		fmt.Println("\n👋 Goodbye!")
		os.Exit(0)
	}()

	// Main input loop
	for {
		input, err := ai.readline.Readline()
		if err != nil {
			if err == readline.ErrInterrupt {
				fmt.Println("\n👋 Goodbye!")
				break
			}
			fmt.Printf("Input error: %v\n", err)
			break
		}

		// Detect if this looks like the start of a multi-line paste
		if ai.looksLikePastedContent(input) {
			input = ai.handlePastedContent(input)
		} else {
			// Handle normal multiline input (lines ending with \)
			input = ai.handleMultilineInput(input)
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		// Handle exit commands
		if ai.isExitCommand(input) {
			fmt.Println("👋 Exiting interactive mode")
			return nil
		}

		// Handle slash commands
		if strings.HasPrefix(input, "/") {
			if ai.handleSlashCommand(input) {
				return nil // Exit was requested
			}
			continue
		}

		// Check if this is a shell command
		if isShellCommand(input) {
			executeShellCommandDirectly(input)
			fmt.Println("")
			continue
		}

		// Check if input contains code blocks
		if ai.looksLikeCode(input) {
			fmt.Println("💻 Code input detected")
		}

		// Validate input length
		if !validateQueryLength(input) {
			fmt.Println("")
			continue
		}

		// Process user request with agent
		if err := ai.processAgentRequest(input); err != nil {
			fmt.Printf("❌ Processing failed: %v\n", err)
		}
		fmt.Println("")

	}

	return nil
}

// showWelcomeMessage displays the welcome message with model info
func (ai *AgentInput) showWelcomeMessage() {
	providerType := ai.agent.GetProviderType()
	providerName := api.GetProviderName(providerType)
	modelName := ai.agent.GetModel()

	// Show a nice header
	fmt.Println("\n" + strings.Repeat("═", 60))
	fmt.Println("🤖 Ledit Agent - Interactive Mode")
	fmt.Println(strings.Repeat("═", 60))

	if providerType == api.OllamaClientType {
		fmt.Printf("📡 Model: %s via %s (local)\n", modelName, providerName)
	} else {
		fmt.Printf("📡 Model: %s via %s\n", modelName, providerName)
	}

	fmt.Println("\n📚 Quick Tips:")
	fmt.Println("  • Type '/quit' or 'exit' to leave")
	fmt.Println("  • Press TAB after '/' for command completion")
	fmt.Println("  • Type '/' and press ENTER for interactive command selection")
	fmt.Println("  • End lines with '\\' for multiline input")
	fmt.Println("  • Note: Multi-line pastes execute line by line (use \\ to join)")
	if providerType != api.OllamaClientType {
		fmt.Println("  • Press ESC during processing to inject new instructions")
	}
	fmt.Println(strings.Repeat("─", 60) + "\n")
}

// isExitCommand checks if the input is an exit command
func (ai *AgentInput) isExitCommand(input string) bool {
	return input == "exit" || input == "quit" || input == "q"
}

// handleSlashCommand processes slash commands and returns true if exit was requested
func (ai *AgentInput) handleSlashCommand(input string) bool {
	// If user typed just "/" show the command selector
	if input == "/" {
		selectedCmd, err := commands.ShowCommandSelector(ai.commandRegistry)
		if err != nil {
			// Selection was cancelled
			return false
		}
		input = selectedCmd
	}

	// Handle quit commands specially (immediate exit)
	if strings.HasPrefix(input, "/quit") || strings.HasPrefix(input, "/exit") || strings.HasPrefix(input, "/q") {
		fmt.Println("👋 Exiting interactive mode")
		return true
	}

	// Use CommandRegistry for all other slash commands
	err := ai.commandRegistry.Execute(input, ai.agent)
	if err != nil {
		fmt.Printf("❌ Command error: %v\n", err)
		fmt.Println("💡 Type '/help' to see available commands")
	}

	return false
}

// processAgentRequest handles the main agent processing
func (ai *AgentInput) processAgentRequest(input string) error {
	// Show processing header with visual separator
	fmt.Println("\n" + strings.Repeat("─", 60))
	fmt.Printf("🔄 Processing: %s\n", truncateString(input, 50))
	fmt.Println(strings.Repeat("─", 60))

	// Enable escape key monitoring during agent processing
	ai.agent.EnableEscMonitoring()

	// Start time tracking
	startTime := time.Now()

	// Execute the agent command
	response, err := ai.agent.ProcessQueryWithContinuity(input)

	// Disable escape key monitoring after agent processing
	ai.agent.DisableEscMonitoring()

	// Calculate duration
	duration := time.Since(startTime)

	if err != nil {
		return err
	}

	// Show response with visual formatting
	fmt.Printf("\n🎯 Agent Response:\n")
	fmt.Println(strings.Repeat("─", 60))
	fmt.Printf("%s\n", response)
	fmt.Println(strings.Repeat("─", 60))

	// Print enhanced summary with model, cost, and duration
	fmt.Println(strings.Repeat("─", 60))
	ai.printEnhancedSummary(duration)
	fmt.Println(strings.Repeat("─", 60))
	return nil
}

// truncateString truncates a string to maxLen characters and adds ellipsis if needed
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%.0fms", d.Seconds()*1000)
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

// printEnhancedSummary displays an enhanced summary with model, cost, and duration
func (ai *AgentInput) printEnhancedSummary(duration time.Duration) {
	modelName := ai.agent.GetModel()
	providerType := ai.agent.GetProviderType()
	providerName := api.GetProviderName(providerType)

	// Print the standard cost summary first
	ai.agent.PrintConciseSummary()

	// Add additional status info
	fmt.Printf("📡 Model: %s via %s\n", modelName, providerName)
	fmt.Printf("⏱️  Duration: %s\n", formatDuration(duration))
	fmt.Printf("🕐 Time: %s\n", time.Now().Format("15:04:05"))
	fmt.Println("✅ Completed")
}

// looksLikePastedContent detects if input looks like pasted multi-line content
func (ai *AgentInput) looksLikePastedContent(input string) bool {
	// Check for common indicators of pasted content:
	// - Starts with common code/data patterns
	// - Contains multiple sentences without being a question
	// - Has structured data markers

	// JSON/YAML/XML
	if strings.HasPrefix(input, "{") || strings.HasPrefix(input, "[") ||
		strings.HasPrefix(input, "---") || strings.HasPrefix(input, "<") {
		return true
	}

	// Code indicators
	if strings.Contains(input, "function ") || strings.Contains(input, "def ") ||
		strings.Contains(input, "class ") || strings.Contains(input, "import ") ||
		strings.Contains(input, "const ") || strings.Contains(input, "var ") {
		return true
	}

	// Markdown/structured text
	if strings.HasPrefix(input, "#") || strings.HasPrefix(input, "```") ||
		strings.HasPrefix(input, "- ") || strings.HasPrefix(input, "* ") {
		return true
	}

	return false
}

// handlePastedContent handles multi-line pasted content by saving to a temp file
func (ai *AgentInput) handlePastedContent(firstLine string) string {
	// Collect lines that arrive quickly (indicating paste)
	lines := []string{firstLine}
	timeout := time.After(50 * time.Millisecond) // Short timeout for paste detection

	// Try to collect more lines
	collectingLines := true
	for collectingLines {
		select {
		case <-timeout:
			collectingLines = false
		default:
			// Set a very short deadline for the next read
			lineChan := make(chan string, 1)
			errChan := make(chan error, 1)

			go func() {
				if line, err := ai.readline.Readline(); err == nil {
					lineChan <- line
				} else {
					errChan <- err
				}
			}()

			select {
			case line := <-lineChan:
				lines = append(lines, line)
			case <-errChan:
				collectingLines = false
			case <-time.After(10 * time.Millisecond):
				collectingLines = false
			}
		}
	}

	// If we only got one line, return it as-is
	if len(lines) == 1 {
		return firstLine
	}

	// Multiple lines detected - save to temp file
	content := strings.Join(lines, "\n")

	// Create temp file
	tempDir := filepath.Join(os.TempDir(), "ledit-paste")
	os.MkdirAll(tempDir, 0755)

	tempFile, err := ioutil.TempFile(tempDir, "paste-*.txt")
	if err != nil {
		fmt.Printf("❌ Failed to create temp file: %v\n", err)
		return content // Fall back to returning the content directly
	}

	_, err = tempFile.WriteString(content)
	tempFile.Close()

	if err != nil {
		fmt.Printf("❌ Failed to write temp file: %v\n", err)
		return content
	}

	// Inform user and return file reference
	fmt.Printf("📋 Multi-line paste detected (%d lines, %d bytes)\n", len(lines), len(content))
	fmt.Printf("📁 Saved to: %s\n", tempFile.Name())
	fmt.Println("💡 Processing as file reference...")

	// Return a query that references the file
	return fmt.Sprintf("I pasted some content. Please analyze and respond to: #%s", tempFile.Name())
}

// handleMultilineInput handles continuation lines ending with backslash
func (ai *AgentInput) handleMultilineInput(input string) string {
	// Check if line ends with backslash (continuation)
	if !strings.HasSuffix(strings.TrimSpace(input), "\\") {
		return input
	}

	// Remove the trailing backslash
	lines := []string{strings.TrimSuffix(strings.TrimSpace(input), "\\")}

	// Keep reading lines until we get one without a trailing backslash
	for {
		// Change prompt to indicate continuation
		ai.readline.SetPrompt("... > ")
		line, err := ai.readline.Readline()
		if err != nil {
			// On error, return what we have so far
			ai.readline.SetPrompt("🤖 > ")
			return strings.Join(lines, "\n")
		}

		trimmedLine := strings.TrimSpace(line)
		if strings.HasSuffix(trimmedLine, "\\") {
			// Another continuation line
			lines = append(lines, strings.TrimSuffix(trimmedLine, "\\"))
		} else {
			// Final line
			lines = append(lines, line)
			break
		}
	}

	// Restore original prompt
	ai.readline.SetPrompt("🤖 > ")

	return strings.Join(lines, "\n")
}

// looksLikeCode checks if the input appears to contain code
func (ai *AgentInput) looksLikeCode(input string) bool {
	codeIndicators := []string{
		"```", "func ", "function ", "class ", "def ", "import ", "const ", "var ", "let ",
		"if (", "for (", "while (", "return ", "package ", "public ", "private ",
		"{", "}", "()", "[]", "=>", "->",
	}

	for _, indicator := range codeIndicators {
		if strings.Contains(input, indicator) {
			return true
		}
	}

	return false
}

// createSlashCompleter creates a tab completion function for slash commands
func createSlashCompleter() *readline.PrefixCompleter {
	return readline.NewPrefixCompleter(
		readline.PcItem("/help"),
		readline.PcItem("/quit"),
		readline.PcItem("/q"),
		readline.PcItem("/exit"),
		readline.PcItem("/init"),
		readline.PcItem("/models",
			readline.PcItem("select"),
			// Add some common model completions
			readline.PcItem("deepseek-ai/DeepSeek-V3.1"),
			readline.PcItem("deepseek-ai/DeepSeek-V3"),
			readline.PcItem("anthropic/claude-4-sonnet"),
			readline.PcItem("anthropic/claude-4-opus"),
			readline.PcItem("meta-llama/Meta-Llama-3.1-70B-Instruct"),
			readline.PcItem("google/gemini-2.5-pro"),
		),
		readline.PcItem("/provider",
			readline.PcItem("select"),
			readline.PcItem("list"),
		),
		readline.PcItem("/shell"),
		readline.PcItem("/exec"),
		readline.PcItem("/info"),
		readline.PcItem("/commit"),
		// Change tracking commands
		readline.PcItem("/changes"),
		readline.PcItem("/status"),
		readline.PcItem("/log"),
		readline.PcItem("/rollback"),
		// MCP commands
		readline.PcItem("/mcp",
			readline.PcItem("add"),
			readline.PcItem("remove"),
			readline.PcItem("list"),
			readline.PcItem("test"),
			readline.PcItem("help"),
		),
	)
}

// isShellCommand checks if the input looks like a shell command
func isShellCommand(input string) bool {
	input = strings.TrimSpace(input)

	// Common shell command prefixes
	shellPrefixes := []string{
		"ls", "cd", "pwd", "cat", "echo", "grep", "find", "git",
		"go ", "python", "node", "npm", "yarn", "docker", "kubectl",
		"curl", "wget", "ssh", "scp", "mv", "cp", "rm", "mkdir",
		"touch", "chmod", "chown", "ps", "top", "kill", "df", "du",
		"tar", "zip", "unzip", "gzip", "gunzip", "head", "tail",
		"diff", "patch", "make", "gcc", "g++", "clang", "javac",
		"rustc", "cargo", "dotnet", "php", "ruby", "perl", "awk",
		"sed", "cut", "sort", "uniq", "wc", "tee", "xargs", "env",
		"export", "source", "./", ".\\", "#", "$",
	}

	for _, prefix := range shellPrefixes {
		if strings.HasPrefix(input, prefix) {
			return true
		}
	}

	// Check for shell operators and redirection
	if strings.Contains(input, " && ") || strings.Contains(input, " || ") ||
		strings.Contains(input, " | ") {
		return true
	}

	// Check for redirection operators with surrounding spaces or at word boundaries
	if strings.Contains(input, " > ") || strings.Contains(input, " >> ") ||
		strings.Contains(input, " < ") || strings.HasSuffix(input, ">") ||
		strings.HasPrefix(input, ">") || strings.HasSuffix(input, "<") ||
		strings.HasPrefix(input, "<") {
		return true
	}

	return false
}

// executeShellCommandDirectly executes a shell command directly
func executeShellCommandDirectly(command string) {
	fmt.Printf("⚡ Direct shell command detected: %s\n", command)

	result, err := tools.ExecuteShellCommand(command)
	if err != nil {
		fmt.Printf("❌ Command failed: %v\n", err)
		fmt.Printf("Output: %s\n", result)
	} else {
		fmt.Printf("✅ Command executed successfully:\n")
		fmt.Printf("Output: %s\n", result)
	}
}

// validateQueryLength validates query length and prompts for confirmation
func validateQueryLength(query string) bool {
	queryLen := len(strings.TrimSpace(query))

	// Absolute minimum: reject anything under 3 characters
	if queryLen < 3 {
		fmt.Printf("❌ Query too short (%d characters). Minimum 3 characters required.\n", queryLen)
		return false
	}

	// For queries under 20 characters, ask for confirmation
	if queryLen < 20 {
		fmt.Printf("⚠️  Short query detected (%d characters): \"%s\"\n", queryLen, query)
		fmt.Print("Are you sure you want to process this? (y/N): ")

		var response string
		fmt.Scanln(&response)
		response = strings.ToLower(strings.TrimSpace(response))
		if response != "y" && response != "yes" {
			fmt.Println("❌ Query cancelled.")
			return false
		}
	}

	return true
}
