package tools

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/history"
)

func TestFormatRevisionOutputExact(t *testing.T) {
	changes := []history.ChangeLog{
		{
			RequestHash:  "abc123",
			Filename:     "test.go",
			AgentModel:   "test-model",
			Status:       "active",
			Instructions: "some instructions",
		},
	}
	group := revisionGroup{ID: "abc123", Changes: changes}

	t.Run("history view format", func(t *testing.T) {
		opts := formatRevisionOpts{
			ShowContent:      false,
			ShowStatus:       true,
			ShowInstructions: true,
			TimeFormat:       "2006-01-02 15:04:05",
			TitlePrefix:      "### Revision: ",
			FilesLabel:       "**Files Changed:** ",
		}
		out := formatRevision(group, opts)
		t.Logf("Output:\n%q", out)

		// Check for exact expected substrings
		if want := "### Revision: abc123\n"; !containsExact(out, want) {
			t.Errorf("expected line %q in output, got:\n%q", want, out)
		}

		if want := "**Files Changed:** 1\n"; !containsExact(out, want) {
			t.Errorf("expected line %q in output, got:\n%q", want, out)
		}
	})

	t.Run("rollback list format", func(t *testing.T) {
		opts := formatRevisionOpts{
			ShowContent:      false,
			ShowStatus:       false,
			ShowInstructions: false,
			TimeFormat:       "2006-01-02 15:04:05",
			TitlePrefix:      "**Revision ID:** ",
			FilesLabel:       "**Files Changed:** ",
		}
		out := formatRevision(group, opts)
		t.Logf("Output:\n%q", out)

		if want := "**Revision ID:** abc123\n"; !containsExact(out, want) {
			t.Errorf("expected line %q in output, got:\n%q", want, out)
		}

		if want := "**Files Changed:** 1\n"; !containsExact(out, want) {
			t.Errorf("expected line %q in output, got:\n%q", want, out)
		}
	})
}

func containsExact(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
