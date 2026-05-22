//go:build !js

// Agent modes: handles interactive and direct execution modes
package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	agent_commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/webcontent"
	"github.com/sprout-foundry/sprout/pkg/webui"
	"golang.org/x/term"
)

// isServiceMode returns true when sprout is running as a managed system
// service (systemd, launchd). In service mode, terminal prompts and
// "Press Ctrl+C" messages are suppressed since there is no interactive
// terminal.
func isServiceMode() bool {
	return configuration.GetEnvSimple("SERVICE") == "1"
}

var queryInProgress atomic.Bool

func setQueryInProgress(active bool) {
	queryInProgress.Store(active)
}

func isQueryInProgress() bool {
	return queryInProgress.Load()
}

func ensureContinuationSessionID(chatAgent *agent.Agent) string {
	if chatAgent == nil {
		return ""
	}
	sessionID := strings.TrimSpace(chatAgent.GetSessionID())
	if sessionID == "" {
		sessionID = fmt.Sprintf("session_%d", time.Now().UnixNano())
		chatAgent.SetSessionID(sessionID)
	}
	return sessionID
}

func printContinuationHint(chatAgent *agent.Agent) {
	sessionID := ensureContinuationSessionID(chatAgent)
	if sessionID == "" {
		return
	}
	fmt.Printf("To Continue: `sprout agent --session-id %s`\n", sessionID)
}

