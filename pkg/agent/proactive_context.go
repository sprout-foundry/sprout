package agent

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// ProactiveContextConfig holds configuration for proactive context retrieval.
// These defaults will later be backed by PersistentContextConfig in
// pkg/configuration (SP-027-2d).
type ProactiveContextConfig struct {
	// MinRelevanceScore is the minimum time-decayed similarity score required
	// for a result to be included. Default: 0.50.
	MinRelevanceScore float64

	// MaxContextualResults caps the number of results returned. Default: 5.
	MaxContextualResults int

	// MaxContextChars is the character budget for FormatProactiveContext.
	// The formatted string is truncated at this limit. Default: 4000.
	MaxContextChars int

	// WorkspaceScoped, if true, filters to turns from the same workingDir.
	// Default: false.
	WorkspaceScoped bool

	// RetentionDays controls how many days to keep persistent context entries.
	// Default: 0 (forever, never expire).
	RetentionDays int
}

// DefaultProactiveContextConfig returns a ProactiveContextConfig with the
// standard defaults specified in SP-027.
func DefaultProactiveContextConfig() ProactiveContextConfig {
	return ProactiveContextConfig{
		MinRelevanceScore:    0.50,
		MaxContextualResults: 5,
		MaxContextChars:      4000,
		WorkspaceScoped:      false,
	}
}

// resolve fills zero/negative fields with defaults and returns the resolved copy.
// Only zero/negative values are replaced — any positive override is preserved.
func (c ProactiveContextConfig) resolve() ProactiveContextConfig {
	d := DefaultProactiveContextConfig()
	// Normalize negative RetentionDays to 0 (never expire) for API consistency
	retentionDays := c.RetentionDays
	if retentionDays < 0 {
		retentionDays = 0
	}
	resolved := ProactiveContextConfig{
		MinRelevanceScore:    c.MinRelevanceScore,
		MaxContextualResults: c.MaxContextualResults,
		MaxContextChars:      c.MaxContextChars,
		WorkspaceScoped:      c.WorkspaceScoped,
		RetentionDays:        retentionDays,
	}
	if resolved.MinRelevanceScore <= 0 {
		resolved.MinRelevanceScore = d.MinRelevanceScore
	}
	if resolved.MaxContextualResults <= 0 {
		resolved.MaxContextualResults = d.MaxContextualResults
	}
	if resolved.MaxContextChars <= 0 {
		resolved.MaxContextChars = d.MaxContextChars
	}
	return resolved
}

// ProactiveContextResult holds a retrieved conversation turn with its
// time-decayed similarity score.
type ProactiveContextResult struct {
	Record embedding.VectorRecord
	Score  float64 // time-decayed cosine similarity
}

