// Simple enhanced agent command with web UI support
package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/agent_commands"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/console"
	"github.com/alantheprice/ledit/pkg/events"
	"github.com/alantheprice/ledit/pkg/security_validator"
	"github.com/alantheprice/ledit/pkg/utils"
	"github.com/alantheprice/ledit/pkg/webui"
	"golang.org/x/term"
)

var (
	disableWebUI bool
	webPort      int
)

func init() {
	agentCmd.Flags().BoolVar(&disableWebUI, "no-web-ui", false, "Disable web UI")
	agentCmd.Flags().IntVar(&webPort, "web-port", 0, "Port for web UI (default: auto-find available port starting from 54321)")
}

// runSimpleEnhancedMode runs the new enhanced mode with web UI
func runSimpleEnhancedMode(chatAgent *agent.Agent, isInteractive bool, args []string) error {
	// Determine if web UI should be enabled
	enableWebUI := !disableWebUI && !isCI()

	// Create event bus
	eventBus := events.NewEventBus()

	// Create a single cancellable context for the entire application
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create web server if enabled
	var webServer *webui.ReactWebServer
	if enableWebUI {
		// Connect agent to event bus for real-time UI updates
		chatAgent.SetEventBus(eventBus)

		// Determine port: use specified port or auto-find from 54321
		port := webPort
		if port == 0 {
			port = webui.FindAvailablePort(54321)
		}

		webServer = webui.NewReactWebServer(chatAgent, eventBus, port)

		// Start web server in background
		go func() {
			if err := webServer.Start(ctx); err != nil && ctx.Err() == nil {
				// Only log error if not due to context cancellation
				fmt.Fprintf(os.Stderr, "⚠️  Web UI failed to start: %v\n", err)
			}
		}()

		// Give web server a moment to start
		time.Sleep(100 * time.Millisecond)
		if webServer.IsRunning() {
			fmt.Printf("🌐 Web UI available at http://localhost:%d\n", webServer.GetPort())
		}
	}

	// Setup signal handling with buffered channel for multiple signals
	// Note: We intentionally do NOT capture SIGTSTP (Ctrl+Z) to allow process suspension
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Handle shutdown gracefully
	shutdown := make(chan struct{})
	go func() {
		sigCount := 0
		for {
			select {
			case sig := <-sigCh:
				sigCount++
				if sigCount == 1 {
					fmt.Printf("\n🛑 Received signal %v, shutting down gracefully...\n", sig)
					fmt.Printf("  (Press Ctrl+C again to force quit)\n")

					// Cancel the context which will stop all operations
					cancel()

					// Signal that shutdown has started
					close(shutdown)

					// Start a timeout goroutine for force quit
					go func() {
						time.Sleep(5 * time.Second)
						fmt.Printf("\n⚡ Force quitting...\n")
						os.Exit(1)
					}()
				} else {
					// Multiple Ctrl+C presses - force quit immediately
					fmt.Printf("\n⚡ Force quitting immediately...\n")
					os.Exit(1)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Set up event publishing for agent
	setupAgentEvents(chatAgent, eventBus)

	// Handle different modes
	var err error
	if isInteractive {
		err = runNewInteractiveMode(ctx, chatAgent, eventBus)
	} else {
		// Direct mode
		var query string
		if len(args) > 0 {
			query = strings.Join(args, " ")
		} else if !term.IsTerminal(int(os.Stdin.Fd())) {
			// Read from stdin - but first check if it's actually available
			stat, statErr := os.Stdin.Stat()
			if statErr == nil && (stat.Mode()&os.ModeCharDevice) == 0 {
				// stdin is not a character device (e.g., pipe or file), try to read
				scanner := bufio.NewScanner(os.Stdin)
				if scanner.Scan() {
					query = scanner.Text()
				}
				// Check if scan encountered an error (like "resource temporarily unavailable")
				if err := scanner.Err(); err != nil {
					// stdin not available - ignore and show welcome message
					query = ""
				}
			}
		}

		if query == "" {
			fmt.Println("Welcome to ledit! 🤖")
			fmt.Println("Agent initialized successfully.")
			fmt.Println("Use 'ledit agent \"your query\"' to execute commands.")
			return nil
		}

		err = runDirectMode(ctx, chatAgent, eventBus, query)
	}

	// Graceful shutdown
	if webServer != nil && webServer.IsRunning() {
		fmt.Printf("🔄 Shutting down web server...\n")

		if webErr := webServer.Shutdown(); webErr != nil {
			fmt.Fprintf(os.Stderr, "⚠️  Error shutting down web server: %v\n", webErr)
		} else {
			fmt.Printf("✅ Web server shut down successfully\n")
		}
	}

	// Check if context was cancelled due to interrupt
	if ctx.Err() == context.Canceled {
		select {
		case <-shutdown:
			fmt.Printf("👋 Shutdown complete\n")
		default:
			fmt.Printf("👋 Goodbye!\n")
		}
	}

	return err
}

// setupAgentEvents configures the agent to publish events to the event bus
func setupAgentEvents(chatAgent *agent.Agent, eventBus *events.EventBus) {
	// Set up streaming callback (unless disabled)
	if !agentNoStreaming {
		chatAgent.EnableStreaming(func(chunk string) {
			eventBus.Publish(events.EventTypeStreamChunk, events.StreamChunkEvent(chunk))
		})
	}
}

// runNewInteractiveMode handles interactive mode with web UI
func runNewInteractiveMode(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus) error {
	fmt.Printf("\n🤖 Welcome to ledit! Enhanced CLI with Web UI\n")
	fmt.Printf("📊 Provider: %s | Model: %s\n\n",
		chatAgent.GetProvider(),
		chatAgent.GetModel())

	// Create enhanced input reader with completion support
	inputReader := console.NewInputReader("ledit> ")

	// Initialize with existing history from agent
	inputReader.SetHistory(chatAgent.GetHistory())

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			query, err := inputReader.ReadLine()

			if err != nil {
				if err.Error() == "interrupted" {
					fmt.Println("Use 'exit' or 'quit' to exit.")
					continue
				}
				return err
			}

			query = strings.TrimSpace(query)
			if query == "" {
				continue
			}

			// Add to agent history
			chatAgent.AddToHistory(query)
			// Update input reader history to stay in sync
			inputReader.SetHistory(chatAgent.GetHistory())

			// Handle exit commands
			if strings.ToLower(query) == "exit" || strings.ToLower(query) == "quit" {
				fmt.Println("\n👋 Goodbye! Here's your session summary:")
				fmt.Println("=====================================")
				chatAgent.PrintConversationSummary(true)
				return nil
			}

			// Try fast path: direct command execution
			if executed, err := tryDirectExecution(ctx, chatAgent, query); err != nil {
				fmt.Fprintf(os.Stderr, "❌ Error: %v\n", err)
			} else if !executed {
				// Fast path didn't trigger, process normally
				if err := processQuery(ctx, chatAgent, eventBus, query); err != nil {
					fmt.Fprintf(os.Stderr, "❌ Error: %v\n", err)
				}
			}
		}
	}
}

// tryDirectExecution attempts to execute simple commands directly using local LLM
// Returns true if command was executed directly, false if normal agent flow should proceed
func tryDirectExecution(ctx context.Context, chatAgent *agent.Agent, query string) (bool, error) {
	// Get the tool registry (has security validator with local LLM access)
	registry := agent.GetToolRegistry()
	if registry == nil {
		return false, nil
	}
	validator := registry.GetValidator()
	if validator == nil {
		// Try to initialize the validator
		agentConfig := chatAgent.GetConfig()
		if agentConfig == nil {
			return false, nil
		}

		// If SecurityValidation is not configured, create a default one
		var securityConfig *configuration.SecurityValidationConfig
		if agentConfig.SecurityValidation == nil {
			securityConfig = &configuration.SecurityValidationConfig{
				Enabled:        true,
				Model:          "qwen2.5-coder:1.5b",
				Threshold:      1,
				TimeoutSeconds: 10,
			}
		} else {
			securityConfig = agentConfig.SecurityValidation
		}

		if !securityConfig.Enabled {
			return false, nil
		}

		// Create a logger
		isNonInteractive := agentConfig.SkipPrompt
		logger := utils.GetLogger(isNonInteractive)

		// Create the validator
		newValidator, err := security_validator.NewValidator(securityConfig, logger, !isNonInteractive)
		if err != nil {
			return false, nil // Failed to create validator, proceed with normal flow
		}
		validator = newValidator
	}

	// Use the LLM to detect if this is a direct command request
	isDirect, detectedCommand, confidence, err := validator.DetectDirectCommand(ctx, query)
	if err != nil {
		// If LLM fails, just proceed with normal agent flow
		return false, nil
	}

	// Only execute directly if high confidence (>0.8)
	if isDirect && confidence > 0.8 && detectedCommand != "" {
		fmt.Printf("⚡ Fast path: %s\n", detectedCommand)

		// Execute the command directly using bash
		output, err := executeCommand(detectedCommand)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			fmt.Printf("%s\n", output)
		}
		return true, nil
	}

	return false, nil
}

// executeCommand runs a shell command and returns its output
func executeCommand(cmd string) (string, error) {
	// Run command through bash -c
	output, err := exec.Command("bash", "-c", cmd).CombinedOutput()
	if err != nil {
		return string(output), err
	}
	return string(output), nil
}

// runDirectMode handles direct command execution
func runDirectMode(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus, query string) error {
	fmt.Printf("🚀 Processing: %s\n", query)

	// Try fast path: direct command execution
	if executed, err := tryDirectExecution(ctx, chatAgent, query); err != nil {
		return err
	} else if executed {
		// Command was executed directly, skip normal agent flow
		return nil
	}

	// Proceed with normal agent flow
	return processQuery(ctx, chatAgent, eventBus, query)
}

// processQuery processes a single query
func processQuery(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus, query string) error {
	// Check if this is a slash command
	registry := commands.NewCommandRegistry()
	if registry.IsSlashCommand(query) {
		if err := registry.Execute(query, chatAgent); err != nil {
			// For slash commands, show error and exit immediately
			fmt.Fprintf(os.Stderr, "❌ Slash command error: %v\n", err)
			fmt.Fprintf(os.Stderr, "💡 Use '/help' to see available commands\n")
			return fmt.Errorf("slash command failed: %w", err)
		}
		return nil
	}

	// Publish query started event
	eventBus.Publish(events.EventTypeQueryStarted, events.QueryStartedEvent(
		query,
		chatAgent.GetProvider(),
		chatAgent.GetModel(),
	))

	startTime := time.Now()

	// Process the query using CI output handler for clean output
	outputHandler := console.NewCIOutputHandler(os.Stdout)

	// Set up progress callback
	chatAgent.SetStatsUpdateCallback(func(totalTokens int, totalCost float64) {
		// Update the CI output handler metrics
		outputHandler.UpdateMetrics(
			totalTokens,
			chatAgent.GetCurrentContextTokens(),
			chatAgent.GetMaxContextTokens(),
			chatAgent.GetCurrentIteration(),
			totalCost,
		)

		// Print progress if in CI mode
		if outputHandler.ShouldShowProgress() {
			outputHandler.PrintProgress()
		}

		// Also publish to event bus for web UI
		eventBus.Publish(events.EventTypeMetricsUpdate, events.MetricsUpdateEvent(
			totalTokens,
			chatAgent.GetCurrentContextTokens(),
			chatAgent.GetMaxContextTokens(),
			chatAgent.GetCurrentIteration(),
			totalCost,
		))
	})

	// Replace streaming callback for direct output (unless disabled)
	if !agentNoStreaming {
		chatAgent.EnableStreaming(func(chunk string) {
			outputHandler.Write([]byte(chunk))
			eventBus.Publish(events.EventTypeStreamChunk, events.StreamChunkEvent(chunk))
		})
		defer chatAgent.DisableStreaming()
	}

	// Run agent processing in a goroutine to support cancellation
	type result struct {
		response string
		err      error
	}

	resultCh := make(chan result, 1)
	go func() {
		response, err := chatAgent.ProcessQueryWithContinuity(query)
		resultCh <- result{response, err}
	}()

	// Wait for either completion or cancellation
	select {
	case res := <-resultCh:
		duration := time.Since(startTime)

		if res.err != nil {
			eventBus.Publish(events.EventTypeError, events.ErrorEvent(
				fmt.Sprintf("Failed to process query: %s", query), res.err,
			))
			return fmt.Errorf("agent processing failed: %w", res.err)
		}

		// Publish query completed event
		eventBus.Publish(events.EventTypeQueryCompleted, events.QueryCompletedEvent(
			query,
			res.response,
			chatAgent.GetCurrentContextTokens(),
			chatAgent.GetTotalCost(),
			duration,
		))

		// Print completion message without automatic summary (use /stats to see summary)
		fmt.Printf("✅ Completed in %s\n", formatDuration(duration))

		return nil

	case <-ctx.Done():
		// Context was cancelled - agent processing was interrupted
		duration := time.Since(startTime)
		fmt.Printf("\n⏹️ Query interrupted after %s\n", formatDuration(duration))

		eventBus.Publish(events.EventTypeError, events.ErrorEvent(
			fmt.Sprintf("Query interrupted: %s", query), ctx.Err(),
		))
		return fmt.Errorf("query interrupted: %w", ctx.Err())
	}
}

// getCompletions provides tab completion for commands and files
func getCompletions(input string, chatAgent *agent.Agent) []string {
	var completions []string

	// Get current word for completion
	words := strings.Fields(input)
	if len(words) == 0 {
		return completions
	}

	currentWord := words[len(words)-1]

	// If it starts with '/', complete slash commands
	if strings.HasPrefix(currentWord, "/") {
		registry := commands.NewCommandRegistry()
		commands := registry.ListCommands()
		for _, cmd := range commands {
			if strings.HasPrefix(cmd.Name(), currentWord[1:]) {
				completions = append(completions, "/"+cmd.Name())
			}
		}
	} else {
		// File path completion
		if strings.Contains(currentWord, "/") || len(words) == 1 {
			// Simple file completion
			matches, _ := filepath.Glob(currentWord + "*")
			for _, match := range matches {
				if info, err := os.Stat(match); err == nil {
					if info.IsDir() {
						match += "/"
					}
					completions = append(completions, match)
				}
			}
		}
	}

	return completions
}

// isCI checks if running in CI environment
func isCI() bool {
	return os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != ""
}

// formatDuration formats duration in human readable format
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	} else if d < time.Hour {
		return fmt.Sprintf("%.1fm", d.Minutes())
	}
	return fmt.Sprintf("%.1fh", d.Hours())
}
