//go:build !js

// Agent modes: handles interactive and direct execution modes
package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	agent_commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/envutil"
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

// printKeyboardHelp is a convenience wrapper that writes to stderr.
// Triggered by typing `?` alone at the idle prompt. Writes to stderr
// so it doesn't interleave with stdout-bound model output if the user
// pipes the session.
func printKeyboardHelp() {
	writeKeyboardHelp(os.Stderr)
}

// writeKeyboardHelp emits a compact, two-column reference of the
// non-obvious keys the CLI exposes — primarily the steer-panel keys
// added by SP-055 since the rest of the bindings (slash commands,
// exit) are documented in the welcome banner and `/help`. Accepts a
// writer so tests can capture output.
func writeKeyboardHelp(w io.Writer) {
	colorOn := envutil.ResolveColorPreference(true)
	dim, reset := "", ""
	if colorOn {
		dim, reset = "\033[2m", "\033[0m"
	}
	rows := [][2]string{
		{"Steer panel (while a turn is running)", ""},
		{"  Enter", "send mid-turn steer (default)"},
		{"  Tab", "toggle steer ↔ queue mode"},
		{"  ↑ / ↓", "recall prior steer messages"},
		{"  Esc", "clear the input"},
		{"  Ctrl+C", "interrupt the current turn"},
		{"", ""},
		{"Idle prompt", ""},
		{"  /<cmd>", "slash command (/help, /commit, /persona, …)"},
		{"  ?", "this help"},
		{"  exit / quit", "end session + print summary"},
		{"  Ctrl+C × 2", "force quit"},
	}
	fmt.Fprintln(w)
	console.GlyphInfo.Fprintf(w, "Keyboard help")
	for _, r := range rows {
		if r[0] == "" {
			fmt.Fprintln(w)
			continue
		}
		if r[1] == "" {
			// Section header — bold if color is on.
			if colorOn {
				fmt.Fprintf(w, "  \033[1m%s%s\n", r[0], reset)
			} else {
				fmt.Fprintf(w, "  %s\n", r[0])
			}
			continue
		}
		// Two-column row. Align the description column at fixed width
		// so the descriptions stack visually.
		fmt.Fprintf(w, "  %-18s %s%s%s\n", r[0], dim, r[1], reset)
	}
	fmt.Fprintln(w)
}

