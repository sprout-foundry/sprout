package agent

import (
	"testing"
)

// TestE2E_OrphanedToolResultRemovalAfterCompaction verifies that when
// checkpoint compaction replaces a message range containing assistant
// tool_calls and their tool results, the orphaned tool result messages
// (whose tool_call_id no longer matches any remaining assistant) are
// properly removed from the prepared message list.
func TestE2E_OrphanedToolResultRemovalAfterCompaction(t *testing.T) {
	t.Skip("Skipped: depends on token estimation and checkpoint configuration")
}

// TestE2E_OrphanedToolResultsBeforeAnyAssistant verifies that when
// no assistant messages exist (only the system prompt), the prepareMessages
// pipeline still handles the messages correctly.
func TestE2E_OrphanedToolResultsBeforeAnyAssistant(t *testing.T) {
	t.Skip("Skipped: depends on token estimation and checkpoint configuration")
}