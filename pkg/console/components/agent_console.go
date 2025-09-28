package components

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	commands "github.com/alantheprice/ledit/pkg/agent_commands"
	tools "github.com/alantheprice/ledit/pkg/agent_tools"
	"github.com/alantheprice/ledit/pkg/console"
	"github.com/alantheprice/ledit/pkg/filesystem"
	"github.com/fatih/color"
	"golang.org/x/term"
)

// ErrUserQuit is returned when the user requests to quit
var ErrUserQuit = errors.New("user requested quit")

// AgentConsole is the main console component for agent interactions
type AgentConsole struct {
	*console.BaseComponent

	// Core dependencies
	agent           *agent.Agent
	commandRegistry *commands.CommandRegistry

	// Sub-components
	inputManager       *InputManager
	historyManager     *HistoryManager
	inputStateManager  *InputStateManager
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

	// Context for background operations
	ctx    context.Context
	cancel context.CancelFunc

	// Streaming TPS tracking
	streamingStartTime  time.Time
	streamingTokenCount int
	isStreaming         bool

	// UI coordination for throttled updates
	uiCoordinator *UICoordinator

	// Dropdown support
	activeDropdown *DropdownComponent

	// Scroll state
	isScrolling    bool
	scrollPosition int

	// Interruption state
	wasInterrupted bool

	// Focus state: "input" or "output"
	focusMode string

	// Help overlay state (output focus)
	showOutputHelp bool
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

	// Create input state manager for history navigation persistence
	inputStateFile := config.HistoryFile + ".state"
	inputStateManager := NewInputStateManager(inputStateFile)

	// Create auto layout manager
	autoLayoutManager := console.NewAutoLayoutManager()

	// Create context for background operations
	ctx, cancel := context.WithCancel(context.Background())

	ac := &AgentConsole{
		BaseComponent:     base,
		agent:             agent,
		commandRegistry:   commands.NewCommandRegistry(),
		inputManager:      inputManager,
		historyManager:    historyManager,
		inputStateManager: inputStateManager,
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
		ctx:               ctx,
		cancel:            cancel,
	}

	// Create streaming formatter
	ac.streamingFormatter = NewStreamingFormatter(&ac.outputMutex)
	ac.streamingFormatter.SetConsoleBuffer(ac.consoleBuffer)

	// Create UI coordinator for throttled footer updates
	ac.uiCoordinator = NewUICoordinator(footer, ac.Terminal())
	ac.uiCoordinator.Start()

	// Set custom output function to use our safe print method
	ac.streamingFormatter.SetOutputFunc(func(text string) {
		// Use our simplified safe print (no complex positioning)
		ac.safePrint("%s", text)
	})

	// Set the interrupt channel and output mutex on the agent (guard for nil)
	if agent != nil {
		agent.SetInterruptChannel(ac.interruptChan)
		agent.SetOutputMutex(&ac.outputMutex)
	}

	// Set up input manager callbacks
	inputManager.SetCallbacks(
		ac.handleInputFromManager,
		ac.handleInterruptFromManager,
	)

	// Set up height change callback to update layout
	inputManager.SetHeightChangeCallback(ac.handleInputHeightChange)

	// Set up history manager for arrow key navigation
	inputManager.SetHistoryManager(ac.historyManager)

	// Set up scroll callbacks
	inputManager.SetScrollCallbacks(
		func(lines int) { ac.scrollUp(lines) },
		func(lines int) { ac.scrollDown(lines) },
	)

    // Provide focus to input manager so arrow keys and vim keys are context-aware
    inputManager.SetFocusProvider(ac.getFocusMode)
    // Provide scrolling state to input manager for contextual hints
    inputManager.SetScrollingProvider(func() bool { return ac.isScrolling })

	// Manual focus toggle via Tab
	inputManager.SetFocusToggle(func() {
		if ac.getFocusMode() == "input" {
			ac.setFocus("output")
		} else {
			ac.setFocus("input")
		}
		// Refresh UI elements after focus change
		// Always refresh content and input field so the input can show hints in output focus
		if ac.getFocusMode() == "output" {
			ac.redrawContent()
		}
		ac.inputManager.showInputField()
	})

	// Toggle help overlay with '?' in output focus
	inputManager.SetHelpToggle(func() {
		if ac.getFocusMode() == "output" {
			ac.showOutputHelp = !ac.showOutputHelp
			ac.redrawContent()
		}
	})

	// Default focus is input
	ac.focusMode = "input"

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

	// Emit welcome message early so tests that don't call Start() still see baseline content
	ac.showWelcomeMessage()
	// Ensure initial focus is rendered on input
	ac.setFocus("input")
}

