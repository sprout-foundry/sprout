package agent

import (
	"crypto/md5"
	"fmt"
	"os"
	"strings"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/changetracker"
)

// ChangeTracker manages change tracking for the agent workflow
type ChangeTracker struct {
	revisionID   string
	sessionID    string
	instructions string
	changes      []TrackedFileChange
	enabled      bool
	agent        *Agent
	committed    bool // Track whether changes have been committed
}

// TrackedFileChange represents a file change made during agent execution
type TrackedFileChange struct {
	FilePath     string    `json:"file_path"`
	OriginalCode string    `json:"original_code"`
	NewCode      string    `json:"new_code"`
	Operation    string    `json:"operation"` // "write", "edit", "create"
	Timestamp    time.Time `json:"timestamp"`
	ToolCall     string    `json:"tool_call"` // Which tool was used
}

// NewChangeTracker creates a new change tracker for an agent session
func NewChangeTracker(agent *Agent, instructions string) *ChangeTracker {
	sessionID := agent.GetSessionID()
	if sessionID == "" {
		sessionID = generateSessionID()
	}

	// Generate revision ID based on session and timestamp
	revisionID := generateRevisionID(sessionID, instructions)

	return &ChangeTracker{
		revisionID:   revisionID,
		sessionID:    sessionID,
		instructions: instructions,
		changes:      make([]TrackedFileChange, 0),
		enabled:      true,
		agent:        agent,
		committed:    false,
	}
}

// Enable enables change tracking
func (ct *ChangeTracker) Enable() {
	ct.enabled = true
}

// Disable disables change tracking
func (ct *ChangeTracker) Disable() {
	ct.enabled = false
}

// IsEnabled returns whether change tracking is enabled
func (ct *ChangeTracker) IsEnabled() bool {
	return ct.enabled
}

// GetRevisionID returns the current revision ID
func (ct *ChangeTracker) GetRevisionID() string {
	return ct.revisionID
}

// TrackFileWrite tracks a write operation (WriteFile tool)
func (ct *ChangeTracker) TrackFileWrite(filePath string, newContent string) error {
	if !ct.enabled {
		return nil
	}

	// Get original content (if file exists)
	originalContent := ""
	if _, err := os.Stat(filePath); err == nil {
		if content, readErr := os.ReadFile(filePath); readErr == nil {
			originalContent = string(content)
		}
	}

	// Record the change
	change := TrackedFileChange{
		FilePath:     filePath,
		OriginalCode: originalContent,
		NewCode:      newContent,
		Operation:    determineWriteOperation(originalContent, newContent),
		Timestamp:    time.Now(),
		ToolCall:     "WriteFile",
	}

	ct.changes = append(ct.changes, change)
	return nil
}

// TrackFileEdit tracks an edit operation (EditFile tool)
func (ct *ChangeTracker) TrackFileEdit(filePath string, originalContent string, newContent string) error {
	if !ct.enabled {
		return nil
	}

	change := TrackedFileChange{
		FilePath:     filePath,
		OriginalCode: originalContent,
		NewCode:      newContent,
		Operation:    "edit",
		Timestamp:    time.Now(),
		ToolCall:     "EditFile",
	}

	ct.changes = append(ct.changes, change)
	return nil
}

// Commit commits all tracked changes to the change tracker
func (ct *ChangeTracker) Commit(llmResponse string) error {
	if !ct.enabled || len(ct.changes) == 0 || ct.committed {
		return nil // Already committed or nothing to commit
	}

	// Record base revision
	revisionID, err := changetracker.RecordBaseRevision(ct.revisionID, ct.instructions, llmResponse)
	if err != nil {
		return fmt.Errorf("failed to record base revision: %w", err)
	}

	// Update our revision ID to match what was actually recorded
	ct.revisionID = revisionID

	// Record each file change
	for _, change := range ct.changes {
		description := fmt.Sprintf("%s via %s", change.Operation, change.ToolCall)
		note := fmt.Sprintf("Agent session: %s", ct.sessionID)

		err := changetracker.RecordChangeWithDetails(
			ct.revisionID,
			change.FilePath,
			change.OriginalCode,
			change.NewCode,
			description,
			note,
			ct.instructions,    // originalPrompt
			llmResponse,        // llmMessage
			ct.getAgentModel(), // editingModel
		)
		if err != nil {
			return fmt.Errorf("failed to record change for %s: %w", change.FilePath, err)
		}
	}

	// Mark as committed
	ct.committed = true
	return nil
}

