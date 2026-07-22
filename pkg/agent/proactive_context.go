package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/embedding"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
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
	// Default: true.
	//
	// Cross-workspace bleed has been a real foot-gun (e.g. a fresh session
	// in repo A pulling in semantically-similar turns from repo B and the
	// model treating them as actionable).  Past work from other workspaces
	// is almost always noise; users who genuinely want cross-workspace
	// recall can opt in by setting WorkspaceScoped: false (or via the
	// PersistentContextConfig override hook in SP-027-2d).
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
		WorkspaceScoped:      true,
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
//  2. Query the HNSW index for the top-K nearest neighbors (O(log N))
//  3. Filter to Type=="conversation_turn" and optionally by working directory
//  4. Re-score with cosine similarity × 30-day half-life decay
//  5. Filter by MinRelevanceScore, sort descending, cap at MaxContextualResults
//
// If the HNSW query returns no same-workspace matches (e.g. when the
// relevant turns rank beyond the top-K window under raw cosine), the
// pipeline falls back to a brute-force LoadAll scan for stores under 2000
// records — preserving the exact-match recall of the pre-HNSW path where
// the O(N) cost is negligible. Larger stores rely on HNSW alone.
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
		debugLogf("[proactive-context] init failed: %v", err)
		return nil, nil
	}

	// Acquire the conversation store (lazy-created by the manager)
	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		debugLogf("[proactive-context] conversation store unavailable: %v", err)
		return nil, nil
	}

	provider := store.Provider()
	if provider == nil {
		debugLogf("[proactive-context] provider unexpectedly nil")
		return nil, nil
	}

	// Embed the query
	queryEmb, err := provider.Embed(ctx, query)
	if err != nil {
		if ctx.Err() != nil {
			debugLogf("[proactive-context] embedding cancelled: %v", ctx.Err())
		} else {
			debugLogf("[proactive-context] query embedding failed: %v", err)
		}
		return nil, nil
	}
	if len(queryEmb) == 0 {
		debugLogf("[proactive-context] query embedding returned empty vector")
		return nil, nil
	}

	scored := retrieveProactiveViaHNSW(store, queryEmb, config, workingDir, now)
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
	debugLogf("[proactive-context] retrieved %d candidates above decayed threshold %.2f",
		len(scored), config.MinRelevanceScore)

	return scored, nil
}

// proactiveRawSimilarityFloor is the minimum RAW cosine similarity an HNSW
// candidate must have to be retrieved at all. It is deliberately lower than
// MinRelevanceScore (which semantically refers to the DECAYED score): a record
// with raw similarity 0.35 and a fresh IndexedAt can decay to a score near
// 0.35, which may clear a user-set MinRelevanceScore of 0.30, but we still
// want a noise floor so HNSW doesn't return thousands of low-signal hits on
// large stores. Keep this as a separate constant so lowering MinRelevanceScore
// below it doesn't flood HNSW with noise.
const proactiveRawSimilarityFloor = 0.30

// retrieveProactiveViaHNSW runs the HNSW query + post-filter pipeline. If the
// filtered set is empty but the store is small, it falls back to the old
// brute-force LoadAll path — HNSW's approximate top-K can miss same-workspace
// matches that rank beyond topK under cosine but are still relevant after
// workspace + type filtering. The fallback keeps the refactoring from
// regressing recall on small stores where the O(N) cost is negligible.
func retrieveProactiveViaHNSW(
	store *embedding.ConversationStore,
	queryEmb []float32,
	config ProactiveContextConfig,
	workingDir string,
	now time.Time,
) []ProactiveContextResult {
	topK := config.MaxContextualResults * 4
	if topK < 4 {
		topK = 4
	}

	// Use the lower raw-similarity floor for the HNSW pre-filter so
	// decayed-but-relevant matches survive; MinRelevanceScore is applied
	// post-query on the decayed score (its semantic purpose).
	rawThreshold := float32(proactiveRawSimilarityFloor)
	if float64(rawThreshold) > config.MinRelevanceScore {
		rawThreshold = float32(config.MinRelevanceScore)
	}

	rawResults, err := store.Query(queryEmb, topK, rawThreshold)
	if err != nil {
		debugLogf("[proactive-context] HNSW query failed: %v", err)
		return nil
	}

	scored := filterAndScoreProactive(rawResults, config, workingDir, now)
	if len(scored) > 0 {
		return scored
	}

	// Fallback: HNSW's approximate top-K may have missed same-workspace
	// matches that rank beyond topK by raw cosine. For small stores the
	// brute-force O(N) cost is negligible, and falling back preserves the
	// exact-match recall of the pre-refactoring LoadAll path. Cap at a few
	// thousand records so a pathological store can't stall startup.
	if store.Size() > 2000 {
		return nil
	}
	allRecords, err := store.LoadAll()
	if err != nil {
		debugLogf("[proactive-context] fallback LoadAll failed: %v", err)
		return nil
	}
	bruteResults := make([]embedding.QueryResult, 0, len(allRecords))
	for _, rec := range allRecords {
		if len(rec.Embedding) == 0 {
			continue
		}
		sim := embedding.CosineSimilarity(queryEmb, rec.Embedding)
		if sim >= rawThreshold {
			bruteResults = append(bruteResults, embedding.QueryResult{Record: rec, Similarity: sim})
		}
	}
	return filterAndScoreProactive(bruteResults, config, workingDir, now)
}

