//go:build !js

package webui

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	agent_commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// chatQueryOptions bundles the per-request overrides that flow into the
// shared query runner. Each field maps 1:1 to a request-body knob on
// either /api/query or /api/proxy/chat.
//
// LogTag is included so each call site can keep its existing log
// prefix ("handleAPIQuery" / "handleAPIProxyChat") instead of switching
// to a generic "runChatQuery" — preserving the operator-facing log
// stream that grep-on-incident flows depend on.
//
// AllowSlashCommands gates the legacy /api/query in-chat slash-command
// path (SP-114 Phase 2). The proxy endpoint does NOT want it: the
// Foundry proxy contract treats /api/proxy/chat as an LLM-only surface,
// and the safe-surface for slash commands is /api/command/execute. The
// shared function picks up the difference via this flag rather than by
// branching on the URL inside the goroutine, which keeps the control
// flow linear and the log line accurate.
type chatQueryOptions struct {
	Provider           string
	Model              string
	WorkspaceRoot      string
	SystemPrompt       string
	AllowSlashCommands bool
	// EchoQueryInAccept controls whether the 202 Accepted response body
	// includes the submitted query text. /api/query historically echoes
	// it; /api/proxy/chat does not (Foundry's CloudAdapter doesn't expect
	// the field). This is a wire-contract concern, not a logging concern,
	// so it gets its own field rather than being inferred from LogTag.
	EchoQueryInAccept bool
	LogTag            string
}

// stopActiveQuery is the shared backend for /api/query/stop and
// /api/proxy/chat/stop (SP-059 Phase 1a consolidation). It encapsulates
// the "no active query → 200 already_completed" / "active →
// TriggerInterrupt + cancel subagents + 202 accepted" branching so both
// handlers stay byte-identical on the wire.
//
// The shared agent is interrupted and every active subagent is cancelled
// via runner.CancelSubagent. Without the subagent cancel, the primary
// unblocks but each subagent's ProcessQuery continues until natural
// completion — the user sees Stop do nothing for tens of seconds while
// work that should have been aborted keeps burning tokens.
//
// Returns cancelledSubagents so the caller can include it in the JSON
// payload (the /api/query handler does; the proxy handler historically
// didn't, but the unified path now does — that's the bug fix).
//
// alreadyCompleted is true when the caller can short-circuit with the
// 200 already_completed response (no active query, nothing to stop).
func (ws *ReactWebServer) stopActiveQuery(w http.ResponseWriter, r *http.Request, clientID, chatID string) (alreadyCompleted bool) {
	ws.mutex.RLock()
	ctx := ws.clientContexts[clientID]
	if ctx == nil || !ctx.hasActiveQueryForChat(chatID) {
		ws.mutex.RUnlock()
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"status":            "ok",
			"already_completed": true,
			"timestamp":         time.Now().Unix(),
		})
		return true
	}
	ws.mutex.RUnlock()

	clientAgent, err := ws.getChatAgent(clientID, chatID)
	if err != nil {
		if isProviderConfigError(err) {
			writeJSONErr(w, http.StatusServiceUnavailable, "no_provider", "AI features require a provider. Please configure one in settings.")
			return true // response written; caller should not write again
		}
		http.Error(w, fmt.Sprintf("Failed to access chat agent: %v", err), http.StatusInternalServerError)
		return true
	}

	clientAgent.TriggerInterrupt()

	// SP-059 Phase 1a: cancel running subagents too. Without this the
	// primary's TriggerInterrupt unblocks its own loop but the
	// subagent's ProcessQuery continues until natural completion —
	// the user sees the Stop button do nothing for tens of seconds.
	cancelledSubagents := 0
	if runner := clientAgent.GetSubagentRunner(); runner != nil {
		for _, sub := range runner.GetActiveSubagents() {
			if runner.CancelSubagent(sub.ID) {
				cancelledSubagents++
			}
		}
	}

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"accepted":            true,
		"mode":                "stop",
		"chat_id":             chatID,
		"timestamp":           time.Now().Unix(),
		"cancelled_subagents": cancelledSubagents,
	})
	return false
}

