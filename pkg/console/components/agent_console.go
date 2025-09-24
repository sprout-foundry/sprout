package components

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	commands "github.com/alantheprice/ledit/pkg/agent_commands"
	tools "github.com/alantheprice/ledit/pkg/agent_tools"
	"github.com/alantheprice/ledit/pkg/console"
	"github.com/alantheprice/ledit/pkg/filesystem"
	"golang.org/x/term"
)

// AgentConsole is the main console component for agent interactions
type AgentConsole struct {
	*console.BaseComponent

	// Core dependencies
	agent           *agent.Agent
	commandRegistry *commands.CommandRegistry

	// Sub-components
	inputManager       *InputManager
	historyManager     *HistoryManager
	footer             *FooterComponent
	streamingFormatter *StreamingFormatter

	// UI Handler
	uiHandler *console.UIHandler

	// Layout management
	autoLayoutManager *console.AutoLayoutManager

	// Console buffer for output management and resize handling
	consoleBuffer *console.ConsoleBuffer

	// State
	sessionStartTime time.Time
	totalTokens      int
	totalCost        float64
	prompt           string
	historyFile      string

	// Content area tracking
	currentContentLine int // Current line within content area (0-based relative to content top)
	contentRegionTop   int // Cached top of content region

	// JSON formatter for structured output
	jsonFormatter *JSONFormatter

	// Interrupt handling
	interruptChan chan string
	outputMutex   sync.Mutex
	ctrlCCount    int
	lastCtrlC     time.Time

	// Concurrent processing state
	isProcessing    bool
	processingMutex sync.RWMutex
	agentDoneChan   chan struct{}

	// Streaming TPS tracking
	streamingStartTime  time.Time
	streamingTokenCount int
	isStreaming         bool
}

// NewAgentConsole creates a new agent console
func NewAgentConsole(agent *agent.Agent, config *AgentConsoleConfig) *AgentConsole {
	if config == nil {
		config = DefaultAgentConsoleConfig()
	}

	base := console.NewBaseComponent("agent-console", "agent")

	// Create sub-components
	footer := NewFooterComponent()

	// Create input manager for concurrent input handling
	inputManager := NewInputManager(config.Prompt)

	// Create history manager
	historyManager := NewHistoryManager(config.HistoryFile, 1000)

	// Create auto layout manager
	autoLayoutManager := console.NewAutoLayoutManager()

	ac := &AgentConsole{
		BaseComponent:     base,
		agent:             agent,
		commandRegistry:   commands.NewCommandRegistry(),
		inputManager:      inputManager,
		historyManager:    historyManager,
		footer:            footer,
		autoLayoutManager: autoLayoutManager,
		consoleBuffer:     console.NewConsoleBuffer(10000), // 10,000 line buffer
		sessionStartTime:  time.Now(),
		prompt:            config.Prompt,
		historyFile:       config.HistoryFile,
		interruptChan:     make(chan string, 1),
		outputMutex:       sync.Mutex{},
		jsonFormatter:     NewJSONFormatter(),
		agentDoneChan:     make(chan struct{}, 1),
		processingMutex:   sync.RWMutex{},
	}

	// Create streaming formatter
	ac.streamingFormatter = NewStreamingFormatter(&ac.outputMutex)
	ac.streamingFormatter.SetConsoleBuffer(ac.consoleBuffer)

	// Set custom output function to use our safe print method
	ac.streamingFormatter.SetOutputFunc(func(text string) {
		// Use our simplified safe print (no complex positioning)
		ac.safePrint("%s", text)
	})

	// Set the interrupt channel and output mutex on the agent
	agent.SetInterruptChannel(ac.interruptChan)
	agent.SetOutputMutex(&ac.outputMutex)

	// Set up input manager callbacks
	inputManager.SetCallbacks(
		ac.handleInputFromManager,
		ac.handleInterruptFromManager,
	)

	return ac
}