// RunAgent runs the agent in interactive or direct mode
func RunAgent(chatAgent *agent.Agent, isInteractive bool, args []string) (err error) {
	ensureContinuationSessionID(chatAgent)
	workflowConfig, workflowLoadErr := loadAgentWorkflowConfig(agentWorkflowConfig)
	if workflowLoadErr != nil {
		return workflowLoadErr
	}
	applyWorkflowCommandOverrides(workflowConfig)

	// When a workflow config defines an initial prompt, force non-interactive
	// (direct) mode. Without this, the isInteractive branch calls
	// runInteractiveMode which never consults the workflow config, so the
	// user sees a blank REPL instead of the workflow executing.
	if workflowConfig != nil && workflowConfig.Initial != nil &&
		(strings.TrimSpace(workflowConfig.Initial.Prompt) != "" || strings.TrimSpace(workflowConfig.Initial.PromptFile) != "") {
		isInteractive = false
	}

	// Determine if web UI should be enabled
	// Web UI requires: interactive mode, daemon mode, not disabled, and not in CI/subagent
	enableWebUI := (isInteractive || daemonMode) && !disableWebUI && !IsCI()

	// Propagate daemon mode to child processes (subagents, agent.NewAgentWithLayers)
	// so that lazy agent creation in the webui does not fast-fail with
	// "no provider configured" when the webui can handle provider setup interactively.
	if daemonMode {
		os.Setenv("SPROUT_DAEMON", "1")

		// Set up log rotation for managed daemon services (SPROUT_SERVICE=1).
		// This must happen early, before any stdout/stderr writes, so that
		// all subsequent output is captured by the rotating log files.
		homeDir, homeErr := os.UserHomeDir()
		if homeErr != nil {
			fmt.Fprintf(os.Stderr, "[WARN] Could not determine home directory, skipping daemon log rotation: %v\n", homeErr)
		} else {
			setupDaemonLogging(homeDir)
		}
	}

	// Create event bus
	eventBus := events.NewEventBus()

	// Always wire the agent's event bus so terminal subscribers (activity
	// indicator, tool timeline) receive PublishToolStart / PublishToolEnd
	// even when the WebUI is disabled. SP-048-1.
	chatAgent.SetEventBus(eventBus)

	// Create a single cancellable context for the entire application
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create web server if enabled
	var webServer *webui.ReactWebServer
	var webUISup *webUISupervisor

	// Resolve bind address early so it's available in all code paths.
	// --bind flag → SPROUT_BIND_ADDR env var → "127.0.0.1" default
	bindAddr := webBindAddr
	if bindAddr == "" {
		bindAddr = configuration.GetEnvSimple("BIND_ADDR")
	}
	if bindAddr == "" {
		bindAddr = "127.0.0.1"
	}

	// Validate the bind address is a plausible IP or hostname.
	if bindAddr != "localhost" && net.ParseIP(bindAddr) == nil {
		return fmt.Errorf("invalid bind address %q: must be a valid IP address", bindAddr)
	}

	if enableWebUI {
		// Warn when binding to all interfaces
		if bindAddr == "0.0.0.0" || bindAddr == "::" {
			fmt.Fprintf(os.Stderr, "[WARN] Binding to %s — web UI is accessible from all network interfaces\n", bindAddr)
		}

		// Determine port strategy.
		//
		// Daemon mode (no explicit port): use the single-port supervisor on
		// the unified daemon port (56000) so all daemons compete for one
		// stable port.  This is the "primary" instance users bookmark.
		//
		// Non-daemon interactive (no explicit port): each instance gets its
		// own unique port so browser windows can connect independently.
		// We scan from 56001 (DaemonPort+1) for a free port.
		//
		// Explicit --web-port N: always start directly on that port,
		// regardless of daemon mode.
		port := webPort
		if port == 0 {
			if daemonMode {
				port = webui.DaemonPort
			} else {
				// Non-daemon: find a free dynamic port.
				dynamicPort, dynErr := webui.FindAvailablePort(webui.DaemonPort + 1)
				if dynErr != nil {
					fmt.Fprintf(os.Stderr, "[WARN] Could not find a dynamic port: %v; web UI disabled\n", dynErr)
					enableWebUI = false
				} else {
					port = dynamicPort
				}
			}
		}

		if enableWebUI {
			var webErr error
			webServer, webErr = webui.NewReactWebServer(chatAgent, eventBus, port, bindAddr)
			if webErr != nil {
				log.Fatalf("%v", webErr)
			}

			// Inject webui-owned managers into the agent so that security
			// prompts and ask_user requests route through the same instances
			// the webui handlers resolve responses on — no global singletons.
			chatAgent.InjectWebUIManagers(webServer.GetSecurityPromptMgr(), webServer.GetAskUserMgr())

			// Wire up the WebUI client check so security prompts route
			// correctly: use the event bus only when a browser tab is open,
			// otherwise fall back to CLI prompting (avoids 5-min timeouts).
			chatAgent.SetHasActiveWebUIClients(webServer.HasActiveWebUIClients)

			startInstanceTracker(ctx, port, chatAgent)

			// Daemon mode without explicit port → single-port supervisor.
			if webPort == 0 && daemonMode {
				webUISup = newWebUISupervisor(
					webServer,
					port,
					func(activePort int) {
						fmt.Printf("\n[web] Web UI available at http://%s:%d\n", webui.DisplayAddr(bindAddr), activePort)
					},
					func(activePort int) {
						fmt.Printf("\n[web] Reusing active Web UI at http://%s:%d\n", webui.DisplayAddr(bindAddr), activePort)
					},
				)
				go webUISup.Run(ctx)

				// Wait for web server to start running before proceeding
				startupDeadline := time.NewTimer(5 * time.Second)
				defer startupDeadline.Stop()
				startupPoll := time.NewTicker(50 * time.Millisecond)
				defer startupPoll.Stop()

			daemonStartupLoop:
				for {
					if webServer.IsRunning() {
						break
					}

					select {
					case <-startupDeadline.C:
						if !webServer.IsRunning() {
							return fmt.Errorf("web UI failed to start on port %d (daemon mode)", port)
						}
						break daemonStartupLoop
					case <-startupPoll.C:
					}
				}
			} else {
				// Explicit port OR non-daemon dynamic port: start directly.
				startErrCh := make(chan error, 1)
				go func() {
					if err := webServer.Start(ctx); err != nil && ctx.Err() == nil {
						select {
						case startErrCh <- err:
						default:
						}
						fmt.Fprintf(os.Stderr, "[WARN] Web UI failed to start: %v\n", err)
					}
				}()

				startupDeadline := time.NewTimer(1500 * time.Millisecond)
				defer startupDeadline.Stop()
				startupPoll := time.NewTicker(50 * time.Millisecond)
				defer startupPoll.Stop()

			loop:
				for {
					if webServer.IsRunning() {
						break
					}

					select {
					case startErr := <-startErrCh:
						return fmt.Errorf("web UI failed to start on port %d: %w", port, startErr)
					case <-startupDeadline.C:
						if !webServer.IsRunning() {
							return fmt.Errorf("web UI failed to start on port %d", port)
						}
						break loop
					case <-startupPoll.C:
					}
				}

				fmt.Printf("\n[web] Web UI available at http://%s:%d\n", webui.DisplayAddr(bindAddr), webServer.GetPort())
			}
		}
	}

	// Setup signal handling with buffered channel for multiple signals
	// Note: We intentionally do NOT capture SIGTSTP (Ctrl+Z) to allow process suspension
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	// Handle shutdown gracefully
	shutdown := make(chan struct{})
	go func() {
		var lastInterruptAt int64
		for {
			select {
			case sig := <-sigCh:
				// SIGHUP: reload on-disk config without shutting down.
				if sig == syscall.SIGHUP {
					fmt.Printf("\n[RELOAD] Received SIGHUP, reloading configuration...\n")
					if mgr := chatAgent.GetConfigManager(); mgr != nil {
						if err := mgr.Reload(); err != nil {
							fmt.Printf("[RELOAD] Failed: %v\n", err)
						} else {
							fmt.Printf("[RELOAD] Configuration reloaded successfully.\n")
						}
					}
					continue
				}

				if isInteractive && isQueryInProgress() {
					nowUnix := time.Now().UnixNano()
					prev := atomic.LoadInt64(&lastInterruptAt)
					if prev > 0 && time.Duration(nowUnix-prev) < 2*time.Second {
						fmt.Printf("\n[!] Force quitting immediately...\n")
						os.Exit(1)
					}

					atomic.StoreInt64(&lastInterruptAt, nowUnix)
					fmt.Printf("\n[||] Received signal %v, interrupting active task...\n", sig)
					fmt.Printf("  (Press Ctrl+C again quickly to force quit)\n")
					chatAgent.TriggerInterrupt()
					continue
				}

				fmt.Printf("\n[STOP] Received signal %v, shutting down gracefully...\n", sig)
				fmt.Printf("  (Press Ctrl+C again to force quit)\n")

				// Cancel the context which will stop all operations
				cancel()

				// Close the global browser renderer to release Chromium resources
				webcontent.CloseGlobalBrowser()

				// Signal that shutdown has started
				close(shutdown)

				// Start a timeout goroutine for force quit
				go func() {
					time.Sleep(5 * time.Second)
					fmt.Printf("\n[!] Force quitting...\n")
					os.Exit(1)
				}()

				// Any subsequent signal after shutdown starts should force quit.
				for {
					select {
					case <-sigCh:
						fmt.Printf("\n[!] Force quitting immediately...\n")
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

	// SP-048-1: Activity indicator renders the "Thinking…" spinner during the
	// gap between user submit and first stream chunk, and shows per-tool
	// progress lines via tool events. Suppressed automatically on non-TTY.
	indicator := console.NewActivityIndicator(os.Stderr)

	// Register globally so CLI prompt sites that can't import pkg/console
	// (logger.AskForConfirmation, AskUser stdin reads, provider-recovery
	// prompts) can call clihooks.SuspendIndicator() to clear the spinner
	// before rendering. Without this, the spinner would overwrite the
	// prompt text on stderr while the prompt is on stdout.
	console.RegisterGlobalIndicator(indicator)

	// Set up event publishing for agent
	SetupAgentEvents(chatAgent, eventBus, indicator)

	// Check for queue mode before interactive mode
	if chatAgent.GetConfigManager().GetConfig().GetEAMode() == "queue" {
		return runQueueMode(ctx, chatAgent, eventBus)
	}

	// Handle different modes
	if isInteractive {
		if err := chatAgent.GetConfigManager().UpdateConfigNoSave(func(cfg *configuration.Config) error {
			cfg.SkipPrompt = agentSkipPrompt
			return nil
		}); err != nil {
			return fmt.Errorf("failed to update config for interactive mode: %w", err)
		}

		// Check if we should prompt for GitHub MCP setup (interactive, non-SkipPrompt)
		promptGitHubMCPSetupIfNeeded(&AgentAdapter{agent: chatAgent})

		err = runInteractiveMode(ctx, chatAgent, eventBus, indicator)
	} else {
		directModeStart := time.Now()
		if err := chatAgent.GetConfigManager().UpdateConfigNoSave(func(cfg *configuration.Config) error {
			cfg.SkipPrompt = true
			return nil
		}); err != nil {
			return fmt.Errorf("failed to update config for direct mode: %w", err)
		}

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

		query, err = resolveWorkflowInitialPrompt(query, workflowConfig)
		if err != nil {
			return fmt.Errorf("failed to resolve workflow initial prompt: %w", err)
		}
		if query == "" && (workflowConfig == nil || len(workflowConfig.Steps) == 0) {
			// No query provided - check if we should keep running (daemon mode)
			if daemonMode && webServer != nil && webServer.IsRunning() {
				// Daemon mode: keep web UI running
				fmt.Printf("\n[web] Web UI running at http://%s:%d\n", webui.DisplayAddr(bindAddr), webServer.GetPort())
				if !isServiceMode() {
					fmt.Println("Press Ctrl+C to stop the server.")
				}

				// Wait for interrupt signal
				<-ctx.Done()
				return nil
			}
			fmt.Println("Welcome to sprout! [bot]")
			fmt.Println("Agent initialized successfully.")
			fmt.Println("Use 'sprout agent \"your query\"' to execute commands.")
			return nil
		}

		restoreRuntimeOverrides, restoreSetupErr := prepareWorkflowRuntimeRestorer(chatAgent, workflowConfig)
		if restoreSetupErr != nil {
			return fmt.Errorf("failed to prepare runtime override restoration: %w", restoreSetupErr)
		}
		if restoreRuntimeOverrides != nil {
			defer func() {
				if restoreErr := restoreRuntimeOverrides(); restoreErr != nil {
					if err == nil {
						err = restoreErr
					} else {
						err = fmt.Errorf("%w (restore failed: %w)", err, restoreErr)
					}
				}
			}()
		}
		workflowState, workflowStateErr := loadWorkflowExecutionState(workflowConfig)
		if workflowStateErr != nil {
			return fmt.Errorf("failed to load workflow execution state: %w", workflowStateErr)
		}
		if restoreErr := restoreWorkflowConversationState(chatAgent, workflowConfig, workflowState); restoreErr != nil {
			return fmt.Errorf("failed to restore workflow conversation state: %w", restoreErr)
		}
		if workflowConfig != nil && workflowConfig.orchestrationEnabled() {
			if eventErr := emitWorkflowOrchestrationEvent(workflowConfig, "workflow_run_started", map[string]interface{}{
				"initial_completed": workflowState.InitialCompleted,
				"next_step_index":   workflowState.NextStepIndex,
			}); eventErr != nil {
				return fmt.Errorf("failed to emit workflow run started event: %w", eventErr)
			}
		}

		shouldRunInitialQuery := strings.TrimSpace(query) != "" && !workflowState.InitialCompleted
		if shouldRunInitialQuery {
			if err := applyWorkflowInitialOverrides(chatAgent, workflowConfig); err != nil {
				return fmt.Errorf("failed to apply workflow initial runtime overrides: %w", err)
			}

			err = runDirectMode(ctx, chatAgent, eventBus, query)
			workflowState.InitialCompleted = true
			workflowState.HasError = err != nil
			workflowState.LastProvider = strings.TrimSpace(chatAgent.GetProvider())
			if err != nil {
				workflowState.FirstError = err.Error()
			}
			if persistErr := persistWorkflowCheckpoint(workflowConfig, workflowState, chatAgent); persistErr != nil {
				return fmt.Errorf("failed to persist workflow checkpoint: %w", persistErr)
			}
			if eventErr := emitWorkflowOrchestrationEvent(workflowConfig, "workflow_initial_completed", map[string]interface{}{
				"provider":  workflowState.LastProvider,
				"has_error": workflowState.HasError,
			}); eventErr != nil {
				return fmt.Errorf("failed to emit workflow initial completed event: %w", eventErr)
			}
		} else {
			err = nil
		}

		workflowState.HasError = workflowState.HasError || err != nil
		workflowYielded, workflowErr := runAgentWorkflow(ctx, chatAgent, eventBus, workflowConfig, workflowState)
		if workflowYielded {
			return nil
		}
		if workflowErr != nil {
			if err != nil {
				return fmt.Errorf("%w (workflow execution failed: %w)", err, workflowErr)
			}
			return workflowErr
		}
		// At this point: workflowErr is nil, workflowYielded is false
		// err could be nil or from runDirectMode
		if err != nil {
			if outputFormatJSON {
				emitJSONResult(query, directModeStart, err, chatAgent)
			}
			return fmt.Errorf("failed to run direct mode: %w", err)
		}
		if outputFormatJSON {
			emitJSONResult(query, directModeStart, nil, chatAgent)
		}
		return nil // No error, workflow completed successfully
	}

	// Graceful shutdown
	if chatAgent != nil {
		done := make(chan struct{})
		go func() {
			chatAgent.Shutdown()
			close(done)
		}()
		select {
		case <-done:
			fmt.Printf("[OK] Agent shut down successfully\n")
		case <-time.After(5 * time.Second):
			fmt.Fprintf(os.Stderr, "[WARN] Agent shutdown timed out after 5s\n")
		}
	}
	if webUISup != nil {
		webUISup.cleanupHostRecordIfOwned()
	}
	if webServer != nil && webServer.IsRunning() {
		fmt.Printf("[~] Shutting down web server...\n")

		if webErr := webServer.Shutdown(); webErr != nil {
			fmt.Fprintf(os.Stderr, "[WARN] Error shutting down web server: %v\n", webErr)
		} else {
			fmt.Printf("[OK] Web server shut down successfully\n")
		}
	}

	// Check if context was cancelled due to interrupt
	continuationPrinted := false
	if ctx.Err() == context.Canceled {
		select {
		case <-shutdown:
			fmt.Printf("-- Shutdown complete\n")
		default:
			fmt.Printf("-- Goodbye!\n")
		}
		printContinuationHint(chatAgent)
		continuationPrinted = true
	}

	if !isInteractive && !continuationPrinted {
		printContinuationHint(chatAgent)
	}

	if err != nil {
		return fmt.Errorf("failed to run agent: %w", err)
	}
	return nil
}

// SetupAgentEvents configures the agent for event-driven output routing.
// The OutputRouter handles dual-path delivery (EventBus + terminal)
// so no separate streaming callback is needed here. This function ensures
// the agent's output router is wired to the event bus for WebUI subscribers.
//
// When indicator is non-nil, the streaming callback also stops it on the
// first chunk so any "Thinking…" spinner is cleared before tokens appear.
func SetupAgentEvents(chatAgent *agent.Agent, eventBus *events.EventBus, indicator *console.ActivityIndicator) {
	// Ensure the output router is connected to the event bus.
	// When WebUI is active, events flow to both terminal and WebUI.
	// When WebUI is inactive, events only flow to terminal.
	if router := chatAgent.OutputRouter(); router != nil {
		router.SetEventBus(eventBus)
		router.SetReasoningTerminalEnabled(agentShowReasoningTerminal)
	}

	// Set a simple streaming callback for direct terminal output of
	// assistant text. The OutputRouter's RouteStreamChunk publishes
	// the event AND calls this callback — no duplicate events or writes.
	if !agentNoStreaming {
		chatAgent.EnableStreaming(func(chunk string) {
			indicator.Stop()
			fmt.Print(chunk)
		})
	}
}

// runQueueMode handles autonomous EA queue mode. It reads pending tasks from
// the persistent task queue and processes each one by delegating to the agent
// via ProcessQuery. The agent's tool handlers (task_queue_read, task_queue_publish,
// run_subagent, etc.) are available so the LLM can manage the task lifecycle.
// After processing a task, it loops back to check for more pending tasks.
// Exits cleanly when the queue is empty.
func runQueueMode(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus) error {
	fmt.Printf("\n[bot] Starting EA queue mode — processing pending tasks autonomously\n")
	fmt.Printf("[chart] Provider: %s | Model: %s\n\n",
		chatAgent.GetProvider(),
		chatAgent.GetModel())

	tq := tools.NewTaskQueue(tools.DefaultTaskQueuePath())

	// Enable streaming so the user can see what's happening
	if !agentNoStreaming {
		chatAgent.EnableStreaming(func(chunk string) {
			fmt.Print(chunk)
		})
	}

	tasksProcessed := 0

	for {
		// Check for cancellation before each iteration
		if err := ctx.Err(); err != nil {
			fmt.Printf("\n[bot] Queue mode cancelled: %v\n", err)
			break
		}

		// Read pending tasks from the queue
		tasks, err := tq.ReadTasks("pending", 10)
		if err != nil {
			return fmt.Errorf("failed to read task queue: %w", err)
		}

		// Exit cleanly when queue is empty
		if len(tasks) == 0 {
			if tasksProcessed > 0 {
				fmt.Printf("\n[OK] Queue mode complete — processed %d task(s)\n", tasksProcessed)
			} else {
				fmt.Printf("\n[bot] No pending tasks in queue — nothing to process\n")
			}
			break
		}

		// Process each pending task
		for _, task := range tasks {
			// Check for cancellation before processing each task
			if err := ctx.Err(); err != nil {
				fmt.Printf("\n[bot] Queue mode cancelled: %v\n", err)
				break
			}

			fmt.Printf("\n[bot] Processing task: %s [%s] (priority: %s)\n",
				task.Title, task.ID, task.Priority)

			// Mark task as in_progress
			_, err = tq.PublishTask(task.ID, "in_progress", "", nil)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[WARN] Failed to mark task %s as in_progress: %v\n", task.ID, err)
			}

			// Construct a query for the agent to process this task.
			// The EA system prompt already knows how to handle task processing,
			// and the agent has access to run_subagent, task_queue_publish, etc.
			query := buildQueueTaskQuery(task)

			err = ProcessQuery(ctx, chatAgent, eventBus, query)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\n[FAIL] Error processing task %s: %v\n", task.ID, err)
				// Mark task as failed
				_, _ = tq.PublishTask(task.ID, "failed", fmt.Sprintf("Error during processing: %v", err), nil)
				continue
			}

			// Check if the agent marked this task as completed/failed via its tool handlers
			// If not, check the current status. The agent should use task_queue_publish
			// during processing, so we re-read the task to see its state.
			updatedTasks, err := tq.ReadTasks("all", 100)
			taskCompleted := false
			if err == nil {
				for _, t := range updatedTasks {
					if t.ID == task.ID {
						if t.Status == "completed" || t.Status == "failed" {
							taskCompleted = true
						}
						break
					}
				}
			}

			if !taskCompleted {
				// Agent didn't update task status; mark as completed by default
				fmt.Printf("[bot] Task %s processed — marking as completed\n", task.Title)
				result := fmt.Sprintf("Task processed via queue mode. Agent did not explicitly set a result.")
				_, _ = tq.PublishTask(task.ID, "completed", result, nil)
			} else {
				fmt.Printf("[OK] Task %s completed\n", task.Title)
			}

			tasksProcessed++
		}
	}

	return nil
}

// buildQueueTaskQuery constructs a prompt for the agent to process a queued task.
func buildQueueTaskQuery(task tools.Task) string {
	var parts []string
	parts = append(parts, fmt.Sprintf("Process queued task: %s", task.Title))
	parts = append(parts, fmt.Sprintf("Task ID: %s", task.ID))

	if task.Description != "" {
		parts = append(parts, fmt.Sprintf("Description: %s", task.Description))
	}
	if task.WorkingDir != "" {
		parts = append(parts, fmt.Sprintf("Working directory: %s", task.WorkingDir))
	}
	if task.Persona != "" {
		parts = append(parts, fmt.Sprintf("Persona: %s", task.Persona))
	}
	parts = append(parts, fmt.Sprintf("Priority: %s", task.Priority))

	if task.ParentTaskID != "" {
		parts = append(parts, fmt.Sprintf("Parent task: %s", task.ParentTaskID))
	}

	parts = append(parts, "")
	parts = append(parts, "Use run_subagent to delegate this task if a persona was specified,")
	parts = append(parts, "or process it directly. When done, use task_queue_publish to mark")
	parts = append(parts, "the task as completed or failed with a summary of what you did.")
	if task.Persona != "" {
		parts = append(parts, fmt.Sprintf("Recommended persona: %s", task.Persona))
	}

	return strings.Join(parts, "\n")
}

// runInteractiveMode handles interactive REPL mode
func runInteractiveMode(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus, indicator *console.ActivityIndicator) error {
	fmt.Printf("\n[bot] Welcome to sprout! Enhanced CLI with Web UI\n")
	fmt.Printf("[chart] Provider: %s | Model: %s\n\n",
		chatAgent.GetProvider(),
		chatAgent.GetModel())

	// Create enhanced input reader with completion support
	inputReader := console.NewInputReader("sprout> ")

	// Initialize with existing history from agent
	inputReader.SetHistory(chatAgent.GetHistory())

	// SP-048-2a: slash command tab completion. Re-builds a fresh registry
	// per call so newly-installed MCP commands (which can be added mid-
	// session) are reflected in completion.
	inputReader.SetCompleter(func(line string, cursorPos int) []string {
		if !strings.HasPrefix(line, "/") || cursorPos != len(line) {
			return nil
		}
		// Don't complete once the user has moved past the command name.
		if strings.ContainsAny(line, " \t") {
			return nil
		}
		prefix := strings.ToLower(line[1:])
		registry := agent_commands.NewCommandRegistry()
		var matches []string
		for _, name := range registry.CompletionCandidates() {
			if strings.HasPrefix(strings.ToLower(name), prefix) {
				matches = append(matches, "/"+name)
			}
		}
		return matches
	})

	// SP-048-1c: Subscribe to tool start/end events so the activity indicator
	// can render a per-tool timeline. Runs until ctx is cancelled.
	subCtx, cancelSub := context.WithCancel(ctx)
	defer cancelSub()
	startTerminalToolSubscriber(subCtx, eventBus, indicator)

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
				return fmt.Errorf("failed to read input: %w", err)
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
				fmt.Println("\n-- Goodbye! Here's your session summary:")
				fmt.Println("=====================================")
				chatAgent.PrintConversationSummary(true)
				printContinuationHint(chatAgent)
				return nil
			}

			// SP-048-1b: spinner during the gap between submit and first
			// stream chunk (or first tool event). The streaming callback
			// registered in SetupAgentEvents stops it on first chunk; we
			// also Stop() defensively after ProcessQuery returns.
			indicator.Start(fmt.Sprintf("Thinking · %s", chatAgent.GetModel()))

			// Slash/bang commands should bypass command-detection fast paths.
			registry := agent_commands.NewCommandRegistry()
			if registry.IsSlashCommand(query) {
				if err := ProcessQuery(ctx, chatAgent, eventBus, query); err != nil {
					indicator.Stop()
					fmt.Fprintf(os.Stderr, "[FAIL] Error: %v\n", err)
				}
				indicator.Stop()
				continue
			}

			// Try zsh command detection first (fast path)
			if executed, err := TryZshCommandExecution(ctx, chatAgent, query); err != nil {
				indicator.Stop()
				fmt.Fprintf(os.Stderr, "[FAIL] Error: %v\n", err)
			} else if !executed {
				// Zsh detection didn't trigger, try LLM-based detection
				if executed, err := TryDirectExecution(ctx, chatAgent, query); err != nil {
					indicator.Stop()
					fmt.Fprintf(os.Stderr, "[FAIL] Error: %v\n", err)
				} else if !executed {
					// Neither fast path triggered, process normally
					if err := ProcessQuery(ctx, chatAgent, eventBus, query); err != nil {
						indicator.Stop()
						fmt.Fprintf(os.Stderr, "[FAIL] Error: %v\n", err)
					}
				}
			}
			// Defensive: ensure the spinner is cleared at the end of every turn
			// even if the streamFn never fired (e.g. zsh fast-path executed).
			indicator.Stop()
		}
	}
}

