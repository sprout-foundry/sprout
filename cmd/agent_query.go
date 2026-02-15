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
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/console"
	"github.com/alantheprice/ledit/pkg/events"
	"github.com/alantheprice/ledit/pkg/security_validator"
	"github.com/alantheprice/ledit/pkg/utils"
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
		fmt.Printf("%s❌ Error:%s %v\n",
			console.ColorRed,
			console.ColorReset,
			err,
		)
		// Command execution failed - ask user if they want to send to LLM instead
		fmt.Print("The command failed. Send this query to the AI assistant instead? [Y/n] ")

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

// tryDirectExecution attempts to execute simple commands directly using local LLM
// Returns true if command was executed directly, false if normal agent flow should proceed
func TryDirectExecution(ctx context.Context, chatAgent *agent.Agent, query string) (bool, error) {
	// Get the tool registry (has security validator with local LLM access)
	registry := agent.GetToolRegistry()
	if registry == nil {
		return false, nil
	}
	validator := registry.GetValidator()
	if validator == nil {
		// Try to initialize the validator
		agentConfig := chatAgent.GetConfig()
		if agentConfig == nil {
			return false, nil
		}

		// If SecurityValidation is not configured, create a default one
		var securityConfig *configuration.SecurityValidationConfig
		if agentConfig.SecurityValidation == nil {
			securityConfig = &configuration.SecurityValidationConfig{
				Enabled:        true,
				Model:          "qwen2.5-coder:1.5b",
				Threshold:      1,
				TimeoutSeconds: 10,
			}
		} else {
			securityConfig = agentConfig.SecurityValidation
		}

		if !securityConfig.Enabled {
			return false, nil
		}

		// Create a logger
		isNonInteractive := agentConfig.SkipPrompt
		logger := utils.GetLogger(isNonInteractive)

		// Create the validator
		newValidator, err := security_validator.NewValidator(securityConfig, logger, !isNonInteractive)
		if err != nil {
			return false, nil // Failed to create validator, proceed with normal flow
		}
		validator = newValidator
	}

	// Use the LLM to detect if this is a direct command request
	isDirect, detectedCommand, confidence, err := validator.DetectDirectCommand(ctx, query)
	if err != nil {
		// If LLM fails, just proceed with normal agent flow
		return false, nil
	}

	// Only execute directly if high confidence (>0.8)
	if isDirect && confidence > 0.8 && detectedCommand != "" {
		fmt.Printf("%s⚡ Fast path:%s %s\n",
			console.ColorizeBold("⚡ Fast path:", console.ColorYellow),
			console.ColorReset,
			detectedCommand,
		)

		// Print separator before streaming output
		separatorWidth := GetTerminalWidth()
		separator := strings.Repeat("─", separatorWidth)
		fmt.Printf("%s%s%s\n",
			console.ColorGray,
			separator,
			console.ColorReset,
		)

		// Execute the command directly using bash (output streams in real-time)
		_, err := ExecuteCommand(detectedCommand)

		// Print separator after output
		fmt.Printf("%s%s%s\n",
			console.ColorGray,
			separator,
			console.ColorReset,
		)

		if err != nil {
			fmt.Printf("%s❌ Error:%s %v\n",
				console.ColorRed,
				console.ColorReset,
				err,
			)
		}
		return true, nil
	}

	return false, nil
}

// ProcessQuery processes a single query
func ProcessQuery(ctx context.Context, chatAgent *agent.Agent, eventBus *events.EventBus, query string) error {
	// Check if this is a slash command
	registry := agent_commands.NewCommandRegistry()
	if registry.IsSlashCommand(query) {
		if err := registry.Execute(query, chatAgent); err != nil {
			// For slash commands, show error and exit immediately
			fmt.Fprintf(os.Stderr, "❌ Slash command error: %v\n", err)
			fmt.Fprintf(os.Stderr, "💡 Use '/help' to see available commands\n")
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

	// Process the query using CI output handler for clean output
	outputHandler := console.NewCIOutputHandler(os.Stdout)

	// Set up progress callback
	chatAgent.SetStatsUpdateCallback(func(totalTokens int, totalCost float64) {
		// Update the CI output handler metrics
		outputHandler.UpdateMetrics(
			totalTokens,
			chatAgent.GetCurrentContextTokens(),
			chatAgent.GetMaxContextTokens(),
			chatAgent.GetCurrentIteration(),
			totalCost,
		)

		// Print progress if in CI mode
		if outputHandler.ShouldShowProgress() {
			outputHandler.PrintProgress()
		}

		// Also publish to event bus for web UI
		eventBus.Publish(events.EventTypeMetricsUpdate, events.MetricsUpdateEvent(
			totalTokens,
			chatAgent.GetCurrentContextTokens(),
			chatAgent.GetMaxContextTokens(),
			chatAgent.GetCurrentIteration(),
			totalCost,
		))
	})

	// Replace streaming callback for direct output (unless disabled)
	if !agentNoStreaming {
		chatAgent.EnableStreaming(func(chunk string) {
			outputHandler.Write([]byte(chunk))
			eventBus.Publish(events.EventTypeStreamChunk, events.StreamChunkEvent(chunk))
		})
		defer chatAgent.DisableStreaming()
	}

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

		// Print completion message without automatic summary (use /stats to see summary)
		fmt.Printf("✅ Completed in %s\n", FormatDuration(duration))

		return nil

	case <-ctx.Done():
		// Context was cancelled - agent processing was interrupted
		duration := time.Since(startTime)
		fmt.Printf("\n⏹️ Query interrupted after %s\n", FormatDuration(duration))

		eventBus.Publish(events.EventTypeError, events.ErrorEvent(
			fmt.Sprintf("Query interrupted: %s", query), ctx.Err(),
		))
		return fmt.Errorf("query interrupted: %w", ctx.Err())
	}
}
