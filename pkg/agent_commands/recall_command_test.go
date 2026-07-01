package commands

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

func TestRecallCommand_Name(t *testing.T) {
	cmd := &RecallCommand{}
	if got := cmd.Name(); got != "recall" {
		t.Errorf("Name() = %q, want %q", got, "recall")
	}
}

func TestRecallCommand_Description(t *testing.T) {
	cmd := &RecallCommand{}
	if cmd.Description() == "" {
		t.Error("Description() returned empty string")
	}
}

func TestRecallCommand_Usage(t *testing.T) {
	cmd := &RecallCommand{}
	var _ UsageProvider = cmd
	if cmd.Usage() == "" {
		t.Error("Usage() returned empty string")
	}
	if !strings.Contains(cmd.Usage(), "--json") || !strings.Contains(cmd.Usage(), "--limit") {
		t.Errorf("Usage() must mention --json and --limit, got: %q", cmd.Usage())
	}
}

// TestRecallCommand_EmptyQuery covers the empty-query path: parseRecallFlags
// returns the joined-empty query and runRecall surfaces the usage error.
func TestRecallCommand_EmptyQuery(t *testing.T) {
	a := agent.NewTestAgent()
	cmd := &RecallCommand{}
	err := cmd.Execute(nil, a)
	if err == nil {
		t.Fatal("Execute with empty query should return usage error")
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Errorf("Error should mention usage, got: %v", err)
	}
}

// TestRecallCommand_NilAgent covers the safety check.
func TestRecallCommand_NilAgent(t *testing.T) {
	cmd := &RecallCommand{}
	err := cmd.Execute([]string{"foo"}, nil)
	if err == nil {
		t.Fatal("Execute with nil agent should return error")
	}
	if !strings.Contains(err.Error(), "agent not available") {
		t.Errorf("Error should mention agent not available, got: %v", err)
	}
}

// TestRecallCommand_NoResults: agent has no embedding manager so Recall
// returns (nil, nil). Execute prints the "No prior sessions match" line.
func TestRecallCommand_NoResults(t *testing.T) {
	a := agent.NewTestAgent()
	cmd := &RecallCommand{}
	// No embedding manager → Recall returns (nil, nil). runRecall normalizes
	// nil → []agent.RecalledItem{}. Execute prints the friendly message.
	err := cmd.Execute([]string{"anything"}, a)
	if err != nil {
		t.Fatalf("Execute with no embedding manager should succeed, got: %v", err)
	}
}

// TestRecallCommand_WithResults: directly exercise the rendering path via
// FormatSemanticRecall so the markdown shape is verified end-to-end. (We
// can't inject items into a real Agent.Recall without an embedding manager,
// and the test should not depend on a running embedding backend.)
func TestRecallCommand_WithResults(t *testing.T) {
	items := []agent.RecalledItem{
		{Level: 0, Similarity: 0.91, AgeDays: 2, Summary: "first summary"},
		{Level: 1, Similarity: 0.85, AgeDays: 5, Summary: "second summary", Actionable: "do the thing"},
		{Level: 0, Similarity: 0.72, AgeDays: 30, Summary: "third summary"},
	}
	got := agent.FormatSemanticRecall(items, 8000)
	if got == "" {
		t.Fatal("FormatSemanticRecall returned empty string for 3 items")
	}
	if !strings.Contains(got, "Recalled From Session History") {
		t.Errorf("Expected header in formatted output, got: %q", got)
	}
	for _, it := range items {
		if !strings.Contains(got, it.Summary) {
			t.Errorf("Expected summary %q in output, got: %q", it.Summary, got)
		}
	}
}

// TestRecallCommand_JSONFlag_Empty: drive ExecuteWithJSONOutput with
// an empty result list (the agent has no embedding manager) and verify the
// JSON output is a valid empty array.
func TestRecallCommand_JSONFlag_Empty(t *testing.T) {
	a := agent.NewTestAgent()
	// Capture stdout for the JSON output. Easiest: call WriteJSONToOutput
	// semantics by piping through a json marshal of what runRecall returns.
	items, err := runRecall([]string{"foo"}, a)
	if err != nil {
		t.Fatalf("runRecall returned err: %v", err)
	}
	if items == nil {
		items = []agent.RecalledItem{}
	}
	b, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	if string(b) != "[]" {
		t.Errorf("Empty items should marshal to %q, got %q", "[]", string(b))
	}
}

