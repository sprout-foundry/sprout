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
		fmt.Println("📝 Change tracking is not enabled for this session")
		return nil
	}

	changeCount := chatAgent.GetChangeCount()
	if changeCount == 0 {
		fmt.Println("📝 No file changes have been tracked in this session yet")
		return nil
	}

	fmt.Printf("📝 Session Changes (Revision: %s)\n", chatAgent.GetRevisionID())
	fmt.Println("=" + fmt.Sprintf("%*s", 50, "="))

	summary := chatAgent.GetChangesSummary()
	fmt.Println(summary)

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
	fmt.Println("📊 Agent Session Status")
	fmt.Println("=" + fmt.Sprintf("%*s", 25, "="))

	// Session info
	fmt.Printf("Session ID: %s\n", chatAgent.GetSessionID())
	fmt.Printf("Model: %s\n", chatAgent.GetModel())
	fmt.Printf("Provider: %s\n", chatAgent.GetProvider())

	// Change tracking info
	if chatAgent.IsChangeTrackingEnabled() {
		fmt.Printf("Change Tracking: ✅ Enabled\n")
		fmt.Printf("Revision ID: %s\n", chatAgent.GetRevisionID())
		fmt.Printf("Files Modified: %d\n", chatAgent.GetChangeCount())

		files := chatAgent.GetTrackedFiles()
		if len(files) > 0 {
			fmt.Println("\nModified Files:")
			for _, file := range files {
				fmt.Printf("  • %s\n", file)
			}
		}
	} else {
		fmt.Printf("Change Tracking: ❌ Disabled\n")
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

// Execute shows the change log
func (l *LogCommand) Execute(args []string, chatAgent *agent.Agent) error {
	fmt.Println("📜 Recent Change History")
	fmt.Println("=" + fmt.Sprintf("%*s", 25, "="))

	// Use the history package to print revision history
	err := history.PrintRevisionHistory()
	if err != nil {
		return fmt.Errorf("failed to show change history: %w", err)
	}

	return nil
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
		fmt.Println("📜 Available revisions for rollback:")
		fmt.Println("Use /log to see the revision history, then use /rollback <revision-id>")
		return nil
	}

	revisionID := args[0]
	fmt.Printf("🔄 Attempting to rollback revision: %s\n", revisionID)

	err := history.RevertChangeByRevisionID(revisionID)
	if err != nil {
		return fmt.Errorf("rollback failed: %w", err)
	}

	fmt.Printf("✅ Successfully rolled back revision: %s\n", revisionID)
	fmt.Println("💡 Tip: Use /changes to see if there are new changes in this session")

	return nil
}
