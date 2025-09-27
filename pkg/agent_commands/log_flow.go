package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/history"
	"github.com/alantheprice/ledit/pkg/ui"
	"golang.org/x/term"
)

// LogFlow manages enhanced log and history operations
type LogFlow struct {
	agent *agent.Agent
}

// NewLogFlow creates a new log flow
func NewLogFlow(chatAgent *agent.Agent) *LogFlow {
	return &LogFlow{
		agent: chatAgent,
	}
}

// LogAction represents different log management actions
type LogAction struct {
	ID          string
	DisplayName string
	Description string
	Action      func(*LogFlow) error
}

// LogActionItem adapts LogAction for dropdown display
type LogActionItem struct {
	ID          string
	DisplayName string
	Description string
}

func (l *LogActionItem) Display() string    { return l.DisplayName }
func (l *LogActionItem) SearchText() string { return l.DisplayName + " " + l.Description }
func (l *LogActionItem) Value() interface{} { return l.ID }

// RevisionItem adapts revision information for dropdown display
type RevisionItem struct {
	RevisionID  string
	Description string
	Timestamp   string
}

func (r *RevisionItem) Display() string { return fmt.Sprintf("%s - %s", r.RevisionID, r.Description) }
func (r *RevisionItem) SearchText() string {
	return r.RevisionID + " " + r.Description + " " + r.Timestamp
}
func (r *RevisionItem) Value() interface{} { return r.RevisionID }

// Execute runs the enhanced log flow
func (lf *LogFlow) Execute(args []string) error {
	// Check for terminal support
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		// Fallback to simple log display
		return lf.viewChangeLog()
	}

	// Show enhanced log options
	return lf.showLogOptions()
}

// showLogOptions displays the main log management options
func (lf *LogFlow) showLogOptions() error {
	// Get revision history to determine available actions
	revisions, err := lf.getAvailableRevisions()
	if err != nil {
		return fmt.Errorf("failed to get revision history: %w", err)
	}

	actions := lf.buildLogActions(revisions)

	if len(actions) == 0 {
		fmt.Printf("\r\n📭 No change history available.\r\n")
		return nil
	}

	// Check if we're in agent console - just show the log
	if os.Getenv("LEDIT_AGENT_CONSOLE") == "1" {
		// In agent console, just display the change log by default
		return lf.viewChangeLog()
	}

	// Check if we're not in a terminal
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		// Non-terminal, show change log
		return lf.viewChangeLog()
	}

	// Convert to dropdown items
	items := make([]ui.DropdownItem, len(actions))
	for i, action := range actions {
		items[i] = &LogActionItem{
			ID:          action.ID,
			DisplayName: action.DisplayName,
			Description: action.Description,
		}
	}

	// Temporarily disable ESC monitoring during dropdown
	lf.agent.DisableEscMonitoring()
	defer lf.agent.EnableEscMonitoring()

	// Create and show dropdown
	selected, err := lf.agent.ShowDropdown(items, ui.DropdownOptions{
		Prompt:       "📜 Change History:",
		SearchPrompt: "Search: ",
		ShowCounts:   false,
	})

	if err != nil {
		if err == ui.ErrCancelled {
			fmt.Printf("\r\nLog operation cancelled.\r\n")
			return nil
		}
		return fmt.Errorf("failed to show action selection: %w", err)
	}

	// Execute selected action
	selectedID := selected.Value().(string)
	for _, action := range actions {
		if action.ID == selectedID {
			return action.Action(lf)
		}
	}

	return fmt.Errorf("unknown action: %s", selectedID)
}