// chatQueryStatus is the shared backend for /api/query/status and
// /api/proxy/chat/status. Returns whether a query is currently active
// for (clientID, chatID). Both handlers wrap this with their own method
// gate and response payload — the lookup itself is byte-identical.
func (ws *ReactWebServer) chatQueryStatus(clientID, chatID string) bool {
	ws.mutex.RLock()
	ctx := ws.clientContexts[clientID]
	active := ctx != nil && ctx.hasActiveQueryForChat(chatID)
	ws.mutex.RUnlock()
	return active
}

// rollbackActiveQuery releases the active-query gate set in step 2 of
// runChatQuery. Called from error paths between the atomic set and the
// goroutine launch so the counter doesn't leak.
func (ws *ReactWebServer) rollbackActiveQuery(clientID, chatID string) {
	ws.mutex.Lock()
	defer ws.mutex.Unlock()
	if ws.activeQueries > 0 {
		ws.activeQueries--
	}
	if ctx := ws.clientContexts[clientID]; ctx != nil {
		ctx.setChatQueryActive(chatID, false, "")
	}
}

// runChatQuery is the unified backend for /api/query and /api/proxy/chat
// (post-Steer dispatch). It encapsulates the full query lifecycle:
//
//  1. Workspace-root resolution (worktree-aware, then request default).
//  2. Atomic active-query check-and-set under ws.mutex (TOCTOU-safe).
//  3. Agent resolution OUTSIDE the lock — provider init can block.
//  4. Per-query overrides (provider / model / workspace_root /
//     system_prompt). Provider and model failures now return
//     writeJSONErr (400) instead of silently logging — this is the
//     fix that the proxy chat path was missing.
//  5. Shared-agent guard for daemon / CLI shared mode.
//  6. Slash-command dispatch (when AllowSlashCommands is true and the
//     input starts with /) with stdout capture.
//  7. Async ProcessQueryWithContinuity goroutine with panic recovery,
//     cost recording, async state sync, and error/success log lines.
//
// On any pre-launch failure the function rolls back the active-query
// state it set in step 2 before returning, so callers never leak the
// gate counter.
//
// On success (after launching the goroutine) the function writes a
// 202 Accepted response. The /api/query caller historically echoes the
// submitted query text; the /api/proxy/chat caller does not — the
// distinction is preserved via opts.EchoQueryInAccept.
//
// logTag distinguishes log lines from the two callers so existing
// log-grep workflows keep working. The function deliberately does NOT
// write method-not-allowed — that's the caller's job (so they can
// return 405 with their own log prefix).
func (ws *ReactWebServer) runChatQuery(
	w http.ResponseWriter,
	r *http.Request,
	clientID, chatID, query string,
	opts chatQueryOptions,
) {
	logTag := opts.LogTag
	if logTag == "" {
		logTag = "runChatQuery"
	}

	// Resolve workspace root with worktree awareness - check if chat has a worktree path
	workspaceRoot := ws.resolveWorkspaceRootForChat(clientID, chatID)
	if workspaceRoot == "" {
		workspaceRoot = ws.getWorkspaceRootForRequest(r)
	}

	ws.mutex.Lock()
	ctx := ws.clientContexts[clientID]
	if ctx == nil {
		ws.mutex.Unlock()
		http.Error(w, "Client context not found", http.StatusBadRequest)
		return
	}
	if ctx.hasActiveQueryForChat(chatID) {
		ws.mutex.Unlock()
		http.Error(w, "A query is already running for this chat", http.StatusConflict)
		return
	}
	// Atomically mark the query as active while still holding the lock so
	// a concurrent request for the same chat cannot also pass the check.
	// Releasing the lock between the check and the set would create a
	// TOCTOU race where two requests could both enter the query
	// goroutine and corrupt the same agent's state.
	ws.queryCount++
	ws.activeQueries++
	ctx.setChatQueryActive(chatID, true, query)
	ws.mutex.Unlock()

	// Resolve the agent AFTER the active-query lock is released. Creating
	// an agent may block (config load, provider init), and holding ws.mutex
	// during that would serialize all incoming queries across all chats.
	clientAgent, err := ws.getChatAgent(clientID, chatID)
	if err != nil {
		ws.rollbackActiveQuery(clientID, chatID)

		if isProviderConfigError(err) {
			writeJSONErr(w, http.StatusServiceUnavailable, "no_provider", "AI features require a provider. Please configure one in settings.")
		} else {
			http.Error(w, fmt.Sprintf("failed to initialize chat agent: %v", err), http.StatusInternalServerError)
		}
		return
	}

	// Apply per-query overrides: provider, model.
	// On failure, return an error to the client instead of silently
	// proceeding with the wrong provider/model — the user's query would
	// run against an unexpected model with no indication.
	if opts.Provider != "" {
		cm := ws.getConfigManager(r, w)
		if cm != nil {
			// Enrich custom providers from disk before mapping — the config
			// manager may not have them loaded if it was created via fallback.
			cm.EnrichCustomProviders()
			providerType, mapErr := cm.MapStringToClientType(opts.Provider)
			if mapErr != nil {
				ws.rollbackActiveQuery(clientID, chatID)
				writeJSONErr(w, http.StatusBadRequest, "invalid_provider",
					fmt.Sprintf("Invalid provider %q: %v", opts.Provider, mapErr))
				return
			}
			if serr := clientAgent.SetProvider(providerType); serr != nil {
				ws.rollbackActiveQuery(clientID, chatID)
				writeJSONErr(w, http.StatusBadRequest, "provider_switch_failed",
					fmt.Sprintf("Failed to switch to provider %q: %v", opts.Provider, serr))
				return
			}
		}
	}
	if opts.Model != "" {
		if err := clientAgent.SetModel(opts.Model); err != nil {
			ws.rollbackActiveQuery(clientID, chatID)
			writeJSONErr(w, http.StatusBadRequest, "model_switch_failed",
				fmt.Sprintf("Failed to switch to model %q: %v", opts.Model, err))
			return
		}
	}

	// Apply per-query workspace root override
	if opts.WorkspaceRoot != "" {
		workspaceRoot = opts.WorkspaceRoot
	}

	// Apply per-query system prompt override (session-scoped, resets after query not needed)
	if opts.SystemPrompt != "" {
		clientAgent.SetSystemPrompt(opts.SystemPrompt)
	}

	// Shared-agent guard: in shared mode the CLI and WebUI share one Agent.
	// If the CLI is mid-query, reject immediately so the user gets a clear
	// "busy" message instead of a 500 or silent timeout.
	if ws.IsSharedMode() && clientAgent.IsQueryInProgress() {
		ws.rollbackActiveQuery(clientID, chatID)
		writeJSONErr(w, http.StatusConflict, "agent_busy",
			"The terminal is currently processing a query. Try again in a moment.")
		return
	}

	// Slash-command safety gate (SP-114 Phase 2). Validate BEFORE launching
	// the goroutine so an unsafe command returns 400 synchronously rather
	// than racing with the 202 Accepted written at the end of this function.
	// Writing the error from inside the goroutine after the parent has
	// already returned 202 causes a double-write on the ResponseWriter.
	var slashCmdSafe bool
	if opts.AllowSlashCommands {
		registry := agent_commands.NewCommandRegistry()
		if registry.IsSlashCommand(query) {
			parts := strings.Fields(strings.TrimSpace(query))
			var headCmd string
			if len(parts) > 0 {
				headCmd = strings.TrimPrefix(parts[0], "/")
			}
			if headCmd != "" {
				if cmd, ok := registry.GetCommand(headCmd); ok {
					if sc, ok := cmd.(agent_commands.SteerCapable); ok && sc.SafeDuringSteer() {
						slashCmdSafe = true
					}
				}
			}
			if !slashCmdSafe {
				ws.rollbackActiveQuery(clientID, chatID)
				writeJSONErr(w, http.StatusBadRequest, "command_not_safe",
					"Command /"+headCmd+" is not safe to run from the WebUI. Use the CLI or the /api/command/execute safe surface.")
				return
			}
		}
	}

	// Run the query asynchronously. The web UI consumes progress and completion via WebSocket.
	//
	// Defer-recover: ProcessQueryWithContinuity can panic on malformed LLM
	// output or upstream provider failures. Without a recover here, a panic
	// would skip the deferred activeQueries decrement and chatQueryActive
	// reset, leaving the client permanently stuck in a "running" state and
	// leaking the activeQueries counter (which gates future requests).
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("%s: panic in query goroutine chat_id=%s: %v", logTag, chatID, r)
			}
			ws.mutex.Lock()
			if ws.activeQueries > 0 {
				ws.activeQueries--
			}
			if ctx := ws.clientContexts[clientID]; ctx != nil {
				ctx.setChatQueryActive(chatID, false, "")
			}
			ws.mutex.Unlock()
		}()
		startedAt := time.Now()

		// Slash-command dispatch (SP-114 Phase 2). The safety gate was
		// already validated synchronously above (slashCmdSafe). Here we
		// only enter this branch for commands that passed that check.
		registry := agent_commands.NewCommandRegistry()
		if slashCmdSafe {
			queryEventData := events.QueryStartedEvent(
				query,
				clientAgent.GetProvider(),
				clientAgent.GetModel(),
			)
			ws.publishClientEventWithChat(clientID, chatID, events.EventTypeQueryStarted, queryEventData)

			clientAgent.SetWorkspaceRoot(workspaceRoot)

			// Capture stdout while the command runs: slash commands write to
			// os.Stdout (fmt.Printf/fmt.Println), but in daemon mode that goes
			// nowhere the browser can see. Redirect stdout to a pipe so we can
			// forward the real command output as stream chunks.
			//
			// The mutex serializes capture across concurrent chats — os.Stdout
			// is process-global, so two simultaneous redirects would race.
			trimmed := strings.TrimSpace(query)
			ws.stdoutCaptureMu.Lock()
			oldStdout := os.Stdout
			pipeR, pipeW, pipeErr := os.Pipe()
			if pipeErr != nil {
				log.Printf("%s: stdout pipe creation failed: %v", logTag, pipeErr)
				ws.stdoutCaptureMu.Unlock()
			} else {
				os.Stdout = pipeW
			}

			err := registry.Execute(query, clientAgent)

			// Drain the pipe. Close the write end BEFORE reading so io.Copy
			// sees EOF and returns — otherwise it blocks forever (classic
			// pipe deadlock). See SC-2.
			var capturedOutput string
			if pipeErr == nil {
				pipeW.Close()
				os.Stdout = oldStdout
				var buf bytes.Buffer
				if _, copyErr := io.Copy(&buf, pipeR); copyErr != nil {
					log.Printf("%s: stdout pipe read failed: %v", logTag, copyErr)
				}
				pipeR.Close()
				ws.stdoutCaptureMu.Unlock()
				capturedOutput = buf.String()
			}

			// Sync state asynchronously so the query goroutine can proceed
			// to publish events without waiting for the state export.
			go func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("%s: panic in slash-command state sync chat_id=%s: %v", logTag, chatID, r)
					}
				}()
				if err := ws.syncAgentStateForClientWithChat(clientID, chatID); err != nil {
					log.Printf("%s: async state sync failed chat_id=%s: %v", logTag, chatID, err)
				}
			}() // Send any captured output as a stream chunk before reporting
			// success or error, so the user sees what the command printed.
			if capturedOutput != "" {
				ws.publishClientEventWithChat(clientID, chatID, events.EventTypeStreamChunk, events.StreamChunkEvent(
					fmt.Sprintf("\n%s\n\n%s", trimmed, capturedOutput),
					"assistant_text",
				))
			}

			if err != nil {
				log.Printf("%s: slash command error: %v", logTag, err)
				if capturedOutput == "" {
					ws.publishClientEventWithChat(clientID, chatID, events.EventTypeStreamChunk, events.StreamChunkEvent(
						fmt.Sprintf("Executed command: `%s`\n", trimmed),
						"assistant_text",
					))
				}
				ws.publishClientEventWithChat(clientID, chatID, events.EventTypeError, events.ErrorEvent("Slash command failed", err))
				return
			}

			if capturedOutput == "" {
				ws.publishClientEventWithChat(clientID, chatID, events.EventTypeStreamChunk, events.StreamChunkEvent(
					fmt.Sprintf("Executed command: `%s`\n", trimmed),
					"assistant_text",
				))
			}
			queryCompletedData := events.QueryCompletedEvent(
				query,
				fmt.Sprintf("Executed command: %s", trimmed),
				0,
				0,
				time.Since(startedAt),
			)
			ws.publishClientEventWithChat(clientID, chatID, events.EventTypeQueryCompleted, queryCompletedData)
			return
		}

		log.Printf("%s: calling ProcessQueryWithContinuity chat_id=%s provider=%s model=%s", logTag, chatID, clientAgent.GetProvider(), clientAgent.GetModel())
		queryStart := time.Now()
		clientAgent.SetWorkspaceRoot(workspaceRoot)
		_, err := clientAgent.ProcessQueryWithContinuity(query)
		queryDuration := time.Since(queryStart)

		// Record cost after query completes
		chargedCost := clientAgent.GetChargedCostTotal()
		tokenCost := clientAgent.GetTokenCostTotal()
		if chargedCost > 0 || tokenCost > 0 {
			providerName := clientAgent.GetProvider()
			GetCostStore().RecordCostWithBilling(
				providerName,
				clientAgent.GetModel(),
				clientAgent.GetSessionID(),
				chatID,
				clientAgent.GetSessionName(),
				clientAgent.GetWorkspaceRoot(),
				resolveBillingTypeForProvider(providerName),
				clientAgent.GetPromptTokens(),
				clientAgent.GetCompletionTokens(),
				chargedCost,
				tokenCost,
			)
		}

		// Sync state asynchronously so the query goroutine returns
		// immediately. ExportState can take seconds for large conversations,
		// and the deferred active-query cleanup must not wait for it.
		go func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("%s: panic in state sync chat_id=%s: %v", logTag, chatID, r)
				}
			}()
			if err := ws.syncAgentStateForClientWithChat(clientID, chatID); err != nil {
				log.Printf("%s: async state sync failed chat_id=%s: %v", logTag, chatID, err)
			}
		}()

		if err != nil {
			log.Printf("%s: ProcessQueryWithContinuity error chat_id=%s duration=%s err=%v", logTag, chatID, queryDuration, err)
			ws.publishClientEventWithChat(clientID, chatID, events.EventTypeError, events.ErrorEvent("Query failed", err))
		} else {
			// Success-path log: lets operators see that the provider responded
			// and at what cost. Without this the log goes silent after the
			// "calling ProcessQueryWithContinuity" line and the server looks hung.
			log.Printf("%s: completed chat_id=%s duration=%s prompt_tokens=%d completion_tokens=%d total_cost=%.6f",
				logTag, chatID, queryDuration,
				clientAgent.GetPromptTokens(), clientAgent.GetCompletionTokens(),
				clientAgent.GetTotalCost())
		}
	}()

	resp := map[string]interface{}{
		"accepted":  true,
		"chat_id":   chatID,
		"timestamp": time.Now().Unix(),
	}
	// Preserve the per-endpoint wire contract: /api/query echoes the
	// submitted query text; /api/proxy/chat does not.
	if opts.EchoQueryInAccept {
		resp["query"] = query
	}
	writeJSON(w, http.StatusAccepted, resp)
}