// startTerminalToolSubscriber subscribes a goroutine to the event bus that
// translates PublishToolStart / PublishToolEnd events into terminal spinner
// updates and ✓/✗ result lines. Runs until ctx is cancelled.
//
// Tools whose ToolConfig declares Interactive=true (e.g. ask_user) bypass
// the spinner entirely so their own prompt rendering isn't clobbered.
//
// Also stops the spinner on any prompt-request event (security approval,
// security prompt, ask_user) so prompts routed through the event bus get
// clean rendering with no spinner frames overwriting the prompt text.
func startTerminalToolSubscriber(ctx context.Context, eventBus *events.EventBus, indicator *console.ActivityIndicator) {
	if eventBus == nil || indicator == nil {
		return
	}
	subName := fmt.Sprintf("cli_tool_indicator_%d", time.Now().UnixNano())
	ch := eventBus.Subscribe(subName)

	go func() {
		defer eventBus.Unsubscribe(subName)
		for {
			select {
			case <-ctx.Done():
				return
			case evt, ok := <-ch:
				if !ok {
					return
				}
				data, _ := evt.Data.(map[string]interface{})
				switch evt.Type {
				case events.EventTypeToolStart:
					name, _ := data["tool_name"].(string)
					if agent.IsInteractiveTool(name) {
						// Tool renders its own prompt — make sure any active
						// spinner is gone before the prompt lands.
						indicator.Stop()
						continue
					}
					args, _ := data["arguments"].(string)
					// Ensure the spinner lands on a fresh line so it never
					// overwrites partial streamed text. Stdout for parity
					// with how stream chunks were just printed.
					fmt.Fprintln(os.Stdout)
					indicator.Start(fmt.Sprintf("  %s%s", name, formatToolArgPreview(name, args)))
				case events.EventTypeToolEnd:
					name, _ := data["tool_name"].(string)
					if agent.IsInteractiveTool(name) {
						// No spinner was started; emit no result chrome.
						continue
					}
					status, _ := data["status"].(string)
					var durationMs int64
					switch v := data["duration_ms"].(type) {
					case int64:
						durationMs = v
					case float64:
						durationMs = int64(v)
					}
					icon := "[OK]"
					if status != "completed" {
						icon = "[FAIL]"
					}
					indicator.Replace(fmt.Sprintf("  %s %s · %.1fs", icon, name, float64(durationMs)/1000.0))
				case events.EventTypeSecurityApprovalRequest,
					events.EventTypeSecurityPromptRequest,
					events.EventTypeAskUserRequest:
					// A prompt is about to render — stop any spinner so it
					// doesn't overwrite the prompt text. Subsequent activity
					// (next tool event, stream chunks) re-starts naturally.
					indicator.Stop()
				}
			}
		}
	}()
}

