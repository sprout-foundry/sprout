//go:build !js

// Agent modes: handles interactive and direct execution modes
package cmd

import (
	"bufio"
	"context"
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
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/webcontent"
	"github.com/sprout-foundry/sprout/pkg/webui"
	"golang.org/x/term"
)

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
		// Unset on RunAgent exit so the flag never leaks to subprocesses
		// the user explicitly runs after us, or to tests sharing the process.
		// Children spawned during the daemon's lifetime inherit the var at
		// fork time and are unaffected by the unset on our exit.
		defer os.Unsetenv("SPROUT_DAEMON")

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

	// Start OOM watchdog in daemon mode to monitor Node.js process count
	// and total RSS. This alerts via the event bus (and WebUI) before
	// the kernel OOM-killer fires.
	if daemonMode && chatAgent != nil {
		oomWatchdog := agent.NewOOMWatchdog(eventBus)
		oomWatchdog.Start(ctx)
		// Goroutine automatically exits when ctx is cancelled on shutdown.
	}

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

			// In shared mode, register the server so the CLI's ProcessQuery
			// wrapper can sync agent state after each CLI query.
			if !daemonMode {
				setSharedWebServer(webServer)
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

				// In shared mode (non-daemon interactive), seed the agent's
				// event metadata with the default client/chat IDs so that
				// CLI-initiated queries publish events the WebUI can route.
				// Without this, CLI events lack client_id/chat_id and the
				// WebUI tab never receives streaming output or completion
				// notifications for CLI queries.
				if !daemonMode {
					chatAgent.SetEventMetadata(map[string]interface{}{
						"client_id": "default",
						"chat_id":   "default",
					})
				}
			}

			startInstanceTracker(ctx, port, chatAgent)

			// Daemon mode without explicit port → single-port supervisor.
			if webPort == 0 && daemonMode {
				webUISup = newWebUISupervisor(
					webServer,
					port,
					func(activePort int) {
						setWebUIDisplayURL(fmt.Sprintf("http://%s:%d", webui.DisplayAddr(bindAddr), activePort))
						console.GlyphInfo.Printf("Web UI available at http://%s:%d\n", webui.DisplayAddr(bindAddr), activePort)
					},
					func(activePort int) {
						setWebUIDisplayURL(fmt.Sprintf("http://%s:%d", webui.DisplayAddr(bindAddr), activePort))
						console.GlyphInfo.Printf("Reusing active Web UI at http://%s:%d\n", webui.DisplayAddr(bindAddr), activePort)
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
					if webServer.IsRunning() || webUISup.HasAttached() {
						break
					}

					select {
					case <-startupDeadline.C:
						if !webServer.IsRunning() && !webUISup.HasAttached() {
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

				setWebUIDisplayURL(fmt.Sprintf("http://%s:%d", webui.DisplayAddr(bindAddr), webServer.GetPort()))
				console.GlyphInfo.Printf("Web UI available at http://%s:%d\n", webui.DisplayAddr(bindAddr), webServer.GetPort())
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
				// SIGHUP in daemon mode = reload on-disk config (Unix
				// daemon convention). In interactive mode SIGHUP means
				// the controlling terminal closed — the kernel sends it
				// to the foreground process group when the tty hangs
				// up. Treating that as "reload" leaves an orphaned
				// sprout running with PPID=1 forever (it keeps
				// heartbeating to instances.json and holding the
				// task_queue.json flock against new sessions). Fall
				// through to the shutdown path so terminal close cleans
				// up the process.
				if sig == syscall.SIGHUP && daemonMode {
					fmt.Println()
					console.GlyphAction.Printf("Received SIGHUP, reloading configuration...")
					if chatAgent != nil {
						if mgr := chatAgent.GetConfigManager(); mgr != nil {
							if err := mgr.Reload(); err != nil {
								console.GlyphError.Fprintf(os.Stdout, "Reload failed: %v", err)
							} else {
								console.GlyphSuccess.Print("Configuration reloaded successfully.")
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
					// SP-056-6d: Resolve any active reasoning fold on interrupt.
					if fold := currentReasoningFold; fold != nil && fold.IsActive() {
						fold.Interrupt()
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
		return runQueueMode(ctx, chatAgent, eventBus, indicator)
	}

	// When agent is nil (provider not configured in daemon mode), skip to
	// the daemon wait path. The web UI handles provider setup interactively.
	if chatAgent == nil && daemonMode && webServer != nil && webServer.IsRunning() {
		console.GlyphInfo.Printf("Web UI running at http://%s:%d (no provider configured — configure via web UI)\n", webui.DisplayAddr(bindAddr), webServer.GetPort())
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
		hasLoop := workflowConfig != nil && workflowConfig.Loop != nil
		if query == "" && !hasLoop && (workflowConfig == nil || len(workflowConfig.Steps) == 0) {
			// No query provided - check if we should keep running (daemon mode)
			if daemonMode && webServer != nil && webServer.IsRunning() {
				// Daemon mode: keep web UI running
				setWebUIDisplayURL(fmt.Sprintf("http://%s:%d", webui.DisplayAddr(bindAddr), webServer.GetPort()))
				console.GlyphInfo.Printf("Web UI running at http://%s:%d\n", webui.DisplayAddr(bindAddr), webServer.GetPort())
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

		// Attach the workflow's USD budget and progress heartbeat before
		// any LLM call. stopBudget MUST be invoked before the agent
		// shuts down so the heartbeat goroutine exits and callbacks are
		// cleared. Safe no-op when no budget is configured.
		stopBudget := attachWorkflowBudget(chatAgent, workflowConfig)
		defer stopBudget()
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

		// Loop mode: iterate over TODO items with stateless gate + context reset.
		if workflowConfig != nil && workflowConfig.Loop != nil {
			workflowYielded, workflowErr := runAgentWorkflowLoop(ctx, chatAgent, eventBus, workflowConfig, workflowState)
			if workflowYielded {
				return nil
			}
			if workflowErr != nil {
				if err != nil {
					return fmt.Errorf("%w (workflow loop failed: %w)", err, workflowErr)
				}
				return workflowErr
			}
			if outputFormatJSON {
				emitJSONResult(query, directModeStart, nil, chatAgent)
			}
			return nil
		}

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

// Turn state singletons (currentTurnRenderer, firstProseChunk) and the
// beginTurn/endTurn helpers live in agent_mode_state.go so they are
// declared in one place and both interactive and queue mode use the same
// reset pattern.
