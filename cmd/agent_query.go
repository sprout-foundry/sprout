//go:build !js

// Agent query processing: handles query execution and detection
package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	agent_commands "github.com/sprout-foundry/sprout/pkg/agent_commands"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/zsh"
)

// tryZshCommandExecution attempts to detect and execute zsh commands directly
// Returns true if command was executed, false if normal flow should proceed
func TryZshCommandExecution(ctx context.Context, chatAgent *agent.Agent, query string) (bool, error) {
	// Check if zsh command detection is enabled
	config := chatAgent.GetConfig()
	if config == nil || !config.EnableZshCommandDetection {
		return false, nil
	}

	// Check if this starts with '!' for auto-execution (skip confirmation)
	autoExecute := strings.HasPrefix(query, "!")
	if autoExecute {
		query = strings.TrimPrefix(query, "!")
		query = strings.TrimSpace(query)
	}

	// Check if this is a zsh command
	isCommand, cmdInfo, err := zsh.IsCommand(query)
	if err != nil {
		// If there's an error checking, just proceed with normal flow
		return false, nil
	}
	if !isCommand || cmdInfo == nil {
		return false, nil
	}

	// Check if we should auto-execute (either '!' prefix or config setting)
	shouldAutoExecute := autoExecute || config.AutoExecuteDetectedCommands

	// Build a one-line detect message with the path/value appended inline.
	switch cmdInfo.Type {
	case zsh.CommandTypeExternal:
		console.GlyphInfo.Fprintf(os.Stdout, "Detected %s: %s (%s)", cmdInfo.Type, cmdInfo.Name, cmdInfo.Path)
	case zsh.CommandTypeAlias:
		console.GlyphInfo.Fprintf(os.Stdout, "Detected %s: %s (%s)", cmdInfo.Type, cmdInfo.Name, cmdInfo.Value)
	default:
		console.GlyphInfo.Fprintf(os.Stdout, "Detected %s: %s", cmdInfo.Type, cmdInfo.Name)
	}

	// Ask for confirmation (unless auto-execute)
	if !shouldAutoExecute {
		_, _ = os.Stdout.Write([]byte("Execute directly? " + console.FormatYesNoPromptStdout(true) + " "))

		// Read response
		reader := bufio.NewReader(os.Stdin)
		response, err := reader.ReadString('\n')
		if err != nil {
			return false, fmt.Errorf("failed to read response: %w", err)
		}
		response = strings.TrimSpace(strings.ToLower(response))

		// Default to yes if empty, otherwise check for y/yes
		if response != "" && response != "y" && response != "yes" {
			// User declined, proceed with normal agent flow
			return false, nil
		}
	}

	// Execute the command with a glyph-led action line.
	console.GlyphAction.Fprintf(os.Stdout, "Executing: %s", query)

	_, err = ExecuteCommand(query)

	if err != nil {
		console.GlyphError.Fprintf(os.Stdout, "Error: %v", err)
		// Command execution failed - ask user if they want to send to LLM instead
		_, _ = os.Stdout.Write([]byte("The command failed. Send this query to the Assistant instead? " + console.FormatYesNoPromptStdout(true) + " "))

		// Read response
		reader := bufio.NewReader(os.Stdin)
		response, err := reader.ReadString('\n')
		if err != nil {
			// If we can't read response, just return true (we attempted)
			return true, nil
		}
		response = strings.TrimSpace(strings.ToLower(response))

		// Default to yes (send to LLM) unless user explicitly says no
		if response == "n" || response == "no" {
			// User declined, return true since we attempted execution
			return true, nil
		}

		// User wants to send to LLM, return false to proceed with normal agent flow
		return false, nil
	}

	return true, nil
}

// directCommands maps a user-typed shorthand to the actual shell command
// the REPL runs without involving the LLM. Entries with an empty value
// (e.g. "which", "whereis") accept a single-arg form matched as a
// prefix at the call site. Pulled out of TryDirectExecution so the
// steer/queue submit handlers can mirror the same membership test.
var directCommands = map[string]string{
	"pwd":        "pwd",
	"ls":         "ls -la",
	"ll":         "ls -la",
	"la":         "ls -la",
	"date":       "date",
	"whoami":     "whoami",
	"id":         "id",
	"uname":      "uname -a",
	"hostname":   "hostname",
	"uptime":     "uptime",
	"git status": "git status",
	"git st":     "git status",
	"git log":    "git log --oneline -20",
	"git branch": "git branch",
	"git diff":   "git diff",
	"git remote": "git remote -v",
	"git stash":  "git stash list",
	"git tag":    "git tag",
	"free":       "free -h",
	"df":         "df -h",
	"du":         "du -sh .",
	"ps":         "ps aux",
	"env":        "env",
	"which":      "", // Requires additional argument matching below
	"whereis":    "", // Requires additional argument matching below
}