// setupLayoutComponents registers components with the layout manager
func (ac *AgentConsole) setupLayoutComponents() {
	// Register footer (bottom, lowest priority = furthest from content)
	ac.autoLayoutManager.RegisterComponent("footer", &console.ComponentInfo{
		Name:     "footer",
		Position: "bottom",
		Height:   4,  // Will be updated dynamically
		Priority: 10, // Lower priority = further from content (at very bottom)
		Visible:  true,
		ZOrder:   100,
	})

	// Register input (bottom, higher priority = closer to content)
	ac.autoLayoutManager.RegisterComponent("input", &console.ComponentInfo{
		Name:     "input",
		Position: "bottom",
		Height:   1,
		Priority: 20, // Higher priority = closer to content (above footer)
		Visible:  true,
		ZOrder:   90,
	})

	// Content region is automatically managed
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

	// Register components with layout manager FIRST
	ac.setupLayoutComponents()

	// Initialize auto layout manager AFTER components are registered
	if err := ac.autoLayoutManager.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize layout manager: %w", err)
	}

	// Create and initialize UI handler
	ac.uiHandler = console.NewUIHandler(deps.Terminal)
	if err := ac.uiHandler.Initialize(); err != nil {
		return fmt.Errorf("failed to initialize UI handler: %w", err)
	}

	// Register components with UI handler
	ac.uiHandler.RegisterComponent("agent", ac)
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
		if err := ac.historyManager.LoadFromFile(); err != nil {
			// Non-fatal, just log
			ac.writeTextWithRawModeFix(fmt.Sprintf("Note: Could not load history: %v\n", err))
		}
	}

	// Initialize console buffer with current terminal width
	width, _, err := ac.Terminal().GetSize()
	if err == nil {
		ac.consoleBuffer.SetTerminalWidth(width)
	}

	// Set up terminal with scroll regions
	if err := ac.setupTerminal(); err != nil {
		return fmt.Errorf("failed to setup terminal: %w", err)
	}

	// Update footer with initial state
	ac.updateFooter()

	// Initialize git and path info for footer
	ac.initializeFooterInfo()

	// Set up signal handling to intercept Ctrl+C
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Handle signals in background
	go func() {
		for range sigChan {
			// When agent is processing, interrupt it
			// When just at prompt, handle through input component
			ac.handleCtrlC()
		}
	}()

	return nil
}