// RunAgent runs the agent in interactive or direct mode
func RunAgent(chatAgent *agent.Agent, isInteractive bool, args []string) (err error) {
	// SP-048-5e: when stdout is being piped/redirected (i.e. not a TTY),
	// auto-set NO_COLOR so every color-aware writer in the process
	// (markdown formatter, default-choice hint, future renderers) emits
	// plain text. The user can override with FORCE_COLOR if they really
	// want ANSI in a log file.
	if !term.IsTerminal(int(os.Stdout.Fd())) &&
		os.Getenv("NO_COLOR") == "" &&
		os.Getenv("FORCE_COLOR") == "" {
		os.Setenv("NO_COLOR", "1")
	}

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
			console.GlyphWarning.Fprintf(os.Stderr, "Could not determine home directory, skipping daemon log rotation: %v", homeErr)
		} else {
			setupDaemonLogging(homeDir)
		}
	}

	// Create event bus
	eventBus := events.NewEventBus()

	// Always wire the agent's event bus so terminal subscribers (activity
	// indicator, tool timeline) receive PublishToolStart / PublishToolEnd
	// even when the WebUI is disabled. SP-048-1.
	if chatAgent != nil {
		chatAgent.SetEventBus(eventBus)
	}

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
			console.GlyphWarning.Fprintf(os.Stderr, "Binding to %s — web UI is accessible from all network interfaces", bindAddr)
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
					console.GlyphWarning.Fprintf(os.Stderr, "Could not find a dynamic port: %v; web UI disabled", dynErr)
					enableWebUI = false
				} else {
					port = dynamicPort
				}
			}
		}

		if enableWebUI {
			var webErr error
			webServer, webErr = webui.NewReactWebServer(chatAgent, eventBus, port, bindAddr, bindSocket, secretToken)
			if webErr != nil {
				log.Fatalf("%v", webErr)
			}

			// Inject webui-owned managers into the agent so that security
			// prompts and ask_user requests route through the same instances
			// the webui handlers resolve responses on — no global singletons.
			// Skip this when agent is nil (provider not configured in daemon mode).
			if chatAgent != nil {
				chatAgent.InjectWebUIManagers(webServer.GetSecurityPromptMgr(), webServer.GetAskUserMgr())

				// Wire up the WebUI client check so security prompts route
				// correctly: use the event bus only when a browser tab is open,
				// otherwise fall back to CLI prompting (avoids 5-min timeouts).
				chatAgent.SetHasActiveWebUIClients(webServer.HasActiveWebUIClients)
			}

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
						console.GlyphWarning.Fprintf(os.Stderr, "Web UI failed to start: %v", err)
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
					if chatAgent != nil {
						if mgr := chatAgent.GetConfigManager(); mgr != nil {
							if err := mgr.Reload(); err != nil {
								fmt.Printf("[RELOAD] Failed: %v\n", err)
							} else {
								fmt.Printf("[RELOAD] Configuration reloaded successfully.\n")
							}
						}
					}
					continue
				}

				if isInteractive && isQueryInProgress() {
					nowUnix := time.Now().UnixNano()
					prev := atomic.LoadInt64(&lastInterruptAt)
					if prev > 0 && time.Duration(nowUnix-prev) < 2*time.Second {
						console.StopGlobalStatusFooter()
						fmt.Println()
						console.GlyphStopped.Printf("Force quitting immediately...")
						os.Exit(1)
					}

					atomic.StoreInt64(&lastInterruptAt, nowUnix)
					fmt.Println()
					console.GlyphPaused.Printf("Received signal %v, interrupting active task...", sig)
					console.GlyphDim.Printf("  (Press Ctrl+C again quickly to force quit)")
					if chatAgent != nil {
						chatAgent.TriggerInterrupt()
					}
					continue
				}

				fmt.Println()
				console.GlyphStopped.Printf("Received signal %v, shutting down gracefully...", sig)
				console.GlyphDim.Printf("  (Press Ctrl+C again to force quit)")

				// Cancel the context which will stop all operations
				cancel()

				// Close the global browser renderer to release Chromium resources
				webcontent.CloseGlobalBrowser()

				// Signal that shutdown has started
				close(shutdown)

				// Start a timeout goroutine for force quit
				go func() {
					time.Sleep(5 * time.Second)
					console.StopGlobalStatusFooter()
					fmt.Println()
					console.GlyphStopped.Printf("Force quitting...")
					os.Exit(1)
				}()

				// Any subsequent signal after shutdown starts should force quit.
				for {
					select {
					case <-sigCh:
						fmt.Println()
						console.GlyphStopped.Printf("Force quitting immediately...")
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

	// Set up event publishing for agent (skip when agent is nil in daemon mode)
	if chatAgent != nil {
		SetupAgentEvents(chatAgent, eventBus, indicator)
	}

	// Check for queue mode before interactive mode
	// (skip when agent is nil in daemon mode — provider not configured yet)
	if chatAgent != nil && chatAgent.GetConfigManager().GetConfig().GetEAMode() == "queue" {
		return runQueueMode(ctx, chatAgent, eventBus)
	}

	// When agent is nil (provider not configured in daemon mode), skip to
	// the daemon wait path. The web UI handles provider setup interactively.
	if chatAgent == nil && daemonMode && webServer != nil && webServer.IsRunning() {
		fmt.Printf("\n[web] Web UI running at http://%s:%d (no provider configured — configure via web UI)\n", webui.DisplayAddr(bindAddr), webServer.GetPort())
		if !isServiceMode() {
			fmt.Println("Press Ctrl+C to stop the server.")
		}
		<-ctx.Done()
		return nil
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
			fmt.Println()
			console.GlyphInfo.Print("Welcome to sprout!")
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
			console.GlyphSuccess.Print("Agent shut down successfully")
		case <-time.After(5 * time.Second):
			console.GlyphWarning.Fprintf(os.Stderr, "Agent shutdown timed out after 5s")
		}
	}
	if webUISup != nil {
		webUISup.cleanupHostRecordIfOwned()
	}
	if webServer != nil && webServer.IsRunning() {
		console.GlyphDim.Print("Shutting down web server...")

		if webErr := webServer.Shutdown(); webErr != nil {
			console.GlyphWarning.Fprintf(os.Stderr, "Error shutting down web server: %v", webErr)
		} else {
			console.GlyphSuccess.Print("Web server shut down successfully")
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
	//
	// Routing: if a per-turn AssistantTurnRenderer is active (set up by
	// the REPL loop), the chunk goes through it for indent + segment
	// tracking. Otherwise it falls back to raw fmt.Print (non-REPL
	// callers like queue mode).
	//
	// Assistant prose flows verbatim end-to-end: the terminal handles
	// soft-wrap on long lines. We deliberately do NOT clamp line length
	// here — prior versions truncated lines beyond `terminalWidth × 2`,
	// which clipped long prose paragraphs that lacked `\n` breaks
	// ("text being shown to the user shouldn't be cut off"). Tool
	// results don't reach this callback (they route via RouteAgentMessage
	// / RouteTerminalOnly), so there's no blob-output risk on this path.
	if !agentNoStreaming {
		chatAgent.EnableStreaming(func(chunk string) {
			indicator.Stop()
			if chunk != "" {
				// CompareAndSwap: only the FIRST non-empty chunk records
				// the ttft. Subsequent chunks are a no-op so reading the
				// timestamp later yields "first token landed at X".
				noteFirstStreamChunk()
			}
			if r := currentTurnRenderer.Load(); r != nil {
				r.WriteChunk(chunk)
				return
			}
			fmt.Print(chunk)
		})
	}
}

// currentTurnRenderer holds the AssistantTurnRenderer for the in-progress
// REPL turn (or nil between turns / outside the REPL). The streaming
// callback registered in SetupAgentEvents loads from this pointer on each
// chunk so per-turn renderers can be swapped without re-registering the
// callback. Safe because only one turn is active at a time in a CLI REPL.
var currentTurnRenderer atomic.Pointer[console.AssistantTurnRenderer]

// runQueueMode handles autonomous EA queue mode. It reads pending tasks from
// the persistent task queue and processes each one by delegating to the agent
// via ProcessQuery. The agent's tool handlers (task_queue_read, task_queue_publish,
// run_subagent, etc.) are available so the LLM can manage the task lifecycle.
// After processing a task, it loops back to check for more pending tasks.
// Exits cleanly when the queue is empty.
func runQueueMode(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus) error {
	fmt.Println()
	console.GlyphInfo.Print("Starting EA queue mode — processing pending tasks autonomously")
	console.GlyphInfo.Printf("Provider: %s | Model: %s",
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
			fmt.Println()
			console.GlyphStopped.Printf("Queue mode cancelled: %v", err)
			break
		}

		// Read pending tasks from the queue
		tasks, err := tq.ReadTasks("pending", 10)
		if err != nil {
			return fmt.Errorf("failed to read task queue: %w", err)
		}

		// Exit cleanly when queue is empty
		if len(tasks) == 0 {
			fmt.Println()
			if tasksProcessed > 0 {
				console.GlyphSuccess.Printf("Queue mode complete — processed %d task(s)", tasksProcessed)
			} else {
				console.GlyphInfo.Print("No pending tasks in queue — nothing to process")
			}
			break
		}

		// Process each pending task
		for _, task := range tasks {
			// Check for cancellation before processing each task
			if err := ctx.Err(); err != nil {
				fmt.Println()
				console.GlyphStopped.Printf("Queue mode cancelled: %v", err)
				break
			}

			fmt.Println()
			console.GlyphAction.Printf("Processing task: %s [%s] (priority: %s)",
				task.Title, task.ID, task.Priority)

			// Mark task as in_progress
			_, err = tq.PublishTask(task.ID, "in_progress", "", nil)
			if err != nil {
				console.GlyphWarning.Fprintf(os.Stderr, "Failed to mark task %s as in_progress: %v", task.ID, err)
			}

			// Construct a query for the agent to process this task.
			// The EA system prompt already knows how to handle task processing,
			// and the agent has access to run_subagent, task_queue_publish, etc.
			query := buildQueueTaskQuery(task)

			err = ProcessQuery(ctx, chatAgent, eventBus, query)
			if err != nil {
				fmt.Fprint(os.Stderr, "\n"+console.FormatErrorBlock(fmt.Sprintf("Error processing task %s", task.ID), err))
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
				console.GlyphInfo.Printf("Task %s processed — marking as completed", task.Title)
				result := "Task processed via queue mode. Agent did not explicitly set a result."
				_, _ = tq.PublishTask(task.ID, "completed", result, nil)
			} else {
				console.GlyphSuccess.Printf("Task %s completed", task.Title)
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
	// SP-048 follow-up: Go's default logger writes to stderr — and so does
	// the activity-indicator spinner. Without redirection, any log.Printf
	// fired during a tool run (e.g. the [WARN] in pkg/configuration/config.go
	// when an AllowedTools override is dropped) interleaves with spinner
	// frames and produces the cursor-thrash bug we caught in real sessions.
	// Route Go's log to .sprout/workspace.log instead so internal noise
	// stops fighting the spinner; user-facing output still goes through
	// fmt.Print which is properly synchronized by the indicator.
	if restoreLog, err := redirectGoLogToWorkspace(); err == nil {
		defer restoreLog()
	}

	// SP-048-3: Persistent status footer pinned at the bottom row of the
	// terminal. Suppressed automatically on non-TTY (e.g., piped output).
	// MUST be Stopped before exit or the user's terminal is left with a
	// broken scroll region — both the defer here AND the signal handler's
	// force-quit path call Stop via the global registration.
	//
	// Started BEFORE the welcome/recent-sessions prints so that intro
	// output lands inside the scroll region (1..N-2) and scrolls naturally
	// as the session grows. Reverse order (prints first, then footer)
	// leaves the cursor inside already-printed content at row N-2, and
	// the input prompt then renders on top of it.
	footer := console.NewStatusFooter(os.Stderr, &agentFooterSource{agent: chatAgent})
	console.RegisterGlobalStatusFooter(footer)
	footer.Start()
	defer footer.Stop()

	fmt.Println()
	console.GlyphInfo.Printf("Welcome to sprout! Enhanced CLI with Web UI")
	console.GlyphInfo.Printf("Provider: %s | Model: %s\n",
		chatAgent.GetProvider(),
		chatAgent.GetModel())

	// SP-048-5a: surface recent sessions (last 7d) with inline numeric
	// selection. Up/down arrows stay reserved for command history; a
	// fresh number on its own line is the affordance. If the user picks
	// a session, this loads its state in-place via LoadStateScoped +
	// ApplyState + SetSessionID — same mechanism as `--session-id`,
	// just triggered interactively.
	//
	// Runs BEFORE the InputReader is constructed so a resumed session's
	// model is reflected in the prompt prefix.
	maybeOfferSessionResume(chatAgent)

	// SP-048-5b: one-shot hint about Tab autocomplete + Ctrl-D, persisted
	// per workspace in ~/.sprout/state.json so it never repeats.
	maybeShowFirstRunHint()

	// Create enhanced input reader with completion support.
	// SP-048-5d: prompt includes the current model so users always know
	// what they're talking to. Falls back to "sprout> " when the model
	// name is empty (e.g. provider failed to resolve at startup).
	inputReader := console.NewInputReader(buildPromptPrefix(chatAgent.GetModel()))

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

// SP-055: steer coordinator owns the pinned steer-input panel for
	// the lifetime of this REPL. Constructed once with the agent +
	// footer references; StartTurn / EndTurn drive the per-iteration
	// lifecycle below.
	steerCoord := NewSteerCoordinator(chatAgent, footer)

	// Capture a ground-truth termios snapshot of stdin in its default
	// cooked state (the terminal is fully cooked at this point — no
	// raw or steer mode active). Both InputReader and SteerInputReader
	// use this for emergency recovery: if a prior mode transition leaves
	// the terminal in raw mode, the pre-flight check restores to this
	// known-good state instead of a potentially-corrupted per-enter
	// snapshot. Must be captured AFTER footer.Start() so the scroll
	// region is established, but BEFORE any ReadLine / StartTurn call.
	groundTruth := console.CaptureGroundTruth()
	inputReader.SetGroundTruth(groundTruth)
	steerCoord.SetGroundTruth(groundTruth)

	// SP-048-1c + 3: Subscribe to tool start/end events so the activity
	// indicator can render a per-tool timeline AND the footer can refresh
	// cost/context after each tool. Runs until ctx is cancelled.
	subCtx, cancelSub := context.WithCancel(ctx)
	defer cancelSub()
	resetSpawnTracking := startTerminalToolSubscriber(subCtx, chatAgent, eventBus, indicator, footer)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// SP-048-5d follow-up: refresh the prompt prefix each loop so
			// it tracks model changes (e.g. an LLM-driven /model switch
			// from inside a previous turn, or interactive provider/model
			// selection during recovery).
			inputReader.SetPrompt(buildPromptPrefix(chatAgent.GetModel()))

			query, err := inputReader.ReadLine()

			if err != nil {
				if err.Error() == "interrupted" {
					fmt.Println("Use 'exit' or 'quit' to exit.")
					continue
				}
				return fmt.Errorf("failed to read input: %w", err)
			}

			query = strings.TrimSpace(query)
			// SP-055 Phase 3b: drain any messages the user queued via
			// Tab+Enter in the steer panel during the previous turn.
			// They prepend to the typed prompt, joined as separate
			// blockquote-style lines so the LLM reads them as ordered
			// context the user wants addressed this turn.
			if queued := chatAgent.DrainDeferredMessages(); len(queued) > 0 {
				prefix := "Queued from prior turn:\n"
				for _, msg := range queued {
					prefix += "  • " + msg + "\n"
				}
				if query == "" {
					query = strings.TrimSpace(prefix)
				} else {
					query = prefix + "\n" + query
				}
				// Refresh the footer so the "⏸ N queued" badge clears
				// the moment we drain. Without this nudge the badge
				// would linger until the next tool/cost event.
				footer.Refresh()
			}
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

			// `?` shortcut: print a compact keyboard-help card and
			// return to the prompt without consuming an LLM turn. Helps
			// users discover the steer-panel keys (Tab toggle, ↑↓
			// history) that aren't advertised elsewhere.
			if query == "?" {
				printKeyboardHelp()
				continue
			}

			// Slash/bang commands run locally — they don't talk to the LLM
			// and often own the terminal themselves (interactive `/commit`,
			// `/persona`, etc.). They MUST NOT have the activity-indicator
			// spinner active during execution: the spinner's stderr writes
			// would interleave with the command's own stdout prompts and
			// produce the input-mangling bug we caught in `/commit` and
			// friends. Slash commands also skip the per-turn cost summary
			// since they don't consume LLM tokens.
			registry := agent_commands.NewCommandRegistry()
			if registry.IsSlashCommand(query) {
				if err := ProcessQuery(ctx, chatAgent, eventBus, query); err != nil {
					fmt.Fprint(os.Stderr, console.FormatErrorBlock(console.GlyphError.Prefix()+"Error", err))
				}
				// `/model` and friends may have changed the active model;
				// rebuild the prompt prefix so the next prompt reflects it.
				inputReader.SetPrompt(buildPromptPrefix(chatAgent.GetModel()))
				footer.Refresh()
				continue
			}

			// SP-048-5c: snapshot per-turn metrics before submit so we can
			// emit a "this turn" cost / tokens / elapsed line after the
			// model finishes.
			turnStart := time.Now()
			turnPromptStart := chatAgent.GetPromptTokens()
			turnCompletionStart := chatAgent.GetCompletionTokens()
			turnCostStart := chatAgent.GetTotalCost()
			// Clear the ttft tracker so the next stream chunk sets a
			// fresh "time to first token" measurement for this turn.
			resetTurnFirstToken()

			// SP-051-2c: clear per-turn spawn dedupe so the next batch of
			// subagents announces fresh "↳ persona spawned" lines instead of
			// silently joining whatever ran in the prior turn.
			resetSpawnTracking()

			// Role header so the boundary between user input and assistant
			// reply is visually obvious. Uses a brand-colored bar + dim
			// "assistant" label — pops out in scrollback without being noisy.
			// Paired at the bottom with the existing dim `⎯ this turn: … ⎯`
			// summary line, which acts as the closing separator.
			fmt.Println()
			printAssistantHeader(chatAgent.GetModel())

			// Per-turn assistant renderer: indents prose with "  " as it
			// streams, and at turn-end optionally re-renders the final
			// prose segment with markdown formatting (cursor-clear +
			// reprint). Wire OnExternalWrite into the OutputRouter so
			// tool-log lines break the current prose segment cleanly.
			turnRenderer := console.NewAssistantTurnRenderer(
				GetTerminalWidth(),
				console.NewMarkdownFormatter(true, true),
			)
			currentTurnRenderer.Store(turnRenderer)
			if router := chatAgent.OutputRouter(); router != nil {
				router.SetExternalWriteHook(turnRenderer.OnExternalWrite)
			}

			// SP-048-1b: Try fast paths BEFORE starting the "Thinking"
			// spinner so the user never sees the LLM spinner for commands
			// that execute directly without LLM involvement.
			var fastPathExecuted bool
			// Try zsh command detection first (fast path)
			if executed, err := TryZshCommandExecution(ctx, chatAgent, query); err != nil {
				fmt.Fprint(os.Stderr, console.FormatErrorBlock(console.GlyphError.Prefix()+"Error", err))
			} else if executed {
				fastPathExecuted = true
			} else {
				// Zsh detection didn't trigger, try LLM-based detection
				if executed, err := TryDirectExecution(ctx, chatAgent, query); err != nil {
					fmt.Fprint(os.Stderr, console.FormatErrorBlock(console.GlyphError.Prefix()+"Error", err))
				} else if executed {
					fastPathExecuted = true
				}
			}

			// Only start the spinner (and the full agent turn) when no fast
			// path handled the query.
			if !fastPathExecuted {
				indicator.Start(fmt.Sprintf("Thinking · %s", chatAgent.GetModel()))

				// Execute the turn inside a func so we can defer EndTurn.
				// This ensures the steer reader is always stopped even if
				// ProcessQuery panics, preventing the terminal from being
				// left in raw/cbreak mode.
				func() {
					// SP-055: turn the steer panel on for the duration of the
					// ProcessQuery call. The coordinator (constructed once at
					// session start) owns the SteerInputReader and the callback
					// wiring to InjectInputContext / TriggerInterrupt.
					steerCoord.StartTurn()
					defer steerCoord.EndTurn()

					// No fast path triggered, process normally via LLM
					if err := ProcessQuery(ctx, chatAgent, eventBus, query); err != nil {
						indicator.Stop()
						fmt.Fprint(os.Stderr, console.FormatErrorBlock(console.GlyphError.Prefix()+"Error", err))
					}
				}()
			} // end if !fastPathExecuted

			// Drain any unsent steer text into the InputReader so it
			// appears pre-filled at the next prompt. This prevents the
			// silent loss of text the user typed into the steer panel
			// but did not submit before the turn ended.
			if unsent := steerCoord.DrainUnsentBuffer(); unsent != "" {
				inputReader.SetInitialContent(unsent)
			}
			// Defensive: ensure the spinner is cleared at the end of every turn
			// even if the streamFn never fired (e.g. zsh fast-path executed).
			indicator.Stop()
			// Finalize the assistant renderer: re-renders the final prose
			// segment with markdown formatting when it's substantial
			// enough to be worth the cursor-clear flicker. Tear down the
			// external-write hook BEFORE FinalizeAtTurnEnd so the
			// re-render's own writes don't loop back through it.
			if router := chatAgent.OutputRouter(); router != nil {
				router.SetExternalWriteHook(nil)
			}
			turnRenderer.FinalizeAtTurnEnd()
			currentTurnRenderer.Store(nil)
			// SP-048-3: refresh the footer at turn-end so cost / context /
			// model changes (e.g. /model switch) land immediately.
			footer.Refresh()
			// SP-048-5c: print the per-turn summary line if any LLM tokens
			// were actually consumed. Suppressed for zero-cost turns (slash
			// commands, zsh fast paths, empty responses).
			printPerTurnSummary(chatAgent, turnStart, turnPromptStart, turnCompletionStart, turnCostStart)
		}
	}
}

// printAssistantHeader writes the dim "▌ assistant · <model>" header that
// marks the start of an assistant turn. Honors NO_COLOR via the existing
// color preference resolver. The brand cyan `▌` aligns visually with the
// glyph vocabulary in pkg/console; the model name sits in dim grey so the
// eye is drawn to the bar, not the metadata.
func printAssistantHeader(model string) {
	colorOn := envutil.ResolveColorPreference(true)
	if !colorOn {
		fmt.Printf("▌ assistant · %s\n", model)
		return
	}
	fmt.Printf("\033[1;96m▌\033[0m \033[2massistant · %s\033[0m\n", model)
}

// shouldShowTurnStats returns true when stderr is connected to a TTY.
// The turn-summary line is written to os.Stderr, so we must check stderr
// (not stdout) to determine whether it will render cleanly. This matters
// in piping scenarios like `sprout agent "query" > output.txt` where
// stdout is piped but stderr is still the terminal. SP-048-5a.
func shouldShowTurnStats() bool {
	return term.IsTerminal(int(os.Stderr.Fd()))
}

// formatTurnStatsLine builds the dim single-line turn-summary string.
// When color is disabled (NO_COLOR), ANSI dim codes are stripped.
// SP-048-5a.
//
// ttft (time to first token) is rendered as a separate segment when
// non-zero. Threshold coloring (yellow >2s, red >5s) makes slow
// provider connections visible at a glance — they're the most common
// cause of "is sprout stuck?" perception even when the actual model
// run is fast once it starts streaming.
func formatTurnStatsLine(promptDelta, completionDelta int, costDelta float64, elapsed, ttft time.Duration) string {
	colorOn := envutil.ResolveColorPreference(true)
	var dim, reset string
	if colorOn {
		dim, reset = "\033[2m", "\033[0m"
	}

	ttftSeg := ""
	if ttft > 0 {
		ttftStr := compactDuration(ttft)
		styled := ttftStr
		if colorOn {
			switch {
			case ttft > 5*time.Second:
				// Pop out of dim into red for the duration of this segment,
				// then drop back into dim so the rest of the line stays muted.
				styled = reset + "\033[31m" + ttftStr + reset + dim
			case ttft > 2*time.Second:
				styled = reset + "\033[33m" + ttftStr + reset + dim
			}
		}
		ttftSeg = fmt.Sprintf(" · ttft %s", styled)
	}

	return fmt.Sprintf("%s⎯ this turn: %s in / %s out · %s · %s%s ⎯%s\n",
		dim,
		compactTokens(promptDelta),
		compactTokens(completionDelta),
		compactCost(costDelta),
		compactDuration(elapsed),
		ttftSeg,
		reset,
	)
}

// turnFirstTokenAt is set (atomically) to the Unix nano time of the
// first non-empty stream chunk in the current turn. Read by
// printPerTurnSummary to compute time-to-first-token, then reset to 0
// at the start of each turn. Package-level so the streaming callback
// in SetupAgentEvents (no agent-state to hang it on) can flip it.
var turnFirstTokenAt int64

// noteFirstStreamChunk is invoked once per turn from the streaming
// callback. CompareAndSwap ensures only the very first non-empty chunk
// updates the timestamp — later chunks are no-ops.
func noteFirstStreamChunk() {
	atomic.CompareAndSwapInt64(&turnFirstTokenAt, 0, time.Now().UnixNano())
}

// resetTurnFirstToken clears the ttft tracker. Called by the REPL just
// before submitting a turn so each turn's measurement is independent.
func resetTurnFirstToken() {
	atomic.StoreInt64(&turnFirstTokenAt, 0)
}

// printPerTurnSummary emits a dim single-line summary of what just happened
// in the LLM round-trip: input/output tokens consumed, $ spent, elapsed
// wall time, plus ttft when available. Silent when no tokens were used
// (e.g. the turn was a slash command or zsh fast path). Only shown when
// stderr is a TTY (respects NO_COLOR for ANSI codes). SP-048-5a.
func printPerTurnSummary(chatAgent *agent.Agent, start time.Time, promptBefore, completionBefore int, costBefore float64) {
	if !shouldShowTurnStats() {
		return
	}
	promptDelta := chatAgent.GetPromptTokens() - promptBefore
	completionDelta := chatAgent.GetCompletionTokens() - completionBefore
	if promptDelta <= 0 && completionDelta <= 0 {
		return
	}
	costDelta := chatAgent.GetTotalCost() - costBefore
	elapsed := time.Since(start)

	var ttft time.Duration
	if firstAt := atomic.LoadInt64(&turnFirstTokenAt); firstAt > 0 {
		ttft = time.Duration(firstAt - start.UnixNano())
		if ttft < 0 {
			ttft = 0
		}
	}

	fmt.Fprint(os.Stderr, formatTurnStatsLine(promptDelta, completionDelta, costDelta, elapsed, ttft))
}

func compactTokens(n int) string {
	if n < 0 {
		n = 0
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

func compactCost(c float64) string {
	switch {
	case c < 0:
		return "$0.00"
	case c < 0.01:
		return fmt.Sprintf("$%.4f", c)
	case c < 1.0:
		return fmt.Sprintf("$%.3f", c)
	default:
		return fmt.Sprintf("$%.2f", c)
	}
}

func compactDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	mins := int(d / time.Minute)
	secs := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%dm%ds", mins, secs)
}

// buildPromptPrefix returns the interactive REPL prompt for the given
// model. SP-048-5d. Format: "<model> ▸ " when a model name is available,
// "sprout> " as the legacy fallback when it isn't.
func buildPromptPrefix(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return "sprout> "
	}
	return model + " ▸ "
}

// agentFooterSource adapts *agent.Agent to the console.ContentSource
// interface, exposing model / context tokens / cost / cwd to the status
// footer renderer.
type agentFooterSource struct{ agent *agent.Agent }

func (s *agentFooterSource) Model() string {
	if s == nil || s.agent == nil {
		return ""
	}
	return s.agent.GetModel()
}

func (s *agentFooterSource) ContextTokens() (used, limit int) {
	if s == nil || s.agent == nil {
		return 0, 0
	}
	return s.agent.GetContextTokens()
}

func (s *agentFooterSource) TotalCost() float64 {
	if s == nil || s.agent == nil {
		return 0
	}
	return s.agent.GetTotalCost()
}

func (s *agentFooterSource) WorkingDir() string {
	wd, _ := os.Getwd()
	return wd
}

// ActiveSubagents satisfies the optional activeSubagentsSource interface in
// pkg/console so the footer can render " · N sub" while subagents are
// in flight. SP-051-2d.
func (s *agentFooterSource) ActiveSubagents() int {
	return agent.GetActiveSubagents()
}

// QueuedMessages satisfies the optional queuedMessagesSource interface
// so the footer renders a "⏸ N queued" badge when the user has
// deferred steer messages via Tab+Enter waiting for the next turn.
// SP-055 Phase 3b.
func (s *agentFooterSource) QueuedMessages() int {
	if s.agent == nil {
		return 0
	}
	return s.agent.DeferredMessageCount()
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
// clean rendering with no spinner frames overwriting the prompt text. When
// footer is non-nil, it is refreshed on each ToolEnd so cost / context
// stay current as tools consume tokens.
//
// The chatAgent reference is used to resolve subagent personas to their
// effective provider/model so `run_subagent` lines can show which model
// will actually run the delegated task (subagents often use cheaper or
// faster models than the parent, and visibility into that matters).
func startTerminalToolSubscriber(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus, indicator *console.ActivityIndicator, footer *console.StatusFooter) func() {
	if eventBus == nil || indicator == nil {
		return func() {}
	}
	subName := fmt.Sprintf("cli_tool_indicator_%d", time.Now().UnixNano())
	ch := eventBus.Subscribe(subName)

	// SP-051-2c: per-turn dedupe of "↳ persona spawned" announcement lines.
	// We track which (depth, persona) pairs have already been announced for
	// the current turn; the returned reset func is invoked by the REPL loop
	// at the start of each user turn so the next batch of subagents gets
	// fresh announcements.
	var spawnMu sync.Mutex
	seenSpawn := make(map[string]bool)
	resetSpawn := func() {
		spawnMu.Lock()
		seenSpawn = make(map[string]bool)
		spawnMu.Unlock()
	}

	// Phase 3: tool-collapse state. Tracks the most recently completed
	// tool's (name, depth, persona) so consecutive identical calls
	// merge into "✓ read_file × N (foo, bar, baz)" instead of stacking
	// N rows. Reset by any inter-tool event that would invalidate the
	// row layout (currently: only sessions where < 30s elapsed since
	// the previous end qualify for the collapse).
	var run *toolRunState

	// pendingArgs caches the `arguments` JSON string from ToolStart
	// events keyed by tool_call_id so the corresponding ToolEnd can
	// recover the args for preview rendering. ToolEndEvent (in
	// pkg/events/events.go) does NOT carry arguments — so without this
	// cache the collapsed-run line rendered as "× N (, , )" with empty
	// parens. Entries clear on the matching ToolEnd to bound growth.
	pendingArgs := map[string]string{}

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
					if id, _ := data["tool_call_id"].(string); id != "" && args != "" {
						pendingArgs[id] = args
					}
					depth := readEventDepth(data)
					persona := readEventPersona(data)
					// SP-051-2c: announce subagent spawn once per (depth,
					// persona) pair per turn, with provider/model so the user
					// can see which cheaper/faster model is doing the work.
					if depth > 0 && persona != "" {
						key := fmt.Sprintf("%d:%s", depth, persona)
						spawnMu.Lock()
						announce := !seenSpawn[key]
						if announce {
							seenSpawn[key] = true
						}
						spawnMu.Unlock()
						if announce {
							indicator.Stop()
							fmt.Fprintln(os.Stderr, formatSpawnLine(chatAgent, depth, persona))
						}
					}
					// Ensure the spinner lands on a fresh line so it never
					// overwrites partial streamed text. Stdout for parity
					// with how stream chunks were just printed.
					fmt.Fprintln(os.Stdout)
					indicator.Start(formatToolStartLine(depth, persona, name, formatToolPreview(chatAgent, name, args)))
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
					icon := console.GlyphSuccess.Prefix()
					if status != "completed" {
						icon = console.GlyphError.Prefix()
					}
					// ToolEnd doesn't carry arguments; recover them from
					// the ToolStart cache so the collapse-line preview
					// shows real paths instead of empty parens.
					args, _ := data["arguments"].(string)
					if args == "" {
						if id, _ := data["tool_call_id"].(string); id != "" {
							if cached, ok := pendingArgs[id]; ok {
								args = cached
								delete(pendingArgs, id)
							}
						}
					}
					depth := readEventDepth(data)
					persona := readEventPersona(data)
					preview := formatToolPreview(chatAgent, name, args)

					// Phase 3 collapse: if this end matches the prior run
					// (same name/depth/persona) AND less than 30s elapsed,
					// merge with the prior tool-end row instead of stacking
					// a new one. The 30s heuristic prevents collapse when
					// the model has streamed text between calls (which
					// would invalidate the row math).
					now := time.Now()
					if run != nil && run.matches(name, depth, persona) && now.Sub(run.lastEnd) < 30*time.Second {
						run.count++
						run.appendArg(preview)
						run.totalMs += durationMs
						run.lastEnd = now
						run.lastIcon = icon
						// 2 rows up: the spinner row (now cleared by
						// Stop) + the blank stdout newline emitted by
						// ToolStart + the previous tool-end row. The
						// indicator's Stop already cleared the spinner
						// row in place, so we walk past the blank line
						// and the previous end-line.
						indicator.ReplaceLastN(formatToolRunLine(
							run.depth, run.persona, run.lastIcon, run.name,
							run.count, run.argsTrail,
							float64(run.totalMs)/1000.0,
						), 2)
					} else {
						indicator.Replace(formatToolEndLine(depth, persona, icon, name,
							preview, float64(durationMs)/1000.0))
						run = &toolRunState{
							name:      name,
							depth:     depth,
							persona:   persona,
							count:     1,
							argsTrail: []string{preview},
							totalMs:   durationMs,
							lastIcon:  icon,
							lastEnd:   now,
						}
					}
					footer.Refresh()
				case events.EventTypeStreamChunk:
					// Assistant text or reasoning chunk landed in the
					// scroll region — any future tool-end can no longer
					// safely use ReplaceLastN to collapse onto the prior
					// row (the rows in between now hold model text).
					// Break the run; the next ToolEnd will print a fresh
					// row.
					if _, isText := data["content_type"].(string); isText {
						run = nil
					}
				case events.EventTypeSubagentActivity:
					// SP-051-2d: render a one-line completion summary for
					// each subagent run. The spawn line ("↳ persona spawned
					// (provider · model)") already prints on the first
					// tool event from the subagent; the matching "done"
					// line below closes the bracket with the actual cost
					// of the delegation — tokens consumed, dollar cost,
					// and wall time — so the user can see at a glance how
					// expensive each subagent run was.
					status, _ := data["status"].(string)
					if status != "completed" && status != "cancelled" {
						break
					}
					persona, _ := data["persona"].(string)
					tokens := readEventInt(data, "tokens_used")
					elapsedMs := readEventInt64(data, "elapsed_ms")
					cost, _ := data["cost"].(float64)
					reason, _ := data["reason"].(string)
					// Subagents nest under the parent that spawned them.
					// Depth on the activity event isn't carried today, so
					// indent at the same level as the run_subagent tool
					// line — depth 1 — which is the common case. Deeper
					// nests fall back to a single indent rather than
					// guessing wrong.
					indicator.Stop()
					fmt.Fprintln(os.Stderr, formatSubagentDoneLine(persona, status, reason, tokens, cost, float64(elapsedMs)/1000.0))
					run = nil
					footer.Refresh()
				case events.EventTypeSecurityApprovalRequest,
					events.EventTypeSecurityPromptRequest,
					events.EventTypeAskUserRequest:
					// A prompt is about to render — stop any spinner so it
					// doesn't overwrite the prompt text. Subsequent activity
					// (next tool event, stream chunks) re-starts naturally.
					indicator.Stop()
					// Same row-layout invalidation as above.
					run = nil
				}
			}
		}
	}()

	return resetSpawn
}

