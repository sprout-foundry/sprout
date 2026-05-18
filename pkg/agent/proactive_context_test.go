package agent

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// ========================================================================
// formatRelativeTime tests
// ========================================================================

func TestFormatRelativeTime_JustNow(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	// 0 seconds
	if got := formatRelativeTime(now, now); got != "just now" {
		t.Errorf("0s: expected 'just now', got %q", got)
	}

	// 30 seconds
	if got := formatRelativeTime(now.Add(-30*time.Second), now); got != "just now" {
		t.Errorf("30s: expected 'just now', got %q", got)
	}

	// 59 seconds
	if got := formatRelativeTime(now.Add(-59*time.Second), now); got != "just now" {
		t.Errorf("59s: expected 'just now', got %q", got)
	}
}

func TestFormatRelativeTime_Minutes(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		ago     time.Duration
		want    string
	}{
		{1 * time.Minute, "1 minute ago"},
		{1*time.Minute + 30*time.Second, "1 minute ago"},
		{5 * time.Minute, "5 minutes ago"},
		{10 * time.Minute, "10 minutes ago"},
		{30 * time.Minute, "30 minutes ago"},
		{59 * time.Minute, "59 minutes ago"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			got := formatRelativeTime(now.Add(-tc.ago), now)
			if got != tc.want {
				t.Errorf("%v ago: expected %q, got %q", tc.ago, tc.want, got)
			}
		})
	}
}

func TestFormatRelativeTime_Hours(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		ago  time.Duration
		want string
	}{
		{1 * time.Hour, "1 hour ago"},
		{1*time.Hour + 30*time.Minute, "1 hour ago"},
		{3 * time.Hour, "3 hours ago"},
		{12 * time.Hour, "12 hours ago"},
		{23 * time.Hour, "23 hours ago"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			got := formatRelativeTime(now.Add(-tc.ago), now)
			if got != tc.want {
				t.Errorf("%v ago: expected %q, got %q", tc.ago, tc.want, got)
			}
		})
	}
}

func TestFormatRelativeTime_Days(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		ago  time.Duration
		want string
	}{
		{24 * time.Hour, "1 day ago"},
		{24*time.Hour + 12*time.Hour, "1 day ago"},
		{2 * 24 * time.Hour, "2 days ago"},
		{5 * 24 * time.Hour, "5 days ago"},
		{6 * 24 * time.Hour, "6 days ago"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			got := formatRelativeTime(now.Add(-tc.ago), now)
			if got != tc.want {
				t.Errorf("%v ago: expected %q, got %q", tc.ago, tc.want, got)
			}
		})
	}
}

func TestFormatRelativeTime_Weeks(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		ago  time.Duration
		want string
	}{
		{7 * 24 * time.Hour,     "1 week ago"},
		{7*24*time.Hour + 12*time.Hour, "1 week ago"},
		{2 * 7 * 24 * time.Hour, "2 weeks ago"},
		{3 * 7 * 24 * time.Hour, "3 weeks ago"},
		{4 * 7 * 24 * time.Hour, "4 weeks ago"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			got := formatRelativeTime(now.Add(-tc.ago), now)
			if got != tc.want {
				t.Errorf("%v ago: expected %q, got %q", tc.ago, tc.want, got)
			}
		})
	}
}

func TestFormatRelativeTime_Months(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		ago  time.Duration
		want string
	}{
		{30 * 24 * time.Hour,     "1 month ago"},
		{30*24*time.Hour + 12*time.Hour, "1 month ago"},
		{60 * 24 * time.Hour,     "2 months ago"},
		{90 * 24 * time.Hour,     "3 months ago"},
		{180 * 24 * time.Hour,    "6 months ago"},
		{365 * 24 * time.Hour,    "12 months ago"},
	}

	for _, tc := range tests {
		t.Run(tc.want, func(t *testing.T) {
			got := formatRelativeTime(now.Add(-tc.ago), now)
			if got != tc.want {
				t.Errorf("%v ago: expected %q, got %q", tc.ago, tc.want, got)
			}
		})
	}
}

func TestFormatRelativeTime_FutureTimestamp(t *testing.T) {
	now := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

	// Future timestamps should return "just now" (not negative time)
	tests := []time.Duration{
		1 * time.Minute,
		1 * time.Hour,
		24 * time.Hour,
		30 * 24 * time.Hour,
	}

	for _, d := range tests {
		got := formatRelativeTime(now.Add(d), now)
		if got != "just now" {
			t.Errorf("future +%.0fh: expected 'just now', got %q", d.Hours(), got)
		}
	}
}

// ========================================================================
// DefaultProactiveContextConfig tests
// ========================================================================

func TestDefaultProactiveContextConfig(t *testing.T) {
	cfg := DefaultProactiveContextConfig()

	if cfg.MinRelevanceScore != 0.50 {
		t.Errorf("MinRelevanceScore: expected 0.50, got %f", cfg.MinRelevanceScore)
	}
	if cfg.MaxContextualResults != 5 {
		t.Errorf("MaxContextualResults: expected 5, got %d", cfg.MaxContextualResults)
	}
	if cfg.MaxContextChars != 4000 {
		t.Errorf("MaxContextChars: expected 4000, got %d", cfg.MaxContextChars)
	}
	if cfg.WorkspaceScoped != false {
		t.Errorf("WorkspaceScoped: expected false, got %v", cfg.WorkspaceScoped)
	}
}

// ========================================================================
// ProactiveContextConfig.resolve() tests
// ========================================================================

func TestProactiveContextConfig_Resolve_AllZero(t *testing.T) {
	cfg := ProactiveContextConfig{}
	resolved := cfg.resolve()

	d := DefaultProactiveContextConfig()
	if resolved.MinRelevanceScore != d.MinRelevanceScore {
		t.Errorf("MinRelevanceScore: expected %f, got %f", d.MinRelevanceScore, resolved.MinRelevanceScore)
	}
	if resolved.MaxContextualResults != d.MaxContextualResults {
		t.Errorf("MaxContextualResults: expected %d, got %d", d.MaxContextualResults, resolved.MaxContextualResults)
	}
	if resolved.MaxContextChars != d.MaxContextChars {
		t.Errorf("MaxContextChars: expected %d, got %d", d.MaxContextChars, resolved.MaxContextChars)
	}
	if resolved.WorkspaceScoped != d.WorkspaceScoped {
		t.Errorf("WorkspaceScoped: expected %v, got %v", d.WorkspaceScoped, resolved.WorkspaceScoped)
	}
}