// buildLogActions creates dynamic actions based on available revisions
func (lf *LogFlow) buildLogActions(revisions []history.RevisionGroup) []LogAction {
	var actions []LogAction

	// Action 1: View full change log
	actions = append(actions, LogAction{
		ID:          "view_log",
		DisplayName: "📋 View Change Log",
		Description: "Display complete change history",
		Action:      (*LogFlow).viewChangeLog,
	})

	// Action 2: Interactive rollback (if revisions exist)
	if len(revisions) > 0 {
		actions = append(actions, LogAction{
			ID:          "rollback_select",
			DisplayName: "⏮️ Select Revision to Rollback",
			Description: fmt.Sprintf("Choose from %d available revisions", len(revisions)),
			Action:      (*LogFlow).interactiveRollback,
		})
	}

	// Action 3: Current session changes
	actions = append(actions, LogAction{
		ID:          "current_changes",
		DisplayName: "📝 Current Session Changes",
		Description: "View changes tracked in this session",
		Action:      (*LogFlow).showCurrentSessionChanges,
	})

	// Action 4: Change statistics
	if len(revisions) > 0 {
		actions = append(actions, LogAction{
			ID:          "change_stats",
			DisplayName: "📊 Change Statistics",
			Description: "View statistics about file changes",
			Action:      (*LogFlow).showChangeStatistics,
		})
	}

	// Action 5: Export change log
	if len(revisions) > 0 {
		actions = append(actions, LogAction{
			ID:          "export_log",
			DisplayName: "📤 Export Change Log",
			Description: "Save change history to file",
			Action:      (*LogFlow).exportChangeLog,
		})
	}

	return actions
}

// getAvailableRevisions gets revision information from history
func (lf *LogFlow) getAvailableRevisions() ([]history.RevisionGroup, error) {
	return history.GetRevisionGroups()
}

// viewChangeLog displays the standard change log
func (lf *LogFlow) viewChangeLog() error {
	// Always use \r\n for consistency in agent console (raw mode)
	// The agent console handles all output in raw mode
	fmt.Print("📜 Recent Change History\r\n")
	fmt.Print("=" + fmt.Sprintf("%*s", 25, "=") + "\r\n")

	// Use the non-interactive buffer version with proper formatting
	historyText, err := history.PrintRevisionHistoryBuffer()
	if err != nil {
		return fmt.Errorf("failed to show change history: %w", err)
	}

	// Always convert to \r\n since we're in agent console raw mode
	historyText = strings.ReplaceAll(historyText, "\n", "\r\n")

	fmt.Print(historyText)
	fmt.Print("\r\n💡 Use /rollback <revision-id> to revert changes\r\n")

	return nil
}

// interactiveRollback provides dropdown-based rollback selection
func (lf *LogFlow) interactiveRollback() error {
	revisions, err := lf.getAvailableRevisions()
	if err != nil {
		return fmt.Errorf("failed to get revisions: %w", err)
	}

	if len(revisions) == 0 {
		fmt.Printf("\r\n📭 No revisions available for rollback.\r\n")
		return nil
	}

	// Check if we're in agent console - show list with help
	if os.Getenv("LEDIT_AGENT_CONSOLE") == "1" {
		fmt.Println("\n⏮️ Available Revisions:")
		fmt.Println("=======================")

		for i, revision := range revisions {
			description := revision.Instructions
			if len(description) > 50 {
				description = description[:47] + "..."
			}
			fmt.Printf("%d. %s - %s (%s)\n", i+1, revision.RevisionID, description,
				revision.Timestamp.Format("2006-01-02 15:04:05"))
		}

		fmt.Println("\n💡 To rollback to a revision, use: /rollback <revision-id>")
		fmt.Println("   Example: /rollback rev_abc123")
		return nil
	}

	// Check if we're not in a terminal
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Println("Interactive rollback selection requires a terminal.")
		fmt.Println("Use /rollback <revision-id> to rollback to a specific revision.")
		return nil
	}

	// Convert revisions to dropdown items
	items := make([]ui.DropdownItem, len(revisions))
	for i, revision := range revisions {
		// Use first few words of Instructions as description
		description := revision.Instructions
		if len(description) > 50 {
			description = description[:47] + "..."
		}

		items[i] = &RevisionItem{
			RevisionID:  revision.RevisionID,
			Description: description,
			Timestamp:   revision.Timestamp.Format("2006-01-02 15:04:05"),
		}
	}

	// Show dropdown
	lf.agent.DisableEscMonitoring()
	defer lf.agent.EnableEscMonitoring()

	// Create revision selection dropdown
	selected, err := lf.agent.ShowDropdown(items, ui.DropdownOptions{
		Prompt:       "⏮️ Select Revision to Rollback:",
		SearchPrompt: "Search: ",
		ShowCounts:   false,
	})

	if err != nil {
		if err == ui.ErrCancelled {
			fmt.Printf("\r\nRollback cancelled.\r\n")
			return nil
		}
		return fmt.Errorf("failed to show revision selection: %w", err)
	}

	revisionID := selected.Value().(string)

	// Confirm rollback
	fmt.Printf("\r\n🔄 Rolling back to revision: %s\r\n", revisionID)

	err = history.RevertChangeByRevisionID(revisionID)
	if err != nil {
		return fmt.Errorf("rollback failed: %w", err)
	}

	fmt.Printf("✅ Successfully rolled back to revision: %s\r\n", revisionID)
	fmt.Printf("💡 Use '/changes' to see current session changes\r\n")

	return nil
}