// isDirectFastPathCommand reports whether query would be intercepted by
// TryDirectExecution at the main prompt — either an exact match against
// the directCommands table or one of the single-arg forms (which/whereis).
// Used by the steer/queue submit handlers to reject command-class text
// instead of silently injecting it into the active turn.
func isDirectFastPathCommand(query string) bool {
	query = strings.TrimSpace(query)
	if query == "" {
		return false
	}
	if cmd, ok := directCommands[query]; ok && cmd != "" {
		return true
	}
	for prefix, cmd := range directCommands {
		if cmd == "" && strings.HasPrefix(query, prefix+" ") {
			return true
		}
	}
	return false
}

// TryDirectExecution attempts to execute simple commands directly using static pattern matching.
// Returns true if command was executed directly, false if normal agent flow should proceed.
func TryDirectExecution(ctx context.Context, chatAgent *agent.Agent, query string) (bool, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return false, nil
	}

	if cmd, ok := directCommands[query]; ok && cmd != "" {
		return executeDirectCommand(cmd)
	}

	for prefix, cmd := range directCommands {
		if cmd == "" && strings.HasPrefix(query, prefix+" ") {
			return executeDirectCommand(query)
		}
	}

	return false, nil
}

// executeDirectCommand executes a command directly and prints output
func executeDirectCommand(command string) (bool, error) {
	console.GlyphAction.Fprintf(os.Stdout, "Fast path: %s", command)

	// Execute the command directly (output streams in real-time)
	_, err := ExecuteCommand(command)

	if err != nil {
		console.GlyphError.Fprintf(os.Stdout, "Error: %v", err)
	}
	return true, nil
}

