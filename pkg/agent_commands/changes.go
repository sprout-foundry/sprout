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
	return "Show change tracking status and session info"
}

// Execute shows the current status
func (s *StatusCommand) Execute(args []string, chatAgent *agent.Agent) error {
	fmt.Print("📊 Agent Session Status\r\n")
	fmt.Print("=" + fmt.Sprintf("%*s", 25, "=") + "\r\n")

	// Session info
	fmt.Printf("Session ID: %s\r\n", chatAgent.GetSessionID())
	fmt.Printf("Model: %s\r\n", chatAgent.GetModel())
	fmt.Printf("Provider: %s\r\n", chatAgent.GetProvider())

	// Change tracking info
	if chatAgent.IsChangeTrackingEnabled() {
		fmt.Print("Change Tracking: ✅ Enabled\r\n")
		fmt.Printf("Revision ID: %s\r\n", chatAgent.GetRevisionID())
		fmt.Printf("Files Modified: %d\r\n", chatAgent.GetChangeCount())

		files := chatAgent.GetTrackedFiles()
		if len(files) > 0 {
			fmt.Print("\r\nModified Files:\r\n")
			for _, file := range files {
				fmt.Printf("  • %s\r\n", file)
			}
		}
	} else {
		fmt.Print("Change Tracking: ❌ Disabled\r\n")
	}

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