func TestProactiveContextConfig_Resolve_NonZeroPreserved(t *testing.T) {
	cfg := ProactiveContextConfig{
		MinRelevanceScore:    0.80,
		MaxContextualResults: 10,
		MaxContextChars:      8000,
		WorkspaceScoped:      true,
	}
	resolved := cfg.resolve()

	if resolved.MinRelevanceScore != 0.80 {
		t.Errorf("MinRelevanceScore: expected 0.80, got %f", resolved.MinRelevanceScore)
	}
	if resolved.MaxContextualResults != 10 {
		t.Errorf("MaxContextualResults: expected 10, got %d", resolved.MaxContextualResults)
	}
	if resolved.MaxContextChars != 8000 {
		t.Errorf("MaxContextChars: expected 8000, got %d", resolved.MaxContextChars)
	}
	if resolved.WorkspaceScoped != true {
		t.Errorf("WorkspaceScoped: expected true, got %v", resolved.WorkspaceScoped)
	}
}

func TestProactiveContextConfig_Resolve_PartialOverride(t *testing.T) {
	// Only override MinRelevanceScore; rest should get defaults
	cfg := ProactiveContextConfig{
		MinRelevanceScore: 0.90,
	}
	resolved := cfg.resolve()

	if resolved.MinRelevanceScore != 0.90 {
		t.Errorf("MinRelevanceScore: expected 0.90, got %f", resolved.MinRelevanceScore)
	}
	if resolved.MaxContextualResults != 5 {
		t.Errorf("MaxContextualResults: expected 5 (default), got %d", resolved.MaxContextualResults)
	}
	if resolved.MaxContextChars != 4000 {
		t.Errorf("MaxContextChars: expected 4000 (default), got %d", resolved.MaxContextChars)
	}
	if resolved.WorkspaceScoped != false {
		t.Errorf("WorkspaceScoped: expected false (default), got %v", resolved.WorkspaceScoped)
	}
}

func TestProactiveContextConfig_Resolve_NegativeValuesUseDefaults(t *testing.T) {
	// Negative values should be replaced by defaults (<= 0 check)
	cfg := ProactiveContextConfig{
		MinRelevanceScore:    -0.5,
		MaxContextualResults: -1,
		MaxContextChars:      -100,
	}
	resolved := cfg.resolve()

	d := DefaultProactiveContextConfig()
	if resolved.MinRelevanceScore != d.MinRelevanceScore {
		t.Errorf("MinRelevanceScore: expected default %f, got %f", d.MinRelevanceScore, resolved.MinRelevanceScore)
	}
	if resolved.MaxContextualResults != d.MaxContextualResults {
		t.Errorf("MaxContextualResults: expected default %d, got %d", d.MaxContextualResults, resolved.MaxContextualResults)
	}
	if resolved.MaxContextChars != d.MaxContextChars {
		t.Errorf("MaxContextChars: expected default %d, got %d", d.MaxContextChars, resolved.MaxContextChars)
	}
}

func TestProactiveContextConfig_Resolve_DoesNotMutateOriginal(t *testing.T) {
	cfg := ProactiveContextConfig{
		MinRelevanceScore: 0.0,
	}
	originalScore := cfg.MinRelevanceScore

	_ = cfg.resolve()

	if cfg.MinRelevanceScore != originalScore {
		t.Error("resolve() should not mutate the original config")
	}
}

// ========================================================================
// FormatProactiveContext tests
// ========================================================================

func TestFormatProactiveContext_EmptyResults(t *testing.T) {
	result := FormatProactiveContext(nil, DefaultProactiveContextConfig(), time.Now().UTC())
	if result != "" {
		t.Errorf("expected empty string for nil results, got %q", result)
	}

	result = FormatProactiveContext([]ProactiveContextResult{}, DefaultProactiveContextConfig(), time.Now().UTC())
	if result != "" {
		t.Errorf("expected empty string for empty results, got %q", result)
	}
}

func TestFormatProactiveContext_SingleResult(t *testing.T) {
	now := time.Now().UTC()
	results := []ProactiveContextResult{
		{
			Record: embedding.VectorRecord{
				ID:        "turn:1",
				Signature: "How do I implement a REST API?",
				IndexedAt: now.Add(-1 * time.Hour),
				Metadata: map[string]interface{}{
					"actionableSummary": "Implement REST API using net/http",
				},
			},
			Score: 0.85,
		},
	}

	output := FormatProactiveContext(results, DefaultProactiveContextConfig(), time.Now().UTC())

	if !strings.Contains(output, "## Previous Work (Contextual Memory)") {
		t.Error("output should contain the header")
	}
	if !strings.Contains(output, "How do I implement a REST API?") {
		t.Error("output should contain the prompt signature in header")
	}
	if !strings.Contains(output, "1 hour ago") {
		t.Error("output should contain '1 hour ago'")
	}
	if !strings.Contains(output, `User: "How do I implement a REST API?"`) {
		t.Error("output should contain the user prompt in quotes")
	}
	if !strings.Contains(output, "Summary: Implement REST API using net/http") {
		t.Error("output should contain the actionable summary")
	}
}

func TestFormatProactiveContext_MultipleResults(t *testing.T) {
	now := time.Now().UTC()
	results := []ProactiveContextResult{
		{
			Record: embedding.VectorRecord{
				ID:        "turn:1",
				Signature: "First query about Go",
				IndexedAt: now.Add(-1 * time.Hour),
				Metadata: map[string]interface{}{
					"actionableSummary": "First summary",
				},
			},
			Score: 0.90,
		},
		{
			Record: embedding.VectorRecord{
				ID:        "turn:2",
				Signature: "Second query about channels",
				IndexedAt: now.Add(-2 * time.Hour),
				Metadata: map[string]interface{}{
					"actionableSummary": "Second summary",
				},
			},
			Score: 0.80,
		},
	}

	output := FormatProactiveContext(results, DefaultProactiveContextConfig(), time.Now().UTC())

	if !strings.Contains(output, "First query about Go") {
		t.Error("should contain first result")
	}
	if !strings.Contains(output, "Second query about channels") {
		t.Error("should contain second result")
	}
	if !strings.Contains(output, "First summary") {
		t.Error("should contain first summary")
	}
	if !strings.Contains(output, "Second summary") {
		t.Error("should contain second summary")
	}
}