// formatToolArgPreview produces a short, single-line preview of a tool's
// arguments for the activity indicator. The arguments string is the raw
// JSON the model emitted; we extract whichever field is most informative
// for the tool at hand. Returns an empty string (no parens) when nothing
// useful is available. Best-effort — invalid JSON yields no preview.
func formatToolArgPreview(toolName, arguments string) string {
	if arguments == "" {
		return ""
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil || len(args) == 0 {
		return ""
	}

	pick := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := args[k].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}

	const maxLen = 60
	truncate := func(s string) string {
		if len(s) > maxLen {
			return s[:maxLen-1] + "…"
		}
		return s
	}

	var preview string
	switch toolName {
	case "read_file", "write_file", "edit_file", "write_structured_file", "patch_structured_file":
		preview = pick("path", "file_path", "filename")
	case "shell_command", "exec":
		preview = pick("command", "cmd")
	case "search_files", "grep":
		preview = pick("pattern", "query", "search")
	case "fetch_url":
		preview = pick("url")
	default:
		// Generic fallback: surface the first short string value.
		for _, v := range args {
			if s, ok := v.(string); ok && len(s) > 0 && len(s) < 120 {
				preview = s
				break
			}
		}
	}

	preview = sanitizeArgForPreview(preview)
	if preview == "" {
		return ""
	}
	return " (" + truncate(preview) + ")"
}