// showCurrentSessionChanges displays current session change tracking
func (lf *LogFlow) showCurrentSessionChanges() error {
	fmt.Printf("\r\n📝 Current Session Changes\r\n")
	fmt.Printf("==============================\r\n")

	if !lf.agent.IsChangeTrackingEnabled() {
		fmt.Printf("Change tracking is not enabled for this session\r\n")
		return nil
	}

	changeCount := lf.agent.GetChangeCount()
	if changeCount == 0 {
		fmt.Printf("No file changes have been tracked in this session yet\r\n")
		return nil
	}

	fmt.Printf("Session Revision: %s\r\n", lf.agent.GetRevisionID())
	fmt.Printf("Total Changes: %d\r\n\r\n", changeCount)

	summary := lf.agent.GetChangesSummary()
	// Convert newlines for raw mode compatibility
	summary = strings.ReplaceAll(summary, "\n", "\r\n")
	fmt.Print(summary)

	return nil
}

// showChangeStatistics displays statistics about all changes
func (lf *LogFlow) showChangeStatistics() error {
	fmt.Print("\r\n📊 Change Statistics\r\n")
	fmt.Print("=" + strings.Repeat("=", 22) + "\r\n")

	// Get current session stats
	if lf.agent.IsChangeTrackingEnabled() {
		changeCount := lf.agent.GetChangeCount()
		revisionID := lf.agent.GetRevisionID()

		fmt.Printf("Current Session:\r\n")
		fmt.Printf("  Revision ID: %s\r\n", revisionID)
		fmt.Printf("  Changes: %d\r\n\r\n", changeCount)
	}

	// TODO: Add more comprehensive statistics
	// - Total revisions across all sessions
	// - Most frequently changed files
	// - Change patterns over time
	fmt.Printf("🚧 Extended statistics coming soon!\r\n")

	return nil
}

// exportChangeLog exports the change log to a file
func (lf *LogFlow) exportChangeLog() error {
	fmt.Printf("\r\n📤 Exporting change log...\r\n")

	// Get change history
	historyText, err := history.PrintRevisionHistoryBuffer()
	if err != nil {
		return fmt.Errorf("failed to get change history: %w", err)
	}

	// Generate filename with timestamp
	filename := fmt.Sprintf("change-log-%s.txt",
		fmt.Sprintf("%d", 1000000)) // Simple timestamp placeholder

	// Write to file
	err = os.WriteFile(filename, []byte(historyText), 0644)
	if err != nil {
		return fmt.Errorf("failed to write log file: %w", err)
	}

	fmt.Printf("✅ Change log exported to: %s\r\n", filename)
	return nil
}
