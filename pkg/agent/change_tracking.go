package agent

import (
	"crypto/md5"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/history"
)

// RedactedContentMarker is the marker used when file content is redacted because
// the file is outside the workspace root (to avoid leaking sensitive data).
const RedactedContentMarker = "[REDACTED - external file]"

// ChangeTracker manages change tracking for the agent workflow
type ChangeTracker struct {
	revisionID           string
	sessionID            string
	instructions         string
	changes              []TrackedFileChange
	enabled              bool
	agent                *Agent
	baseRevisionRecorded bool
	committedChangeCount int
	// checkpointedChangeCount is len(changes) at the time of the most
	// recent turn-checkpoint capture. CollectFileChangesForCheckpoint
	// returns only changes appended since this watermark so each
	// checkpoint's manifest scopes to its own turn's writes, not the
	// cumulative session.
	checkpointedChangeCount int
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
	// Initialize history paths based on configuration
	history.InitializeHistoryPaths(nil)

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

	// Redact content if file is outside the workspace root
	if ct.isOutsideWorkspace(filePath) {
		originalContent = RedactedContentMarker
		newContent = RedactedContentMarker
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

	// Redact content if file is outside the workspace root
	if ct.isOutsideWorkspace(filePath) {
		originalContent = RedactedContentMarker
		newContent = RedactedContentMarker
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
func (ct *ChangeTracker) Commit(llmResponse string, conversation []api.Message) error {
	if !ct.enabled || len(ct.changes) == 0 {
		return nil
	}
	if ct.committedChangeCount >= len(ct.changes) {
		return nil
	}

	// Convert agent_api.Message to history.APIMessage for storage
	historyConversation := convertToHistoryMessages(conversation)

	if !ct.baseRevisionRecorded {
		// Record base revision with conversation
		revisionID, err := history.RecordBaseRevision(ct.revisionID, ct.instructions, llmResponse, historyConversation)
		if err != nil {
			return fmt.Errorf("failed to record base revision: %w", err)
		}

		// Update our revision ID to match what was actually recorded
		ct.revisionID = revisionID
		ct.baseRevisionRecorded = true
	}

	// Record each file change
	for _, change := range ct.changes[ct.committedChangeCount:] {
		description := fmt.Sprintf("%s via %s", change.Operation, change.ToolCall)
		note := fmt.Sprintf("Agent session: %s", ct.sessionID)

		err := history.RecordChangeWithDetails(
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

	ct.committedChangeCount = len(ct.changes)
	return nil
}

// convertToHistoryMessages converts api.Message to history.APIMessage format
func convertToHistoryMessages(messages []api.Message) []history.APIMessage {
	if messages == nil {
		return nil
	}

	result := make([]history.APIMessage, len(messages))
	for i, msg := range messages {
		result[i] = history.APIMessage{
			Role:             msg.Role,
			Content:          msg.Content,
			ReasoningContent: msg.ReasoningContent,
			ToolCallID:       msg.ToolCallID,
		}

		// Convert tool calls if present
		if len(msg.ToolCalls) > 0 {
			result[i].ToolCalls = make([]history.APIToolCall, len(msg.ToolCalls))
			for j, tc := range msg.ToolCalls {
				result[i].ToolCalls[j] = history.APIToolCall{
					ID:   tc.ID,
					Type: tc.Type,
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				}
			}
		}
	}

	return result
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
	ct.baseRevisionRecorded = false
	ct.committedChangeCount = 0
	ct.checkpointedChangeCount = 0
}

// CollectFileChangesForCheckpoint returns the (path, op) manifest of
// changes appended since the most recent checkpoint capture, along with
// the current revision ID. Advances the internal watermark so a
// subsequent call returns only the next turn's changes. Safe to call
// when tracking is disabled — returns (nil, "") in that case.
//
// Ops are git-style: "A" (added/created), "M" (modified), "D" (deleted),
// "R" (renamed). The ChangeTracker today only records create/write/edit
// — never delete or rename — so the manifest produced here will only
// contain A and M entries. When the tracker grows D/R support, extend
// the mapping table below.
//
// Multiple writes to the same path within the same turn collapse to one
// entry, preferring "A" over "M" (a turn that creates then modifies
// the same file is recorded as A).
func (ct *ChangeTracker) CollectFileChangesForCheckpoint() ([]CheckpointFileChange, string) {
	if ct == nil || !ct.enabled {
		return nil, ""
	}
	if ct.checkpointedChangeCount >= len(ct.changes) {
		// No new changes since last capture.
		return nil, ct.revisionID
	}

	window := ct.changes[ct.checkpointedChangeCount:]
	ct.checkpointedChangeCount = len(ct.changes)

	if len(window) == 0 {
		return nil, ct.revisionID
	}

	// Collapse multiple writes to the same path → one entry per path.
	// Track first-seen op; "create" beats "edit"/"write" so a turn that
	// creates a file (and then edits it) shows up as A, not M.
	seen := make(map[string]string, len(window))
	order := make([]string, 0, len(window))
	for _, c := range window {
		op := mapTrackedOperationToGit(c.Operation)
		existing, ok := seen[c.FilePath]
		if !ok {
			order = append(order, c.FilePath)
			seen[c.FilePath] = op
			continue
		}
		// "A" wins over "M"; otherwise keep the first.
		if op == "A" && existing != "A" {
			seen[c.FilePath] = op
		}
	}

	manifest := make([]CheckpointFileChange, 0, len(order))
	for _, path := range order {
		manifest = append(manifest, CheckpointFileChange{Path: path, Op: seen[path]})
	}
	return manifest, ct.revisionID
}

// mapTrackedOperationToGit maps a TrackedFileChange.Operation value to the
// git-style op code used in CheckpointFileChange.Op.
func mapTrackedOperationToGit(op string) string {
	switch op {
	case "create":
		return "A"
	case "write", "edit", "overwrite":
		return "M"
	case "delete":
		return "D"
	case "rename":
		return "R"
	default:
		return "?"
	}
}

// Reset resets the change tracker with a new revision ID and instructions
func (ct *ChangeTracker) Reset(instructions string) {
	ct.instructions = instructions
	ct.revisionID = generateRevisionID(ct.sessionID, instructions)
	ct.Clear()
}

// Helper functions

// isOutsideWorkspace returns true if filePath is outside the agent's workspace root.
// If the workspace root is empty or the agent is nil, it returns false (treats all files as in-workspace).
func (ct *ChangeTracker) isOutsideWorkspace(filePath string) bool {
	if ct.agent == nil {
		return false
	}
	workspaceRoot := ct.agent.workspaceRoot
	if workspaceRoot == "" {
		return false
	}

	absFile, err := filepath.Abs(filePath)
	if err != nil {
		return false // If we can't resolve the path, don't redact
	}

	absWorkspace, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return false // If we can't resolve workspace, don't redact
	}

	// Resolve symlinks on both sides for consistent comparison.
	// On macOS, /var → /private/var and os.Chdir may resolve the symlink
	// in the process's CWD, causing absFile and absWorkspace to diverge.
	absFile = resolveSymlinksPath(absFile)
	resolvedWorkspace, werr := filepath.EvalSymlinks(absWorkspace)
	if werr == nil {
		absWorkspace = resolvedWorkspace
	}

	rel, err := filepath.Rel(absWorkspace, absFile)
	if err != nil {
		return false
	}

	// If the relative path starts with "..", it's outside the workspace
	return strings.HasPrefix(rel, "..")
}

// resolveSymlinksPath resolves symlinks in a path, handling non-existent
// files/directories by walking up to the nearest existing ancestor and
// appending the remaining components.
func resolveSymlinksPath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved
	}
	// Walk up the directory tree until we find an existing ancestor.
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	for {
		resolvedDir, derr := filepath.EvalSymlinks(dir)
		if derr == nil {
			return filepath.Join(resolvedDir, base)
		}
		base = filepath.Join(filepath.Base(dir), base)
		dir = filepath.Dir(dir)
		if dir == "/" || dir == "." {
			// Reached the root without resolving; return original.
			return path
		}
	}
}

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
