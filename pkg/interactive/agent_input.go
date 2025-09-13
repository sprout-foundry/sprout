package interactive

import (
	"fmt"
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
)

// AgentInput provides an interactive input interface for the agent
type AgentInput struct {
	agent           *agent.Agent
	commandRegistry *commands.CommandRegistry
	rawInput        *BufferedInputHandler
	historyFile     string
	termUI          *TerminalUI
	enableFooter    bool
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

// New creates a new AgentInput instance
func New(chatAgent *agent.Agent, config *Config) (*AgentInput, error) {
	if config == nil {
		config = DefaultConfig()
	}

	// Create command registry
	commandRegistry := commands.NewCommandRegistry()

	// Create buffered input handler for better performance
	rawInput := NewBufferedInputHandler(config.Prompt)

	// Load history
	if err := rawInput.LoadHistory(config.HistoryFile); err != nil {
		// Non-fatal error, just log it
		fmt.Printf("Note: Could not load history: %v\n", err)
	}

	// Create terminal UI with footer
	var termUI *TerminalUI
	var err error

	// Allow disabling footer for performance debugging
	if os.Getenv("LEDIT_NO_FOOTER") != "1" {
		termUI, err = NewTerminalUI()
		if err != nil {
			// If we can't create the UI, continue without it
			termUI = nil
		}
	}
	enableFooter := termUI != nil

	return &AgentInput{
		agent:           chatAgent,
		commandRegistry: commandRegistry,
		rawInput:        rawInput,
		historyFile:     config.HistoryFile,
		termUI:          termUI,
		enableFooter:    enableFooter,
	}, nil
}

// Close cleans up resources
func (ai *AgentInput) Close() {
	// Save history
	if err := ai.rawInput.SaveHistory(ai.historyFile); err != nil {
		fmt.Printf("Warning: Could not save history: %v\n", err)
	}
}

// Run starts the interactive input loop
func (ai *AgentInput) Run() error {
	defer ai.Close()

	// Initially disable escape monitoring during normal input
	ai.agent.DisableEscMonitoring()

	// Setup terminal UI if enabled
	if ai.enableFooter && ai.termUI != nil {
		ai.termUI.Clear()
		ai.termUI.SetupScrollRegion()
		defer ai.termUI.ResetScrollRegion()

		// Set up callback for real-time stats updates
		ai.agent.SetStatsUpdateCallback(func(totalTokens int, totalCost float64) {
			// Debug logging
			if os.Getenv("DEBUG") == "1" {
				fmt.Fprintf(os.Stderr, "\n[DEBUG] Stats callback: tokens=%d, cost=%.4f\n", totalTokens, totalCost)
			}
			ai.updateFooterStats()
		})

		// Initialize footer with current stats
		ai.updateFooterStats()
	}

	// Show welcome message
	ai.showWelcomeMessage()

	// Set up signal handling for graceful shutdown
	interruptChannel := make(chan os.Signal, 1)
	signal.Notify(interruptChannel, syscall.SIGINT, syscall.SIGTERM)

	// Goroutine to handle graceful shutdown
	go func() {
		<-interruptChannel
		fmt.Println("\n👋 Goodbye!")
		ai.Close()
		os.Exit(0)
	}()

	// Main input loop
	for {
		// Read line with paste detection
		input, isPaste, err := ai.rawInput.ReadLine()
		if err != nil {
			if err.Error() == "interrupted" {
				fmt.Println("\n👋 Goodbye!")
				break
			}
			if err.Error() == "EOF" {
				fmt.Println("\n👋 Goodbye!")
				break
			}
			fmt.Printf("Input error: %v\n", err)
			break
		}

		// Handle pasted content
		if isPaste {
			fmt.Printf("📋 Paste detected (%d lines)\n", strings.Count(input, "\n")+1)
			input = ai.handlePastedContent(input)
		}

		// Process input
		ai.processSingleLine(input)
	}

	return nil
}

// handlePasteMode enters paste mode to collect multi-line input
func (ai *AgentInput) handlePasteMode() {
	fmt.Println("\n📋 Paste Mode - Paste your content and press Ctrl+D when done:")
	fmt.Println(strings.Repeat("─", 60))

	// Temporarily change prompt
	oldPrompt := ai.rawInput.prompt
	ai.rawInput.SetPrompt("")
	defer ai.rawInput.SetPrompt(oldPrompt)

	// Collect lines until EOF (Ctrl+D)
	var lines []string
	for {
		line, _, err := ai.rawInput.ReadLine()
		if err != nil { // EOF or error
			break
		}
		lines = append(lines, line)
	}

	if len(lines) == 0 {
		fmt.Println("❌ No content pasted")
		return
	}

	// Join lines and handle as pasted content
	content := strings.Join(lines, "\n")
	query := ai.handlePastedContent(content)

	// Process the generated query
	ai.processSingleLine(query)
}

// processSingleLine processes a single line of input
func (ai *AgentInput) processSingleLine(input string) {
	// Check if this is a file path
	if ai.looksLikeFilePath(input) {
		if fileQuery := ai.handleFileInput(input); fileQuery != "" {
			input = fileQuery
		}
	} else if !strings.Contains(input, "\n") {
		// Handle normal multiline input (lines ending with \)
		input = ai.handleMultilineInput(input)
	}

	input = strings.TrimSpace(input)
	if input == "" {
		return
	}

	// Handle exit commands
	if ai.isExitCommand(input) {
		fmt.Println("👋 Exiting interactive mode")
		ai.Close()
		os.Exit(0)
	}

	// Handle slash commands
	if strings.HasPrefix(input, "/") {
		if ai.handleSlashCommand(input) {
			// Exit was requested by slash command
			fmt.Println("👋 Exiting interactive mode")
			ai.Close()
			os.Exit(0)
		}
		return
	}

	// Check if this is a shell command
	if isShellCommand(input) {
		executeShellCommandDirectly(input)
		fmt.Println("")
		return
	}

	// Validate input length
	if !validateQueryLength(input) {
		fmt.Println("")
		return
	}

	// Process user request with agent
	if err := ai.processAgentRequest(input); err != nil {
		fmt.Printf("❌ Processing failed: %v\n", err)
	}
	fmt.Println("")
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
	fmt.Println("  • Type '/exit' or 'exit' to leave")
	fmt.Println("  • Press TAB after '/' for command completion")
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

	// Handle paste command specially
	if input == "/paste" {
		ai.handlePasteMode()
		return false
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

	// Update footer if enabled
	if ai.enableFooter && ai.termUI != nil {
		ai.updateFooterStats()
	}
}

// updateFooterStats updates the terminal footer with current stats
func (ai *AgentInput) updateFooterStats() {
	if ai.termUI == nil {
		return
	}

	modelName := ai.agent.GetModel()
	providerType := ai.agent.GetProviderType()
	providerName := api.GetProviderName(providerType)

	// Get token and cost info from agent
	totalTokens := ai.agent.GetTotalTokens()
	totalCost := ai.agent.GetTotalCost()

	// Debug logging
	if os.Getenv("DEBUG") == "1" {
		fmt.Fprintf(os.Stderr, "[DEBUG] updateFooterStats: tokens=%d, cost=%.4f\n", totalTokens, totalCost)
	}

	stats := &UIStats{
		Model:       modelName,
		Provider:    providerName,
		TotalTokens: totalTokens,
		TotalCost:   totalCost,
		LastUpdated: time.Now(),
	}

	ai.termUI.UpdateStats(stats)
}

// handlePastedContent handles multi-line pasted content by saving to a temp file
func (ai *AgentInput) handlePastedContent(content string) string {
	// Determine file extension based on content
	ext := ai.detectContentType(content)

	// Create temp file with appropriate extension
	tempDir := filepath.Join(os.TempDir(), "ledit-paste")
	os.MkdirAll(tempDir, 0755)

	tempFile, err := os.CreateTemp(tempDir, fmt.Sprintf("paste-*.%s", ext))
	if err != nil {
		fmt.Printf("❌ Failed to create temp file: %v\n", err)
		return content
	}

	_, err = tempFile.WriteString(content)
	tempFile.Close()

	if err != nil {
		fmt.Printf("❌ Failed to write temp file: %v\n", err)
		return content
	}

	// Count lines and size
	lines := strings.Count(content, "\n") + 1
	size := len(content)

	// Inform user
	fmt.Printf("\n📋 Pasted content saved (%d lines, %d bytes)\n", lines, size)
	fmt.Printf("📁 File: %s\n", tempFile.Name())
	fmt.Printf("📝 Type: %s\n", ai.getContentTypeDescription(ext))

	// Return a natural query that references the file
	return fmt.Sprintf("Please analyze this %s file: #%s", ai.getContentTypeDescription(ext), tempFile.Name())
}

// detectContentType determines the appropriate file extension based on content
func (ai *AgentInput) detectContentType(content string) string {
	trimmed := strings.TrimSpace(content)

	// JSON
	if (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
		(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {
		return "json"
	}

	// YAML
	if strings.HasPrefix(trimmed, "---") || strings.Contains(content, "\n  ") {
		return "yaml"
	}

	// XML/HTML
	if strings.HasPrefix(trimmed, "<") && strings.HasSuffix(trimmed, ">") {
		if strings.Contains(trimmed, "<html") {
			return "html"
		}
		return "xml"
	}

	// SQL
	if strings.Contains(strings.ToUpper(content), "SELECT ") ||
		strings.Contains(strings.ToUpper(content), "CREATE TABLE") {
		return "sql"
	}

	// Markdown
	if strings.Contains(content, "```") || strings.HasPrefix(trimmed, "#") {
		return "md"
	}

	// Programming languages
	if strings.Contains(content, "function ") || strings.Contains(content, "const ") {
		return "js"
	}
	if strings.Contains(content, "def ") && strings.Contains(content, "import ") {
		return "py"
	}
	if strings.Contains(content, "func ") && strings.Contains(content, "package ") {
		return "go"
	}
	if strings.Contains(content, "class ") && strings.Contains(content, "public ") {
		return "java"
	}

	// Default to txt
	return "txt"
}

// getContentTypeDescription returns a human-readable description of the content type
func (ai *AgentInput) getContentTypeDescription(ext string) string {
	descriptions := map[string]string{
		"json": "JSON data",
		"yaml": "YAML configuration",
		"xml":  "XML document",
		"html": "HTML document",
		"sql":  "SQL query",
		"md":   "Markdown document",
		"js":   "JavaScript code",
		"py":   "Python code",
		"go":   "Go code",
		"java": "Java code",
		"txt":  "text",
	}

	if desc, ok := descriptions[ext]; ok {
		return desc
	}
	return "text"
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
		ai.rawInput.SetPrompt("... > ")
		line, _, err := ai.rawInput.ReadLine()
		if err != nil {
			// On error, return what we have so far
			ai.rawInput.SetPrompt("🤖 > ")
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
	ai.rawInput.SetPrompt("🤖 > ")

	return strings.Join(lines, "\n")
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

// looksLikeFilePath checks if the input looks like a file path
func (ai *AgentInput) looksLikeFilePath(input string) bool {
	// Check for common file path patterns
	if strings.HasPrefix(input, "/") || strings.HasPrefix(input, "./") ||
		strings.HasPrefix(input, "../") || strings.HasPrefix(input, "~/") ||
		strings.Contains(input, ":\\") || // Windows paths
		(strings.Contains(input, "/") && (strings.Contains(input, ".") || strings.HasSuffix(input, "/"))) {
		return true
	}
	return false
}

// handleFileInput processes a file path input (for future drag-and-drop support)
func (ai *AgentInput) handleFileInput(filePath string) string {
	// Clean the file path
	filePath = strings.TrimSpace(filePath)
	filePath = strings.Trim(filePath, "'\"") // Remove quotes if present

	// Expand home directory if needed
	if strings.HasPrefix(filePath, "~/") {
		homeDir, _ := os.UserHomeDir()
		filePath = filepath.Join(homeDir, filePath[2:])
	}

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		// Don't treat as file path if it doesn't exist
		return ""
	}

	// Get file info
	fileInfo, _ := os.Stat(filePath)
	size := fileInfo.Size()

	fmt.Printf("\n📎 File detected: %s\n", filepath.Base(filePath))
	fmt.Printf("📏 Size: %d bytes\n", size)

	// Return a query that references the file
	return fmt.Sprintf("Please analyze this file: #%s", filePath)
}
