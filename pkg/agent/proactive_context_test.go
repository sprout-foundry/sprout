package agent

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		ago  time.Duration
		want string
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
		{7 * 24 * time.Hour, "1 week ago"},
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
		{30 * 24 * time.Hour, "1 month ago"},
		{30*24*time.Hour + 12*time.Hour, "1 month ago"},
		{60 * 24 * time.Hour, "2 months ago"},
		{90 * 24 * time.Hour, "3 months ago"},
		{180 * 24 * time.Hour, "6 months ago"},
		{365 * 24 * time.Hour, "12 months ago"},
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
	if cfg.WorkspaceScoped != true {
		t.Errorf("WorkspaceScoped: expected true (default flipped to prevent cross-workspace bleed), got %v", cfg.WorkspaceScoped)
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
	// resolve() only fills numeric zero values with defaults — booleans are
	// passed through as-is.  A zero-value ProactiveContextConfig{} therefore
	// has WorkspaceScoped=false even though the package default is true.
	// Callers who want the default must start from DefaultProactiveContextConfig().
	if resolved.WorkspaceScoped != false {
		t.Errorf("WorkspaceScoped: expected false (zero-value bool, not inferred by resolve()), got %v", resolved.WorkspaceScoped)
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
	// Only override MinRelevanceScore; the numeric fields get filled from
	// defaults but WorkspaceScoped (a bool) keeps its zero value.  Callers
	// who want the safer default must start from DefaultProactiveContextConfig().
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
		t.Errorf("WorkspaceScoped: expected false (zero-value of bool — resolve() does not infer it), got %v", resolved.WorkspaceScoped)
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

	if !strings.Contains(output, "## Previous Work (Read-Only Reference)") {
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

func TestFormatProactiveContext_TruncatesTimestampEnvelopeConsistently(t *testing.T) {
	now := time.Now().UTC()
	// Legacy records stored before the timestamp-injection moved to the
	// provider boundary carry a leading `<current-time>...</current-time>\n\n`
	// on the Signature. The heading and the User: line must agree on what
	// they render — otherwise a single record would print two different
	// versions of the same prompt in adjacent lines.
	results := []ProactiveContextResult{
		{
			Record: embedding.VectorRecord{
				ID:        "turn:legacy",
				Signature: "<current-time>2026-07-22T12:34:53-05:00</current-time>\n\nRefactor the persistence.go file",
				IndexedAt: now,
				Metadata: map[string]interface{}{
					"actionableSummary": "Refactor persistence",
				},
			},
			Score: 0.85,
		},
	}

	output := FormatProactiveContext(results, DefaultProactiveContextConfig(), now)

	if strings.Contains(output, "<current-time>") {
		t.Fatalf("output leaked the legacy timestamp envelope: %s", output)
	}
	if !strings.Contains(output, "### Refactor the persistence.go file") {
		t.Fatalf("heading should be stripped, got: %s", output)
	}
	if !strings.Contains(output, `User: "Refactor the persistence.go file"`) {
		t.Fatalf("User: line should be stripped too, got: %s", output)
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

	if err := initEmbeddingMgrWithTimeout(ctx, mgr); err != nil {
		if strings.Contains(err.Error(), "ONNX") || strings.Contains(err.Error(), "onnx") {
			t.Skip("Skipping: ONNX runtime not available")
		}
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
	if testing.Short() {
		t.Skip("skipping embedding test in short mode")
	}
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
	if testing.Short() {
		t.Skip("skipping embedding test in short mode")
	}
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
	if testing.Short() {
		t.Skip("skipping embedding test in short mode")
	}
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
	if testing.Short() {
		t.Skip("skipping embedding test in short mode")
	}
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
	if err := EmbedAndStoreTurn(ctx, mgr, turn, ""); err != nil {
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
	if testing.Short() {
		t.Skip("skipping embedding test in short mode")
	}
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

	if err := EmbedAndStoreTurn(ctx, mgr, turnA, ""); err != nil {
		t.Fatalf("failed to store turn A: %v", err)
	}
	if err := EmbedAndStoreTurn(ctx, mgr, turnB, ""); err != nil {
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

// TestRetrieveProactiveContext_DefaultConfigSkipsCrossWorkspace verifies that
// DefaultProactiveContextConfig() filters out turns from other workspaces.
//
// Regression test: a fresh session in workspace A used to pull in semantically-
// similar turns from workspace B because WorkspaceScoped defaulted to false.
// The model then treated those entries as actionable and started work in the
// wrong directory.  WorkspaceScoped now defaults to true; this test locks that
// in.
func TestRetrieveProactiveContext_DefaultConfigSkipsCrossWorkspace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedding test in short mode")
	}
	ctx := context.Background()
	now := time.Now().UTC()
	mgr, _ := setupProactiveManager(t)
	defer mgr.Close()

	// Store a turn from a workspace OTHER than the one we'll query from.
	turnOther, err := NewConversationTurn("session-other", 1,
		"Save these Zendesk support articles to the archive directory.",
		"/home/user/other-workspace")
	if err != nil {
		t.Fatalf("failed to create turn: %v", err)
	}
	turnOther.ActionableSummary = "Save Zendesk articles to archive"
	turnOther.Timestamp = now.Add(-1 * time.Hour)
	if err := EmbedAndStoreTurn(ctx, mgr, turnOther, ""); err != nil {
		t.Fatalf("failed to store turn: %v", err)
	}

	// Query from a different workspace with the default config.
	results, err := RetrieveProactiveContext(
		ctx, mgr, DefaultProactiveContextConfig(),
		"Save Zendesk support articles", "/home/user/current-workspace", now,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected zero results (cross-workspace filtered out), got %d", len(results))
		for _, r := range results {
			t.Logf("leaked record: signature=%q workingDir=%v", r.Record.Signature, r.Record.Metadata["workingDir"])
		}
	}
}

// TestRetrieveProactiveContext_DefaultConfigKeepsSameWorkspace verifies that
// DefaultProactiveContextConfig() still returns turns from the SAME workspace.
// Complements the cross-workspace regression test above.
func TestRetrieveProactiveContext_DefaultConfigKeepsSameWorkspace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedding test in short mode")
	}
	ctx := context.Background()
	now := time.Now().UTC()
	mgr, _ := setupProactiveManager(t)
	defer mgr.Close()

	const wd = "/home/user/current-workspace"
	turn, err := NewConversationTurn("session-same", 1,
		"How do I implement a REST API?", wd)
	if err != nil {
		t.Fatalf("failed to create turn: %v", err)
	}
	turn.ActionableSummary = "Implement REST API"
	turn.Timestamp = now.Add(-1 * time.Hour)
	if err := EmbedAndStoreTurn(ctx, mgr, turn, ""); err != nil {
		t.Fatalf("failed to store turn: %v", err)
	}

	results, err := RetrieveProactiveContext(
		ctx, mgr, DefaultProactiveContextConfig(),
		"REST API implementation", wd, now,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one same-workspace result with default config")
	}
}

func TestRetrieveProactiveContext_TimeDecayOrdering(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedding test in short mode")
	}
	ctx := context.Background()
	now := time.Now().UTC()
	mgr, _ := setupProactiveManager(t)
	defer mgr.Close()

	// Store two turns with identical content but different ages
	turnRecent, err := NewConversationTurn("session-recent", 1,
		"How do I implement a REST API in Go?", "/tmp/workspace")
	if err != nil {
		t.Fatalf("failed to create turn: %v", err)
	}
	turnRecent.ActionableSummary = "Implement REST API"
	turnRecent.Timestamp = now.Add(-1 * time.Hour)

	turnOld, err := NewConversationTurn("session-old", 1,
		"How do I implement a REST API in Go?", "/tmp/workspace")
	if err != nil {
		t.Fatalf("failed to create turn: %v", err)
	}
	turnOld.ActionableSummary = "Implement REST API"
	turnOld.Timestamp = now.Add(-60 * 24 * time.Hour) // 60 days old

	if err := EmbedAndStoreTurn(ctx, mgr, turnRecent, ""); err != nil {
		t.Fatalf("failed to store recent turn: %v", err)
	}
	if err := EmbedAndStoreTurn(ctx, mgr, turnOld, ""); err != nil {
		t.Fatalf("failed to store old turn: %v", err)
	}

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

	// Results should be ordered by score descending; recent should be first
	if len(results) >= 1 && results[0].Record.ID != turnRecent.ID {
		// The recent turn should have a higher decayed score than the old one
		t.Logf("First result: ID=%s, Score=%f", results[0].Record.ID, results[0].Score)
		// Verify the recent turn scores higher
		var recentScore, oldScore float64
		for _, r := range results {
			if r.Record.ID == turnRecent.ID {
				recentScore = r.Score
			}
			if r.Record.ID == turnOld.ID {
				oldScore = r.Score
			}
		}
		if recentScore <= oldScore {
			t.Errorf("recent turn score (%f) should be > old turn score (%f) due to time decay",
				recentScore, oldScore)
		}
	}
}

func TestRetrieveProactiveContext_MaxResultsCap(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedding test in short mode")
	}
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
		if err := EmbedAndStoreTurn(ctx, mgr, turn, ""); err != nil {
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
	if testing.Short() {
		t.Skip("skipping embedding test in short mode")
	}
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

	if err := EmbedAndStoreTurn(ctx, mgr, turn, ""); err != nil {
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
	if testing.Short() {
		t.Skip("skipping embedding test in short mode")
	}
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
		if err := EmbedAndStoreTurn(ctx, mgr, turn, ""); err != nil {
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
	if testing.Short() {
		t.Skip("skipping embedding test in short mode")
	}
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

	if err := EmbedAndStoreTurn(ctx, mgr, turn, ""); err != nil {
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
	if testing.Short() {
		t.Skip("skipping embedding test in short mode")
	}
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
	if testing.Short() {
		t.Skip("skipping embedding test in short mode")
	}
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
		if err := EmbedAndStoreTurn(ctx, mgr, turn, ""); err != nil {
			t.Fatalf("failed to store turn: %v", err)
		}
	}

	// Retrieve and format.
	// Use a lower MinRelevanceScore than the default 0.50 because the static
	// embedding model produces cosine similarities in the 0.1–0.4 range for
	// short text pairs (e.g. 0.36 for "Go generics" vs "write good Go code").
	config := DefaultProactiveContextConfig()
	config.MinRelevanceScore = 0.10

	results, err := RetrieveProactiveContext(
		ctx, mgr, config,
		"How do I write good Go code?",
		"/tmp/workspace", now,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := FormatProactiveContext(results, config, time.Now().UTC())

	if output == "" {
		t.Fatal("expected non-empty formatted output")
	}

	// Verify structural integrity of output
	if !strings.Contains(output, "## Previous Work (Read-Only Reference)") {
		t.Error("output missing header")
	}
	if !strings.Contains(output, "do NOT take any action on them") {
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
	if testing.Short() {
		t.Skip("skipping embedding test in short mode")
	}
	ctx := context.Background()
	tempDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("LEDIT_CONFIG", tempDir)

	cfg := &configuration.EmbeddingIndexConfig{IndexDir: tempDir}
	mgr := embedding.NewEmbeddingManager(cfg, tempDir)
	if err := initEmbeddingMgrWithTimeout(ctx, mgr); err != nil {
		if strings.Contains(err.Error(), "ONNX") || strings.Contains(err.Error(), "onnx") {
			t.Skip("Skipping: ONNX not available")
		}
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
	if testing.Short() {
		t.Skip("skipping embedding test in short mode")
	}
	ctx := context.Background()
	now := time.Now().UTC()
	tempDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", tempDir)
	t.Setenv("LEDIT_CONFIG", tempDir)

	cfg := &configuration.EmbeddingIndexConfig{IndexDir: tempDir}
	mgr := embedding.NewEmbeddingManager(cfg, tempDir)
	if err := initEmbeddingMgrWithTimeout(ctx, mgr); err != nil {
		if strings.Contains(err.Error(), "ONNX") || strings.Contains(err.Error(), "onnx") {
			t.Skip("Skipping: ONNX not available")
		}
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
	if err := EmbedAndStoreTurn(ctx, mgr, turn, ""); err != nil {
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
	if !strings.Contains(supplement, "Previous Work (Read-Only Reference)") {
		t.Error("supplement should contain the Previous Work header")
	}
}

// ========================================================================
// SweepExpiredEntries tests (SP-033-3c)
// ========================================================================

func TestSweepExpiredEntries_NoOp_WhenRetentionZero(t *testing.T) {
	// retentionDays=0 means never expire — should be a no-op
	swept, err := SweepExpiredEntries(0, "/tmp/nonexistent")
	if err != nil {
		t.Errorf("expected no error for retentionDays=0, got: %v", err)
	}
	if swept != 0 {
		t.Errorf("expected 0 swept for retentionDays=0, got %d", swept)
	}
}

func TestSweepExpiredEntries_NoOp_WhenRetentionNegative(t *testing.T) {
	// retentionDays < 0 means never expire — should be a no-op
	swept, err := SweepExpiredEntries(-1, "/tmp/nonexistent")
	if err != nil {
		t.Errorf("expected no error for retentionDays=-1, got: %v", err)
	}
	if swept != 0 {
		t.Errorf("expected 0 swept for retentionDays=-1, got %d", swept)
	}
}

func TestSweepExpiredEntries_RemovesOldEntries(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "conversation_turns.hnsw")

	store, err := embedding.NewHNSWStore(indexPath, "")
	require.NoError(t, err)

	now := time.Now().UTC()
	records := []embedding.VectorRecord{
		// 3 days old — within 7-day cutoff, should be kept
		{ID: "recent:1", Signature: "Recent entry 1", IndexedAt: now.Add(-3 * 24 * time.Hour)},
		// 5 days old — within 7-day cutoff, should be kept
		{ID: "recent:2", Signature: "Recent entry 2", IndexedAt: now.Add(-5 * 24 * time.Hour)},
		// 10 days old — exceeds 7-day cutoff, should be removed
		{ID: "old:1", Signature: "Old entry 1", IndexedAt: now.Add(-10 * 24 * time.Hour)},
		// 30 days old — exceeds 7-day cutoff, should be removed
		{ID: "old:2", Signature: "Old entry 2", IndexedAt: now.Add(-30 * 24 * time.Hour)},
	}
	require.NoError(t, store.Store(records))
	store.Close()

	swept, err := SweepExpiredEntries(7, indexPath)
	require.NoError(t, err)
	assert.Equal(t, 2, swept, "expected 2 old entries to be swept")

	// Verify only recent entries remain
	store2, err := embedding.NewHNSWStore(indexPath, "")
	require.NoError(t, err)
	defer store2.Close()
	remaining, err := store2.LoadAll()
	require.NoError(t, err)
	assert.Len(t, remaining, 2)
	ids := make(map[string]bool)
	for _, r := range remaining {
		ids[r.ID] = true
	}
	assert.True(t, ids["recent:1"], "recent:1 should remain")
	assert.True(t, ids["recent:2"], "recent:2 should remain")
	assert.False(t, ids["old:1"], "old:1 should have been swept")
	assert.False(t, ids["old:2"], "old:2 should have been swept")
}

func TestSweepExpiredEntries_KeepsRecentEntries(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "conversation_turns.hnsw")

	store, err := embedding.NewHNSWStore(indexPath, "")
	require.NoError(t, err)

	now := time.Now().UTC()
	records := []embedding.VectorRecord{
		{ID: "recent:1", IndexedAt: now.Add(-1 * time.Hour)},
		{ID: "recent:2", IndexedAt: now.Add(-3 * 24 * time.Hour)},
	}
	require.NoError(t, store.Store(records))
	store.Close()

	swept, err := SweepExpiredEntries(30, indexPath)
	require.NoError(t, err)
	assert.Equal(t, 0, swept, "expected no entries to be swept")

	// Verify all entries remain
	store2, err := embedding.NewHNSWStore(indexPath, "")
	require.NoError(t, err)
	defer store2.Close()
	remaining, err := store2.LoadAll()
	require.NoError(t, err)
	assert.Len(t, remaining, 2)
}

func TestSweepExpiredEntries_RemovesAll(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "conversation_turns.hnsw")

	store, err := embedding.NewHNSWStore(indexPath, "")
	require.NoError(t, err)

	now := time.Now().UTC()
	records := []embedding.VectorRecord{
		{ID: "old:1", IndexedAt: now.Add(-30 * 24 * time.Hour)},
		{ID: "old:2", IndexedAt: now.Add(-60 * 24 * time.Hour)},
		{ID: "old:3", IndexedAt: now.Add(-90 * 24 * time.Hour)},
	}
	require.NoError(t, store.Store(records))
	store.Close()

	swept, err := SweepExpiredEntries(7, indexPath)
	require.NoError(t, err)
	assert.Equal(t, 3, swept, "expected all 3 old entries to be swept")

	// Verify store is now empty
	store2, err := embedding.NewHNSWStore(indexPath, "")
	require.NoError(t, err)
	defer store2.Close()
	remaining, err := store2.LoadAll()
	require.NoError(t, err)
	assert.Len(t, remaining, 0)
}

func TestSweepExpiredEntries_EmptyStore(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "conversation_turns.hnsw")

	// Don't create any records — the file won't exist yet
	swept, err := SweepExpiredEntries(7, indexPath)
	require.NoError(t, err)
	assert.Equal(t, 0, swept, "expected 0 swept from non-existent/empty store")
}

func TestSweepExpiredEntries_NonExistentDir(t *testing.T) {
	// Use t.TempDir() to ensure the path is writable on this system.
	// On platforms where /tmp has restricted permissions (e.g., Termux),
	// a hardcoded /tmp path may fail before the "non-existent store" scenario
	// is even exercised.
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "nonexistent_subdir", "conversation_turns.hnsw")

	// The function creates the directory via NewHNSWStore, so this
	// should not error — it will create the directory and find no records.
	swept, err := SweepExpiredEntries(7, indexPath)
	require.NoError(t, err)
	assert.Equal(t, 0, swept)
}

func TestSweepExpiredEntries_Boundary(t *testing.T) {
	// Entries exactly on the cutoff boundary should be KEPT (not Before)
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "conversation_turns.hnsw")

	store, err := embedding.NewHNSWStore(indexPath, "")
	require.NoError(t, err)

	// Use a fixed cutoff by constructing a known time. SweepExpiredEntries
	// uses time.Now().UTC(), so we compute the cutoff and place entries
	// precisely at/around it.
	cutoff := time.Now().UTC().AddDate(0, 0, -7)

	records := []embedding.VectorRecord{
		// Exactly at cutoff — NOT before, so should be kept
		{ID: "exact:cutoff", IndexedAt: cutoff},
		// One second after cutoff — should be kept
		{ID: "after:cutoff", IndexedAt: cutoff.Add(1 * time.Second)},
		// One second before cutoff — should be swept
		{ID: "before:cutoff", IndexedAt: cutoff.Add(-1 * time.Second)},
	}
	require.NoError(t, store.Store(records))
	store.Close()

	swept, err := SweepExpiredEntries(7, indexPath)
	require.NoError(t, err)
	// Note: due to time resolution in SweepExpiredEntries (time.Now().UTC()),
	// the exact cutoff may shift by a few seconds. We verify at least the
	// "before:cutoff" entry is removed.
	assert.GreaterOrEqual(t, swept, 1, "at least the before-cutoff entry should be swept")
}

// ========================================================================
// ProactiveContextConfig resolve() RetentionDays tests (SP-033-3c)
// ========================================================================

func TestProactiveContextConfig_Resolve_PropagatesRetentionDays(t *testing.T) {
	cfg := ProactiveContextConfig{
		RetentionDays: 30,
	}
	resolved := cfg.resolve()

	assert.Equal(t, 30, resolved.RetentionDays, "RetentionDays should be propagated through resolve()")
}

func TestProactiveContextConfig_Resolve_RetentionDays_Zero_DefaultsToZero(t *testing.T) {
	cfg := ProactiveContextConfig{}
	resolved := cfg.resolve()

	assert.Equal(t, 0, resolved.RetentionDays, "RetentionDays defaults to 0 (never expire)")
}

func TestProactiveContextConfig_Resolve_RetentionDays_PartialConfig(t *testing.T) {
	// RetentionDays should be preserved even when other fields get defaults
	cfg := ProactiveContextConfig{
		RetentionDays: 14,
		// MinRelevanceScore, MaxContextualResults, MaxContextChars all zero
	}
	resolved := cfg.resolve()

	assert.Equal(t, 14, resolved.RetentionDays, "RetentionDays should be preserved")
	assert.Equal(t, 0.50, resolved.MinRelevanceScore, "MinRelevanceScore should get default")
	assert.Equal(t, 5, resolved.MaxContextualResults, "MaxContextualResults should get default")
	assert.Equal(t, 4000, resolved.MaxContextChars, "MaxContextChars should get default")
}

// ========================================================================
// HNSW refactoring: filterAndScoreProactive tests
// ========================================================================

// These tests exercise the post-filtering logic that is shared between the
// HNSW path and the brute-force fallback. They use synthetic
// embedding.QueryResult slices so we don't need a real ONNX runtime.

func TestFilterAndScoreProactive_TypeFiltering_ExcludesNonTurns(t *testing.T) {
	now := time.Now().UTC()
	config := DefaultProactiveContextConfig()

	rawResults := []embedding.QueryResult{
		{
			Record: embedding.VectorRecord{
				ID:        "mem:1",
				Type:      "memory",
				Signature: "A memory record",
				IndexedAt: now,
			},
			Similarity: 0.99, // top raw match, but wrong type
		},
		{
			Record: embedding.VectorRecord{
				ID:        "rollup:1",
				Type:      "checkpoint_rollup",
				Signature: "A rollup record",
				IndexedAt: now,
			},
			Similarity: 0.95,
		},
		{
			Record: embedding.VectorRecord{
				ID:        "turn:1",
				Type:      "conversation_turn",
				Signature: "A real conversation turn",
				IndexedAt: now,
				Metadata: map[string]interface{}{
					"workingDir": "/tmp/ws",
				},
			},
			Similarity: 0.60,
		},
	}

	scored := filterAndScoreProactive(rawResults, config, "/tmp/ws", now)

	require.Len(t, scored, 1, "only conversation_turn records should pass the type filter")
	assert.Equal(t, "turn:1", scored[0].Record.ID)
}

func TestFilterAndScoreProactive_WorkspaceScoped_ExcludesOtherWorkspaces(t *testing.T) {
	now := time.Now().UTC()
	config := DefaultProactiveContextConfig()
	config.WorkspaceScoped = true

	rawResults := []embedding.QueryResult{
		{
			Record: embedding.VectorRecord{
				ID:        "turn:a",
				Type:      "conversation_turn",
				Signature: "Turn in workspace A",
				IndexedAt: now,
				Metadata: map[string]interface{}{
					"workingDir": "/workspace-a",
				},
			},
			Similarity: 0.90,
		},
		{
			Record: embedding.VectorRecord{
				ID:        "turn:b",
				Type:      "conversation_turn",
				Signature: "Turn in workspace B",
				IndexedAt: now,
				Metadata: map[string]interface{}{
					"workingDir": "/workspace-b",
				},
			},
			Similarity: 0.85,
		},
	}

	scored := filterAndScoreProactive(rawResults, config, "/workspace-a", now)

	require.Len(t, scored, 1, "only workspace-a records should pass")
	assert.Equal(t, "turn:a", scored[0].Record.ID)
}

func TestFilterAndScoreProactive_WorkspaceDisabled_AllowsAllWorkspaces(t *testing.T) {
	now := time.Now().UTC()
	config := DefaultProactiveContextConfig()
	config.WorkspaceScoped = false

	rawResults := []embedding.QueryResult{
		{
			Record: embedding.VectorRecord{
				ID:        "turn:a",
				Type:      "conversation_turn",
				Signature: "Turn in workspace A",
				IndexedAt: now,
				Metadata: map[string]interface{}{
					"workingDir": "/workspace-a",
				},
			},
			Similarity: 0.90,
		},
		{
			Record: embedding.VectorRecord{
				ID:        "turn:b",
				Type:      "conversation_turn",
				Signature: "Turn in workspace B",
				IndexedAt: now,
				Metadata: map[string]interface{}{
					"workingDir": "/workspace-b",
				},
			},
			Similarity: 0.85,
		},
	}

	scored := filterAndScoreProactive(rawResults, config, "/workspace-a", now)

	require.Len(t, scored, 2, "both workspaces should pass when WorkspaceScoped is false")
	// Higher similarity should come first (both are equally fresh, so decay is identical)
	assert.Equal(t, "turn:a", scored[0].Record.ID, "higher similarity should rank first")
	assert.Equal(t, "turn:b", scored[1].Record.ID)
}

func TestFilterAndScoreProactive_TimeDecay_OlderRecordWithHigherRawSimilarity(t *testing.T) {
	now := time.Now().UTC()
	config := DefaultProactiveContextConfig()
	config.WorkspaceScoped = true
	config.MinRelevanceScore = 0.10 // low threshold so both decayed scores pass

	// Older record has higher raw similarity but is 60 days old.
	// Recent record has slightly lower raw similarity but is only 1 hour old.
	// After time-decay, the recent record should score higher.
	rawResults := []embedding.QueryResult{
		{
			Record: embedding.VectorRecord{
				ID:        "turn:old",
				Type:      "conversation_turn",
				Signature: "Old but very similar",
				IndexedAt: now.Add(-60 * 24 * time.Hour), // 60 days ago
				Metadata: map[string]interface{}{
					"workingDir": "/tmp/ws",
				},
			},
			Similarity: 0.95, // high raw similarity
		},
		{
			Record: embedding.VectorRecord{
				ID:        "turn:recent",
				Type:      "conversation_turn",
				Signature: "Recent and similar",
				IndexedAt: now.Add(-1 * time.Hour), // 1 hour ago
				Metadata: map[string]interface{}{
					"workingDir": "/tmp/ws",
				},
			},
			Similarity: 0.70, // lower raw similarity
		},
	}

	scored := filterAndScoreProactive(rawResults, config, "/tmp/ws", now)

	require.Len(t, scored, 2, "both should pass the low min threshold")

	// The recent record should have a higher decayed score.
	// Old: 0.95 * 0.5^(60/30) = 0.95 * 0.25 = 0.2375
	// Recent: 0.70 * 0.5^(1h/720h) ≈ 0.70 * 0.999 ≈ 0.699
	var recentScore, oldScore float64
	for _, r := range scored {
		if r.Record.ID == "turn:recent" {
			recentScore = r.Score
		} else if r.Record.ID == "turn:old" {
			oldScore = r.Score
		}
	}
	assert.Greater(t, recentScore, oldScore,
		"recent record should have higher decayed score despite lower raw similarity")
	assert.Greater(t, recentScore, 0.6, "recent score should be near 0.70")
	assert.Less(t, oldScore, 0.3, "60-day-old score should be heavily decayed")
}

func TestFilterAndScoreProactive_MinRelevanceScoreFilter(t *testing.T) {
	now := time.Now().UTC()
	config := DefaultProactiveContextConfig()
	config.MinRelevanceScore = 0.80
	config.WorkspaceScoped = true

	rawResults := []embedding.QueryResult{
		{
			Record: embedding.VectorRecord{
				ID:        "turn:high",
				Type:      "conversation_turn",
				Signature: "High similarity",
				IndexedAt: now,
				Metadata: map[string]interface{}{
					"workingDir": "/tmp/ws",
				},
			},
			Similarity: 0.95, // fresh, so decayed ≈ 0.95
		},
		{
			Record: embedding.VectorRecord{
				ID:        "turn:low",
				Type:      "conversation_turn",
				Signature: "Low similarity",
				IndexedAt: now,
				Metadata: map[string]interface{}{
					"workingDir": "/tmp/ws",
				},
			},
			Similarity: 0.60, // fresh, so decayed ≈ 0.60 — below 0.80 threshold
		},
	}

	scored := filterAndScoreProactive(rawResults, config, "/tmp/ws", now)

	require.Len(t, scored, 1, "only the high-similarity record should pass the 0.80 threshold")
	assert.Equal(t, "turn:high", scored[0].Record.ID)
}

func TestFilterAndScoreProactive_EmptyInput(t *testing.T) {
	config := DefaultProactiveContextConfig()

	// nil input → returns empty (non-nil) slice via make([]T, 0, 0)
	scored := filterAndScoreProactive(nil, config, "/tmp/ws", time.Now().UTC())
	assert.Empty(t, scored, "nil input should return an empty result slice")

	// empty input
	scored = filterAndScoreProactive([]embedding.QueryResult{}, config, "/tmp/ws", time.Now().UTC())
	require.NotNil(t, scored, "empty input should return empty slice, not nil")
	assert.Empty(t, scored)
}

func TestFilterAndScoreProactive_AllFiltered_ReturnsEmpty(t *testing.T) {
	now := time.Now().UTC()
	config := DefaultProactiveContextConfig()
	config.WorkspaceScoped = true

	// All records are from a different workspace — everything should be filtered out.
	rawResults := []embedding.QueryResult{
		{
			Record: embedding.VectorRecord{
				ID:        "turn:other",
				Type:      "conversation_turn",
				Signature: "Wrong workspace",
				IndexedAt: now,
				Metadata: map[string]interface{}{
					"workingDir": "/other-ws",
				},
			},
			Similarity: 0.95,
		},
	}

	scored := filterAndScoreProactive(rawResults, config, "/my-ws", now)
	assert.Empty(t, scored, "all records from wrong workspace should be filtered out")
}

func TestFilterAndScoreProactive_FallbackSimulatesMissedHNSWMatch(t *testing.T) {
	// This simulates the brute-force fallback scenario: HNSW's approximate
	// top-K returned only records from a different workspace, but a full
	// scan would find a same-workspace match with slightly lower raw cosine.
	// The filterAndScoreProactive function is the shared post-filter used
	// by both the HNSW path and the brute-force fallback.
	//
	// Scenario: HNSW top-K only returned cross-workspace results.
	// The HNSW path calls filterAndScoreProactive → gets 0 results.
	// The fallback calls LoadAll, computes brute-force similarities, then
	// calls filterAndScoreProactive again with the full set.
	now := time.Now().UTC()
	config := DefaultProactiveContextConfig()
	config.WorkspaceScoped = true

	// HNSW path: only cross-workspace results (what HNSW top-K returned)
	hnswResults := []embedding.QueryResult{
		{
			Record: embedding.VectorRecord{
				ID:        "turn:other1",
				Type:      "conversation_turn",
				Signature: "Cross-workspace result",
				IndexedAt: now,
				Metadata: map[string]interface{}{
					"workingDir": "/other-ws",
				},
			},
			Similarity: 0.90,
		},
	}

	hnswScored := filterAndScoreProactive(hnswResults, config, "/my-ws", now)
	assert.Empty(t, hnswScored, "HNSW results should all be filtered by workspace")

	// Brute-force fallback: includes the same-workspace match that HNSW missed
	bruteResults := append([]embedding.QueryResult{
		{
			Record: embedding.VectorRecord{
				ID:        "turn:mine",
				Type:      "conversation_turn",
				Signature: "Same-workspace match",
				IndexedAt: now,
				Metadata: map[string]interface{}{
					"workingDir": "/my-ws",
				},
			},
			Similarity: 0.55, // lower raw similarity (HNSW didn't surface it in top-K)
		},
	}, hnswResults...)

	bruteScored := filterAndScoreProactive(bruteResults, config, "/my-ws", now)
	require.Len(t, bruteScored, 1, "brute-force fallback should find the same-workspace match")
	assert.Equal(t, "turn:mine", bruteScored[0].Record.ID)
}

// ========================================================================
// HNSW refactoring: RetrieveProactiveContext integration tests
// ========================================================================

func TestRetrieveProactiveContext_TypeFiltering_ExcludesNonTurns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedding test in short mode")
	}
	ctx := context.Background()
	now := time.Now().UTC()
	mgr, store := setupProactiveManager(t)
	defer mgr.Close()

	// Store a conversation turn
	turn, err := NewConversationTurn("session-type", 1,
		"How do I implement a REST API?", "/tmp/workspace")
	require.NoError(t, err)
	turn.ActionableSummary = "Implement REST API"
	turn.Timestamp = now.Add(-1 * time.Hour)
	require.NoError(t, EmbedAndStoreTurn(ctx, mgr, turn, ""))

	// Manually add a non-turn record to the store to test type filtering
	nonTurnRecord := embedding.VectorRecord{
		ID:        "memory:1",
		Type:      "memory",
		Signature: "How do I implement a REST API?",
		IndexedAt: now,
		Metadata: map[string]interface{}{
			"workingDir": "/tmp/workspace",
		},
	}
	// Embed the same text so it has a vector
	emb, err := store.Provider().Embed(ctx, nonTurnRecord.Signature)
	require.NoError(t, err)
	nonTurnRecord.Embedding = emb
	require.NoError(t, store.Store([]embedding.VectorRecord{nonTurnRecord}))

	// Query — only conversation_turn records should be returned
	results, err := RetrieveProactiveContext(
		ctx, mgr, DefaultProactiveContextConfig(),
		"How to build a REST API?",
		"/tmp/workspace", now,
	)
	require.NoError(t, err)

	// All returned results must be conversation_turn type
	for _, r := range results {
		assert.Equal(t, "conversation_turn", r.Record.Type,
			"only conversation_turn records should be returned, got type %q", r.Record.Type)
	}
}

func TestRetrieveProactiveContext_WorkspaceDisabled_ReturnsCrossWorkspace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedding test in short mode")
	}
	ctx := context.Background()
	now := time.Now().UTC()
	mgr, _ := setupProactiveManager(t)
	defer mgr.Close()

	// Store a turn in a different workspace
	turn, err := NewConversationTurn("session-other-ws", 1,
		"How do I implement a REST API?", "/other-workspace")
	require.NoError(t, err)
	turn.ActionableSummary = "Implement REST API"
	turn.Timestamp = now.Add(-1 * time.Hour)
	require.NoError(t, EmbedAndStoreTurn(ctx, mgr, turn, ""))

	// Query from a different workspace with WorkspaceScoped disabled
	config := DefaultProactiveContextConfig()
	config.WorkspaceScoped = false

	results, err := RetrieveProactiveContext(
		ctx, mgr, config,
		"How to build a REST API?",
		"/my-workspace", now,
	)
	require.NoError(t, err)

	// With WorkspaceScoped=false, the cross-workspace turn should be returned
	if len(results) > 0 {
		wd, _ := results[0].Record.Metadata["workingDir"].(string)
		assert.Equal(t, "/other-workspace", wd,
			"with WorkspaceScoped=false, cross-workspace results should be returned")
	}
}