// setFocus switches focus between input and output and refreshes indicators
func (ac *AgentConsole) setFocus(mode string) {
	if mode != "input" && mode != "output" {
		return
	}
	ac.focusMode = mode
	// Update footer hint
	if ac.footer != nil {
		ac.footer.SetFocusMode(mode)
		// Render footer promptly only if initialized with deps
		if ac.footer.State() != nil && ac.footer.Terminal() != nil {
			_ = ac.footer.Render()
		}
	}
	ac.renderFocusIndicators()
    // Adjust buffer wrapping width based on focus margin
    if ac.Terminal() != nil && ac.consoleBuffer != nil {
        w, _, _ := ac.Terminal().GetSize()
        contentWidth := w
        // Reserve a 3-column gutter (bar+bar+space) when output-focused
        if mode == "output" {
            contentWidth = w - 3
        }
        if contentWidth < 1 {
            contentWidth = 1
        }
        ac.consoleBuffer.SetTerminalWidth(contentWidth)
        // Redraw to apply immediately
        ac.redrawContent()
    }
}

func (ac *AgentConsole) getFocusMode() string { return ac.focusMode }

// renderFocusIndicators draws a light teal bar on the focused component
func (ac *AgentConsole) renderFocusIndicators() {
	if ac.Terminal() == nil || ac.autoLayoutManager == nil {
		return
	}

    // Build a wider cyan bar using two thin line glyphs, plus a space padding (total gutter = 3)
    bar := "\033[36m││\033[0m"
    clear := "   "

	// Clear any existing bar in content area first
	top, bottom := ac.autoLayoutManager.GetScrollRegion()
	for y := top; y <= bottom; y++ {
		ac.Terminal().MoveCursor(1, y)
        if ac.focusMode == "output" {
            ac.Terminal().WriteText(bar + " ")
        } else {
            ac.Terminal().WriteText(clear)
        }
	}

	// Draw or clear input focus bar at input field line
	inputLine := ac.inputManager.GetCurrentInputFieldLine()
	ac.Terminal().MoveCursor(1, inputLine)
    if ac.focusMode == "input" {
        ac.Terminal().WriteText(bar + " ")
    } else {
        ac.Terminal().WriteText(clear)
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
		Prompt:      "> ",
	}
}

// Init initializes the component
func (ac *AgentConsole) Init(ctx context.Context, deps console.Dependencies) error {
	if err := ac.BaseComponent.Init(ctx, deps); err != nil {
		return err
	}

	// Store deps for dropdown support
	ac.BaseComponent.Deps = deps

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

	// Subscribe to dropdown events
	ac.BaseComponent.Deps.Events.Subscribe("dropdown.show", ac.handleDropdownShow)
	ac.BaseComponent.Deps.Events.Subscribe("dropdown.hide", ac.handleDropdownHide)

	// Handle exclusive input requests for dropdown
	ac.BaseComponent.Deps.Events.Subscribe("input.request_exclusive", func(e console.Event) error {
		// Grant exclusive input to dropdown when requested
		if e.Source == "dropdown" {
			ac.BaseComponent.Deps.Events.Publish(console.Event{
				Type: "input.exclusive_granted",
				Data: map[string]interface{}{"component": e.Source},
			})
		}
		return nil
	})

	// Load history
	if ac.historyFile != "" {
		if err := ac.historyManager.LoadFromFile(); err != nil {
			// Non-fatal, just log
			ac.writeTextWithRawModeFix(fmt.Sprintf("Note: Could not load history: %v\n", err))
		}
	}

	// Load input state (history navigation position)
	if ac.inputStateManager != nil {
		if err := ac.inputStateManager.LoadFromFile(); err != nil {
			// Non-fatal, just log
			ac.writeTextWithRawModeFix(fmt.Sprintf("Note: Could not load input state: %v\n", err))
		}
		
		// Apply loaded state to input manager
		if ac.inputManager != nil {
			state := ac.inputStateManager.GetState()
			ac.inputManager.SetHistoryState(state.HistoryIndex, state.TempInput)
		}
	}

	// Set up UI for the agent
	if ac.agent != nil {
		SetupAgentUI(ac, ac.agent)
	}

	// Initialize console buffer with current terminal width
	width, _, err := ac.Terminal().GetSize()
	if err == nil {
		ac.consoleBuffer.SetTerminalWidth(width)
	}

	// Subscribe to terminal resize events
	deps.Events.Subscribe("terminal.resized", func(event console.Event) error {
		if data, ok := event.Data.(map[string]int); ok {
			width := data["width"]
			height := data["height"]
			ac.OnResize(width, height)
		}
		return nil
	})

	// Set up terminal with scroll regions
	if err := ac.setupTerminal(); err != nil {
		return fmt.Errorf("failed to setup terminal: %w", err)
	}

	// Update footer with initial state
	ac.updateFooter()

	// Initialize git and path info for footer
	ac.initializeFooterInfo()

	// Subscribe to terminal interrupt events from controller
	deps.Events.Subscribe("terminal.interrupted", func(event console.Event) error {
		ac.handleCtrlC()
		return nil
	})

	// Resize handling is now managed by TerminalController via events

	return nil
}

