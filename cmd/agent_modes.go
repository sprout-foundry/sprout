//go:build !js

// Agent modes: handles interactive and direct execution modes
package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/cliui"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/webui"
	"github.com/sprout-foundry/sprout/pkg/workflow"
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
	workflowOverrides := buildWorkflowCLIOverrides()
	workflowConfig, workflowLoadErr := workflow.LoadAgentWorkflowConfig(agentWorkflowConfig)
	if workflowLoadErr != nil {
		return workflowLoadErr
	}
	workflow.ApplyWorkflowCommandOverrides(workflowConfig, workflowOverrides)

	// When a workflow config defines an initial prompt, force non-interactive
	// (direct) mode. Without this, the isInteractive branch calls
	// runInteractiveMode which never consults the workflow config, so the
	// user sees a blank REPL instead of the workflow executing.
	if workflowConfig != nil && workflowConfig.Initial != nil &&
		(strings.TrimSpace(workflowConfig.Initial.Prompt) != "" || strings.TrimSpace(workflowConfig.Initial.PromptFile) != "") {
		isInteractive = false
	}

	// A workflow/automate run is driven by a config file, not a person at the
	// keyboard. Even when it is launched from a terminal (so os.Stdin is a
	// TTY), nobody answers the Caution approval prompt, so a run must never
	// wait on it: Caution results follow the configured risk profile without
	// prompting, while hard blocks still block. Marking the agent here makes
	// every approval surface (Gate 1, the broker, the filesystem gate) treat
	// the run as non-interactive regardless of the console's interactivity.
	markWorkflowRun(chatAgent, workflowConfig)

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
		setupDaemonLogging()
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

	webServer, webUISup, bindAddr, webSocketCleanup, webSetupErr := setupWebUIServer(ctx, cancel, chatAgent, eventBus, enableWebUI)
	if webSetupErr != nil {
		return webSetupErr
	}
	defer webSocketCleanup()

	shutdown := startSignalHandler(ctx, cancel, chatAgent, isInteractive)

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

	// Start progress event emitter (opt-in via --progress-events flag).
	// Works for both interactive and direct modes. Emits one-line milestones
	// to stderr, stdout, or a file. Safe no-op when flag is not set.
	progressEmitter := startProgressEmitter(ctx, eventBus)
	defer progressEmitter.stop()

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

		err = runInteractiveMode(ctx, chatAgent, eventBus, indicator)
	} else {
		directModeStart := time.Now()
		if err := chatAgent.GetConfigManager().UpdateConfigNoSave(func(cfg *configuration.Config) error {
			cfg.SkipPrompt = true
			return nil
		}); err != nil {
			return fmt.Errorf("failed to update config for direct mode: %w", err)
		}

		// SP-048-4: When the direct-mode run has a terminal on stderr
		// (workflow coordinator invoked with shared stdout/stderr but no
		// stdin), start the status footer and terminal tool subscriber
		// so the user sees the same tool timeline and footer as the
		// interactive CLI. The footer is TTY-gated internally; we gate
		// the subscriber too so agent_message rendering through the
		// subscriber only activates when there is a real terminal.
		if chatAgent != nil && !daemonMode && term.IsTerminal(int(os.Stderr.Fd())) {
			footerSource := &agentFooterSource{agent: chatAgent}
			footer := console.NewStatusFooter(os.Stderr, footerSource)
			console.RegisterGlobalStatusFooter(footer)
			footer.Start()
			defer footer.Stop()

			subCtx, cancelSub := context.WithCancel(ctx)
			defer cancelSub()
			_ = cliui.StartTerminalToolSubscriber(subCtx, chatAgent, eventBus, indicator, footer)
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

		query, err = workflow.ResolveWorkflowInitialPrompt(query, workflowConfig)
		if err != nil {
			return fmt.Errorf("failed to resolve workflow initial prompt: %w", err)
		}

		// SP-136 P4: one-shot CLI-on-daemon. Plain (non-workflow) one-shot
		// queries route through the daemon's agent socket when it is
		// available; the daemon owns the agent. Falls back to in-process
		// when the socket is unreachable or when a behavior-changing flag
		// that can't travel the wire protocol is set (agentSkipDaemonRouting).
		// Routing also requires the daemon to match this binary and config
		// (see tryDaemonOneShot); on any mismatch the turn runs in-process.
		if query != "" && workflowConfig == nil && !daemonMode && !agentSkipDaemonRouting() {
			if handled, derr := tryDaemonOneShot(ctx, query, outputFormatJSON); handled {
				return derr
			}
		}
		// The turn will run in-process: say so, naming the binary and config
		// that will run it, so the user is never left guessing which of the
		// two (CLI or daemon) actually handled the query.
		if query != "" && workflowConfig == nil && !daemonMode {
			printInProcessIdentity()
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

		restoreRuntimeOverrides, restoreSetupErr := workflow.PrepareWorkflowRuntimeRestorer(chatAgent, workflowConfig, workflowOverrides)
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
		workflowState, workflowStateErr := workflow.LoadWorkflowExecutionState(workflowConfig)
		if workflowStateErr != nil {
			return fmt.Errorf("failed to load workflow execution state: %w", workflowStateErr)
		}
		if restoreErr := workflow.RestoreWorkflowConversationState(chatAgent, workflowConfig, workflowState); restoreErr != nil {
			return fmt.Errorf("failed to restore workflow conversation state: %w", restoreErr)
		}

		// Attach the workflow's USD budget and progress heartbeat before
		// any LLM call. stopBudget MUST be invoked before the agent
		// shuts down so the heartbeat goroutine exits and callbacks are
		// cleared. Safe no-op when no budget is configured.
		stopBudget := workflow.AttachWorkflowBudget(chatAgent, workflowConfig)
		defer stopBudget()
		if workflowConfig != nil && workflowConfig.OrchestrationEnabled() {
			if eventErr := workflow.EmitWorkflowOrchestrationEvent(workflowConfig, "workflow_run_started", map[string]interface{}{
				"initial_completed": workflowState.InitialCompleted,
				"next_step_index":   workflowState.NextStepIndex,
			}); eventErr != nil {
				return fmt.Errorf("failed to emit workflow run started event: %w", eventErr)
			}
		}

		// SP-127 Phase 2.3: apply Initial.AllowedPaths (if any) to the
		// session allowlist before anything else. Unlike per-step paths,
		// initial paths are NOT removed — they persist for the entire
		// workflow run. This ensures the cd-target gate (Phase 2.1) and
		// the filesystem gate (Phase 2.2) see these paths as approved
		// from step 1 onward. On resume, RestoreWorkflowConversationState
		// (called above) restores the agent state which includes the
		// session allowlist — the re-apply here is a cheap no-op since
		// AddSessionAllowedFolder is idempotent.
		if workflowConfig != nil && workflowConfig.Initial != nil && len(workflowConfig.Initial.AllowedPaths) > 0 {
			if _, _, _, applyErr := workflow.ApplyWorkflowRuntimeAllowedPaths(chatAgent, workflowConfig.Initial.AllowedPaths); applyErr != nil {
				return fmt.Errorf("failed to apply initial allowed_paths: %w", applyErr)
			}
		}

		shouldRunInitialQuery := strings.TrimSpace(query) != "" && !workflowState.InitialCompleted
		if shouldRunInitialQuery {
			if err := workflow.ApplyWorkflowInitialOverrides(chatAgent, workflowConfig, workflowOverrides); err != nil {
				return fmt.Errorf("failed to apply workflow initial runtime overrides: %w", err)
			}

			err = runDirectMode(ctx, chatAgent, eventBus, query)
			workflowState.InitialCompleted = true
			workflowState.HasError = err != nil
			workflowState.LastProvider = strings.TrimSpace(chatAgent.GetProvider())
			if err != nil {
				workflowState.FirstError = err.Error()
			}
			if persistErr := workflow.PersistWorkflowCheckpoint(workflowConfig, workflowState, chatAgent); persistErr != nil {
				return fmt.Errorf("failed to persist workflow checkpoint: %w", persistErr)
			}
			if eventErr := workflow.EmitWorkflowOrchestrationEvent(workflowConfig, "workflow_initial_completed", map[string]interface{}{
				"provider":  workflowState.LastProvider,
				"has_error": workflowState.HasError,
			}); eventErr != nil {
				return fmt.Errorf("failed to emit workflow initial completed event: %w", eventErr)
			}
		} else {
			err = nil
		}

		workflowState.HasError = workflowState.HasError || err != nil

		// Coordinator continuation: an "initial" coordinator run has no
		// steps and used to exit the moment its one turn answered, even
		// with runnable `[ ]` items left. Keep issuing continuation turns
		// until nothing runnable remains or a turn makes no progress, then
		// record the stop reason on the run record. Loop mode is the other
		// TODO driver; when it is configured, continuation defers to it.
		if err == nil && workflowConfig != nil && workflowConfig.Continuation != nil && workflowConfig.Loop == nil {
			contResult, contErr := workflow.RunInitialContinuation(ctx, chatAgent, eventBus, workflowConfig, workflowState, workflow.QueryExecutor(ProcessQuery))
			recordContinuationStopReason(contResult)
			if contErr != nil {
				return contErr
			}
		}

		// Loop mode: iterate over TODO items with stateless gate + context reset.
		if workflowConfig != nil && workflowConfig.Loop != nil {
			workflowYielded, workflowErr := workflow.RunAgentWorkflowLoop(ctx, chatAgent, eventBus, workflowConfig, workflowState, workflow.QueryExecutor(ProcessQuery), workflowOverrides)
			if workflowYielded {
				return nil
			}
			if workflowErr != nil {
				if err != nil {
					return fmt.Errorf("%w (workflow loop failed: %w)", err, workflowErr)
				}
				return workflowErr
			}
			// A completed non-interactive run whose
			// verification is enabled and fails exits non-zero. Yielded
			// runs return above (workflow-loop continuation is a workflow
			// decision, not the exit code's).
			if vErr := verificationRunExitError(chatAgent); vErr != nil {
				if outputFormatJSON {
					emitJSONResult(query, directModeStart, vErr, chatAgent)
				}
				return vErr
			}
			if outputFormatJSON {
				emitJSONResult(query, directModeStart, nil, chatAgent)
			}
			return nil
		}

		workflowYielded, workflowErr := workflow.RunAgentWorkflow(ctx, chatAgent, eventBus, workflowConfig, workflowState, workflow.QueryExecutor(ProcessQuery), workflowOverrides)
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
		// A completed non-interactive run whose
		// verification is enabled and fails exits non-zero. The
		// failure report is already in the final reply shown to the user;
		// this error adds the exit code (1 via exitCodeFor) and, for
		// --json, the status:"error" envelope scripts read.
		if vErr := verificationRunExitError(chatAgent); vErr != nil {
			if outputFormatJSON {
				emitJSONResult(query, directModeStart, vErr, chatAgent)
			}
			return vErr
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
			console.GlyphWarning.Fprintf(os.Stderr, "Web server did not shut down cleanly: %v", webErr)
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