func TestFormatProactiveContext_Truncation(t *testing.T) {
	now := time.Now().UTC()

	// Create results that will exceed the budget when formatted
	longPrompt := strings.Repeat("X", 1000)
	longSummary := strings.Repeat("Y", 1000)

	results := make([]ProactiveContextResult, 5)
	for i := range results {
		results[i] = ProactiveContextResult{
			Record: embedding.VectorRecord{
				Signature: longPrompt,
				IndexedAt: now,
				Metadata: map[string]interface{}{
					"actionableSummary": longSummary,
				},
			},
			Score: 0.9,
		}
	}

	// Set a very small budget to force truncation
	config := DefaultProactiveContextConfig()
	config.MaxContextChars = 500

	output := FormatProactiveContext(results, config, time.Now().UTC())

	if len(output) > config.MaxContextChars+20 {
		// Allow small tolerance for the "[Context truncated...]" suffix
		t.Errorf("output too long: expected <= %d + suffix, got %d", config.MaxContextChars, len(output))
	}

	if !strings.Contains(output, "[Context truncated...]") {
		t.Error("truncated output should contain '[Context truncated...]'")
	}
}

func TestFormatProactiveContext_EmptySignature(t *testing.T) {
	now := time.Now().UTC()
	results := []ProactiveContextResult{
		{
			Record: embedding.VectorRecord{
				ID:        "turn:empty",
				Signature: "",
				IndexedAt: now,
				Metadata:  map[string]interface{}{},
			},
			Score: 0.5,
		},
	}

	output := FormatProactiveContext(results, DefaultProactiveContextConfig(), time.Now().UTC())

	if !strings.Contains(output, "No prompt available") {
		t.Error("empty signature should show 'No prompt available' in header")
	}
	if !strings.Contains(output, `User: ""`) {
		t.Error("empty signature should show 'User: \"\"'")
	}
}

func TestFormatProactiveContext_NoSummaryAvailable(t *testing.T) {
	now := time.Now().UTC()
	results := []ProactiveContextResult{
		{
			Record: embedding.VectorRecord{
				Signature: "My query",
				IndexedAt: now,
				Metadata:  map[string]interface{}{}, // no actionableSummary key
			},
			Score: 0.7,
		},
	}

	output := FormatProactiveContext(results, DefaultProactiveContextConfig(), time.Now().UTC())

	if !strings.Contains(output, "No summary available") {
		t.Error("missing summary should show 'No summary available'")
	}
}

func TestFormatProactiveContext_EmptySummary(t *testing.T) {
	now := time.Now().UTC()
	results := []ProactiveContextResult{
		{
			Record: embedding.VectorRecord{
				Signature: "My query",
				IndexedAt: now,
				Metadata: map[string]interface{}{
					"actionableSummary": "", // present but empty
				},
			},
			Score: 0.7,
		},
	}

	output := FormatProactiveContext(results, DefaultProactiveContextConfig(), time.Now().UTC())

	if !strings.Contains(output, "No summary available") {
		t.Error("empty summary string should show 'No summary available'")
	}
}

func TestFormatProactiveContext_MultilineSignatureTruncatedInHeader(t *testing.T) {
	now := time.Now().UTC()
	results := []ProactiveContextResult{
		{
			Record: embedding.VectorRecord{
				Signature: "First line\nSecond line that should be dropped in header",
				IndexedAt: now,
				Metadata: map[string]interface{}{
					"actionableSummary": "A summary",
				},
			},
			Score: 0.8,
		},
	}

	output := FormatProactiveContext(results, DefaultProactiveContextConfig(), time.Now().UTC())

	// Header should show only "First line", not the second line
	if strings.Contains(output, "### First line") {
		// Good — header has first line only
	} else {
		t.Error("header should start with '### First line'")
	}

	// The full signature should still appear in the User: line
	if !strings.Contains(output, `User: "First line`) && !strings.Contains(output, `User: "First line\n`) {
		t.Error("User field should contain the full multi-line signature")
	}
}

func TestFormatProactiveContext_LongSignatureTruncated(t *testing.T) {
	now := time.Now().UTC()
	longSig := strings.Repeat("A", 200)
	results := []ProactiveContextResult{
		{
			Record: embedding.VectorRecord{
				Signature: longSig,
				IndexedAt: now,
				Metadata: map[string]interface{}{
					"actionableSummary": "Summary",
				},
			},
			Score: 0.8,
		},
	}

	output := FormatProactiveContext(results, DefaultProactiveContextConfig(), time.Now().UTC())

	// Header text should be truncated to 80 chars + "..."
	headerLine := ""
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "### ") {
			headerLine = line
			break
		}
	}
	if headerLine == "" {
		t.Fatal("no header line found")
	}

	headerContent := strings.TrimPrefix(headerLine, "### ")
	// Before the " (just now)" suffix
	idx := strings.Index(headerContent, " (")
	if idx < 0 {
		t.Fatalf("no time suffix found in header: %q", headerContent)
	}
	titlePart := headerContent[:idx]

	if len(titlePart) > 80 {
		t.Errorf("header title too long: expected <= 80 chars, got %d", len(titlePart))
	}
	if !strings.HasSuffix(titlePart, "...") {
		t.Errorf("truncated header title should end with '...', got %q", titlePart)
	}
}

// ========================================================================
// RetrieveProactiveContext integration tests
// ========================================================================

// setupManager creates an EmbeddingManager in a temp dir and initializes it.
// Returns the manager, store, and a cleanup function.
func setupProactiveManager(t *testing.T) (*embedding.EmbeddingManager, *embedding.ConversationStore) {
	t.Helper()
	ctx := context.Background()
	tempDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("LEDIT_CONFIG", tempDir)

	cfg := &configuration.EmbeddingIndexConfig{IndexDir: tempDir}
	mgr := embedding.NewEmbeddingManager(cfg, tempDir)

	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("failed to init embedding manager: %v", err)
	}

	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		t.Fatalf("failed to get conversation store: %v", err)
	}

	return mgr, store
}

