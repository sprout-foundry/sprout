package commands

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/history"
)

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
		fmt.Print("📝 Change tracking is not enabled for this session\r\n")
		return nil
	}
	if !chatAgent.IsChangeTrackingEnabled() {
		fmt.Print("📝 Change tracking is not enabled for this session\r\n")
		return nil
	}

	changeCount := chatAgent.GetChangeCount()
	if changeCount == 0 {
		fmt.Print("📝 No file changes have been tracked in this session yet\r\n")
		return nil
	}

	fmt.Printf("📝 Session Changes (Revision: %s)\r\n", chatAgent.GetRevisionID())
	fmt.Print("=" + fmt.Sprintf("%*s", 50, "=") + "\r\n")

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
		fmt.Print("📊 Agent Session Status\n")
		fmt.Println("========================")
		fmt.Println("Change Tracking: ❌ Disabled")
		return nil
	}

	fmt.Print("📊 Agent Session Status\n")
	fmt.Println("========================")

	// Provider and Model (critical)
	fmt.Printf("Provider: %s\n", chatAgent.GetProvider())
	fmt.Printf("Model: %s\n", chatAgent.GetModel())

	// Token usage
	fmt.Println("\n📈 Token Usage:")
	fmt.Printf("  Prompt Tokens: %d\n", chatAgent.GetPromptTokens())
	fmt.Printf("  Completion Tokens: %d\n", chatAgent.GetCompletionTokens())
	fmt.Printf("  Total Tokens: %d\n", chatAgent.GetTotalTokens())
	fmt.Printf("  Cached Tokens: %d\n", chatAgent.GetCachedTokens())

	// Cost
	cost := chatAgent.GetTotalCost()
	fmt.Printf("\n💰 Cost: $%.6f\n", cost)

	// Change tracking and files
	fmt.Println("\n📝 Changes:")
	if chatAgent.IsChangeTrackingEnabled() {
		fmt.Println("Tracking: ✅ Enabled")
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
		fmt.Println("Tracking: ❌ Disabled")
	}

	// Session
	fmt.Printf("\n🔖 Session: %s\n", chatAgent.GetSessionID())

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
		fmt.Print("📜 Available revisions for rollback:\r\n")
		fmt.Print("Use /log to see the revision history, then use /rollback <revision-id>\r\n")
		return nil
	}

	revisionID := args[0]
	fmt.Printf("🔄 Attempting to rollback revision: %s\r\n", revisionID)

	err := history.RevertChangeByRevisionID(revisionID)
	if err != nil {
		return fmt.Errorf("rollback failed: %w", err)
	}

	fmt.Printf("✅ Successfully rolled back revision: %s\r\n", revisionID)
	fmt.Print("💡 Tip: Use /changes to see if there are new changes in this session\r\n")

	return nil
}