// Start starts the interactive loop with concurrent input handling
func (ac *AgentConsole) Start() error {
	// Connect input manager to layout manager
	ac.inputManager.SetLayoutManager(ac.autoLayoutManager)

	// Start the concurrent input manager
	if err := ac.inputManager.Start(); err != nil {
		return fmt.Errorf("failed to start input manager: %w", err)
	}

	// Ensure cleanup when done
	defer ac.inputManager.Stop()

	// Layout manager handles positioning automatically

	ac.safePrint("   Press Tab to switch between inputs and output focus.\n")
	ac.safePrint("   Use Ctrl+C to interrupt the agent.\n\n")

	// Initial footer render (MUST be last)
	ac.updateFooter()

	// Main event loop - non-blocking
	for {
		select {
		case <-ac.ctx.Done():
			return ErrUserQuit

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
	// Lock output to prevent interleaving (brief lock for initial setup)
	ac.outputMutex.Lock()

	// Clear the current input line and move to a new line for clean output
	ac.safePrint("\r\033[K\n")

	// Show processing indicator
	ac.safePrint("🔄 Processing your request...\n")

	// CRITICAL: Release mutex before streaming starts to prevent deadlock
	// The streaming formatter has its own mutex coordination
	ac.outputMutex.Unlock()

	// CRITICAL FIX: Ensure cursor is positioned correctly for streaming output
	// processInput() does its own output which might affect cursor position
	ac.repositionCursorToContentArea()

	// Store the original prompt for potential conversation rewrite
	originalPrompt := input
	_ = originalPrompt // TODO: Use for conversation rewrite

	// Set up streaming with our formatter
	// Reset formatter for new session
	ac.streamingFormatter.Reset()
	ac.isStreaming = true
	if ac.uiCoordinator != nil {
		ac.uiCoordinator.SetStreaming(true)
	}
	if console.DebugEnabled() {
		console.DebugPrintf("Starting streaming mode\n")
	}

	// Check if agent is properly initialized
	if ac.agent == nil {
		ac.safePrint("❌ Error: Agent not initialized\n")
		return nil
	}

	// If debug is enabled and a debug log file exists, show its path once per run
	if path := ac.agent.GetDebugLogPath(); path != "" && (os.Getenv("LEDIT_DEBUG") == "1" || os.Getenv("LEDIT_DEBUG") == "true" || os.Getenv("LEDIT_DEBUG") != "") {
		ac.safePrint("\n🪵 Debug log: %s\n\n", path)
	}

	// Track last streaming activity for timeout reset
	var lastStreamingActivity time.Time

    // Enable streaming with our formatter callback for all providers
    // Make callback non-blocking to avoid potential deadlocks under contention
    if console.DebugEnabled() {
        console.DebugPrintf("Setting up streaming callback for provider=%s\n", ac.agent.GetProvider())
    }
    ac.agent.EnableStreaming(func(content string) {
        // Dispatch write asynchronously and return immediately
        go func() {
            // Debug: log streaming content
            if len(content) > 0 {
                if console.DebugEnabled() {
                    fmt.Fprintf(os.Stderr, "[DEBUG] Streaming content: %d chars, first 20: %q\n", len(content), content[:min(20, len(content))])
                }
            }
            ac.streamingFormatter.Write(content)
            // Update TPS estimation during streaming
            ac.updateStreamingTPS(content)
            // Update last activity time when we receive content
            lastStreamingActivity = time.Now()
        }()
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

			// Ensure cursor is positioned correctly after streaming completes
			// This helps with immediate resize responsiveness
			ac.finalizeStreamingPosition()
		}
		ac.agent.DisableStreaming()
		// Reset streaming TPS tracking
		ac.resetStreamingTPS()
	}()

	// Process with timeout detection to help diagnose hanging
	type queryResult struct {
		response string
		err      error
	}

	resultChan := make(chan queryResult, 1)
	go func() {
		if console.DebugEnabled() {
			console.DebugPrintf("Calling ProcessQueryWithContinuity\n")
		}
		response, err := ac.agent.ProcessQueryWithContinuity(input)
		if console.DebugEnabled() {
			console.DebugPrintf("ProcessQueryWithContinuity returned: err=%v, response len=%d\n", err, len(response))
		}
		resultChan <- queryResult{response: response, err: err}
	}()

	// Wait for result with progress-aware timeout detection
	// Reset timeout when streaming content is received
	timeout := 15 * time.Minute // Longer base timeout for complex operations
	lastActivity := time.Now()
	activityTicker := time.NewTicker(30 * time.Second)
	defer activityTicker.Stop()

	var response string
	var err error

	for {
		select {
		case result := <-resultChan:
			response = result.response
			err = result.err
			if console.DebugEnabled() {
				fmt.Fprintf(os.Stderr, "[DEBUG] Got result from ProcessQueryWithContinuity, going to processComplete\n")
			}
			goto processComplete

		case <-activityTicker.C:
			// Check if we've had recent streaming activity
			if !lastStreamingActivity.IsZero() && lastStreamingActivity.After(lastActivity) {
				// Reset timeout - we've received streaming content
				lastActivity = lastStreamingActivity
			} else if ac.streamingFormatter.HasProcessedContent() {
				// Fallback: reset timeout if formatter shows any content processed
				lastActivity = time.Now()
			}

			// Check if we've exceeded timeout without any activity
			if time.Since(lastActivity) > timeout {
				err = fmt.Errorf("query processing timeout after %v with no streaming activity - this may indicate a hanging issue", timeout)
				ac.safePrint("\n🚨 TIMEOUT: No processing activity for %v\n", timeout)
				ac.safePrint("  This may indicate a deadlock or very complex operation\n")
				ac.safePrint("  Try breaking down the request into smaller parts\n")
				goto processComplete
			}
		}
	}

processComplete:
	// End streaming mode
	ac.isStreaming = false
	if ac.uiCoordinator != nil {
		ac.uiCoordinator.SetStreaming(false)
	}
	if console.DebugEnabled() {
		console.DebugPrintf("Ending streaming mode\n")
	}

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

			// Ensure cursor is positioned correctly after streaming completes
			ac.finalizeStreamingPosition()
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
			// Call conversation rewrite functionality for better formatting
			if ac.streamingFormatter.HasProcessedContent() {
				ac.streamingFormatter.RewriteConversationOnComplete(originalPrompt)
			}

			// Check if we need spacing before summary
			if ac.streamingFormatter.HasProcessedContent() && !ac.streamingFormatter.EndedWithNewline() {
				ac.safePrint("\n")
			}
			ac.safePrint("\n")

			// Add a subtle separator before the summary to improve readability
			separator := color.New(color.FgHiBlack).Sprint("─────────────────────────────────────────────────\n")
			ac.safePrint("%s", separator)
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
		// Return a special error to signal clean exit
		return ErrUserQuit
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
			// Check if this is an interactive command that needs passthrough mode
			// Interactive commands use dropdowns and need raw mode control
			isInteractiveCommand := cmd == "models" || cmd == "mcp" || cmd == "commit" || cmd == "shell" || cmd == "providers" || cmd == "memory" || cmd == "log"

			if isInteractiveCommand {
				// Enable passthrough mode for interactive command
				ac.inputManager.SetPassthroughMode(true)

				// Set environment variable so child processes know they're in agent console
				os.Setenv("LEDIT_AGENT_CONSOLE", "1")
				defer os.Unsetenv("LEDIT_AGENT_CONSOLE")

				// Give terminal time to settle
				time.Sleep(100 * time.Millisecond)

				// Execute the command
				err := cmdHandler.Execute(args, ac.agent)

				// Give command time to finish
				time.Sleep(100 * time.Millisecond)

				// Disable passthrough mode after command completes
				ac.inputManager.SetPassthroughMode(false)

				// CRITICAL FIX: Restore console layout after passthrough mode
				ac.restoreLayoutAfterPassthrough()

				return err
			}

			// For non-interactive commands, execute normally
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
	if console.DebugEnabled() {
		console.DebugPrintf("updateFooter called\n")
	}
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

	// Use UI coordinator for throttled updates during streaming
	if ac.uiCoordinator != nil {
		ac.uiCoordinator.QueueFooterUpdate(FooterUpdate{
			Model:            model,
			Provider:         provider,
			Tokens:           actualTotalTokens,
			Cost:             ac.totalCost,
			Iteration:        iteration,
			ContextTokens:    contextTokens,
			MaxContextTokens: maxContextTokens,
		})
	} else {
		// Fallback to direct update if coordinator not available
		ac.footer.UpdateStats(model, provider, actualTotalTokens, ac.totalCost, iteration, contextTokens, maxContextTokens)

		// Force render to ensure tokens update is shown
		if console.DebugEnabled() {
			fmt.Fprintf(os.Stderr, "[DEBUG] Calling footer.Render()\n")
		}
		if err := ac.footer.Render(); err != nil {
			// Non-fatal, log if debug
			fmt.Fprintf(os.Stderr, "Warning: Footer render failed: %v\n", err)
		}
	}
}

func (ac *AgentConsole) showWelcomeMessage() {
	ac.safePrint(`
Welcome to Ledit Agent! 🤖

I can help you with:
• Code analysis and generation
• File exploration and editing
• Shell command execution
• Project understanding

Type /help for available commands, or just start chatting!

`)
	// Normalize content tracking so subsequent output starts near the top of content area
	ac.currentContentLine = 1
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
  • Press Esc to interrupt agent processing
  • Press Esc twice quickly to exit the program
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
		if console.DebugEnabled() {
			fmt.Fprintf(os.Stderr, "[DEBUG] Content region setup: top=%d, currentContentLine=0\n", contentRegion.Y)
		}
	}

	// DEBUG: Test that content goes to the right place
	// ac.writeTextWithRawModeFix("TEST: This should appear at top of content area\n")
	// Commented out - this test message might be interfering with actual content

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
		// Mark that we were interrupted to influence next input behavior
		ac.wasInterrupted = true
		// First Esc - try to interrupt agent if it's processing
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
		// Second Esc - exit immediately
		ac.writeTextWithRawModeFix("\r\033[K🚪 Exiting...\n")

		// Try to interrupt agent one more time if it's still running
		select {
		case ac.interruptChan <- "Force exit requested":
		default:
		}

		// Signal that we want to exit cleanly
		// Don't use os.Exit as it bypasses cleanup
		ac.cancel() // Cancel the context to stop the main loop
	}
}

