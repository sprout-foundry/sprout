package agent

import (
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestTurnCheckpointSummariesStripLegacyUserTimestamp(t *testing.T) {
	const stamped = "<current-time>2026-07-22T12:37:28-05:00 (Local: 2026-07-22 12:37:28, CDT)</current-time>\n\nfix the cache"
	messages := []api.Message{
		{Role: "user", Content: stamped},
		{Role: "assistant", Content: "Fixed the cache."},
	}

	for name, summary := range map[string]string{
		"go summary":         buildTurnCheckpointGoSummary(messages),
		"actionable summary": buildTurnCheckpointActionableSummary(messages),
	} {
		t.Run(name, func(t *testing.T) {
			if strings.Contains(summary, "<current-time>") {
				t.Fatalf("summary retained legacy timestamp: %q", summary)
			}
			if !strings.Contains(summary, "fix the cache") {
				t.Fatalf("summary lost clean user request: %q", summary)
			}
		})
	}
}
