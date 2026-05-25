package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/history"
)

func getChangeTrackingStatus(chatAgent *agent.Agent) string {
	if chatAgent == nil {
		return console.GlyphError.Prefix() + "Disabled"
	}
	if chatAgent.IsChangeTrackingEnabled() {
		return console.GlyphSuccess.Prefix() + "Enabled"
	}
	if chatAgent.GetChangeTracker() == nil {
		return console.GlyphInfo.Prefix() + "Idle (no tracked session yet)"
	}
	return console.GlyphError.Prefix() + "Disabled"
}

// ChangesCommand shows tracked file changes in the current session
type ChangesCommand struct{}

// Name returns the command name
func (c *ChangesCommand) Name() string {
	return "changes"
}

// Description returns the command description
func (c *ChangesCommand) Description() string {
	return "Show file changes tracked in the current session"
}

// Execute shows the tracked changes for this session
func (c *ChangesCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		// Gracefully handle nil agent in tests or non-interactive contexts
		console.GlyphInfo.Fprintf(os.Stdout, "No active tracked session")
		return nil
	}
	if !chatAgent.IsChangeTrackingEnabled() {
		if chatAgent.GetChangeTracker() == nil {
			console.GlyphInfo.Fprintf(os.Stdout, "No tracked session has started yet")
		} else {
			console.GlyphInfo.Fprintf(os.Stdout, "Change tracking is disabled for this session")
		}
		return nil
	}

	changeCount := chatAgent.GetChangeCount()
	if changeCount == 0 {
		console.GlyphInfo.Fprintf(os.Stdout, "No file changes have been tracked in this session yet")
		return nil
	}

	console.GlyphInfo.Fprintf(os.Stdout, "Session Changes (Revision: %s)", chatAgent.GetRevisionID())

	summary := chatAgent.GetChangesSummary()
	fmt.Print(summary + "\r\n")

	return nil
}

// StatusCommand shows the current change tracking status
type StatusCommand struct{}

// Name returns the command name
func (s *StatusCommand) Name() string {
	return "status"
}

// Description returns the command description
func (s *StatusCommand) Description() string {
	return "Show session status, provider, model, token usage, and files modified"
}

// Execute shows the current status
func (s *StatusCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if chatAgent == nil {
		console.GlyphInfo.Fprintln(os.Stdout, "Agent Session Status")
		fmt.Printf("Change Tracking: %s\n", getChangeTrackingStatus(chatAgent))
		return nil
	}

	console.GlyphInfo.Fprintln(os.Stdout, "Agent Session Status")

	// Provider and Model (critical)
	fmt.Printf("Provider: %s\n", chatAgent.GetProvider())
	fmt.Printf("Model: %s\n", chatAgent.GetModel())
	fmt.Printf("Persona: %s\n", chatAgent.GetActivePersona())
	if tools.HasVisionCapability() {
		fmt.Printf("Vision Capability: %sAvailable\n", console.GlyphSuccess.Prefix())
	} else {
		fmt.Printf("Vision Capability: %sNot configured\n", console.GlyphError.Prefix())
	}

	toolNames := chatAgent.GetAvailableToolNames()
	if len(toolNames) == 0 {
		fmt.Println("Available Tools: none")
	} else {
		fmt.Printf("Available Tools (%d): %s\n", len(toolNames), strings.Join(toolNames, ", "))
	}

	lastToolNames := chatAgent.GetLastPreparedToolNames()
	if len(lastToolNames) == 0 {
		fmt.Println("Last Request Tools: none yet")
	} else {
		fmt.Printf("Last Request Tools (%d): %s\n", len(lastToolNames), strings.Join(lastToolNames, ", "))
	}

	// Token usage
	fmt.Println()
	console.GlyphInfo.Print("Token Usage:")
	fmt.Printf("  Prompt Tokens: %d\n", chatAgent.GetPromptTokens())
	fmt.Printf("  Completion Tokens: %d\n", chatAgent.GetCompletionTokens())
	fmt.Printf("  Total Tokens: %d\n", chatAgent.GetTotalTokens())
	fmt.Printf("  Cached Tokens: %d\n", chatAgent.GetCachedTokens())

	// Cost
	cost := chatAgent.GetTotalCost()
	fmt.Printf("\n$ Cost: $%.6f\n", cost)

	// Change tracking and files
	fmt.Println()
	console.GlyphInfo.Print("Changes:")
	if chatAgent.IsChangeTrackingEnabled() {
		fmt.Printf("Tracking: %s\n", getChangeTrackingStatus(chatAgent))
		fmt.Printf("Revision: %s\n", chatAgent.GetRevisionID())
		fmt.Printf("Files Modified: %d\n", chatAgent.GetChangeCount())

		files := chatAgent.GetTrackedFiles()
		if len(files) > 0 {
			fmt.Println("\nModified Files:")
			for _, file := range files {
				fmt.Printf("  • %s\n", file)
			}
		}
	} else {
		fmt.Printf("Tracking: %s\n", getChangeTrackingStatus(chatAgent))
	}

	// Session
	fmt.Println()
	console.GlyphInfo.Printf("Session: %s", chatAgent.GetSessionID())

	return nil
}

// LogCommand shows the change history using the history package
type LogCommand struct{}

// Name returns the command name
func (l *LogCommand) Name() string {
	return "log"
}

// Description returns the command description
func (l *LogCommand) Description() string {
	return "Show recent change history from all sessions"
}

// Execute shows the change log using enhanced flow
func (l *LogCommand) Execute(args []string, chatAgent *agent.Agent) error {
	// Use the enhanced log flow for better UX
	logFlow := NewLogFlow(chatAgent)
	return logFlow.Execute(args)
}

// RollbackCommand provides rollback functionality
type RollbackCommand struct{}

// Name returns the command name
func (r *RollbackCommand) Name() string {
	return "rollback"
}

// Description returns the command description
func (r *RollbackCommand) Description() string {
	return "Rollback changes by revision ID (use /log to see available revisions)"
}

// Execute performs a rollback
func (r *RollbackCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if len(args) == 0 {
		console.GlyphInfo.Fprintf(os.Stdout, "Available revisions for rollback:")
		fmt.Print("Use /log to see the revision history, then use /rollback <revision-id>\r\n")
		return nil
	}

	revisionID := args[0]
	console.GlyphDim.Fprintf(os.Stdout, "Attempting to rollback revision: %s", revisionID)

	err := history.RevertChangeByRevisionID(revisionID)
	if err != nil {
		return fmt.Errorf("rollback failed: %w", err)
	}

	console.GlyphSuccess.Fprintf(os.Stdout, "Successfully rolled back revision: %s", revisionID)
	console.GlyphInfo.Fprintf(os.Stdout, "Tip: Use /changes to see if there are new changes in this session")

	return nil
}