// RetrieveProactiveContext retrieves relevant conversation turns from the
// conversation store based on semantic similarity with time-decay scoring.
//
// The retrieval pipeline:
//  1. Embed the query text using the static provider
//  2. Load all conversation_turn records from the store
//  3. Optionally filter by working directory (WorkspaceScoped)
//  4. Score each candidate with cosine similarity + 30-day half-life decay
//  5. Filter by MinRelevanceScore, sort descending, cap at MaxContextualResults
//
// Graceful degradation: all errors are logged and nil/empty is returned.
// The agent should never be blocked by a retrieval failure.
func RetrieveProactiveContext(
	ctx context.Context,
	mgr *embedding.EmbeddingManager,
	config ProactiveContextConfig,
	query string,
	workingDir string,
	now time.Time,
) ([]ProactiveContextResult, error) {
	config = config.resolve()

	// Nil-safe input validation. Routine "feature unavailable" paths —
	// demoted to debug so they don't fire on every query when embedding
	// is unconfigured.
	if mgr == nil {
		debugLogf("[proactive-context] skipping: embedding manager is nil")
		return nil, nil
	}
	if ctx == nil {
		debugLogf("[proactive-context] skipping: context is nil")
		return nil, nil
	}
	if query == "" {
		debugLogf("[proactive-context] skipping: empty query")
		return nil, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	// Ensure the embedding manager is initialized
	if err := mgr.Init(ctx); err != nil {
		log.Printf("[proactive-context] init failed: %v", err)
		return nil, nil
	}

	// Acquire the conversation store (lazy-created by the manager)
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		log.Printf("[proactive-context] conversation store unavailable: %v", err)
		return nil, nil
	}

	provider := store.Provider()
	if provider == nil {
		log.Printf("[proactive-context] provider unexpectedly nil")
		return nil, nil
	}

	// Embed the query
	queryEmb, err := provider.Embed(ctx, query)
	if err != nil {
		if ctx.Err() != nil {
			log.Printf("[proactive-context] embedding cancelled: %v", ctx.Err())
		} else {
			log.Printf("[proactive-context] query embedding failed: %v", err)
		}
		return nil, nil
	}
	if len(queryEmb) == 0 {
		log.Printf("[proactive-context] query embedding returned empty vector")
		return nil, nil
	}

	// Load all records and filter to conversation turns
	allRecords, err := store.LoadAll()
	if err != nil {
		log.Printf("[proactive-context] failed to load records: %v", err)
		return nil, nil
	}

	candidates := make([]embedding.VectorRecord, 0, len(allRecords))
	for i := range allRecords {
		if allRecords[i].Type == "conversation_turn" {
			candidates = append(candidates, allRecords[i])
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	// Score, filter, and collect (negative cosine similarities are filtered
	// by the MinRelevanceScore >= 0.50 threshold, so they are never included).
	scored := make([]ProactiveContextResult, 0, len(candidates))
	for _, record := range candidates {
		// Workspace-scoped filtering
		if config.WorkspaceScoped && workingDir != "" {
			recWD, ok := record.Metadata["workingDir"].(string)
			if !ok || recWD != workingDir {
				continue
			}
		}

		// Skip records with no embedding (shouldn't happen, but defensive)
		if len(record.Embedding) == 0 {
			continue
		}

		similarity := embedding.CosineSimilarity(queryEmb, record.Embedding)
		decayedScore := embedding.ScoreWithDecay(float64(similarity), record.IndexedAt, now)

		if decayedScore >= config.MinRelevanceScore {
			scored = append(scored, ProactiveContextResult{
				Record: record,
				Score:  decayedScore,
			})
		}
	}

	if len(scored) == 0 {
		return nil, nil
	}

	// Sort by score descending (stable for deterministic ordering)
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	// Cap at max results
	if len(scored) > config.MaxContextualResults {
		scored = scored[:config.MaxContextualResults]
	}

	// Per-retrieval informational log — debug-only.
	debugLogf("[proactive-context] retrieved %d/%d candidates above threshold %.2f",
		len(scored), len(candidates), config.MinRelevanceScore)

	return scored, nil
}

// FormatProactiveContext formats retrieved results as a "Previous Work" section
// suitable for injection into the agent's system prompt.
//
// Output format:
//
//	## Previous Work (Contextual Memory)
//
//	The following past work may be relevant. Evaluate critically and discard anything irrelevant.
//
//	### <first line of prompt> (<relative time>)
//	User: "<user prompt>"
//	Summary: <actionable summary>
//
// Returns "" when results is empty. The output is capped at
// config.MaxContextChars characters. Pass now=time.Time{} to use the current
// time (same pattern as RetrieveProactiveContext).
func FormatProactiveContext(results []ProactiveContextResult, config ProactiveContextConfig, now time.Time) string {
	config = config.resolve()

	if len(results) == 0 {
		return ""
	}

	if now.IsZero() {
		now = time.Now().UTC()
	}

	var b strings.Builder

	const header = "## Previous Work (Contextual Memory)\n\n" +
		"The following past work may be relevant. Evaluate critically and discard anything irrelevant.\n\n"

	b.WriteString(header)

	for _, result := range results {
		record := result.Record

		// Determine header text: first line of the user prompt, capped at 80 chars
		headerText := record.Signature
		if headerText == "" {
			headerText = "No prompt available"
		}
		if idx := strings.Index(headerText, "\n"); idx >= 0 {
			headerText = headerText[:idx]
		}
		if len(headerText) > 80 {
			// Truncate at a rune boundary
			runes := []rune(headerText)
			if len(runes) > 80 {
				headerText = string(runes[:77]) + "..."
			}
		}

		relativeTime := formatRelativeTime(record.IndexedAt, now)

		// Actionable summary from metadata
		summary := "No summary available"
		if s, ok := record.Metadata["actionableSummary"].(string); ok && s != "" {
			summary = s
		}

		fmt.Fprintf(&b, "### %s (%s)\n", headerText, relativeTime)
		fmt.Fprintf(&b, "User: \"%s\"\n", record.Signature)
		fmt.Fprintf(&b, "Summary: %s\n\n", summary)

		// Truncate if over budget (rune-safe to avoid splitting multi-byte chars)
		if b.Len() > config.MaxContextChars {
			raw := b.String()
			runes := []rune(raw)
			if len(runes) > config.MaxContextChars {
				raw = string(runes[:config.MaxContextChars])
			}
			if lastNL := strings.LastIndex(raw, "\n"); lastNL > 0 {
				raw = raw[:lastNL]
			}
			return raw + "\n\n[Context truncated...]"
		}
	}

	return b.String()
}

// SweepExpiredEntries removes persistent context entries older than retentionDays
// from the conversation store at storePath. If retentionDays <= 0, this is a no-op.
// Returns the number of entries removed.
func SweepExpiredEntries(retentionDays int, storePath string) (int, error) {
	if retentionDays <= 0 {
		return 0, nil
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)

	store, err := embedding.NewHNSWStore(storePath, "")
	if err != nil {
		return 0, fmt.Errorf("sweep: open store %s: %w", storePath, err)
	}
	defer store.Close()

	// NOTE: LoadAll() and ReplaceAll() are separate operations. Any concurrent
	// writes between them would be lost. This is safe at startup (no concurrent
	// access) but should not be called during active embedding operations.
	allRecords, err := store.LoadAll()
	if err != nil {
		return 0, fmt.Errorf("sweep: load records: %w", err)
	}

	if len(allRecords) == 0 {
		return 0, nil
	}

	kept := make([]embedding.VectorRecord, 0, len(allRecords))
	for i := range allRecords {
		if !allRecords[i].IndexedAt.Before(cutoff) {
			kept = append(kept, allRecords[i])
		}
	}

	swept := len(allRecords) - len(kept)
	if swept > 0 {
		err = store.ReplaceAll(kept)
		if err != nil {
			return 0, fmt.Errorf("sweep: write back records: %w", err)
		}
	}

	return swept, nil
}

// InjectProactiveContext retrieves semantically relevant past turns and
// injects them into the agent's system prompt supplement. This is called
// once per session — on the first turn or after a cold session restore.
//
// Graceful degradation: all errors are logged; the agent is never blocked.
func (a *Agent) InjectProactiveContext(ctx context.Context, query string) error {
	if a == nil {
		return nil
	}

	mgr := a.GetEmbeddingManager()
	if mgr == nil {
		a.debugLog("[proactive-context] skipping: no embedding manager\n")
		return nil
	}

	config := DefaultProactiveContextConfig()
	workingDir := a.currentWorkspaceRoot()
	now := time.Now().UTC()

	results, err := RetrieveProactiveContext(ctx, mgr, config, query, workingDir, now)
	if err != nil {
		a.debugLog("[proactive-context] retrieval failed: %v\n", err)
		return nil // graceful degradation
	}

	if len(results) == 0 {
		return nil
	}

	formatted := FormatProactiveContext(results, config, now)
	if formatted == "" {
		return nil
	}

	// Prepend any existing supplement (e.g. "Context From Previous Session"
	// set by ProcessQueryWithContinuity) so both sections are preserved.
	existing := ""
	if a.state != nil {
		existing = a.consumePendingSystemSupplement()
	}

	combined := formatted
	if existing != "" {
		combined = existing + "\n\n" + formatted
	}
	a.setPendingSystemSupplement(combined)

	// Per-injection informational log — debug-only. Fires on every cold
	// session start or first turn; only operators debugging the proactive
	// path actually want to see this.
	debugLogf("[proactive-context] injected %d results (%d chars) into system prompt",
		len(results), len(formatted))

	return nil
}

// formatRelativeTime returns a human-readable relative time string such as
// "just now", "2 hours ago", "3 days ago". Future timestamps return "just now".
func formatRelativeTime(t time.Time, now time.Time) string {
	d := now.Sub(t)
	if d < 0 {
		d = 0
	}

	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	case d < 7*24*time.Hour:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	case d < 30*24*time.Hour:
		w := int(d.Hours() / (24 * 7))
		if w == 1 {
			return "1 week ago"
		}
		return fmt.Sprintf("%d weeks ago", w)
	default:
		m := int(d.Hours() / (24 * 30))
		if m == 1 {
			return "1 month ago"
		}
		return fmt.Sprintf("%d months ago", m)
	}
}