func TestRetrieveProactiveContext_NilManager(t *testing.T) {
	ctx := context.Background()
	results, err := RetrieveProactiveContext(
		ctx, nil, DefaultProactiveContextConfig(),
		"test query", "/tmp/workspace", time.Now().UTC(),
	)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results, got %v", results)
	}
}

func TestRetrieveProactiveContext_NilContext(t *testing.T) {
	mgr, _ := setupProactiveManager(t)
	defer mgr.Close()

	results, err := RetrieveProactiveContext(
		nil, mgr, DefaultProactiveContextConfig(),
		"test query", "/tmp/workspace", time.Now().UTC(),
	)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results, got %v", results)
	}
}

func TestRetrieveProactiveContext_EmptyQuery(t *testing.T) {
	ctx := context.Background()
	mgr, _ := setupProactiveManager(t)
	defer mgr.Close()

	results, err := RetrieveProactiveContext(
		ctx, mgr, DefaultProactiveContextConfig(),
		"", "/tmp/workspace", time.Now().UTC(),
	)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results for empty query, got %v", results)
	}
}

func TestRetrieveProactiveContext_EmptyStore(t *testing.T) {
	ctx := context.Background()
	mgr, _ := setupProactiveManager(t)
	defer mgr.Close()

	// Store is empty — no turns stored yet
	results, err := RetrieveProactiveContext(
		ctx, mgr, DefaultProactiveContextConfig(),
		"some query", "/tmp/workspace", time.Now().UTC(),
	)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results for empty store, got %v", results)
	}
}

func TestRetrieveProactiveContext_HappyPath(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	mgr, _ := setupProactiveManager(t)
	defer mgr.Close()

	// Create a turn about REST APIs and store it
	turn, err := NewConversationTurn("session-1", 1,
		"How do I implement a REST API in Go?", "/tmp/workspace")
	if err != nil {
		t.Fatalf("failed to create turn: %v", err)
	}
	turn.ActionableSummary = "Implement a REST API using net/http package"
	turn.Timestamp = now.Add(-1 * time.Hour)

	// Store the turn using the existing embedding helper
	if err := EmbedAndStoreTurn(ctx, mgr, turn); err != nil {
		t.Fatalf("failed to embed and store turn: %v", err)
	}

	// Query with a semantically similar query
	results, err := RetrieveProactiveContext(
		ctx, mgr, DefaultProactiveContextConfig(),
		"How to build a REST API in Go?",
		"/tmp/workspace", now,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}

	// The result should match our stored turn
	result := results[0]
	if result.Record.ID != turn.ID {
		t.Errorf("expected result ID %s, got %s", turn.ID, result.Record.ID)
	}

	if result.Score <= 0 {
		t.Errorf("expected positive score, got %f", result.Score)
	}

	// Score should be high for a similar query (above default min)
	if result.Score < 0.50 {
		t.Errorf("score too low for similar query: %f", result.Score)
	}
}

func TestRetrieveProactiveContext_WorkspaceScopedFiltering(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	mgr, _ := setupProactiveManager(t)
	defer mgr.Close()

	// Store turns in different workspaces
	turnA, err := NewConversationTurn("session-a", 1,
		"How do I implement a REST API?", "/workspace-a")
	if err != nil {
		t.Fatalf("failed to create turn: %v", err)
	}
	turnA.ActionableSummary = "Implement REST API"
	turnA.Timestamp = now.Add(-1 * time.Hour)

	turnB, err := NewConversationTurn("session-b", 1,
		"How do I implement a REST API?", "/workspace-b")
	if err != nil {
		t.Fatalf("failed to create turn: %v", err)
	}
	turnB.ActionableSummary = "Implement REST API"
	turnB.Timestamp = now.Add(-1 * time.Hour)

	if err := EmbedAndStoreTurn(ctx, mgr, turnA); err != nil {
		t.Fatalf("failed to store turn A: %v", err)
	}
	if err := EmbedAndStoreTurn(ctx, mgr, turnB); err != nil {
		t.Fatalf("failed to store turn B: %v", err)
	}

	// Query with workspace scoped to /workspace-a only
	config := DefaultProactiveContextConfig()
	config.WorkspaceScoped = true

	results, err := RetrieveProactiveContext(
		ctx, mgr, config,
		"REST API implementation", "/workspace-a", now,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected at least one result from workspace-a")
	}

	// Should only return turnA from workspace-a
	for _, r := range results {
		wd, ok := r.Record.Metadata["workingDir"].(string)
		if !ok || wd != "/workspace-a" {
			t.Errorf("expected workingDir '/workspace-a', got %v", r.Record.Metadata["workingDir"])
		}
	}
}

// ========================================================================
// Time decay deterministic tests (hand-crafted embeddings)
// ========================================================================

