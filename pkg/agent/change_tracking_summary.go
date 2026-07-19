package agent

import (
	"fmt"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// GenerateAISummary creates an AI-generated summary of the changes.
// Snapshots the tracked-changes slice under ct.mu so concurrent
// Clear/Reset can't truncate the slice while we iterate below. Falls
// back to the manual summary if the LLM call fails or the agent is
// nil (e.g. test fixtures without a live agent).
//
// SP-075-extension: extracted from change_tracking.go. No behavior change.
func (ct *ChangeTracker) GenerateAISummary() (string, error) {
	// Snapshot the state we need under the lock so concurrent Clear/Reset
	// can't truncate ct.changes while we iterate below.
	ct.mu.Lock()
	if len(ct.changes) == 0 {
		ct.mu.Unlock()
		return "No changes to summarize", nil
	}
	changesSnapshot := make([]TrackedFileChange, len(ct.changes))
	copy(changesSnapshot, ct.changes)
	instructions := ct.instructions
	ct.mu.Unlock()

	if ct.agent == nil {
		return ct.GetSummary(), nil // Fallback to manual summary
	}

	// Build context for the AI summary
	var contextBuilder strings.Builder
	contextBuilder.WriteString("Changes made in this session:\n\n")
	contextBuilder.WriteString(fmt.Sprintf("Original instruction: %s\n\n", instructions))

	for i, change := range changesSnapshot {
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

// GetSummary returns a deterministic summary of tracked changes (no
// LLM call). Used as a fallback when GenerateAISummary fails or the
// agent is unavailable, and as the synchronous path for status UIs.
//
// SP-075-extension: extracted from change_tracking.go. No behavior change.
func (ct *ChangeTracker) GetSummary() string {
	ct.mu.Lock()
	if len(ct.changes) == 0 {
		ct.mu.Unlock()
		return "No file changes tracked"
	}
	changesSnapshot := make([]TrackedFileChange, len(ct.changes))
	copy(changesSnapshot, ct.changes)
	ct.mu.Unlock()

	var summary strings.Builder
	summary.WriteString(fmt.Sprintf("Tracked %d file changes:\n", len(changesSnapshot)))

	for _, change := range changesSnapshot {
		summary.WriteString(fmt.Sprintf("• %s (%s via %s)\n",
			change.FilePath, change.Operation, change.ToolCall))
	}

	return summary.String()
}