// handleInputFromManager processes input from the concurrent input manager
func (ac *AgentConsole) handleInputFromManager(input string) error {
	// Add to history
	ac.historyManager.AddEntry(input)

	// Decide whether to inject into existing processing or start fresh
	ac.processingMutex.Lock()
	startNew := false
	if ac.isProcessing {
		// If a recent interrupt occurred, treat this as a new conversation
		interrupted := ac.wasInterrupted
		if !interrupted && ac.agent != nil {
			interrupted = ac.agent.IsInterrupted()
		}
		if interrupted {
			// Start a fresh conversation
			startNew = true
		} else {
			// Inject into ongoing processing
			if ac.agent != nil {
				ac.agent.InjectInput(input)
			}
			ac.processingMutex.Unlock()

			// Show feedback that input was injected
			ac.safePrint("\n💬 Input injected into conversation: %s\n", input)
			ac.inputManager.ScrollOutput() // Make room for the message
			ac.repositionCursorToContentArea()
			return nil
		}
	} else {
		startNew = true
	}

	if startNew {
		ac.isProcessing = true
	}
	ac.processingMutex.Unlock()

	// Clear any previous interrupt state to allow fresh processing
	if ac.agent != nil {
		ac.agent.ClearInterrupt()
	}
	ac.wasInterrupted = false

	// Switch focus to output while processing
	ac.setFocus("output")

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

			// Return focus to input after completion
			ac.setFocus("input")
		}()

		ac.inputManager.SetProcessing(true)

		// Show processing message in content area (not below input)
		ac.safePrint("\n🔄 Processing: %s\n", input)
		ac.inputManager.ScrollOutput()

		// CRITICAL FIX: Reposition cursor to content area after ScrollOutput()
		ac.repositionCursorToContentArea()

		err := ac.processInput(input)
		if err != nil {
			// Check if user requested quit
			if errors.Is(err, ErrUserQuit) {
				// Cancel the context to trigger clean shutdown
				ac.cancel()
				return
			}

			ac.safePrint("❌ Error: %v\n", err)
			ac.inputManager.ScrollOutput()

			// CRITICAL FIX: Reposition cursor to content area after ScrollOutput()
			ac.repositionCursorToContentArea()
		}
	}()

	return nil
}