func TestRetrieveProactiveContext_TimeDecayDeterministic(t *testing.T) {
	/*
		SP-027-2e: Verify that RetrieveProactiveContext correctly applies
		time-decay scoring via ScoreWithDecay.

		Strategy: create hand-crafted embeddings with identical cosine
		similarity to the query, but at different ages. Verify that:
		- The decay formula (30-day half-life) produces expected scores
		- Results are sorted by descending score
		- Results are capped at MaxContextualResults
	*/
	ctx := context.Background()
	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC) // fixed time for determinism
	mgr, store := setupProactiveManager(t)
	defer mgr.Close()

	provider := store.Provider()

	// Get the query embedding once — all records will have the same similarity
	queryEmb, err := provider.Embed(ctx, "How do I implement a REST API?")
	if err != nil {
		t.Fatalf("failed to embed query: %v", err)
	}

	// Create 8 records with the SAME embedding (cosine similarity = 1.0) but
	// different ages. This isolates the time-decay component.
	type recordSpec struct {
		id      string
		daysAgo int
	}
	specs := []recordSpec{
		{"rec-today", 0},
		{"rec-1day", 1},
		{"rec-7day", 7},
		{"rec-14day", 14},
		{"rec-30day", 30},
		{"rec-60day", 60},
		{"rec-90day", 90},
		{"rec-180day", 180},
	}

	for _, spec := range specs {
		record := embedding.VectorRecord{
			ID:        spec.id,
			Type:      "conversation_turn",
			Signature: "How do I implement a REST API?",
			Embedding: make([]float32, len(queryEmb)),
			IndexedAt: now.Add(-time.Duration(spec.daysAgo) * 24 * time.Hour),
			Metadata: map[string]interface{}{
				"workingDir":        "/test/ws",
				"actionableSummary": "Implement REST API",
			},
		}
		copy(record.Embedding, queryEmb) // identical → cosine = 1.0
		if err := store.Store([]embedding.VectorRecord{record}); err != nil {
			t.Fatalf("failed to store %s: %v", spec.id, err)
		}
	}

	// Set max results to 5 (default), so oldest records get cut off
	config := DefaultProactiveContextConfig()
	config.MaxContextualResults = 5

	results, err := RetrieveProactiveContext(
		ctx, mgr, config,
		"How do I implement a REST API?",
		"/test/ws", now,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected results from deterministic embeddings")
	}

	// Verify cap
	if len(results) > config.MaxContextualResults {
		t.Errorf("expected at most %d results, got %d", config.MaxContextualResults, len(results))
	}

	// Verify that the oldest record (rec-180day) is excluded by the cap
	for _, r := range results {
		if r.Record.ID == "rec-180day" {
			t.Errorf("rec-180day should be excluded by cap (oldest, lowest score), but it was returned")
		}
	}

	// Verify descending order
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted descending: [%d]=%.6f > [%d]=%.6f",
				i, results[i].Score, i-1, results[i-1].Score)
		}
	}

	// Verify the decay formula for each result
	// With identical embeddings, cosine similarity = 1.0, so decayed score = decay factor
	tolerance := 0.01 // allow small float32→float64 conversion error
	for _, r := range results {
		// Find the daysAgo for this record
		var daysAgo int
		for _, spec := range specs {
			if spec.id == r.Record.ID {
				daysAgo = spec.daysAgo
				break
			}
		}

		// Expected: 1.0 * 0.5^(daysAgo/30)
		expectedDecay := math.Pow(0.5, float64(daysAgo)/30.0)
		if math.Abs(r.Score-expectedDecay) > tolerance {
			t.Errorf("record %s: score %.6f, expected %.6f (decay for %d days)",
				r.Record.ID, r.Score, expectedDecay, daysAgo)
		}
	}

	// Verify that the most recent record is first (highest decay factor ≈ 1.0)
	if results[0].Record.ID != "rec-today" {
		t.Errorf("expected most recent record first, got %s", results[0].Record.ID)
	}

	// Verify 30-day record scores ≈ half of today's score
	var todayScore, thirtyDayScore float64
	for _, r := range results {
		if r.Record.ID == "rec-today" {
			todayScore = r.Score
		}
		if r.Record.ID == "rec-30day" {
			thirtyDayScore = r.Score
		}
	}
	if todayScore > 0 {
		ratio := thirtyDayScore / todayScore
		if math.Abs(ratio-0.5) > tolerance {
			t.Errorf("30-day half-life: ratio %.6f, expected ~0.5", ratio)
		}
	}
}

func TestRetrieveProactiveContext_DifferentSimilarities(t *testing.T) {
	/*
		Verify that records with different cosine similarities are correctly
		scored and sorted, even when combined with time decay.

		Strategy: Use provider embeddings with verifiable similarity relationships:
		- Get embedding for "implement REST API" - this is our query
		- Use the same embedding for records with high similarity (similarity = 1.0)
		- Get embedding for a different but related topic for medium similarity
		- Get embedding for a completely different topic for low similarity
	*/
	ctx := context.Background()
	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	mgr, store := setupProactiveManager(t)
	defer mgr.Close()

	provider := store.Provider()

	// Get embeddings for different strings to create known similarity relationships
	// The query will be "implement REST API"
	queryEmb, err := provider.Embed(ctx, "implement REST API")
	if err != nil {
		t.Fatalf("failed to embed query: %v", err)
	}

	// Get embedding for a related topic (should have moderate similarity)
	relatedEmb, err := provider.Embed(ctx, "build HTTP endpoints")
	if err != nil {
		t.Fatalf("failed to embed related: %v", err)
	}

	// Get embedding for a completely different topic (should have low similarity)
	differentEmb, err := provider.Embed(ctx, "bake chocolate chip cookies")
	if err != nil {
		t.Fatalf("failed to embed different: %v", err)
	}

	// Verify cosine similarity expectations
	relatedSim := embedding.CosineSimilarity(queryEmb, relatedEmb)
	differentSim := embedding.CosineSimilarity(queryEmb, differentEmb)

	t.Logf("query vs related similarity: %.4f (expected > 0.5)", relatedSim)
	t.Logf("query vs different similarity: %.4f (expected < 0.5)", differentSim)

	if relatedSim <= 0.5 {
		t.Errorf("query vs related similarity %.4f is not > 0.5", relatedSim)
	}

	// Store records with different embeddings at different ages
	// Record A: high similarity (same as query), 1 hour ago (recent)
	// Record B: high similarity (same as query), 90 days ago (old)
	// Record C: medium similarity (related), just now (recent)
	// Record D: low similarity (different), 1 day ago (old and irrelevant)

	records := []embedding.VectorRecord{
		{
			ID:        "rec-A",
			Type:      "conversation_turn",
			Signature: "How do I implement a REST API?",
			Embedding: queryEmb,
			IndexedAt: now.Add(-1 * time.Hour),
			Metadata: map[string]interface{}{
				"workingDir":        "/test/ws",
				"actionableSummary": "Implement REST API",
			},
		},
		{
			ID:        "rec-B",
			Type:      "conversation_turn",
			Signature: "How do I implement a REST API?",
			Embedding: queryEmb,
			IndexedAt: now.Add(-90 * 24 * time.Hour),
			Metadata: map[string]interface{}{
				"workingDir":        "/test/ws",
				"actionableSummary": "Implement REST API",
			},
		},
		{
			ID:        "rec-C",
			Type:      "conversation_turn",
			Signature: "How do I build HTTP endpoints?",
			Embedding: relatedEmb,
			IndexedAt: now,
			Metadata: map[string]interface{}{
				"workingDir":        "/test/ws",
				"actionableSummary": "Build HTTP endpoints",
			},
		},
		{
			ID:        "rec-D",
			Type:      "conversation_turn",
			Signature: "How do I bake cookies?",
			Embedding: differentEmb,
			IndexedAt: now.Add(-24 * time.Hour),
			Metadata: map[string]interface{}{
				"workingDir":        "/test/ws",
				"actionableSummary": "Bake cookies",
			},
		},
	}

	for _, rec := range records {
		if err := store.Store([]embedding.VectorRecord{rec}); err != nil {
			t.Fatalf("failed to store %s: %v", rec.ID, err)
		}
	}

	// Query with "implement REST API" - this will create an embedding
	// identical to what we stored for rec-A and rec-B (embedding provider is deterministic)
	results, err := RetrieveProactiveContext(
		ctx, mgr, DefaultProactiveContextConfig(),
		"implement REST API",
		"/test/ws", now,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Build a map of returned results by ID
	resultMap := make(map[string]ProactiveContextResult)
	for _, r := range results {
		resultMap[r.Record.ID] = r
	}

	// rec-A should always be in results (highest similarity = 1.0, recent)
	if _, ok := resultMap["rec-A"]; !ok {
		t.Error("rec-A (high-sim, recent) should be in results")
	}

	// rec-B should be in results (high similarity = 1.0, though decayed)
	// but should have a lower score than rec-A
	if scoreA, hasA := resultMap["rec-A"]; hasA {
		if scoreB, hasB := resultMap["rec-B"]; hasB {
			if scoreB.Score >= scoreA.Score {
				t.Errorf("rec-B (90 days old) should score < rec-A (1 hour old), got %.6f vs %.6f",
					scoreB.Score, scoreA.Score)
			}
		}
	}

	// rec-C should be present if related similarity > 0.5
	// If related similarity <= 0.5, it will be filtered by MinRelevanceScore
	if relatedSim > 0.5 {
		if _, ok := resultMap["rec-C"]; !ok {
			t.Errorf("rec-C (medium-sim %.4f > 0.5, very recent) should be in results", relatedSim)
		}
	}

	// rec-D should either be excluded or have a very low score (below 0.5 threshold)
	if scoreD, hasD := resultMap["rec-D"]; hasD {
		t.Logf("rec-D (low-sim %.4f) is included with score %.6f", differentSim, scoreD.Score)
		// This is unusual - low similarity might still pass if it's high enough
		if scoreD.Score >= 0.5 {
			t.Logf("warning: rec-D has score %.6f >= 0.5 despite low similarity %.4f", scoreD.Score, differentSim)
		}
	}

	// Results should be sorted in descending order by score
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted descending: [%d]=%.6f > [%d]=%.6f",
				i, results[i].Score, i-1, results[i-1].Score)
		}
	}

	// At minimum, rec-A and rec-B should be returned (high similarity = 1.0)
	// rec-C may or may not be returned depending on related similarity
	if len(results) < 2 {
		t.Errorf("expected at least 2 results (rec-A, rec-B with similarity = 1.0), got %d", len(results))
	}
}