// filterAndScoreProactive applies the type filter, workspace filter, and
// time-decay re-scoring to a set of raw query results, returning only those
// whose decayed score meets config.MinRelevanceScore. Shared between the HNSW
// path and the brute-force fallback so both apply identical post-filtering.
func filterAndScoreProactive(
	rawResults []embedding.QueryResult,
	config ProactiveContextConfig,
	workingDir string,
	now time.Time,
) []ProactiveContextResult {
	scored := make([]ProactiveContextResult, 0, len(rawResults))
	for _, r := range rawResults {
		// Only surface per-turn records (not rollups or memories).
		if r.Record.Type != "conversation_turn" {
			continue
		}

		// Workspace-scoped filtering
		if config.WorkspaceScoped && workingDir != "" {
			recWD, ok := r.Record.Metadata["workingDir"].(string)
			if !ok || recWD != workingDir {
				continue
			}
		}

		decayedScore := embedding.ScoreWithDecay(float64(r.Similarity), r.Record.IndexedAt, now)

		if decayedScore >= config.MinRelevanceScore {
			scored = append(scored, ProactiveContextResult{
				Record: r.Record,
				Score:  decayedScore,
			})
		}
	}
	return scored
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

	// IMPORTANT: this section is passive history, not a TODO list.  Past
	// agent runs that scored above the similarity threshold are surfaced
	// here as background context only — if the user's current request
	// doesn't explicitly reference one of these entries, treat them as
	// read-only.  Acting on these unsolicited has caused the model to
	// silently start work in unrelated workspaces.
	const header = "## Previous Work (Read-Only Reference)\n\n" +
		"The entries below are past conversation turns retrieved by semantic " +
		"similarity to the current prompt.  They are FYI background only — " +
		"**do NOT take any action on them unless the user's current message " +
		"explicitly asks about them**.  Treat this section like read-only " +
		"history, not a TODO list.  If the entries seem unrelated to what the " +
		"user actually asked, ignore them entirely.\n\n"

	b.WriteString(header)

	for _, result := range results {
		record := result.Record

		// Strip the provider-only <current-time> envelope from records stored
		// before the timestamp-injection was moved to the provider boundary.
		// Both the heading and the body line stay in sync — using
		// `record.Signature` raw would render the envelope in the body while
		// the heading was clean, an inconsistency only visible for legacy
		// records.
		userPrompt := StripUserMessageTimestamp(record.Signature)
		headerText := userPrompt
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
		fmt.Fprintf(&b, "User: \"%s\"\n", userPrompt)
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
		return 0, agenterrors.NewConfig("sweep: open store "+storePath, err)
	}
	defer store.Close()

	// NOTE: LoadAll() and ReplaceAll() are separate operations. Any concurrent
	// writes between them would be lost. This is safe at startup (no concurrent
	// access) but should not be called during active embedding operations.
	allRecords, err := store.LoadAll()
	if err != nil {
		return 0, agenterrors.Wrap(err, "sweep: load records")
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
			return 0, agenterrors.Wrap(err, "sweep: write back records")
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
		a.Logger().Debug("[proactive-context] skipping: no embedding manager\n")
		return nil
	}

	// Honor the user's PersistentContextConfig (Agent → Memory settings).
	// When ProactiveContextEnabled is false, skip retrieval entirely.
	config := a.proactiveContextConfigFromUserSettings()
	if !a.proactiveContextEnabled() {
		a.Logger().Debug("[proactive-context] skipping: disabled in user settings\n")
		return nil
	}
	workingDir := a.currentWorkspaceRoot()
	now := time.Now().UTC()

	results, err := RetrieveProactiveContext(ctx, mgr, config, query, workingDir, now)
	if err != nil {
		a.Logger().Debug("[proactive-context] retrieval failed: %v\n", err)
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

// proactiveContextEnabled returns whether proactive context retrieval is on
// for this agent's config. Defaults to true when the user hasn't customized
// PersistentContext.
func (a *Agent) proactiveContextEnabled() bool {
	if a == nil {
		return false
	}
	cfg := a.GetConfig()
	if cfg == nil || cfg.PersistentContext == nil {
		return true
	}
	return cfg.PersistentContext.ProactiveContextEnabled
}

// proactiveContextConfigFromUserSettings maps the user's PersistentContext
// configuration into the internal ProactiveContextConfig. Falls back to
// built-in defaults when the user hasn't customized anything.
func (a *Agent) proactiveContextConfigFromUserSettings() ProactiveContextConfig {
	defaults := DefaultProactiveContextConfig()
	if a == nil {
		return defaults
	}
	cfg := a.GetConfig()
	if cfg == nil {
		return defaults
	}
	resolved := cfg.PersistentContext.Resolve()
	return ProactiveContextConfig{
		MinRelevanceScore:    resolved.MinRelevanceScore,
		MaxContextualResults: resolved.MaxContextualResults,
		MaxContextChars:      resolved.MaxContextChars,
		// WorkspaceScopedRetrieval is a bool, so it carries through directly.
		WorkspaceScoped: resolved.WorkspaceScopedRetrieval,
		RetentionDays:   resolved.RetentionDays,
	}
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