// sanitizeArgForPreview collapses whitespace and strips control characters
// so the preview always renders on one line inside parentheses.
func sanitizeArgForPreview(s string) string {
	out := make([]rune, 0, len(s))
	prevSpace := false
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			if !prevSpace {
				out = append(out, ' ')
				prevSpace = true
			}
			continue
		}
		if r < 32 {
			continue
		}
		out = append(out, r)
		prevSpace = r == ' '
	}
	return strings.TrimSpace(string(out))
}

// runDirectMode handles single query execution
func runDirectMode(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus, query string) error {
	if configuration.GetEnvSimple("SUBAGENT") != "1" {
		fmt.Printf("[>>] Processing: %s\n", query)
	}

	// Slash/bang commands should bypass command-detection fast paths.
	registry := agent_commands.NewCommandRegistry()
	if registry.IsSlashCommand(query) {
		return ProcessQuery(ctx, chatAgent, eventBus, query)
	}

	// Try zsh command detection first
	if executed, err := TryZshCommandExecution(ctx, chatAgent, query); err != nil {
		return fmt.Errorf("zsh command execution failed: %w", err)
	} else if executed {
		// Command was executed directly, skip normal agent flow
		return nil
	}

	// Try LLM-based fast path: direct command execution
	if executed, err := TryDirectExecution(ctx, chatAgent, query); err != nil {
		return fmt.Errorf("direct command execution failed: %w", err)
	} else if executed {
		// Command was executed directly, skip normal agent flow
		return nil
	}

	// Proceed with normal agent flow
	return ProcessQuery(ctx, chatAgent, eventBus, query)
}