func TestRetrieveProactiveContext_AllScoresBelowThreshold(t *testing.T) {
	/*
		SP-027-2e: Verify graceful no-op when all records score below
		MinRelevanceScore. No error should be returned.

		Uses hand-crafted orthogonal embeddings for deterministic behavior:
		- Create a query embedding
		- Create an orthogonal embedding by negating the query embedding (cosine similarity = -1.0)
		- Store a record with the negated embedding
		- Query with default threshold of 0.5
		- Assert results is nil (negative similarity * any decay = negative, always below 0.5)
	*/
	ctx := context.Background()
	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	mgr, store := setupProactiveManager(t)
	defer mgr.Close()

	provider := store.Provider()

	// Get a query embedding
	queryEmb, err := provider.Embed(ctx, "How do I implement a REST API?")
	if err != nil {
		t.Fatalf("failed to embed query: %v", err)
	}

	// Create an orthogonal embedding by negating the query embedding
	// Cosine similarity with query will be -1.0 (completely opposite)
	orthogonalEmb := make([]float32, len(queryEmb))
	for i, v := range queryEmb {
		orthogonalEmb[i] = -v
	}

	// Store a record with the orthogonal embedding
	record := embedding.VectorRecord{
		ID:        "rec-orthogonal",
		Type:      "conversation_turn",
		Signature: "Completely unrelated topic",
		Embedding: orthogonalEmb,
		IndexedAt: now,
		Metadata: map[string]interface{}{
			"workingDir":        "/test/ws",
			"actionableSummary": "Unrelated summary",
		},
	}

	if err := store.Store([]embedding.VectorRecord{record}); err != nil {
		t.Fatalf("failed to store record: %v", err)
	}

	// Query with default threshold of 0.5
	// The orthogonal embedding has similarity = -1.0 with the query
	// Even with no time decay, -1.0 < 0.5, so no results should be returned
	config := DefaultProactiveContextConfig()
	config.MinRelevanceScore = 0.5

	results, err := RetrieveProactiveContext(
		ctx, mgr, config,
		"How to implement a REST API?",
		"/test/ws", now,
	)
	if err != nil {
		t.Errorf("expected nil error for graceful no-op, got %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results when all scores below threshold (orthogonal similarity = -1.0), got %d results", len(results))
	}
}