// Start starts the interactive loop with concurrent input handling
func (ac *AgentConsole) Start() error {
	// Start the concurrent input manager
	if err := ac.inputManager.Start(); err != nil {
		return fmt.Errorf("failed to start input manager: %w", err)
	}

	// Ensure cleanup when done
	defer ac.inputManager.Stop()

	// Layout manager handles positioning automatically

	// Display welcome message
	ac.showWelcomeMessage()

	// Initial footer render
	ac.updateFooter()

	// Show initial help about the new features
	ac.safePrint("\n💡 You can now type while the agent is processing!\n")
	ac.safePrint("   Press Enter to send new prompts immediately, even during agent responses.\n")
	ac.safePrint("   Use Ctrl+C to interrupt the agent.\n\n")

	// Main event loop - non-blocking
	ctx := context.Background()

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-ac.inputManager.GetInterruptChannel():
			// Handle interrupt from input manager
			// The callback already handles this, just continue

		case <-ac.agentDoneChan:
			// Agent processing completed, update footer
			ac.updateFooter()

		default:
			// Small sleep to prevent busy waiting
			time.Sleep(10 * time.Millisecond)
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
		ac.safePrint("\033[34m[shell]\033[0m Executing: %s\n", input)
		output, err := ac.executeShellCommand(input)
		if err != nil {
			ac.safePrint("Error: %v\n", err)
		} else {
			ac.safePrint("%s", output)
			if !strings.HasSuffix(output, "\n") {
				ac.safePrint("\n")
			}
		}
		return nil
	}

	// Check for short/accidental input
	if len(input) <= 2 && !strings.Contains(input, "?") {
		ac.safePrint("Input too short. Did you mean to type something else? (Press Enter to continue or type your full query)\n")
		return nil
	}

	// Mark as processing
	// Lock output to prevent interleaving
	ac.outputMutex.Lock()

	// Clear the current input line and move to a new line for clean output
	ac.safePrint("\r\033[K\n")

	// Show processing indicator
	ac.safePrint("🔄 Processing your request...\n")

	ac.outputMutex.Unlock()

	// Set up streaming with our formatter
	// Reset formatter for new session
	ac.streamingFormatter.Reset()

	// Check if agent is properly initialized
	if ac.agent == nil {
		ac.safePrint("❌ Error: Agent not initialized\n")
		return nil
	}

	// Enable streaming with our formatter callback for all providers
	ac.agent.EnableStreaming(func(content string) {
		ac.streamingFormatter.Write(content)
		// Update TPS estimation during streaming
		ac.updateStreamingTPS(content)
	})

	// Set flush callback to force flush buffered content when needed
	ac.agent.SetFlushCallback(func() {
		ac.streamingFormatter.ForceFlush()
	})

	// Ensure cleanup
	streamingFinalized := false
	defer func() {
		// Only finalize if we actually streamed content and haven't already finalized
		if ac.streamingFormatter.HasProcessedContent() && !streamingFinalized {
			ac.streamingFormatter.Finalize()
			streamingFinalized = true
		}
		ac.agent.DisableStreaming()
		// Reset streaming TPS tracking
		ac.resetStreamingTPS()
	}()

	// Process synchronously to avoid formatting issues
	response, err := ac.agent.ProcessQueryWithContinuity(input)

	// Force flush any remaining streaming content immediately after processing
	// This ensures all narrative text is displayed before final output processing
	ac.streamingFormatter.ForceFlush()

	// Lock for output
	ac.outputMutex.Lock()
	defer ac.outputMutex.Unlock()

	if err != nil {
		// Clear any partial streaming output
		ac.safePrint("\r\033[K")
		ac.safePrint("\n❌ Error: %v\n", err)
	} else {
		// Check if the response contains error indicators (from handleAPIFailure)
		// This happens when API fails but conversation is preserved
		if strings.Contains(response, "⚠️ API request failed") ||
			strings.Contains(response, "⚠️ **API Request Failed") ||
			strings.Contains(response, "API Request Failed") ||
			strings.Contains(response, "❌ **Model Error**") {
			// The error message was returned as content, not an error
			// Check if streaming actually processed any content
			if !ac.streamingFormatter.HasProcessedContent() {
				// Nothing was streamed, clear the processing message and show error
				ac.safePrint("\r\033[K")
			}
			// Format and print the error response
			ac.streamingFormatter.Write(response)
			ac.streamingFormatter.Finalize()
			streamingFinalized = true
		} else if response != "" && !ac.streamingFormatter.HasProcessedContent() {
			// We have a response but nothing was streamed
			// This can happen if streaming failed immediately
			ac.safePrint("\r\033[K")
			ac.safePrint("\n%s\n", response)
		} else if response != "" && ac.streamingFormatter.HasProcessedContent() {
			// With the fix, response should be empty when streaming processed content
			// If not empty, it's likely a provider that doesn't support streaming properly
			// Just ignore the duplicate content in this case
		}

		// Update metrics
		ac.totalTokens = ac.agent.GetTotalTokens()
		ac.totalCost = ac.agent.GetTotalCost()

		// Print summary if we used tokens
		if ac.agent.GetTotalTokens() > 0 {
			// Check if we need spacing before summary
			if ac.streamingFormatter.HasProcessedContent() && !ac.streamingFormatter.EndedWithNewline() {
				ac.safePrint("\n")
			}
			ac.safePrint("\n")
			ac.agent.PrintConciseSummary()
		}
	}

	// Reset Ctrl+C counter after successful processing
	ac.ctrlCCount = 0

	// Update footer after processing
	ac.updateFooter()

	// Save history after each command
	if ac.historyFile != "" && ac.historyManager != nil {
		if err := ac.historyManager.SaveToFile(); err != nil {
			// Don't show warning for every command, just log it silently
			_ = err
		}
	}

	// Ensure we're on a new line before returning
	// The prompt will be shown by ReadLine
	if ac.streamingFormatter.HasProcessedContent() && !ac.streamingFormatter.EndedWithNewline() {
		ac.safePrint("\n")
	}

	// Flush stdout to ensure all output is visible before input
	os.Stdout.Sync()

	// Small delay to ensure terminal is ready
	time.Sleep(50 * time.Millisecond)

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
		// Clear the console buffer
		ac.consoleBuffer.Clear()

		// Clear only the content area within the scroll region
		ac.Terminal().SaveCursor()
		_, height, _ := ac.Terminal().GetSize()
		// Clear each line in the scroll region
		for i := 1; i <= height-2; i++ {
			ac.Terminal().MoveCursor(1, i)
			ac.Terminal().ClearLine()
		}
		ac.Terminal().MoveCursor(1, 1)

		// Clear conversation history
		if ac.agent != nil {
			ac.agent.ClearConversationHistory()
			ac.safePrint("🧹 Screen and conversation history cleared\n")
		}
		return nil
	case "history":
		history := ac.historyManager.GetHistory()
		ac.writeTextWithRawModeFix(fmt.Sprintf("History has %d items:\n", len(history)))
		for i, line := range history {
			ac.writeTextWithRawModeFix(fmt.Sprintf("%3d: %s\n", i+1, line))
		}
		return nil

	case "debug":
		// Toggle debug mode for input
		ac.writeTextWithRawModeFix("Debug mode: Press keys to see their codes, 'q' to exit debug\n")
		fd := int(os.Stdin.Fd())
		oldState, _ := term.MakeRaw(fd)
		defer term.Restore(fd, oldState)

		buf := make([]byte, 10)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				break
			}

			ac.writeTextWithRawModeFix(fmt.Sprintf("\r\nRead %d bytes: ", n))
			for i := 0; i < n; i++ {
				ac.writeTextWithRawModeFix(fmt.Sprintf("%02X ", buf[i]))
			}

			// Check for 'q' to quit
			if n == 1 && buf[0] == 'q' {
				ac.writeTextWithRawModeFix("\r\nExiting debug mode\n")
				break
			}
		}
		return nil
	case "stats":
		ac.showStats()
		return nil
	case "debug-layout":
		ac.showLayoutDebug()
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
		builtins := []string{"help", "quit", "exit", "clear", "history", "stats", "debug-layout"}
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
	iteration := 0
	contextTokens := 0
	maxContextTokens := 0
	if ac.agent != nil {
		provider = ac.agent.GetProvider()
		model = ac.agent.GetModel()
		iteration = ac.agent.GetCurrentIteration()
		contextTokens = ac.agent.GetCurrentContextTokens()
		maxContextTokens = ac.agent.GetMaxContextTokens()
	}

	// Update footer with current stats - use agent's total tokens to avoid double counting
	actualTotalTokens := ac.totalTokens
	if ac.agent != nil {
		actualTotalTokens = ac.agent.GetTotalTokens()
	}
	ac.footer.UpdateStats(model, provider, actualTotalTokens, ac.totalCost, iteration, contextTokens, maxContextTokens)

	// Force render to ensure tokens update is shown
	if err := ac.footer.Render(); err != nil {
		// Non-fatal, log if debug
		fmt.Fprintf(os.Stderr, "Warning: Footer render failed: %v\n", err)
	}
}