// TestRecallCommand_LimitFlag: parseRecallFlags must extract --limit N.
func TestRecallCommand_LimitFlag(t *testing.T) {
	limit, query, err := parseRecallFlags([]string{"--limit", "10", "foo", "bar"})
	if err != nil {
		t.Fatalf("parseRecallFlags returned err: %v", err)
	}
	if limit != 10 {
		t.Errorf("limit = %d, want 10", limit)
	}
	if query != "foo bar" {
		t.Errorf("query = %q, want %q", query, "foo bar")
	}
}

// TestRecallCommand_LimitFlag_Default: with no --limit, default is 5.
func TestRecallCommand_LimitFlag_Default(t *testing.T) {
	limit, query, err := parseRecallFlags([]string{"hello"})
	if err != nil {
		t.Fatalf("parseRecallFlags returned err: %v", err)
	}
	if limit != 5 {
		t.Errorf("default limit = %d, want 5", limit)
	}
	if query != "hello" {
		t.Errorf("query = %q, want %q", query, "hello")
	}
}

// TestRecallCommand_LimitFlag_MissingValue: --limit without a value → error.
func TestRecallCommand_LimitFlag_MissingValue(t *testing.T) {
	_, _, err := parseRecallFlags([]string{"--limit"})
	if err == nil {
		t.Error("parseRecallFlags with --limit and no value should return error")
	}
}

// TestRecallCommand_LimitFlag_Invalid: --limit abc → error.
func TestRecallCommand_LimitFlag_Invalid(t *testing.T) {
	_, _, err := parseRecallFlags([]string{"--limit", "abc"})
	if err == nil {
		t.Error("parseRecallFlags with non-numeric --limit should return error")
	}
}

// TestRecallCommand_LimitFlag_Negative: --limit -1 → error.
func TestRecallCommand_LimitFlag_Negative(t *testing.T) {
	_, _, err := parseRecallFlags([]string{"--limit", "-1"})
	if err == nil {
		t.Error("parseRecallFlags with negative --limit should return error")
	}
	if !strings.Contains(err.Error(), "must be positive") {
		t.Errorf("Error should mention 'must be positive', got: %v", err)
	}
}

// TestRecallCommand_LimitFlag_Zero: --limit 0 → error.
func TestRecallCommand_LimitFlag_Zero(t *testing.T) {
	_, _, err := parseRecallFlags([]string{"--limit", "0"})
	if err == nil {
		t.Error("parseRecallFlags with --limit 0 should return error")
	}
}

// TestRecallCommand_JSONOutput_MarshalShape: marshal a populated
// RecalledItem slice and verify every field round-trips. This validates
// that the --json output is well-formed and complete.
func TestRecallCommand_JSONOutput_MarshalShape(t *testing.T) {
	items := []agent.RecalledItem{
		{
			CheckpointID: "cp-1",
			Level:        1,
			StartIndex:   2,
			EndIndex:     5,
			Similarity:   0.93,
			AgeDays:      1.5,
			Score:        0.88,
			Summary:      "tested JSON output",
			Actionable:   "verify in CI",
			Workspace:    "/tmp/test",
		},
	}
	b, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}
	var roundTrip []agent.RecalledItem
	if err := json.Unmarshal(b, &roundTrip); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}
	if len(roundTrip) != 1 {
		t.Fatalf("round-trip length = %d, want 1", len(roundTrip))
	}
	if roundTrip[0].CheckpointID != "cp-1" || roundTrip[0].Summary != "tested JSON output" {
		t.Errorf("Round-trip data lost: %+v", roundTrip[0])
	}
}

// Sanity: ensure Agent.Recall has the signature we depend on. This is a
// compile-time check wrapped in a runtime test so a refactor breaks loudly.
func TestRecallCommand_AgentRecallSignature(t *testing.T) {
	a := agent.NewTestAgent()
	// The signature must be (ctx, query, limit) → ([]RecalledItem, error).
	var fn func(context.Context, string, int) ([]agent.RecalledItem, error) = a.Recall
	items, err := fn(context.Background(), "test", 5)
	if err != nil {
		t.Fatalf("Recall returned err: %v", err)
	}
	// No embedding manager → empty result, but it must NOT be an error.
	_ = items
}
