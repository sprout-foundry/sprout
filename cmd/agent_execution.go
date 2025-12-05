// Simple enhanced agent command with web UI support
package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/agent_commands"
	"github.com/alantheprice/ledit/pkg/console"
	"github.com/alantheprice/ledit/pkg/events"
	"github.com/alantheprice/ledit/pkg/webui"
	"golang.org/x/term"
)

var (
	disableWebUI bool
)

func init() {
	agentCmd.Flags().BoolVar(&disableWebUI, "no-web-ui", false, "Disable web UI")
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
		webServer = webui.NewReactWebServer(chatAgent, eventBus, 54321)

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
			// Read from stdin
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				query = scanner.Text()
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
	// Set up stats callback
	chatAgent.SetStatsUpdateCallback(func(totalTokens int, totalCost float64) {
		eventBus.Publish(events.EventTypeMetricsUpdate, events.MetricsUpdateEvent(
			totalTokens,
			chatAgent.GetCurrentContextTokens(),
			chatAgent.GetMaxContextTokens(),
			chatAgent.GetCurrentIteration(),
			totalCost,
		))
	})

	// Set up streaming callback
	chatAgent.EnableStreaming(func(chunk string) {
		eventBus.Publish(events.EventTypeStreamChunk, events.StreamChunkEvent(chunk))
	})
}

// runNewInteractiveMode handles interactive mode with web UI
func runNewInteractiveMode(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus) error {
	fmt.Printf("\n🤖 Welcome to ledit! Enhanced CLI with Web UI\n")
	fmt.Printf("📊 Provider: %s | Model: %s\n\n",
		chatAgent.GetProvider(),
		chatAgent.GetModel())

	scanner := bufio.NewScanner(os.Stdin)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			fmt.Print("ledit> ")

			if !scanner.Scan() {
				return nil
			}

			query := strings.TrimSpace(scanner.Text())
			if query == "" {
				continue
			}

			// Handle exit commands
			if strings.ToLower(query) == "exit" || strings.ToLower(query) == "quit" {
				fmt.Println("Goodbye! 👋")
				return nil
			}

			// Process the query
			if err := processQuery(ctx, chatAgent, eventBus, query); err != nil {
				fmt.Fprintf(os.Stderr, "❌ Error: %v\n", err)
			}
		}
	}
}

// runDirectMode handles direct command execution
func runDirectMode(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus, query string) error {
	fmt.Printf("🚀 Processing: %s\n", query)
	return processQuery(ctx, chatAgent, eventBus, query)
}

// processQuery processes a single query
func processQuery(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus, query string) error {
	// Check if this is a slash command
	registry := commands.NewCommandRegistry()
	if registry.IsSlashCommand(query) {
		return registry.Execute(query, chatAgent)
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

	// Replace streaming callback for direct output
	chatAgent.EnableStreaming(func(chunk string) {
		outputHandler.Write([]byte(chunk))
		eventBus.Publish(events.EventTypeStreamChunk, events.StreamChunkEvent(chunk))
	})
	defer chatAgent.DisableStreaming()

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

		// Print completion summary
		fmt.Printf("\n✅ Completed in %s\n", formatDuration(duration))
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
