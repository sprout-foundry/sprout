// Agent query processing: handles query execution and detection
package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	agent_commands "github.com/alantheprice/ledit/pkg/agent_commands"
	"github.com/alantheprice/ledit/pkg/console"
	"github.com/alantheprice/ledit/pkg/events"
	"github.com/alantheprice/ledit/pkg/zsh"
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
		fmt.Print(displayMsg.String())
		fmt.Print("Execute directly? [Y/n] ")

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
			fmt.Printf("%s%s[Auto-executing with !]%s\n",
				displayMsg.String(),
				console.ColorizeBold("Auto-executing with !", console.ColorYellow),
				console.ColorReset,
			)
		} else {
			fmt.Printf("%s%s[Auto-executing]%s\n",
				displayMsg.String(),
				console.ColorizeBold("Auto-executing", console.ColorGreen),
				console.ColorReset,
			)
		}
	}

	// Execute the command with color indicator
	fmt.Printf("%s▶ Executing:%s %s\n",
		console.ColorizeBold("▶ Executing:", console.ColorBlue),
		console.ColorReset,
		query,
	)

	// Print separator before streaming output
	separatorWidth := GetTerminalWidth()
	separator := strings.Repeat("─", separatorWidth)
	fmt.Printf("%s%s%s\n",
		console.ColorGray,
		separator,
		console.ColorReset,
	)

	_, err = ExecuteCommand(query)

	// Print separator after output
	fmt.Printf("%s%s%s\n",
		console.ColorGray,
		separator,
		console.ColorReset,
	)

	if err != nil {
		fmt.Printf("%s[FAIL] Error:%s %v\n",
			console.ColorRed,
			console.ColorReset,
			err,
		)
		// Command execution failed - ask user if they want to send to LLM instead
		fmt.Print("The command failed. Send this query to the Assistant instead? [Y/n] ")

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

// TryDirectExecution attempts to execute simple commands directly using static pattern matching.
// Returns true if command was executed directly, false if normal agent flow should proceed.
func TryDirectExecution(ctx context.Context, chatAgent *agent.Agent, query string) (bool, error) {
	// Trim and check for empty
	query = strings.TrimSpace(query)
	if query == "" {
		return false, nil
	}

	// Static list of commands that can be executed directly without LLM involvement.
	// These are safe, informational commands that an LLM is not needed for.
	directCommands := map[string]string{
		"pwd":          "pwd",
		"ls":           "ls -la",
		"ll":           "ls -la",
		"la":           "ls -la",
		"date":         "date",
		"whoami":       "whoami",
		"id":           "id",
		"uname":        "uname -a",
		"hostname":     "hostname",
		"uptime":       "uptime",
		"git status":   "git status",
		"git st":       "git status",
		"git log":      "git log --oneline -20",
		"git branch":   "git branch",
		"git diff":     "git diff",
		"git remote":   "git remote -v",
		"git stash":    "git stash list",
		"git tag":      "git tag",
		"free":         "free -h",
		"df":           "df -h",
		"du":           "du -sh .",
		"ps":           "ps aux",
		"env":          "env",
		"which":        "", // Requires additional argument matching below
		"whereis":      "", // Requires additional argument matching below
	}

	// Check for exact match first
	if cmd, ok := directCommands[query]; ok && cmd != "" {
		return executeDirectCommand(cmd)
	}

	// Check for commands that take a single argument
	for prefix, cmd := range directCommands {
		if cmd == "" && strings.HasPrefix(query, prefix+" ") {
			return executeDirectCommand(query)
		}
	}

	// Also check "how to see X" patterns — e.g., "how to see current directory" → pwd
	queryLower := strings.ToLower(query)
	naturalLanguageMap := []struct {
		pattern string
		cmd     string
	}{
		{"current directory", "pwd"},
		{"current dir", "pwd"},
		{"working directory", "pwd"},
		{"what's the date", "date"},
		{"what time", "date"},
		{"who am i", "whoami"},
		{"what user", "whoami"},
		{"disk space", "df -h"},
		{"disk usage", "du -sh ."},
		{"memory", "free -h"},
		{"ram", "free -h"},
		{"git status", "git status"},
		{"git log", "git log --oneline -20"},
		{"show me the files", "ls -la"},
		{"list files", "ls -la"},
	}
	for _, entry := range naturalLanguageMap {
		if strings.Contains(queryLower, entry.pattern) && len(query) < 60 {
			return executeDirectCommand(entry.cmd)
		}
	}

	return false, nil
}

// executeDirectCommand executes a command directly and prints output
func executeDirectCommand(command string) (bool, error) {
	fmt.Printf("%s[!] Fast path:%s %s\n",
		console.ColorizeBold("[!] Fast path:", console.ColorYellow),
		console.ColorReset,
		command,
	)

	// Print separator before streaming output
	separatorWidth := GetTerminalWidth()
	separator := strings.Repeat("─", separatorWidth)
	fmt.Printf("%s%s%s\n",
		console.ColorGray,
		separator,
		console.ColorReset,
	)

	// Execute the command directly (output streams in real-time)
	_, err := ExecuteCommand(command)

	// Print separator after output
	fmt.Printf("%s%s%s\n",
		console.ColorGray,
		separator,
		console.ColorReset,
	)

	if err != nil {
		fmt.Printf("%s[FAIL] Error:%s %v\n",
			console.ColorRed,
			console.ColorReset,
			err,
		)
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
			fmt.Fprintf(os.Stderr, "[FAIL] Slash command error: %v\n", err)
			fmt.Fprintf(os.Stderr, "[i] Use '/help' to see available commands\n")
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

	// Process the query
	// Note: streaming callback is already set by SetupAgentEvents (called once at startup).
	// The OutputRouter's RouteStreamChunk handles both event publishing and terminal output.
	// StatsUpdateCallback is set once; subsequent calls overwrite which is fine.
	chatAgent.SetStatsUpdateCallback(func(totalTokens int, totalCost float64) {
		// Publish metrics to event bus for WebUI
		eventBus.Publish(events.EventTypeMetricsUpdate, events.MetricsUpdateEvent(
			totalTokens,
			chatAgent.GetCurrentContextTokens(),
			chatAgent.GetMaxContextTokens(),
			chatAgent.GetCurrentIteration(),
			totalCost,
		))
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
			eventBus.Publish(events.EventTypeError, events.ErrorEvent(
				fmt.Sprintf("Failed to process query: %s", query), res.err,
			))
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
		eventBus.Publish(events.EventTypeQueryCompleted, completedEvent)

		switch chatAgent.GetLastRunTerminationReason() {
		case agent.RunTerminationMaxIterations:
			fmt.Printf("\n[WARN] Reached max iterations (%d) in %s\n", chatAgent.GetMaxIterations(), FormatDuration(duration))
		case agent.RunTerminationInterrupted:
			fmt.Printf("\n[STOP] Stopped in %s\n", FormatDuration(duration))
		default:
			// Print completion message without automatic summary (use /stats to see summary)
			fmt.Printf("\n[OK] Completed in %s\n", FormatDuration(duration))
		}

		return nil

	case <-ctx.Done():
		// Context was cancelled - agent processing was interrupted
		chatAgent.TriggerInterrupt()
		duration := time.Since(startTime)
		fmt.Printf("\n[STOP] Query interrupted after %s\n", FormatDuration(duration))

		// Allow the agent goroutine to stop cleanly after receiving interrupt.
		select {
		case <-resultCh:
		case <-time.After(3 * time.Second):
		}

		eventBus.Publish(events.EventTypeError, events.ErrorEvent(
			fmt.Sprintf("Query interrupted: %s", query), ctx.Err(),
		))
		return fmt.Errorf("query interrupted: %w", ctx.Err())
	}
}
