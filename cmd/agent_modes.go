// Agent modes: handles interactive and direct execution modes
package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/console"
	"github.com/alantheprice/ledit/pkg/events"
	"github.com/alantheprice/ledit/pkg/webui"
	"golang.org/x/term"
)

var queryInProgress atomic.Bool

func setQueryInProgress(active bool) {
	queryInProgress.Store(active)
}

func isQueryInProgress() bool {
	return queryInProgress.Load()
}

// RunAgent runs the agent in interactive or direct mode
func RunAgent(chatAgent *agent.Agent, isInteractive bool, args []string) error {
	// Determine if web UI should be enabled
	// Web UI requires: interactive mode, daemon mode, not disabled, and not in CI/subagent
	enableWebUI := (isInteractive || daemonMode) && !disableWebUI && !IsCI()

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
		var lastInterruptAt int64
		for {
			select {
			case sig := <-sigCh:
				if isInteractive && isQueryInProgress() {
					nowUnix := time.Now().UnixNano()
					prev := atomic.LoadInt64(&lastInterruptAt)
					if prev > 0 && time.Duration(nowUnix-prev) < 2*time.Second {
						fmt.Printf("\n⚡ Force quitting immediately...\n")
						os.Exit(1)
					}

					atomic.StoreInt64(&lastInterruptAt, nowUnix)
					fmt.Printf("\n⏸️ Received signal %v, interrupting active task...\n", sig)
					fmt.Printf("  (Press Ctrl+C again quickly to force quit)\n")
					chatAgent.TriggerInterrupt()
					continue
				}

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

				// Any subsequent signal after shutdown starts should force quit.
				for {
					select {
					case <-sigCh:
						fmt.Printf("\n⚡ Force quitting immediately...\n")
						os.Exit(1)
					case <-ctx.Done():
						return
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Set up event publishing for agent
	SetupAgentEvents(chatAgent, eventBus)

	// Handle different modes
	var err error
	if isInteractive {
		err = runInteractiveMode(ctx, chatAgent, eventBus)
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
			// No query provided - check if we should keep running (daemon mode)
			if daemonMode && webServer != nil && webServer.IsRunning() {
				// Daemon mode: keep web UI running
				fmt.Printf("🌐 Web UI running at http://localhost:%d\n", webServer.GetPort())
				fmt.Println("Press Ctrl+C to stop the server.")

				// Wait for interrupt signal
				<-ctx.Done()
				return nil
			}
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
func SetupAgentEvents(chatAgent *agent.Agent, eventBus *events.EventBus) {
	// Set up streaming callback (unless disabled)
	if !agentNoStreaming {
		chatAgent.EnableStreaming(func(chunk string) {
			eventBus.Publish(events.EventTypeStreamChunk, events.StreamChunkEvent(chunk))
		})
	}
}

// runInteractiveMode handles interactive REPL mode
func runInteractiveMode(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus) error {
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

			// Try zsh command detection first (fast path)
			if executed, err := TryZshCommandExecution(ctx, chatAgent, query); err != nil {
				fmt.Fprintf(os.Stderr, "❌ Error: %v\n", err)
			} else if !executed {
				// Zsh detection didn't trigger, try LLM-based detection
				if executed, err := TryDirectExecution(ctx, chatAgent, query); err != nil {
					fmt.Fprintf(os.Stderr, "❌ Error: %v\n", err)
				} else if !executed {
					// Neither fast path triggered, process normally
					if err := ProcessQuery(ctx, chatAgent, eventBus, query); err != nil {
						fmt.Fprintf(os.Stderr, "❌ Error: %v\n", err)
					}
				}
			}
		}
	}
}

// runDirectMode handles single query execution
func runDirectMode(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus, query string) error {
	fmt.Printf("🚀 Processing: %s\n", query)

	// Try zsh command detection first
	if executed, err := TryZshCommandExecution(ctx, chatAgent, query); err != nil {
		return err
	} else if executed {
		// Command was executed directly, skip normal agent flow
		return nil
	}

	// Try LLM-based fast path: direct command execution
	if executed, err := TryDirectExecution(ctx, chatAgent, query); err != nil {
		return err
	} else if executed {
		// Command was executed directly, skip normal agent flow
		return nil
	}

	// Proceed with normal agent flow
	return ProcessQuery(ctx, chatAgent, eventBus, query)
}