// GetTrackedFiles returns a list of files that have been modified
func (ct *ChangeTracker) GetTrackedFiles() []string {
	files := make([]string, len(ct.changes))
	for i, change := range ct.changes {
		files[i] = change.FilePath
	}
	return files
}

// GetChangeCount returns the number of tracked changes
func (ct *ChangeTracker) GetChangeCount() int {
	return len(ct.changes)
}

// GetChanges returns a copy of the tracked changes
func (ct *ChangeTracker) GetChanges() []TrackedFileChange {
	changesCopy := make([]TrackedFileChange, len(ct.changes))
	copy(changesCopy, ct.changes)
	return changesCopy
}

// Clear clears all tracked changes (but keeps the tracker enabled)
func (ct *ChangeTracker) Clear() {
	ct.changes = ct.changes[:0]
	ct.committed = false
}

// Reset resets the change tracker with a new revision ID and instructions
func (ct *ChangeTracker) Reset(instructions string) {
	ct.instructions = instructions
	ct.revisionID = generateRevisionID(ct.sessionID, instructions)
	ct.Clear()
}

// Helper functions

func generateSessionID() string {
	return fmt.Sprintf("agent-%d", time.Now().UnixNano())
}

func generateRevisionID(sessionID, instructions string) string {
	// Create a unique revision ID based on session and instructions
	hash := md5.Sum([]byte(sessionID + "-" + instructions + "-" + fmt.Sprint(time.Now().UnixNano())))
	return fmt.Sprintf("agent-%x", hash)[:16] // Truncate to reasonable length
}

func determineWriteOperation(originalContent, newContent string) string {
	if originalContent == "" {
		return "create"
	}
	if originalContent != newContent {
		return "write"
	}
	return "overwrite"
}

func (ct *ChangeTracker) getAgentModel() string {
	if ct.agent != nil {
		return ct.agent.GetModel()
	}
	return "unknown"
}


// limitString truncates a string to the specified length with ellipsis
func limitString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// Summary methods for reporting

// GenerateAISummary creates an AI-generated summary of the changes
func (ct *ChangeTracker) GenerateAISummary() (string, error) {
	if len(ct.changes) == 0 {
		return "No changes to summarize", nil
	}

	if ct.agent == nil {
		return ct.GetSummary(), nil // Fallback to manual summary
	}

	// Build context for the AI summary
	var contextBuilder strings.Builder
	contextBuilder.WriteString("Changes made in this session:\n\n")
	contextBuilder.WriteString(fmt.Sprintf("Original instruction: %s\n\n", ct.instructions))

	for i, change := range ct.changes {
		contextBuilder.WriteString(fmt.Sprintf("Change %d: %s %s\n", i+1, change.Operation, change.FilePath))
		contextBuilder.WriteString(fmt.Sprintf("Tool used: %s\n", change.ToolCall))

		// For large changes, show a diff summary instead of full content
		if len(change.OriginalCode)+len(change.NewCode) > 2000 {
			contextBuilder.WriteString("(Large file change - details in full diff)\n")
		} else if change.Operation == "edit" {
			contextBuilder.WriteString(fmt.Sprintf("Original: %s\n", limitString(change.OriginalCode, 300)))
			contextBuilder.WriteString(fmt.Sprintf("New: %s\n", limitString(change.NewCode, 300)))
		} else {
			contextBuilder.WriteString(fmt.Sprintf("Content: %s\n", limitString(change.NewCode, 300)))
		}
		contextBuilder.WriteString("\n")
	}

	prompt := fmt.Sprintf(`Please provide a concise 2-3 sentence summary of these code changes:

%s

Focus on WHAT was changed and WHY (based on the instruction). Be specific about files and functionality affected.`, contextBuilder.String())

	// Generate summary using the current model
	response, err := ct.agent.GenerateResponse([]api.Message{
		{Role: "user", Content: prompt},
	})

	if err != nil {
		return ct.GetSummary(), nil // Fallback to manual summary on error
	}

	return strings.TrimSpace(response), nil
}

// GetSummary returns a summary of tracked changes
func (ct *ChangeTracker) GetSummary() string {
	if len(ct.changes) == 0 {
		return "No file changes tracked"
	}

	var summary strings.Builder
	summary.WriteString(fmt.Sprintf("Tracked %d file changes:\n", len(ct.changes)))

	for _, change := range ct.changes {
		summary.WriteString(fmt.Sprintf("• %s (%s via %s)\n",
			change.FilePath, change.Operation, change.ToolCall))
	}

	return summary.String()
}