// formatSpawnLine renders the one-shot "↳ persona spawned (provider · model)"
// line emitted the first time the CLI sees a new (depth, persona) pair in a
// turn. Indent matches the corresponding tool-line depth so it visually
// nests under the parent that spawned it.
func formatSpawnLine(chatAgent *agent.Agent, depth int, persona string) string {
	indent := console.PersonaIndent(depth)
	badge := console.PersonaBadge(depth, persona)
	suffix := ""
	if chatAgent != nil {
		if provider, model, err := chatAgent.GetPersonaProviderModel(persona); err == nil && (provider != "" || model != "") {
			suffix = fmt.Sprintf(" (%s · %s)", provider, model)
		}
	}
	return fmt.Sprintf("%s  ↳ %sspawned%s", indent, badge, suffix)
}

// formatSubagentDoneLine renders the per-subagent completion summary —
// the closing bracket for the spawn line. Format:
//
//	  ↳ [persona] done · 12,345 tok · $0.0234 · 4.2s
//	  ↳ [persona] cancelled (budget_exceeded) · 8,901 tok · $0.0102 · 2.1s
//
// Indents at depth 1 to nest visually under the parent's run_subagent
// row. Numeric fields are omitted when zero so a no-cost / no-token
// cancellation stays terse rather than printing "0 tok · $0.0000".
func formatSubagentDoneLine(persona, status, reason string, tokens int, cost, elapsedSec float64) string {
	indent := console.PersonaIndent(1)
	badge := console.PersonaBadge(1, persona)
	icon := console.GlyphSuccess.Prefix()
	verb := "done"
	if status == "cancelled" {
		icon = console.GlyphPaused.Prefix()
		verb = "cancelled"
		if reason != "" {
			verb = fmt.Sprintf("cancelled (%s)", reason)
		}
	}
	parts := []string{}
	if tokens > 0 {
		parts = append(parts, fmt.Sprintf("%s tok", formatThousands(tokens)))
	}
	if cost > 0 {
		parts = append(parts, fmt.Sprintf("$%.4f", cost))
	}
	if elapsedSec > 0 {
		parts = append(parts, fmt.Sprintf("%.1fs", elapsedSec))
	}
	suffix := ""
	if len(parts) > 0 {
		suffix = " · " + strings.Join(parts, " · ")
	}
	return fmt.Sprintf("%s  ↳ %s %s%s%s", indent, icon, badge, verb, suffix)
}

