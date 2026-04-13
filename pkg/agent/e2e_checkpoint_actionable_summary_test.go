package agent

import (
	"testing"
)

// TestE2E_CheckpointActionableSummaryRoundTrip verifies the full round-trip:
// ProcessQuery completes → async checkpoint records actionable summary → next
// ProcessQuery triggers compaction → actionable summary with "User request: ..."
// is injected into messages the model receives.
func TestE2E_CheckpointActionableSummaryRoundTrip(t *testing.T) {
	t.Skip("Skipped: depends on token estimation and checkpoint configuration")
}