// ProcessQuery processes a single query
func ProcessQuery(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus, query string) error {
	setQueryInProgress(true)
	defer setQueryInProgress(false)

	// New turn: allow the "output is in the Web UI" handoff line to print once
	// if a browser is connected for this query.
	resetWebUIHandoff()

	// Check if this is a slash command
	registry := agent_commands.NewCommandRegistry()
	if registry.IsSlashCommand(query) {
		if err := registry.Execute(query, chatAgent); err != nil {
			// For slash commands, show error and exit immediately
			console.GlyphError.Fprintf(os.Stderr, "Slash command error: %v", err)
			console.GlyphInfo.Fprintf(os.Stderr, "Use '/help' to see available commands")
			return fmt.Errorf("slash command failed: %w", err)
		}
		return nil
	}

	// Publish query started event
	startedEvent := events.QueryStartedEvent(
		query,
		chatAgent.GetProvider(),
		chatAgent.GetModel(),
	)
	// Decorate with agent metadata for event routing
	if clientID := chatAgent.GetEventClientID(); clientID != "" {
		startedEvent["client_id"] = clientID
	}
	if chatID := chatAgent.GetEventChatID(); chatID != "" {
		startedEvent["chat_id"] = chatID
	}
	eventBus.Publish(events.EventTypeQueryStarted, startedEvent)

	startTime := time.Now()

	// Process the query
	// Note: streaming callback is already set by SetupAgentEvents (called once at startup).
	// The OutputRouter's RouteStreamChunk handles both event publishing and terminal output.
	// StatsUpdateCallback is set once; subsequent calls overwrite which is fine.
	chatAgent.SetStatsUpdateCallback(func(totalTokens int, totalCost float64) {
		// Publish metrics to event bus for WebUI
		metricsEvent := events.MetricsUpdateEvent(
			totalTokens,
			chatAgent.GetCurrentContextTokens(),
			chatAgent.GetMaxContextTokens(),
			chatAgent.GetCurrentIteration(),
			totalCost,
		)
		// Decorate with agent metadata for event routing
		if clientID := chatAgent.GetEventClientID(); clientID != "" {
			metricsEvent["client_id"] = clientID
		}
		if chatID := chatAgent.GetEventChatID(); chatID != "" {
			metricsEvent["chat_id"] = chatID
		}
		eventBus.Publish(events.EventTypeMetricsUpdate, metricsEvent)
	})

	// Run agent processing in a goroutine to support cancellation
	type result struct {
		response string
		err      error
	}

	resultCh := make(chan result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				resultCh <- result{response: "", err: fmt.Errorf("agent panic recovered: %v", r)}
			}
		}()
		response, err := chatAgent.ProcessQueryWithContinuity(query)
		resultCh <- result{response, err}
	}()

	// Wait for either completion or cancellation
	select {
	case res := <-resultCh:
		duration := time.Since(startTime)

		if res.err != nil {
			// If the WebUI is using the shared agent, show a friendly message
			// instead of the raw error. The query was not processed.
			if errors.Is(res.err, agent.ErrQueryInProgress) {
				console.GlyphWarning.Fprintf(os.Stderr, "The Web UI is currently processing a query. Try again in a moment.\n")
				return markReported(res.err)
			}
			// Print the response (user-friendly error message) if available.
			// When we show it here, mark the returned error as already-reported
			// so Execute() doesn't print the raw wrapped chain a second time.
			reported := false
			if res.response != "" {
				console.GlyphError.Fprintln(os.Stderr, res.response)
				reported = true
			}
			errorEvent := events.ErrorEvent(
				fmt.Sprintf("Failed to process query: %s", query), res.err,
			)
			// Decorate with agent metadata for event routing
			if clientID := chatAgent.GetEventClientID(); clientID != "" {
				errorEvent["client_id"] = clientID
			}
			if chatID := chatAgent.GetEventChatID(); chatID != "" {
				errorEvent["chat_id"] = chatID
			}
			eventBus.Publish(events.EventTypeError, errorEvent)
			if reported {
				return markReported(res.err)
			}
			return fmt.Errorf("agent processing failed: %w", res.err)
		}

		// Publish query completed event
		completedEvent := events.QueryCompletedEvent(
			query,
			res.response,
			chatAgent.GetCurrentContextTokens(),
			chatAgent.GetTotalCost(),
			duration,
		)
		if reason := chatAgent.GetLastRunTerminationReason(); reason != "" {
			completedEvent["status"] = reason
		}
		// Decorate with agent metadata for event routing
		if clientID := chatAgent.GetEventClientID(); clientID != "" {
			completedEvent["client_id"] = clientID
		}
		if chatID := chatAgent.GetEventChatID(); chatID != "" {
			completedEvent["chat_id"] = chatID
		}
		eventBus.Publish(events.EventTypeQueryCompleted, completedEvent)

		// In shared-agent mode, sync the agent state back to the WebUI so
		// the browser tab's conversation history stays current after CLI queries.
		if ws := getSharedWebServer(); ws != nil {
			_ = ws.SyncSharedAgentState(chatAgent)
		}

		switch chatAgent.GetLastRunTerminationReason() {
		case agent.RunTerminationMaxIterations:
			fmt.Println()
			console.GlyphWarning.Printf("Reached max iterations (%d) in %s", chatAgent.GetMaxIterations(), FormatDuration(duration))
		case agent.RunTerminationInterrupted:
			fmt.Println()
			console.GlyphStopped.Printf("Stopped in %s", FormatDuration(duration))
		default:
			// Print completion message without automatic summary (use /stats to see summary)
			fmt.Println()
			console.GlyphSuccess.Printf("Completed in %s", FormatDuration(duration))
		}

		return nil

	case <-ctx.Done():
		// Context was cancelled - agent processing was interrupted
		chatAgent.TriggerInterrupt()
		duration := time.Since(startTime)
		console.GlyphStopped.Fprintf(os.Stdout, "Query interrupted after %s", FormatDuration(duration))

		// Allow the agent goroutine to stop cleanly after receiving interrupt.
		select {
		case <-resultCh:
		case <-time.After(3 * time.Second):
		}

		errorEvent := events.ErrorEvent(
			fmt.Sprintf("Query interrupted: %s", query), ctx.Err(),
		)
		// Decorate with agent metadata for event routing
		if clientID := chatAgent.GetEventClientID(); clientID != "" {
			errorEvent["client_id"] = clientID
		}
		if chatID := chatAgent.GetEventChatID(); chatID != "" {
			errorEvent["chat_id"] = chatID
		}
		eventBus.Publish(events.EventTypeError, errorEvent)
		return fmt.Errorf("query interrupted: %w", ctx.Err())
	}
}