// readEventInt extracts an int from an event payload, tolerating the
// numeric types the event bus may marshal through (int / int64 /
// float64 round-trip via JSON).
func readEventInt(data map[string]interface{}, key string) int {
	if data == nil {
		return 0
	}
	switch v := data[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

func readEventInt64(data map[string]interface{}, key string) int64 {
	if data == nil {
		return 0
	}
	switch v := data[key].(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	}
	return 0
}

// formatThousands renders an integer with comma separators (e.g.
// 1234567 → "1,234,567"). Negative numbers keep the sign.
func formatThousands(n int) string {
	if n < 0 {
		return "-" + formatThousands(-n)
	}
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	// Insert commas from the right.
	rem := len(s) % 3
	var b strings.Builder
	if rem > 0 {
		b.WriteString(s[:rem])
		if len(s) > rem {
			b.WriteByte(',')
		}
	}
	for i := rem; i < len(s); i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < len(s) {
			b.WriteByte(',')
		}
	}
	return b.String()
}

// readEventDepth reads the subagent_depth from an event payload. Returns 0
// for missing or malformed values — matches today's "primary agent" rendering
// when older events that pre-date SP-051 metadata land in the bus.
func readEventDepth(data map[string]interface{}) int {
	if data == nil {
		return 0
	}
	switch v := data["subagent_depth"].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

// readEventPersona reads the active_persona from an event payload, trimmed.
// Returns "" when absent — which suppresses the persona badge.
func readEventPersona(data map[string]interface{}) string {
	if data == nil {
		return ""
	}
	if s, ok := data["active_persona"].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

// formatToolStartLine builds the activity-indicator line for a ToolStart
// event. At depth 0 it's byte-identical to the pre-SP-051 format
// ("  tool_name(preview)") so primary-agent tool calls render unchanged.
// At depth >= 1 it adds a depth indent and a colored "[persona]" badge.
func formatToolStartLine(depth int, persona, toolName, preview string) string {
	indent := console.PersonaIndent(depth)
	badge := console.PersonaBadge(depth, persona)
	return fmt.Sprintf("%s  %s%s%s", indent, badge, toolName, preview)
}

// formatToolEndLine builds the activity-indicator replacement line for a
// ToolEnd event. Same depth/badge logic as formatToolStartLine.
func formatToolEndLine(depth int, persona, icon, toolName, preview string, durationSec float64) string {
	indent := console.PersonaIndent(depth)
	badge := console.PersonaBadge(depth, persona)
	return fmt.Sprintf("%s  %s %s%s%s · %.1fs", indent, icon, badge, toolName, preview, durationSec)
}

// formatToolRunLine renders a collapsed line for repeated calls of the
// same tool. Replaces N stacked "✓ read_file (foo.go) · 0.1s" entries
// with a single "✓ read_file × N (foo.go, bar.go, baz.go) · 0.3s" line
// updated in place via ActivityIndicator.ReplaceLastN.
//
// argsTrail holds the most recent up-to-3 arg previews so the user can
// still see what was touched without scrolling through identical
// entries. totalSec is the cumulative duration across all N calls so
// the line still surfaces "this batch took a moment" even when each
// individual call was quick.
func formatToolRunLine(depth int, persona, icon, toolName string, count int, argsTrail []string, totalSec float64) string {
	indent := console.PersonaIndent(depth)
	badge := console.PersonaBadge(depth, persona)
	preview := ""
	if len(argsTrail) > 0 {
		preview = " (" + strings.Join(argsTrail, ", ") + ")"
	}
	return fmt.Sprintf("%s  %s%s%s × %d%s · %.1fs",
		indent, icon, badge, toolName, count, preview, totalSec)
}

// toolRunState tracks a sequence of consecutive identical tool calls
// so the subscriber can collapse them into a single in-place row
// (Phase 3 of CLI ergonomics). A run is broken — set to nil — whenever
// any non-tool event would invalidate the row math: streaming
// assistant text, a different tool, or a user-prompt boundary.
type toolRunState struct {
	name      string
	depth     int
	persona   string
	count     int
	argsTrail []string // most recent up to maxArgsTrail entries
	totalMs   int64
	lastIcon  string
	lastEnd   time.Time
}

// maxArgsTrail caps the per-arg preview list shown in the collapsed
// line. The earliest entries get dropped — the user usually cares
// about the most recent few calls in a run.
const maxArgsTrail = 3

func (r *toolRunState) matches(name string, depth int, persona string) bool {
	return r != nil && r.name == name && r.depth == depth && r.persona == persona
}

func (r *toolRunState) appendArg(preview string) {
	// formatToolPreview returns its result already wrapped in " (...)"
	// so that the start/end lines render as "tool (arg)". For the
	// collapsed run line we re-wrap argsTrail as a single parenthesised
	// list ("(a, b, c)"), so strip the per-arg wrap here. Otherwise
	// the line read "tool × N ( (a),  (b))" with doubled parens.
	stripped := strings.TrimPrefix(preview, " (")
	stripped = strings.TrimSuffix(stripped, ")")
	stripped = strings.TrimSpace(stripped)
	if stripped == "" {
		// No useful arg captured — skip rather than append "" which
		// renders as a stray comma-space ("× N (, , foo)").
		return
	}
	r.argsTrail = append(r.argsTrail, stripped)
	if len(r.argsTrail) > maxArgsTrail {
		r.argsTrail = r.argsTrail[len(r.argsTrail)-maxArgsTrail:]
	}
}

// formatToolPreview produces a short, single-line preview of a tool call
// for the activity-indicator timeline. For subagent tools (run_subagent,
// run_parallel_subagents) it surfaces the persona and the resolved
// provider/model so users can see which subagent — and which underlying
// model, often a cheaper/faster one than the parent's — is doing the
// work. For everything else it falls through to formatToolArgPreview.
func formatToolPreview(chatAgent *agent.Agent, toolName, arguments string) string {
	switch toolName {
	case "run_subagent":
		return formatRunSubagentPreview(chatAgent, arguments)
	case "run_parallel_subagents":
		return formatRunParallelSubagentsPreview(arguments)
	default:
		return formatToolArgPreview(toolName, arguments)
	}
}

// formatRunSubagentPreview extracts the persona from args and looks up its
// effective provider/model via the agent's persona resolver. Format:
//
//	 (coder · anthropic/claude-haiku-4-5)
//
// Falls back to just persona name (or empty) when the lookup fails.
func formatRunSubagentPreview(chatAgent *agent.Agent, arguments string) string {
	if arguments == "" || chatAgent == nil {
		return ""
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return ""
	}
	persona, _ := args["persona"].(string)
	persona = strings.TrimSpace(persona)
	if persona == "" {
		return ""
	}
	provider, model, err := chatAgent.GetPersonaProviderModel(persona)
	if err != nil || (provider == "" && model == "") {
		return fmt.Sprintf(" (%s)", persona)
	}
	return fmt.Sprintf(" (%s · %s/%s)", persona, provider, model)
}

// formatRunParallelSubagentsPreview shows the task count so the user
// knows how many subagents fanned out. No per-task persona since the
// parallel form doesn't accept per-task persona overrides today; users
// see the count and infer fan-out from the line.
func formatRunParallelSubagentsPreview(arguments string) string {
	if arguments == "" {
		return ""
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return ""
	}
	if tasks, ok := args["subagents"].([]interface{}); ok && len(tasks) > 0 {
		return fmt.Sprintf(" (%d tasks)", len(tasks))
	}
	return ""
}

// formatToolArgPreview produces a short, single-line preview of a tool's
// arguments for the activity indicator. The arguments string is the raw
// JSON the model emitted; we extract whichever field is most informative
// for the tool at hand. Returns an empty string (no parens) when nothing
// useful is available. Best-effort — invalid JSON yields no preview.
//
// Per-tool max widths and truncation strategies:
//   - File paths use abbreviatePath so the filename always survives even
//     when the directory prefix has to be dropped — "…/last/two/seg.go"
//     reads better than "webui/src/components/sett…" where the actual
//     file is lost.
//   - shell_command / exec preserve more context (80 chars) because the
//     suffix of a command is often the meaningful part (pipes, args).
//   - Everything else gets the conservative 70-char tail truncation.
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

	var preview string
	var maxLen int
	isPath := false
	switch toolName {
	case "read_file", "write_file", "edit_file", "write_structured_file", "patch_structured_file":
		preview = pick("path", "file_path", "filename")
		maxLen = 70
		isPath = true
	case "shell_command", "exec":
		preview = pick("command", "cmd")
		maxLen = 80
	case "search_files", "grep":
		preview = pick("pattern", "query", "search")
		maxLen = 70
	case "fetch_url":
		preview = pick("url")
		maxLen = 70
	default:
		// Generic fallback: surface the first short string value.
		for _, v := range args {
			if s, ok := v.(string); ok && len(s) > 0 && len(s) < 120 {
				preview = s
				break
			}
		}
		maxLen = 70
	}

	preview = sanitizeArgForPreview(preview)
	if preview == "" {
		return ""
	}
	if isPath {
		preview = abbreviatePath(preview, maxLen)
	} else if len(preview) > maxLen {
		preview = preview[:maxLen-1] + "…"
	}
	return " (" + preview + ")"
}

// abbreviatePath shortens a path while preserving the filename. A path
// like "webui/src/components/settings/ProviderSettingsTab.tsx" that
// exceeds maxLen renders as "…/ProviderSettingsTab.tsx" — the user
// almost always cares about the file at the tail more than the
// directory chain.
//
// When the path has a separator we always prefer "…/basename" even if
// the basename itself is still over maxLen: the alternative (tail-
// truncating the basename) drops the suffix that usually identifies
// the file type, which is worse than overshooting maxLen by a few
// chars on a pathological filename. The only path with no separator
// falls back to a plain tail-truncate.
func abbreviatePath(p string, maxLen int) string {
	if len(p) <= maxLen {
		return p
	}
	slash := strings.LastIndex(p, "/")
	if slash < 0 {
		return p[:maxLen-1] + "…"
	}
	return "…/" + p[slash+1:]
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
