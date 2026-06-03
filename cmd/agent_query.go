//go:build !js

// Agent query processing: handles query execution and detection
package cmd

import (
	"bufio"
	"context"
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

	// Build the display message with colors
	var displayMsg strings.Builder
	displayMsg.WriteString(fmt.Sprintf("\n%s[Detected %s command: %s%s]%s",
		console.ColorCyan,
		cmdInfo.Type,
		cmdInfo.Name,
		console.ColorReset,
		console.ColorReset,
	))
	switch cmdInfo.Type {
	case zsh.CommandTypeExternal:
		displayMsg.WriteString(fmt.Sprintf(" %s[%s%s]%s",
			console.ColorGray,
			cmdInfo.Path,
			console.ColorReset,
			console.ColorReset,
		))
	case zsh.CommandTypeAlias:
		displayMsg.WriteString(fmt.Sprintf(" %s[%s%s]%s",
			console.ColorGray,
			cmdInfo.Value,
			console.ColorReset,
			console.ColorReset,
		))
	}
	displayMsg.WriteString("\n")

	// Check if we should auto-execute (either '!' prefix or config setting)
	shouldAutoExecute := autoExecute || config.AutoExecuteDetectedCommands

	// Ask for confirmation (unless auto-execute)
	if !shouldAutoExecute {
		_, _ = os.Stdout.Write([]byte(displayMsg.String()))
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
	} else {
		// Auto-execute with color
		if autoExecute {
			_, _ = os.Stdout.Write([]byte(fmt.Sprintf("%s%s[Auto-executing with !]%s\n",
				displayMsg.String(),
				console.ColorizeBold("Auto-executing with !", console.ColorYellow),
				console.ColorReset,
			)))
		} else {
			_, _ = os.Stdout.Write([]byte(fmt.Sprintf("%s%s[Auto-executing]%s\n",
				displayMsg.String(),
				console.ColorizeBold("Auto-executing", console.ColorGreen),
				console.ColorReset,
			)))
		}
	}

	// Execute the command with color indicator
	_, _ = os.Stdout.Write([]byte(fmt.Sprintf("%s▶ Executing:%s %s\n",
		console.ColorizeBold("▶ Executing:", console.ColorBlue),
		console.ColorReset,
		query,
	)))

	// Print separator before streaming output
	separatorWidth := GetTerminalWidth()
	separator := strings.Repeat("─", separatorWidth)
	_, _ = os.Stdout.Write([]byte(fmt.Sprintf("%s%s%s\n",
		console.ColorGray,
		separator,
		console.ColorReset,
	)))

	_, err = ExecuteCommand(query)

	// Print separator after output
	_, _ = os.Stdout.Write([]byte(fmt.Sprintf("%s%s%s\n",
		console.ColorGray,
		separator,
		console.ColorReset,
	)))

	if err != nil {
		_, _ = os.Stdout.Write([]byte(fmt.Sprintf("%s[FAIL] Error:%s %v\n",
			console.ColorRed,
			console.ColorReset,
			err,
		)))
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
	_, _ = os.Stdout.Write([]byte(fmt.Sprintf("%s[!] Fast path:%s %s\n",
		console.ColorizeBold("[!] Fast path:", console.ColorYellow),
		console.ColorReset,
		command,
	)))

	// Print separator before streaming output
	separatorWidth := GetTerminalWidth()
	separator := strings.Repeat("─", separatorWidth)
	_, _ = os.Stdout.Write([]byte(fmt.Sprintf("%s%s%s\n",
		console.ColorGray,
		separator,
		console.ColorReset,
	)))

	// Execute the command directly (output streams in real-time)
	_, err := ExecuteCommand(command)

	// Print separator after output
	_, _ = os.Stdout.Write([]byte(fmt.Sprintf("%s%s%s\n",
		console.ColorGray,
		separator,
		console.ColorReset,
	)))

	if err != nil {
		_, _ = os.Stdout.Write([]byte(fmt.Sprintf("%s[FAIL] Error:%s %v\n",
			console.ColorRed,
			console.ColorReset,
			err,
		)))
	}
	return true, nil
}

// ProcessQuery processes a single query
func ProcessQuery(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus, query string) error {
	setQueryInProgress(true)
	defer setQueryInProgress(false)

	// Check if this is a slash command
	registry := agent_commands.NewCommandRegistry()
	if registry.IsSlashCommand(query) {
		if err := registry.Execute(query, chatAgent); err != nil {
			// For slash commands, show error and exit immediately
			_, _ = os.Stderr.Write([]byte(fmt.Sprintf("[FAIL] Slash command error: %v\n", err)))
			_, _ = os.Stderr.Write([]byte("[i] Use '/help' to see available commands\n"))
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
		response, err := chatAgent.ProcessQueryWithContinuity(query)
		resultCh <- result{response, err}
	}()

	// Wait for either completion or cancellation
	select {
	case res := <-resultCh:
		duration := time.Since(startTime)

		if res.err != nil {
			// Print the response (user-friendly error message) if available
			if res.response != "" {
				_, _ = os.Stderr.Write([]byte(fmt.Sprintf("[FAIL] %s\n", res.response)))
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
		_, _ = os.Stdout.Write([]byte(fmt.Sprintf("\n[STOP] Query interrupted after %s\n", FormatDuration(duration))))

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