func (ac *AgentConsole) showWelcomeMessage() {
	ac.safePrint(`Welcome to Ledit Agent! 🤖

I can help you with:
• Code analysis and generation
• File exploration and editing
• Shell command execution
• Project understanding

✨ NEW FEATURES:
• Real-time input: Type while I'm processing!
• Input injection: Send new prompts mid-conversation!
• Interrupt support: Use Ctrl+C to stop processing

Type /help for available commands, or just start chatting!

`)
}

func (ac *AgentConsole) showHelp() {
	ac.writeTextWithRawModeFix(`
Available Commands:
  /help, /?      - Show this help message
  /quit, /exit   - Exit the program
  /clear         - Clear screen and conversation history
  /history       - Show command history
  /stats         - Show session statistics
  /debug-layout  - Show layout manager debug information
  /stop          - Stop current agent processing (during execution)
  
Real-time Features:
  • Type while agent is processing - input will be injected into conversation
  • Press Ctrl+C once to interrupt agent processing
  • Press Ctrl+C twice to exit the program
  • Input field is always visible at the bottom
  
Agent Commands:`)

	if ac.commandRegistry != nil {
		for _, cmd := range ac.commandRegistry.ListCommands() {
			ac.writeTextWithRawModeFix(fmt.Sprintf("  /%-12s - %s\n", cmd.Name(), cmd.Description()))
		}
	}

	ac.writeTextWithRawModeFix(`
Tips:
• Conversations continue between prompts - context is preserved
• Use /clear to start a fresh conversation
• Common shell commands (ls, pwd, etc.) are executed directly
• Short inputs (1-2 chars) will prompt for confirmation
• Use /model to change the AI model
• Use /providers to switch between providers
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

	ac.writeTextWithRawModeFix(fmt.Sprintf(`
Session Statistics:
  Duration: %s
  Tokens:   %d
  Cost:     $%.4f
  Provider: %s
  Model:    %s
`, formatDuration(duration), ac.totalTokens, ac.totalCost, provider, model))
}

func (ac *AgentConsole) showLayoutDebug() {
	ac.writeTextWithRawModeFix("\n=== Layout Manager Debug ===\n")
	ac.autoLayoutManager.PrintDebugLayout()
	ac.writeTextWithRawModeFix("\n")
}

// updateStreamingTPS estimates tokens per second during streaming
func (ac *AgentConsole) updateStreamingTPS(content string) {
	now := time.Now()

	// Initialize streaming tracking on first call
	if !ac.isStreaming {
		ac.isStreaming = true
		ac.streamingStartTime = now
		ac.streamingTokenCount = 0
	}

	// Better token estimation: ~3.3 chars per token for realistic text
	// This accounts for spaces, punctuation, and typical word lengths
	estimatedTokens := float64(len(content)) / 3.3
	if estimatedTokens < 0.25 && len(content) > 0 {
		estimatedTokens = 0.25 // Minimum fractional token for any content
	}

	// Accumulate streaming tokens
	ac.streamingTokenCount += int(estimatedTokens)

	// TODO: Fix token counting - don't double count streaming tokens
	// Total tokens should come from agent.GetTotalTokens() only
	// ac.totalTokens += int(estimatedTokens)

	// Update footer less frequently to allow proper TPS calculation
	elapsed := now.Sub(ac.streamingStartTime).Seconds()
	if elapsed > 0.2 { // Only update every 200ms to allow meaningful TPS calculation
		ac.updateFooter()
	}
}

// resetStreamingTPS resets streaming tracking (call when streaming ends)
func (ac *AgentConsole) resetStreamingTPS() {
	ac.isStreaming = false
	ac.streamingTokenCount = 0
}

func (ac *AgentConsole) setupTerminal() error {
	// Clear screen and home cursor
	ac.Terminal().ClearScreen()

	// Update footer height in layout manager
	footerHeight := ac.footer.GetHeight()
	ac.autoLayoutManager.SetComponentHeight("footer", footerHeight)

	// Get scroll region from layout manager
	top, bottom := ac.autoLayoutManager.GetScrollRegion()

	// DEBUG: Show terminal size and scroll region
	width, height, _ := ac.Terminal().GetSize()
	fmt.Fprintf(os.Stderr, "[SETUP] Terminal: %dx%d, Scroll region: %d-%d\n", width, height, top, bottom)

	// Set up scroll region based on layout calculation
	if err := ac.Terminal().SetScrollRegion(top, bottom); err != nil {
		return err
	}

	// Move cursor to the beginning of the content area (top of scroll region)
	if err := ac.Terminal().MoveCursor(1, top); err != nil {
		return err
	}

	// Cache content region info for positioning
	if contentRegion, err := ac.autoLayoutManager.GetContentRegion(); err == nil {
		ac.contentRegionTop = contentRegion.Y
		ac.currentContentLine = 0
	}

	// DEBUG: Test that content goes to the right place
	ac.writeTextWithRawModeFix("TEST: This should appear at top of content area\n")

	// Configure input manager to use layout manager for positioning
	ac.inputManager.SetLayoutManager(ac.autoLayoutManager)

	// Initial footer render
	if err := ac.footer.Render(); err != nil {
		return err
	}

	// Ensure cursor is back in content area after footer render
	if err := ac.Terminal().MoveCursor(1, top); err != nil {
		return err
	}

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
		// First Ctrl+C - try to interrupt agent if it's processing
		select {
		case ac.interruptChan <- "Ctrl+C pressed - stopping current operation":
			ac.writeTextWithRawModeFix("\r\033[K^C  🛑 Stopping current operation... (Press Ctrl+C again to exit)\n")
			// Don't redraw prompt yet, let the agent finish gracefully
		default:
			// Agent not processing, show exit message
			ac.writeTextWithRawModeFix("\r\033[K^C  💡 Press Ctrl+C again to exit\n")
			// Redraw prompt
			ac.writeTextWithRawModeFix(ac.prompt)
		}
	} else {
		// Second Ctrl+C - exit immediately
		ac.writeTextWithRawModeFix("\r\033[K🚪 Exiting...\n")

		// Try to interrupt agent one more time if it's still running
		select {
		case ac.interruptChan <- "Force exit requested":
		default:
		}

		// Input manager handles its own terminal restoration

		ac.cleanup()
		os.Exit(0)
	}
}

// handleInputFromManager processes input from the concurrent input manager
func (ac *AgentConsole) handleInputFromManager(input string) error {
	// Add to history
	ac.historyManager.AddEntry(input)

	ac.processingMutex.Lock()
	if ac.isProcessing {
		// Inject the input into the agent's conversation flow
		ac.agent.InjectInput(input)
		ac.processingMutex.Unlock()

		// Show feedback that input was injected
		ac.safePrint("\n💬 Input injected into conversation: %s\n", input)
		ac.inputManager.ScrollOutput() // Make room for the message

		return nil
	}
	ac.isProcessing = true
	ac.processingMutex.Unlock()

	// Process the input immediately (start new conversation)
	go func() {
		defer func() {
			ac.processingMutex.Lock()
			ac.isProcessing = false
			ac.inputManager.SetProcessing(false)
			ac.processingMutex.Unlock()

			// Signal completion
			select {
			case ac.agentDoneChan <- struct{}{}:
			default:
			}
		}()

		ac.inputManager.SetProcessing(true)

		// Show processing message in content area (not below input)
		ac.safePrint("\n🔄 Processing: %s\n", input)
		ac.inputManager.ScrollOutput()

		err := ac.processInput(input)
		if err != nil {
			ac.safePrint("❌ Error: %v\n", err)
			ac.inputManager.ScrollOutput()
		}
	}()

	return nil
}

// handleInterruptFromManager processes interrupt signals from input manager
func (ac *AgentConsole) handleInterruptFromManager() {
	ac.handleCtrlC()
}

func (ac *AgentConsole) cleanup() {
	// Stop input manager first
	if ac.inputManager != nil {
		ac.inputManager.Stop()
	}

	// Reset scroll region
	ac.Terminal().ResetScrollRegion()

	// Save history
	if ac.historyFile != "" && ac.historyManager != nil {
		if err := ac.historyManager.SaveToFile(); err != nil {
			ac.writeTextWithRawModeFix(fmt.Sprintf("Warning: Could not save history: %v\n", err))
		}
	}

	// Clean up components handled by individual managers
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

// writeTextWithRawModeFix is a helper to write text with proper line ending handling
func (ac *AgentConsole) writeTextWithRawModeFix(text string) {
	if ac.Terminal().IsRawMode() {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	fmt.Print(text)
}

// safePrint writes output that respects the content area and updates buffer
func (ac *AgentConsole) safePrint(format string, args ...interface{}) {
	content := fmt.Sprintf(format, args...)

	// Filter out completion signals that should not be displayed
	content = ac.filterCompletionSignals(content)

	// Only write if there's content left after filtering
	if strings.TrimSpace(content) != "" {
		// Add to console buffer for tracking
		if ac.consoleBuffer != nil {
			ac.consoleBuffer.AddContent(content)
		}

		// Key fix: Track if we need to position in content area vs continuing from current position
		// For the first output after setup, we need to be in content area
		// For subsequent outputs, we continue from where we are (sequential output)

		// If current content line is 0, we're starting fresh - position in content area
		if ac.currentContentLine == 0 {
			top, _ := ac.autoLayoutManager.GetScrollRegion()
			ac.Terminal().MoveCursor(1, top)
		}

		// Write content - this will advance cursor naturally
		ac.writeTextWithRawModeFix(content)

		// Update our tracking of content lines
		newlines := strings.Count(content, "\n")
		ac.currentContentLine += newlines
	}
}

// OnResize handles terminal resize events
func (ac *AgentConsole) OnResize(width, height int) {
	// Lock output mutex to prevent interleaving with agent output
	ac.outputMutex.Lock()
	defer ac.outputMutex.Unlock()

	// Update console buffer with new width for rewrapping
	ac.consoleBuffer.SetTerminalWidth(width)

	// Let footer component handle its own resize first (updates dynamic height)
	if ac.footer != nil {
		ac.footer.OnResize(width, height)
	}

	// Update layout manager with new terminal size
	ac.autoLayoutManager.OnTerminalResize(width, height)

	// Update footer height in layout
	footerHeight := ac.footer.GetHeight()
	ac.autoLayoutManager.SetComponentHeight("footer", footerHeight)

	// Get updated scroll region from layout manager
	top, bottom := ac.autoLayoutManager.GetScrollRegion()
	ac.Terminal().SetScrollRegion(top, bottom)

	// Layout manager automatically handles input positioning

	// Skip redraw to preserve scrollback content - let terminal handle natural scrollback
	// The RedrawBuffer call was clearing content area and only showing recent content
	// contentHeight := bottom - top + 1
	// if err := ac.consoleBuffer.RedrawBuffer(ac.Terminal(), contentHeight); err != nil {
	//     fmt.Fprintf(os.Stderr, "Warning: Buffer redraw failed: %v\n", err)
	// }

	// Footer already renders itself in its OnResize method, no need to render again
	// This was causing duplicate footer rendering during resize events

	// Note: Cursor will be repositioned by the input component when it redraws
}

// refreshLayoutAfterSetup applies the same layout positioning logic as OnResize
// This ensures content appears in the right place after initial setup
func (ac *AgentConsole) refreshLayoutAfterSetup(width, height int) {
	// Update layout manager with terminal size (similar to resize)
	ac.autoLayoutManager.OnTerminalResize(width, height)

	// Update footer height in layout
	footerHeight := ac.footer.GetHeight()
	ac.autoLayoutManager.SetComponentHeight("footer", footerHeight)

	// Get updated scroll region from layout manager
	top, bottom := ac.autoLayoutManager.GetScrollRegion()
	ac.Terminal().SetScrollRegion(top, bottom)

	// Position cursor at start of content area
	ac.Terminal().MoveCursor(1, top)
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
	data, err := filesystem.ReadFileBytes(filename)
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
	return filesystem.WriteFileWithDir(filename, []byte(data), 0600)
}

// initializeFooterInfo sets up git and path information for the footer
func (ac *AgentConsole) initializeFooterInfo() {
	// Get current working directory
	cwd, err := os.Getwd()
	if err == nil && ac.footer != nil {
		ac.footer.UpdatePath(cwd)
	}

	// Get git info - run git command directly
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	branchOut, err := cmd.CombinedOutput()
	if err == nil {
		branch := strings.TrimSpace(string(branchOut))

		// Get remote URL
		remoteCmd := exec.Command("git", "remote", "get-url", "origin")
		remoteOut, _ := remoteCmd.CombinedOutput()
		remote := strings.TrimSpace(string(remoteOut))

		// Convert to SSH-style format (git@host:user/repo)
		if remote != "" {
			// Handle https URLs: https://github.com/user/repo.git
			if strings.HasPrefix(remote, "https://") {
				remote = strings.TrimPrefix(remote, "https://")
				remote = strings.TrimSuffix(remote, ".git")
				parts := strings.Split(remote, "/")
				if len(parts) >= 3 {
					host := parts[0]
					user := parts[1]
					repo := parts[2]
					remote = fmt.Sprintf("git@%s:%s/%s", host, user, repo)
				}
			} else if strings.HasPrefix(remote, "git@") {
				// Already in SSH format, just trim .git
				remote = strings.TrimSuffix(remote, ".git")
			}
		}

		// Get number of changes
		statusCmd := exec.Command("git", "status", "--porcelain")
		statusOut, _ := statusCmd.CombinedOutput()
		changes := 0
		if len(statusOut) > 0 {
			lines := strings.Split(string(statusOut), "\n")
			for _, line := range lines {
				if line != "" {
					changes++
				}
			}
		}

		if ac.footer != nil {
			ac.footer.UpdateGitInfo(branch, changes, true)
			ac.footer.UpdateGitRemote(remote)
		}
	} else {
		// Not a git repo
		if ac.footer != nil {
			ac.footer.UpdateGitInfo("", 0, false)
		}
	}
}

// filterCompletionSignals removes task completion signals from content
func (ac *AgentConsole) filterCompletionSignals(content string) string {
	completionSignals := []string{
		"[[TASK_COMPLETE]]",
		"[[TASKCOMPLETE]]",
		"[[TASK COMPLETE]]",
		"[[task_complete]]",
		"[[taskcomplete]]",
		"[[task complete]]",
	}

	filtered := content
	for _, signal := range completionSignals {
		filtered = strings.ReplaceAll(filtered, signal, "")
	}

	return strings.TrimSpace(filtered)
}