// handleInterruptFromManager processes interrupt signals from input manager
func (ac *AgentConsole) handleInterruptFromManager() {
	ac.handleCtrlC()
}

// handleInputHeightChange handles input field height changes and updates layout
func (ac *AgentConsole) handleInputHeightChange(newHeight int) {
	// Update the layout manager with the new input height
	ac.autoLayoutManager.SetComponentHeight("input", newHeight)

	// Recalculate layout positions
	width, height, err := ac.Terminal().GetSize()
	if err == nil {
		ac.autoLayoutManager.OnTerminalResize(width, height)

		// Update footer height in layout
		footerHeight := ac.footer.GetHeight()
		ac.autoLayoutManager.SetComponentHeight("footer", footerHeight)

		// Get updated scroll region from layout manager
		top, bottom := ac.autoLayoutManager.GetScrollRegion()

		// Re-establish scroll region
		if err := ac.Terminal().SetScrollRegion(top, bottom); err == nil {
			// Force input manager to recalculate its position
			ac.inputManager.updateTerminalSize()
		}
	}
}

func (ac *AgentConsole) cleanup() {
	// Cancel background operations
	if ac.cancel != nil {
		ac.cancel()
	}

	// Stop input manager first
	if ac.inputManager != nil {
		ac.inputManager.Stop()
	}

	// Stop UI coordinator
	if ac.uiCoordinator != nil {
		ac.uiCoordinator.Stop()
	}

	// Reset scroll region
	ac.Terminal().ResetScrollRegion()

	// Save history
	if ac.historyFile != "" && ac.historyManager != nil {
		if err := ac.historyManager.SaveToFile(); err != nil {
			ac.writeTextWithRawModeFix(fmt.Sprintf("Warning: Could not save history: %v\n", err))
		}
	}

	// Save input state (history navigation position)
	if ac.inputStateManager != nil {
		// Get current state from input manager
		if ac.inputManager != nil {
			historyIndex, tempInput := ac.inputManager.GetHistoryState()
			state := InputState{
				HistoryIndex: historyIndex,
				TempInput:    tempInput,
			}
			ac.inputStateManager.SetState(state)
		}
		if err := ac.inputStateManager.SaveToFile(); err != nil {
			ac.writeTextWithRawModeFix(fmt.Sprintf("Warning: Could not save input state: %v\n", err))
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
	// Use terminal's Write method instead of fmt.Print to ensure proper routing
	ac.Terminal().Write([]byte(text))
}

// safePrint writes output that respects the content area and updates buffer
func (ac *AgentConsole) safePrint(format string, args ...interface{}) {
	content := fmt.Sprintf(format, args...)

	// Debug output
	if console.DebugEnabled() && len(content) > 0 && content != "\n" {
		console.DebugPrintf("safePrint called with %d chars\n", len(content))
	}

	// Filter out completion signals that should not be displayed
	content = ac.filterCompletionSignals(content)

	// Only write if there's content left after filtering
	if strings.TrimSpace(content) != "" {
		// Ensure currentContentLine is initialized and within bounds when we have content
		if ac.autoLayoutManager != nil && ac.Terminal() != nil {
			top, bottom := ac.autoLayoutManager.GetScrollRegion()
			contentHeight := bottom - top + 1
			if ac.currentContentLine == 0 {
				ac.currentContentLine = 1
			}
			if ac.currentContentLine > contentHeight {
				ac.currentContentLine = contentHeight
			}
		} else {
			if ac.currentContentLine == 0 {
				ac.currentContentLine = 1
			}
		}

		// Fallback for uninitialized console (e.g., lightweight tests)
		if ac.autoLayoutManager == nil || ac.Terminal() == nil {
			if ac.consoleBuffer != nil {
				ac.consoleBuffer.AddContent(content)
			}
			// Best-effort stdout write without terminal ops
			fmt.Print(content)
			if ac.currentContentLine == 0 {
				ac.currentContentLine = 1
			}
			ac.currentContentLine += strings.Count(content, "\n")
			return
		}
		// Add to console buffer for tracking
		if ac.consoleBuffer != nil {
			ac.consoleBuffer.AddContent(content)
		}

		// If we're scrolling, don't auto-display new content
		if ac.isScrolling {
			return
		}

		// Redraw from buffer to ensure consistent ordering and wrapping
		ac.redrawContent()

		// Update currentContentLine by the number of newlines in the content, with bounds checking
		newlines := strings.Count(content, "\n")
		if newlines > 0 {
			ac.currentContentLine += newlines
			if ac.autoLayoutManager != nil {
				top, bottom := ac.autoLayoutManager.GetScrollRegion()
				contentHeight := bottom - top + 1
				if ac.currentContentLine > contentHeight {
					ac.currentContentLine = contentHeight
				}
				if ac.currentContentLine < 1 {
					ac.currentContentLine = 1
				}
			}
		}

		// For non-streaming output, ensure cursor returns to the tracked content area position
		if !ac.isStreaming {
			ac.repositionCursorToContentArea()
		}
	}
}

// OnResize handles terminal resize events
func (ac *AgentConsole) OnResize(width, height int) {
	// Terminal controller now handles debouncing, so we can process immediately

	// Lock output mutex to prevent interleaving with agent output
	ac.outputMutex.Lock()
	defer ac.outputMutex.Unlock()

	// Update console buffer with new width for rewrapping (account for left gutter + padding = 2 cols)
	contentWidth := width - 2
	if contentWidth < 1 {
		contentWidth = 1
	}
	ac.consoleBuffer.SetTerminalWidth(contentWidth)

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

	// Only redraw buffer if absolutely necessary - this can cause display issues during active output
	// The layout and cursor repositioning below should handle most resize cases without full redraw
	contentHeight := bottom - top + 1

	// Queue a buffer redraw through the UI coordinator
	// This ensures proper serialization with other UI updates
	if ac.uiCoordinator != nil && ac.consoleBuffer != nil && ac.currentContentLine > 0 {
		// Create a closure that captures the necessary context
		redrawCallback := func(height int) error {
			// Get fresh scroll region in case it changed
			top, _ := ac.autoLayoutManager.GetScrollRegion()

			// Create a custom redraw that respects our scroll region
			lines := ac.consoleBuffer.GetVisibleLines(height)

			// Clear and redraw the content area
			for i := 0; i < height && i < len(lines); i++ {
				ac.Terminal().MoveCursor(1, top+i)
				ac.Terminal().ClearLine()
				// Use writeTextWithRawModeFix to handle raw mode conversion
				ac.writeTextWithRawModeFix(lines[i])
				if i < len(lines)-1 {
					ac.writeTextWithRawModeFix("\n")
				}
			}

			// Clear any remaining lines in the content area
			for i := len(lines); i < height; i++ {
				ac.Terminal().MoveCursor(1, top+i)
				ac.Terminal().ClearLine()
			}

			return nil
		}

		// Queue the redraw - it will be skipped if streaming is active
		ac.uiCoordinator.QueueRedraw(contentHeight, redrawCallback)
	}

	// CRITICAL FIX: Reposition cursor and currentContentLine after resize
	// This ensures that ongoing agent output continues at the correct location
	// instead of writing below the footer
	if ac.currentContentLine > 0 {
		// Ensure currentContentLine is within the new scroll region bounds
		if ac.currentContentLine > contentHeight {
			// Content line is beyond new scroll region - reposition to bottom of content area
			ac.currentContentLine = contentHeight
			ac.Terminal().MoveCursor(1, top+contentHeight-1) // Move to bottom of scroll region
		} else {
			// Content line is still valid - just reposition cursor to current location
			ac.Terminal().MoveCursor(1, top+ac.currentContentLine-1)
		}
	} else {
		// No content yet - position at top of scroll region
		ac.Terminal().MoveCursor(1, top)
		ac.currentContentLine = 1
	}

	// Footer already renders itself in its OnResize method, no need to render again
	// This was causing duplicate footer rendering during resize events

	// Update input manager with new terminal size and trigger its redraw
	if ac.inputManager != nil {
		ac.inputManager.updateTerminalSize()
		// Force input manager to redraw with new positioning
		ac.inputManager.showInputField()
	}

	// Note: Cursor will be repositioned by the input component when it redraws
}

// finalizeStreamingPosition ensures cursor is properly positioned after streaming completes
func (ac *AgentConsole) finalizeStreamingPosition() {
	ac.outputMutex.Lock()
	defer ac.outputMutex.Unlock()

	// Get current scroll region
	top, bottom := ac.autoLayoutManager.GetScrollRegion()
	contentHeight := bottom - top + 1

	// Ensure currentContentLine is within bounds
	if ac.currentContentLine > contentHeight {
		ac.currentContentLine = contentHeight
	}

	// Position cursor at the current content line
	if ac.currentContentLine > 0 {
		ac.Terminal().MoveCursor(1, top+ac.currentContentLine-1)
	} else {
		ac.Terminal().MoveCursor(1, top)
		ac.currentContentLine = 1
	}
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

	// Don't use TrimSpace as it removes important newlines
	// Just remove empty/whitespace-only content after filtering
	if strings.TrimSpace(filtered) == "" {
		return ""
	}
	return filtered
}

// repositionCursorToContentArea moves cursor back to content area after input manager positioning
func (ac *AgentConsole) repositionCursorToContentArea() {
	// Ensure subsequent output is written within the content area, not at input line

	// If layout/terminal not initialized yet (e.g., tests), track minimally
	if ac.autoLayoutManager == nil || ac.Terminal() == nil {
		if ac.currentContentLine == 0 {
			ac.currentContentLine = 1
		}
		return
	}

	top, bottom := ac.autoLayoutManager.GetScrollRegion()

	// If no content yet, move to the top of content area
	if ac.currentContentLine == 0 {
		ac.Terminal().MoveCursor(1, top)
		ac.currentContentLine = 1
		return
	}

	// Clamp to valid content area and move to tracked line
	contentHeight := bottom - top + 1
	line := ac.currentContentLine
	if line < 1 {
		line = 1
	} else if line > contentHeight {
		line = contentHeight
	}
	ac.Terminal().MoveCursor(1, top+line-1)
}

// restoreLayoutAfterPassthrough restores the console layout after interactive commands
func (ac *AgentConsole) restoreLayoutAfterPassthrough() {
	// Give input manager time to fully restore raw mode
	time.Sleep(50 * time.Millisecond)

	// Get current terminal dimensions
	width, height, err := ac.Terminal().GetSize()
	if err != nil {
		return
	}

	// Re-establish layout with current dimensions
	ac.autoLayoutManager.OnTerminalResize(width, height)

	// Update footer height in layout
	footerHeight := ac.footer.GetHeight()
	ac.autoLayoutManager.SetComponentHeight("footer", footerHeight)

	// Get updated scroll region from layout manager
	top, bottom := ac.autoLayoutManager.GetScrollRegion()

	// Re-establish scroll region
	if err := ac.Terminal().SetScrollRegion(top, bottom); err != nil {
		// Non-fatal, but log for debugging
		if console.DebugEnabled() {
			fmt.Fprintf(os.Stderr, "[DEBUG] Failed to restore scroll region after passthrough: %v\n", err)
		}
	}

	// Re-render footer to ensure it's properly positioned
	if err := ac.footer.Render(); err != nil {
		// Non-fatal
		if console.DebugEnabled() {
			fmt.Fprintf(os.Stderr, "[DEBUG] Failed to render footer after passthrough: %v\n", err)
		}
	}

	// Position cursor in content area for next output
	ac.repositionCursorToContentArea()
}

// handleDropdownShow handles request to show dropdown
func (ac *AgentConsole) handleDropdownShow(e console.Event) error {
	data, ok := e.Data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid dropdown show event data")
	}

	// Extract items
	items, ok := data["items"].([]DropdownItem)
	if !ok {
		// Try to convert from interface slice
		if itemsInterface, ok := data["items"].([]interface{}); ok {
			items = make([]DropdownItem, 0, len(itemsInterface))
			for _, item := range itemsInterface {
				if dropdownItem, ok := item.(DropdownItem); ok {
					items = append(items, dropdownItem)
				}
			}
		} else {
			return fmt.Errorf("invalid items in dropdown show event")
		}
	}

	// Extract options
	options := DropdownOptions{
		ShowSearch: true,
		ShowCounts: true,
	}

	if prompt, ok := data["prompt"].(string); ok {
		options.Prompt = prompt
	}
	if maxHeight, ok := data["maxHeight"].(int); ok {
		options.MaxHeight = maxHeight
	}

	// Extract callbacks
	var onSelect func(DropdownItem) error
	var onCancel func() error

	if fn, ok := data["onSelect"].(func(DropdownItem) error); ok {
		onSelect = fn
	}
	if fn, ok := data["onCancel"].(func() error); ok {
		onCancel = fn
	}

	// Create and show dropdown
	if ac.activeDropdown == nil {
		ac.activeDropdown = NewDropdownComponent()
		ac.activeDropdown.Init(ac.ctx, ac.BaseComponent.Deps)
	}

	// Enable passthrough mode for dropdown
	ac.inputManager.SetPassthroughMode(true)

	// Show dropdown
	return ac.activeDropdown.Show(items, options, onSelect, onCancel)
}

// handleDropdownHide handles request to hide dropdown
func (ac *AgentConsole) handleDropdownHide(e console.Event) error {
	if ac.activeDropdown != nil {
		ac.activeDropdown.Cleanup()
		ac.activeDropdown = nil

		// Disable passthrough mode
		ac.inputManager.SetPassthroughMode(false)

		// Restore layout
		ac.restoreLayoutAfterPassthrough()
	}
	return nil
}

// scrollUp scrolls the console buffer up by the given number of lines
func (ac *AgentConsole) scrollUp(lines int) {
	ac.outputMutex.Lock()
	defer ac.outputMutex.Unlock()

	ac.isScrolling = true
	ac.consoleBuffer.ScrollUp(lines)
	ac.redrawContent()
}

// scrollDown scrolls the console buffer down by the given number of lines
func (ac *AgentConsole) scrollDown(lines int) {
	ac.outputMutex.Lock()
	defer ac.outputMutex.Unlock()

	ac.consoleBuffer.ScrollDown(lines)

	// If we're at the bottom, exit scroll mode
	stats := ac.consoleBuffer.GetStats()
	if stats.ScrollPosition == 0 {
		ac.isScrolling = false
	}

	ac.redrawContent()
}

// redrawContent redraws the content area with the current scroll position
func (ac *AgentConsole) redrawContent() {
	// Get content region boundaries
	contentTop, contentBottom := ac.autoLayoutManager.GetScrollRegion()
	height := contentBottom - contentTop + 1

	// Get visible lines from buffer
	lines := ac.consoleBuffer.GetVisibleLines(height)

	// Save cursor position
	ac.Terminal().SaveCursor()

	// Clear content area first
	ac.Terminal().MoveCursor(1, contentTop)
    for i := 0; i < height; i++ {
        // Clear entire line then draw gutter
        ac.Terminal().ClearLine()
        if ac.focusMode == "output" {
            ac.Terminal().WriteText("\033[36m││\033[0m ")
        } else {
            ac.Terminal().WriteText("   ")
        }
        if i < height-1 {
            ac.Terminal().WriteText("\r\n")
        }
    }

    // Draw visible lines
    // If output focused, start content at column 4 (after 2-bar gutter + padding)
    startX := 1
    if ac.focusMode == "output" {
        startX = 4
    }
	for i := 0; i < len(lines); i++ {
		ac.Terminal().MoveCursor(startX, contentTop+i)
		ac.Terminal().ClearToEndOfLine()
		ac.Terminal().WriteText(lines[i])
	}

	// Draw help overlay (non-destructive overlay on top of content)
	if ac.showOutputHelp && ac.focusMode == "output" {
		width, _, _ := ac.Terminal().GetSize()
		boxW := width - startX - 2
		if boxW > 60 {
			boxW = 60
		}
		if boxW < 24 {
			boxW = 24
		}
		help := []string{
			"Output Navigation Help",
			"",
			"j / k        • scroll down / up one line",
			"PgDn / PgUp  • scroll page down / up",
			"Ctrl-D / U   • half-page down / up",
			"gg / G       • jump to top / bottom",
			"Tab          • toggle focus (input/output)",
			"Esc          • interrupt current operation",
			"?            • toggle this help",
		}
		// Position overlay below top content line
		boxX := startX
		boxY := contentTop + 1
		horizontal := strings.Repeat("-", boxW)
		ac.Terminal().MoveCursor(boxX, boxY)
		ac.Terminal().WriteText("+" + horizontal + "+")
		y := boxY + 1
		for _, line := range help {
			if len(line) > boxW {
				line = line[:boxW]
			}
			padding := boxW - len(line)
			ac.Terminal().MoveCursor(boxX, y)
			ac.Terminal().WriteText("|" + line + strings.Repeat(" ", padding) + "|")
			y++
			if y > contentBottom-1 {
				break
			}
		}
		if y <= contentBottom {
			ac.Terminal().MoveCursor(boxX, y)
			ac.Terminal().WriteText("+" + horizontal + "+")
		}
	}

	// Add scroll indicator if scrolling
	if ac.isScrolling {
		stats := ac.consoleBuffer.GetStats()
		if stats.ScrollPosition > 0 {
			indicator := fmt.Sprintf(" [Scroll: -%d lines] ", stats.ScrollPosition)
			// Draw indicator at top right of content area
			width, _, _ := ac.Terminal().GetSize()
			ac.Terminal().MoveCursor(width-len(indicator), contentTop)
			ac.Terminal().WriteText("\033[7m" + indicator + "\033[0m") // Inverse video
		}
	}

	// Restore cursor position
	ac.Terminal().RestoreCursor()
}