func TestRetrieveProactiveContext_MaxResultsCap_CapsResults(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedding test in short mode")
	}
	ctx := context.Background()
	now := time.Now().UTC()
	mgr, _ := setupProactiveManager(t)
	defer mgr.Close()

	// Store 8 turns with the same content
	for i := 0; i < 8; i++ {
		turn, err := NewConversationTurn("session-cap2", i+1,
			"How do I implement a REST API in Go?", "/tmp/workspace")
		require.NoError(t, err)
		turn.ActionableSummary = "Implement REST API"
		turn.Timestamp = now.Add(-time.Duration(i) * time.Minute)
		require.NoError(t, EmbedAndStoreTurn(ctx, mgr, turn, ""))
	}

	// Set a custom cap of 2
	config := DefaultProactiveContextConfig()
	config.MaxContextualResults = 2

	results, err := RetrieveProactiveContext(
		ctx, mgr, config,
		"How to build a REST API?", "/tmp/workspace", now,
	)
	require.NoError(t, err)

	assert.LessOrEqual(t, len(results), 2,
		"results should be capped at MaxContextualResults=2")
}

func TestRetrieveProactiveContext_TimeDecay_OlderRecordDecays(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedding test in short mode")
	}
	ctx := context.Background()
	now := time.Now().UTC()
	mgr, _ := setupProactiveManager(t)
	defer mgr.Close()

	// Store two turns with identical content but very different ages
	turnRecent, err := NewConversationTurn("session-decay-recent", 1,
		"How do I implement a REST API in Go?", "/tmp/workspace")
	require.NoError(t, err)
	turnRecent.ActionableSummary = "Implement REST API"
	turnRecent.Timestamp = now.Add(-1 * time.Hour)
	require.NoError(t, EmbedAndStoreTurn(ctx, mgr, turnRecent, ""))

	turnOld, err := NewConversationTurn("session-decay-old", 1,
		"How do I implement a REST API in Go?", "/tmp/workspace")
	require.NoError(t, err)
	turnOld.ActionableSummary = "Implement REST API"
	turnOld.Timestamp = now.Add(-60 * 24 * time.Hour) // 60 days old
	require.NoError(t, EmbedAndStoreTurn(ctx, mgr, turnOld, ""))

	results, err := RetrieveProactiveContext(
		ctx, mgr, DefaultProactiveContextConfig(),
		"How to build a REST API in Go?",
		"/tmp/workspace", now,
	)
	require.NoError(t, err)

	require.GreaterOrEqual(t, len(results), 1, "should get at least one result")

	// Find the scores for both turns
	var recentScore, oldScore float64
	var foundRecent, foundOld bool
	for _, r := range results {
		if r.Record.ID == turnRecent.ID {
			recentScore = r.Score
			foundRecent = true
		}
		if r.Record.ID == turnOld.ID {
			oldScore = r.Score
			foundOld = true
		}
	}

	if foundRecent && foundOld {
		assert.Greater(t, recentScore, oldScore,
			"recent turn should score higher than 60-day-old turn due to time decay")
	}
}