func TestRetrieveProactiveContext_WorkspaceScopedFalse(t *testing.T) {
	/*
		SP-027-2e: Verify that when WorkspaceScoped is false, ALL records
		regardless of workingDir are candidates for retrieval.
	*/
	ctx := context.Background()
	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	mgr, _ := setupProactiveManager(t)
	defer mgr.Close()

	// Store turns in DIFFERENT workspaces
	turns := []struct {
		session string
		dir     string
		prompt  string
	}{
		{"session-alpha", "/workspace-alpha", "How do I implement authentication?"},
		{"session-beta", "/workspace-beta", "How do I implement authentication?"},
		{"session-gamma", "/workspace-gamma", "How do I implement authentication?"},
	}

	for _, t2 := range turns {
		turn, err := NewConversationTurn(t2.session, 1, t2.prompt, t2.dir)
		if err != nil {
			t.Fatalf("failed to create turn: %v", err)
		}
		turn.ActionableSummary = "Implement authentication"
		turn.Timestamp = now.Add(-1 * time.Hour)
		if err := EmbedAndStoreTurn(ctx, mgr, turn); err != nil {
			t.Fatalf("failed to store turn: %v", err)
		}
	}

	// Query with WorkspaceScoped=false (default) — should return ALL matching records
	config := DefaultProactiveContextConfig()
	config.WorkspaceScoped = false

	results, err := RetrieveProactiveContext(
		ctx, mgr, config,
		"How to implement authentication?",
		"/any/workspace", now,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All 3 turns should be returned (same embedding = high similarity, recent = high decay)
	if len(results) < 3 {
		t.Errorf("expected at least 3 results (all workspaces), got %d", len(results))
	}

	// Verify results come from ALL different workspaces
	dirs := make(map[string]bool)
	for _, r := range results {
		if wd, ok := r.Record.Metadata["workingDir"].(string); ok {
			dirs[wd] = true
		}
	}

	expectedDirs := map[string]bool{
		"/workspace-alpha": true,
		"/workspace-beta": true,
		"/workspace-gamma": true,
	}
	for dir := range expectedDirs {
		if !dirs[dir] {
			t.Errorf("WorkspaceScoped=false: missing results from %s", dir)
		}
	}
}

func TestRetrieveProactiveContext_MaxResultsCap(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	mgr, _ := setupProactiveManager(t)
	defer mgr.Close()

	// Store 10 turns with the same content
	for i := 0; i < 10; i++ {
		turn, err := NewConversationTurn("session-cap", i+1,
			"How do I implement a REST API in Go?", "/tmp/workspace")
		if err != nil {
			t.Fatalf("failed to create turn %d: %v", i, err)
		}
		turn.ActionableSummary = "Implement REST API"
		turn.Timestamp = now.Add(-time.Duration(i) * time.Minute)
		if err := EmbedAndStoreTurn(ctx, mgr, turn); err != nil {
			t.Fatalf("failed to store turn %d: %v", i, err)
		}
	}

	// Default config caps at 5 results
	results, err := RetrieveProactiveContext(
		ctx, mgr, DefaultProactiveContextConfig(),
		"How to build a REST API?", "/tmp/workspace", now,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) > 5 {
		t.Errorf("expected at most 5 results, got %d", len(results))
	}
}

func TestRetrieveProactiveContext_MinRelevanceScoreFilter(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	mgr, _ := setupProactiveManager(t)
	defer mgr.Close()

	// Store a turn about a completely different topic
	turn, err := NewConversationTurn("session-irrelevant", 1,
		"How do I bake chocolate cookies?", "/tmp/workspace")
	if err != nil {
		t.Fatalf("failed to create turn: %v", err)
	}
	turn.ActionableSummary = "Bake chocolate cookies with butter and sugar"
	turn.Timestamp = now.Add(-1 * time.Hour)

	if err := EmbedAndStoreTurn(ctx, mgr, turn); err != nil {
		t.Fatalf("failed to store turn: %v", err)
	}

	// Query with a very different topic and high min score
	config := DefaultProactiveContextConfig()
	config.MinRelevanceScore = 0.99

	results, err := RetrieveProactiveContext(
		ctx, mgr, config,
		"How to deploy a Kubernetes cluster?",
		"/tmp/workspace", now,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With high threshold, irrelevant results should be filtered out
	// (Result may or may not be empty depending on embedding quality,
	// but the score threshold should filter it if it's below 0.99)
	for _, r := range results {
		if r.Score < config.MinRelevanceScore {
			t.Errorf("result score %f is below min threshold %f", r.Score, config.MinRelevanceScore)
		}
	}
}

func TestRetrieveProactiveContext_CustomConfigPreserved(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	mgr, _ := setupProactiveManager(t)
	defer mgr.Close()

	// Store 7 turns
	for i := 0; i < 7; i++ {
		turn, err := NewConversationTurn("session-custom", i+1,
			"How do I implement a REST API in Go?", "/tmp/workspace")
		if err != nil {
			t.Fatalf("failed to create turn %d: %v", i, err)
		}
		turn.ActionableSummary = "Implement REST API"
		turn.Timestamp = now.Add(-time.Duration(i) * time.Minute)
		if err := EmbedAndStoreTurn(ctx, mgr, turn); err != nil {
			t.Fatalf("failed to store turn %d: %v", i, err)
		}
	}

	// Use a custom MaxContextualResults of 3
	config := DefaultProactiveContextConfig()
	config.MaxContextualResults = 3

	results, err := RetrieveProactiveContext(
		ctx, mgr, config,
		"How to build a REST API?", "/tmp/workspace", now,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) > 3 {
		t.Errorf("expected at most 3 results (custom cap), got %d", len(results))
	}
}

func TestRetrieveProactiveContext_ZeroTimeUsesCurrent(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	mgr, _ := setupProactiveManager(t)
	defer mgr.Close()

	// Store a turn
	turn, err := NewConversationTurn("session-zero", 1,
		"How do I implement a REST API?", "/tmp/workspace")
	if err != nil {
		t.Fatalf("failed to create turn: %v", err)
	}
	turn.ActionableSummary = "Implement REST API"
	turn.Timestamp = now.Add(-1 * time.Hour)

	if err := EmbedAndStoreTurn(ctx, mgr, turn); err != nil {
		t.Fatalf("failed to store turn: %v", err)
	}

	// Pass zero time — should default to time.Now()
	results, err := RetrieveProactiveContext(
		ctx, mgr, DefaultProactiveContextConfig(),
		"How to build a REST API?",
		"/tmp/workspace", time.Time{},
	)
	if err != nil {
		t.Fatalf("unexpected error with zero time: %v", err)
	}

	// Should succeed without panicking and return results
	if len(results) == 0 {
		t.Fatal("expected at least one result even with zero time")
	}
}

// Test that RetrieveProactiveContext only processes conversation_turn records,
// and ignores records of other types.
func TestRetrieveProactiveContext_IgnoresNonConversationTurns(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	mgr, store := setupProactiveManager(t)
	defer mgr.Close()

	// Get the provider and create an embedding directly
	provider := store.Provider()

	// Create a non-conversation-turn record (e.g., code_unit)
	emb, err := provider.Embed(ctx, "How do I implement a REST API?")
	if err != nil {
		t.Fatalf("failed to embed: %v", err)
	}

	record := embedding.VectorRecord{
		ID:        "code:1",
		Type:      "code_unit", // NOT conversation_turn
		Signature: "func handler(w http.ResponseWriter, r *http.Request)",
		Embedding: emb,
		IndexedAt: now.Add(-1 * time.Hour),
		Metadata:  map[string]interface{}{"workingDir": "/tmp/workspace"},
	}

	if err := store.Store([]embedding.VectorRecord{record}); err != nil {
		t.Fatalf("failed to store record: %v", err)
	}

	// Query — should return nil because there are no conversation_turn records
	results, err := RetrieveProactiveContext(
		ctx, mgr, DefaultProactiveContextConfig(),
		"How to build a REST API?", "/tmp/workspace", now,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results (no conversation_turn records), got %d", len(results))
	}
}

// ========================================================================
// FormatProactiveContext + RetrieveProactiveContext round-trip
// ========================================================================

func TestProactiveContext_FullRoundTrip(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	mgr, _ := setupProactiveManager(t)
	defer mgr.Close()

	// Store a few turns
	turns := []struct {
		prompt  string
		summary string
	}{
		{"How do I use Go generics?", "Use type parameters with constraints from iter package"},
		{"What are Go interfaces?", "Interfaces are defined by method sets, implemented implicitly"},
		{"How to handle errors in Go?", "Use multi-value returns and error wrapping with fmt.Errorf and errors.Is"},
	}

	for i, tc := range turns {
		turn, err := NewConversationTurn("session-roundtrip", i+1, tc.prompt, "/tmp/workspace")
		if err != nil {
			t.Fatalf("failed to create turn: %v", err)
		}
		turn.ActionableSummary = tc.summary
		turn.Timestamp = now.Add(-time.Duration(i+1) * time.Hour)
		if err := EmbedAndStoreTurn(ctx, mgr, turn); err != nil {
			t.Fatalf("failed to store turn: %v", err)
		}
	}

	// Retrieve and format
	results, err := RetrieveProactiveContext(
		ctx, mgr, DefaultProactiveContextConfig(),
		"How do I write good Go code?",
		"/tmp/workspace", now,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := FormatProactiveContext(results, DefaultProactiveContextConfig(), time.Now().UTC())

	if output == "" {
		t.Fatal("expected non-empty formatted output")
	}

	// Verify structural integrity of output
	if !strings.Contains(output, "## Previous Work (Contextual Memory)") {
		t.Error("output missing header")
	}
	if !strings.Contains(output, "The following past work may be relevant") {
		t.Error("output missing instructions")
	}

	// Each result should have a ### header, User:, and Summary:
	for _, result := range results {
		if !strings.Contains(output, result.Record.Signature) {
			t.Logf("signature %q may be truncated or in header form", result.Record.Signature)
		}
	}
}

// ========================================================================
// FormatProactiveContext with Resolve integration
// ========================================================================

func TestFormatProactiveContext_UsesResolveForZeroConfig(t *testing.T) {
	now := time.Now().UTC()
	results := []ProactiveContextResult{
		{
			Record: embedding.VectorRecord{
				Signature: "Test query",
				IndexedAt: now,
				Metadata: map[string]interface{}{
					"actionableSummary": "Test summary",
				},
			},
			Score: 0.8,
		},
	}

	// Pass a zero config — resolve should fill in defaults
	output := FormatProactiveContext(results, ProactiveContextConfig{}, time.Now().UTC())

	// Should still format correctly (using default MaxContextChars of 4000)
	if output == "" {
		t.Fatal("expected non-empty output even with zero config")
	}
	if !strings.Contains(output, "Test query") {
		t.Error("output should contain the signature")
	}
}

// ========================================================================
// InjectProactiveContext tests
// ========================================================================

func TestInjectProactiveContext_NilAgent(t *testing.T) {
	// Should not panic on nil agent
	var a *Agent
	err := a.InjectProactiveContext(context.Background(), "test query")
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestInjectProactiveContext_NoEmbeddingManager(t *testing.T) {
	// Agent without embedding manager should be a graceful no-op
	a := &Agent{}
	a.state = NewAgentStateManager(false)
	a.output = NewAgentOutputManager()

	err := a.InjectProactiveContext(context.Background(), "test query")
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestInjectProactiveContext_EmptyStore(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("LEDIT_CONFIG", tempDir)

	cfg := &configuration.EmbeddingIndexConfig{IndexDir: tempDir}
	mgr := embedding.NewEmbeddingManager(cfg, tempDir)
	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("failed to init: %v", err)
	}
	defer mgr.Close()

	a := &Agent{}
	a.state = NewAgentStateManager(false)
	a.output = NewAgentOutputManager()
	a.workspaceRoot = tempDir
	a.embeddingMgr = mgr

	err := a.InjectProactiveContext(ctx, "some query")
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}

	// No supplement should be set
	supplement := a.consumePendingSystemSupplement()
	if supplement != "" {
		t.Errorf("expected no supplement for empty store, got %q", supplement)
	}
}

func TestInjectProactiveContext_WithResults(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	tempDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("LEDIT_CONFIG", tempDir)

	cfg := &configuration.EmbeddingIndexConfig{IndexDir: tempDir}
	mgr := embedding.NewEmbeddingManager(cfg, tempDir)
	if err := mgr.Init(ctx); err != nil {
		t.Fatalf("failed to init: %v", err)
	}
	defer mgr.Close()

	// Store a turn about REST APIs
	turn, err := NewConversationTurn("session-1", 1, "How do I implement a REST API?", tempDir)
	if err != nil {
		t.Fatalf("failed to create turn: %v", err)
	}
	turn.ActionableSummary = "Implemented REST API"
	turn.Timestamp = now.Add(-1 * time.Hour)
	if err := EmbedAndStoreTurn(ctx, mgr, turn); err != nil {
		t.Fatalf("failed to embed and store: %v", err)
	}

	a := &Agent{}
	a.state = NewAgentStateManager(false)
	a.output = NewAgentOutputManager()
	a.workspaceRoot = tempDir
	a.embeddingMgr = mgr

	err = a.InjectProactiveContext(ctx, "How to build REST endpoints?")
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}

	// Supplement should be set with formatted results
	supplement := a.consumePendingSystemSupplement()
	if supplement == "" {
		t.Fatal("expected non-empty supplement after successful retrieval")
	}
	if !strings.Contains(supplement, "Previous Work (Contextual Memory)") {
		t.Error("supplement should contain the Previous Work header")
	}
}
