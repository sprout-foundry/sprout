package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/envutil"
	"github.com/sprout-foundry/sprout/pkg/history"
	"github.com/sprout-foundry/sprout/pkg/ui"
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

	// Check if we're in agent console - just show the log
	if envutil.GetEnvSimple("AGENT_CONSOLE") == "1" {
		// In agent console, just display the change log by default
		return lf.viewChangeLog()
	}

	// Check if we're not in a terminal
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		// Non-terminal, show change log
		return lf.viewChangeLog()
	}

	// Build available actions
	actions := lf.buildLogActions(revisions)

	if len(actions) == 0 {
		fmt.Print("\r\n")
		console.GlyphInfo.Fprintf(os.Stdout, "No change history available.")
		return nil
	}

	// Build options for numeric prompt
	var options []ui.NumericPromptOption
	for i, action := range actions {
		options = append(options, ui.NumericPromptOption{
			Index:       i + 1,
			DisplayName: action.DisplayName,
			Description: action.Description,
			Value:       action.ID,
		})
	}

	// Use shared numeric selector
	selection, ok := ui.PromptForSelectionWithOptions(options, "Enter option number (or 0 to cancel): ")
	if !ok || selection == 0 {
		return nil
	}

	// Execute the selected action
	return actions[selection-1].Action(lf)
}

// buildLogActions creates dynamic actions based on available revisions
func (lf *LogFlow) buildLogActions(revisions []history.RevisionGroup) []LogAction {
	var actions []LogAction

	// Action 1: View full change log
	actions = append(actions, LogAction{
		ID:          "view_log",
		DisplayName: "View Change Log",
		Description: "Display complete change history",
		Action:      (*LogFlow).viewChangeLog,
	})

	// Action 2: Interactive rollback (if revisions exist)
	if len(revisions) > 0 {
		actions = append(actions, LogAction{
			ID:          "rollback_select",
			DisplayName: "Select Revision to Rollback",
			Description: fmt.Sprintf("Choose from %d available revisions", len(revisions)),
			Action:      (*LogFlow).interactiveRollback,
		})
	}

	// Action 3: Current session changes
	actions = append(actions, LogAction{
		ID:          "current_changes",
		DisplayName: "Current Session Changes",
		Description: "View changes tracked in this session",
		Action:      (*LogFlow).showCurrentSessionChanges,
	})

	// Action 4: Change statistics
	if len(revisions) > 0 {
		actions = append(actions, LogAction{
			ID:          "change_stats",
			DisplayName: "Change Statistics",
			Description: "View statistics about file changes",
			Action:      (*LogFlow).showChangeStatistics,
		})
	}

	// Action 5: Export change log
	if len(revisions) > 0 {
		actions = append(actions, LogAction{
			ID:          "export_log",
			DisplayName: "Export Change Log",
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
	console.GlyphInfo.Fprintf(os.Stdout, "Recent Change History")

	// Use the non-interactive buffer version with proper formatting
	historyText, err := history.PrintRevisionHistoryBuffer()
	if err != nil {
		return fmt.Errorf("failed to show change history: %w", err)
	}

	// Always convert to \r\n since we're in agent console raw mode
	historyText = strings.ReplaceAll(historyText, "\n", "\r\n")

	fmt.Print(historyText)
	fmt.Print("\r\n")
	console.GlyphInfo.Fprintf(os.Stdout, "Use /rollback <revision-id> to revert changes")

	return nil
}

// interactiveRollback provides dropdown-based rollback selection
func (lf *LogFlow) interactiveRollback() error {
	revisions, err := lf.getAvailableRevisions()
	if err != nil {
		return fmt.Errorf("failed to get revisions: %w", err)
	}

	if len(revisions) == 0 {
		fmt.Print("\r\n")
		console.GlyphInfo.Fprintf(os.Stdout, "No revisions available for rollback.")
		return nil
	}

	// Check if we're in agent console - show list with help
	if envutil.GetEnvSimple("AGENT_CONSOLE") == "1" {
		fmt.Println()
		console.GlyphInfo.Print("Available Revisions:")

		for i, revision := range revisions {
			description := revision.Instructions
			if len(description) > 50 {
				description = description[:47] + "..."
			}
			fmt.Printf("%d. %s - %s (%s)\n", i+1, revision.RevisionID, description,
				revision.Timestamp.Format("2006-01-02 15:04:05"))
		}

		fmt.Println()
		console.GlyphInfo.Print("To rollback to a revision, use: /rollback <revision-id>")
		fmt.Println("   Example: /rollback rev_abc123")
		return nil
	}

	// Check if we're not in a terminal
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Println("Interactive rollback selection requires a terminal.")
		fmt.Println("Use /rollback <revision-id> to rollback to a specific revision.")
		return nil
	}

	// Numeric selector for rollback
	fmt.Println()
	console.GlyphInfo.Print("Available Revisions:")

	for i, revision := range revisions {
		description := revision.Instructions
		if len(description) > 60 {
			description = description[:57] + "..."
		}
		fmt.Printf("%d. %s - %s\n", i+1, revision.RevisionID, description)
		fmt.Printf("   [Time: %s, Files: %d]\n", revision.Timestamp.Format("2006-01-02 15:04:05"), len(revision.Changes))
	}

	// Use shared numeric selector
	selection, ok := ui.PromptForSelection(nil, "Enter revision number to rollback (or 0 to cancel): ")
	if !ok || selection == 0 {
		return nil
	}

	// Validate selection is within range (extra safety check)
	if selection < 1 || selection > len(revisions) {
		fmt.Printf("Invalid selection. Please enter a number between 1 and %d.\n", len(revisions))
		return nil
	}

	// Confirm before executing rollback
	revisionID := revisions[selection-1].RevisionID
	fmt.Println()
	console.GlyphWarning.Printf("Confirm rollback of revision: %s", revisionID)
	fmt.Printf("This will restore %d file(s). Continue? (y/n): ", len(revisions[selection-1].Changes))

	if !ui.PromptForConfirmation("") {
		fmt.Println("Rollback cancelled.")
		return nil
	}

	// Perform rollback
	fmt.Print("\r\n")
	console.GlyphDim.Fprintf(os.Stdout, "Rolling back to revision: %s", revisionID)

	err = history.RevertChangeByRevisionID(revisionID)
	if err != nil {
		return fmt.Errorf("rollback failed: %w", err)
	}

	console.GlyphSuccess.Fprintf(os.Stdout, "Successfully rolled back to revision: %s", revisionID)
	console.GlyphInfo.Fprintf(os.Stdout, "Use '/changes' to see current session changes")

	return nil
}

// showCurrentSessionChanges displays current session change tracking
func (lf *LogFlow) showCurrentSessionChanges() error {
	fmt.Print("\r\n")
	console.GlyphInfo.Fprintf(os.Stdout, "Current Session Changes")

	if !lf.agent.IsChangeTrackingEnabled() {
		if lf.agent.GetChangeTracker() == nil {
			fmt.Printf("No tracked session has started yet\r\n")
		} else {
			fmt.Printf("Change tracking is disabled for this session\r\n")
		}
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
	fmt.Print("\r\n")
	console.GlyphInfo.Fprintf(os.Stdout, "Change Statistics")

	// Get current session stats
	if lf.agent.IsChangeTrackingEnabled() {
		changeCount := lf.agent.GetChangeCount()
		revisionID := lf.agent.GetRevisionID()

		fmt.Printf("Current Session:\r\n")
		fmt.Printf("  Revision ID: %s\r\n", revisionID)
		fmt.Printf("  Changes: %d\r\n\r\n", changeCount)
	}

	// NOTE: Extended statistics (total revisions, frequent files, change patterns)
	// are tracked as a future enhancement.

	return nil
}

// exportChangeLog exports the change log to a file
func (lf *LogFlow) exportChangeLog() error {
	fmt.Print("\r\n")
	console.GlyphAction.Fprintf(os.Stdout, "Exporting change log...")

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

	console.GlyphSuccess.Fprintf(os.Stdout, "Change log exported to: %s", filename)
	return nil
}
