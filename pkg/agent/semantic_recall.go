package agent

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// SP-066 Phase 3: semantic recall of historical conversation summaries.
//
// On user-turn ingest, embed the user message, query the conversation store
// for top-K similar past summaries (per-turn or rollup), and inject any
// that aren't already in the current substitution window as a "recalled
// context" block on the next prompt. The store is permanent memory; this
// surface lets a long-running session re-reference work the user has long
// since rolled past.

// Tunable defaults for the recall pipeline. Conservative values to start;
// tune from the recall_diagnostic telemetry once we see real sessions.
const (
	// semanticRecallTopK is the number of nearest neighbors retrieved
	// from the store before recency filtering.
	semanticRecallTopK = 3

	// semanticRecallSimilarityThreshold is the minimum cosine similarity
	// required for a candidate to be considered. Below this, recall stays
	// silent to avoid injecting irrelevant context.
	semanticRecallSimilarityThreshold = 0.45

	// semanticRecallHalfLifeDays is the recency decay half-life in days.
	// Older summaries score lower; the effective ranking is
	// cosine_similarity × exp(−age_days × ln(2) / half_life_days).
	semanticRecallHalfLifeDays = 7.0

	// semanticRecallMaxInjectedChars caps the total size of injected
	// recall blocks per turn so we don't undo the substitution savings.
	semanticRecallMaxInjectedChars = 8000
)

// RecalledItem is one historical summary retrieved by the semantic recall
// pass and surfaced to the model on the next prompt. It carries both the
// scored numbers (for telemetry) and the text the prompt will render.
type RecalledItem struct {
	CheckpointID string
	Level        int
	StartIndex   int
	EndIndex     int
	Similarity   float32
	AgeDays      float64
	Score        float64
	Summary      string
	Actionable   string
	Workspace    string
}

// recallRetrievalDiagnostic captures the per-call summary that the recall
// telemetry event surfaces. Lives alongside the items so callers can ship
// both in one publish.
type recallRetrievalDiagnostic struct {
	EmbedDurationMS      float64
	CandidatesConsidered int
	Injected             int
	InjectedChars        int
	TopScores            []float32
}

// retrieveSemanticRecall runs the embed + query + recency-rerank + filter
// pipeline and returns the list of items worth surfacing on the next
// prompt. Pure function over its inputs so it's easy to unit-test without
// spinning up an embedding provider.
//
// presentCheckpointIDs is the set of checkpoint IDs already present in the
// active substitution window. Matches in this set are skipped — the model
// will already see them via the normal substitute-on-every-prompt-build pass.
//
// limit overrides semanticRecallTopK for the returned slice length; the raw
// retrieval count uses limit*4 for recency re-ranking headroom.
func retrieveSemanticRecall(
	ctx context.Context,
	mgr *embedding.EmbeddingManager,
	sessionID string,
	query string,
	presentCheckpointIDs map[string]struct{},
	limit int,
	now time.Time,
) ([]RecalledItem, recallRetrievalDiagnostic, error) {
	var diag recallRetrievalDiagnostic
	if mgr == nil {
		return nil, diag, nil
	}
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, diag, nil
	}

	store, err := mgr.GetConversationStore(ctx)
	if err != nil {
		return nil, diag, fmt.Errorf("get conversation store: %w", err)
	}
	provider := store.Provider()
	if provider == nil {
		return nil, diag, fmt.Errorf("conversation store provider is nil")
	}

	embedStart := time.Now()
	vec, err := provider.Embed(ctx, q)
	diag.EmbedDurationMS = float64(time.Since(embedStart).Microseconds()) / 1000.0
	if err != nil {
		return nil, diag, fmt.Errorf("embed query: %w", err)
	}
	if len(vec) == 0 {
		return nil, diag, nil
	}

	// Pull more than topK so we have headroom to re-rank by recency before
	// applying the final cut. 4× limit is a balance between work and the
	// chance that the best-by-recency item is outside the cosine top-K.
	rawResults, err := store.Query(vec, limit*4, semanticRecallSimilarityThreshold)
	if err != nil {
		return nil, diag, fmt.Errorf("query store: %w", err)
	}
	diag.CandidatesConsidered = len(rawResults)

	if len(rawResults) == 0 {
		return nil, diag, nil
	}

	// Re-rank by similarity × exp(−age × ln(2) / half_life). This makes a
	// year-old high-similarity match lose to a recent moderate-similarity
	// match, which matches the "recency bias" goal from the design.
	candidates := make([]RecalledItem, 0, len(rawResults))
	topScores := make([]float32, 0, len(rawResults))
	halfLife := semanticRecallHalfLifeDays
	if halfLife <= 0 {
		halfLife = 1 // guard against misconfiguration
	}
	decayRate := math.Ln2 / halfLife

	for _, r := range rawResults {
		item, ok := recalledItemFromRecord(r, sessionID, now, decayRate)
		if !ok {
			continue
		}
		if _, present := presentCheckpointIDs[item.CheckpointID]; present {
			continue
		}
		candidates = append(candidates, item)
		topScores = append(topScores, r.Similarity)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}

	diag.TopScores = topScores
	return candidates, diag, nil
}

// recalledItemFromRecord converts a store QueryResult into a RecalledItem,
// applying the recency decay. Returns ok=false when the record's metadata
// is missing the fields recall needs (covers schema drift gracefully —
// older per-turn embeddings written before SP-066 won't have a
// checkpoint_id and just won't be eligible).
func recalledItemFromRecord(r embedding.QueryResult, expectedSessionID string, now time.Time, decayRate float64) (RecalledItem, bool) {
	// Filter to the active session's records. Cross-session recall is
	// explicitly out of scope per SP-066's "Out of Scope" section.
	if expectedSessionID != "" {
		if sid, _ := r.Record.Metadata["sessionId"].(string); sid != "" && sid != expectedSessionID {
			return RecalledItem{}, false
		}
	}

	indexedAt := r.Record.IndexedAt
	if indexedAt.IsZero() {
		indexedAt = now
	}
	ageDays := now.Sub(indexedAt).Hours() / 24.0
	if ageDays < 0 {
		ageDays = 0
	}
	score := float64(r.Similarity) * math.Exp(-ageDays*decayRate)

	item := RecalledItem{
		Similarity: r.Similarity,
		AgeDays:    ageDays,
		Score:      score,
	}

	// Pull metadata fields if present. Both per-turn and rollup records
	// expose checkpoint_id; legacy per-turn records (pre-SP-066) may not.
	if v, ok := r.Record.Metadata["checkpoint_id"].(string); ok {
		item.CheckpointID = v
	}
	if v, ok := r.Record.Metadata["level"].(int); ok {
		item.Level = v
	} else if v, ok := r.Record.Metadata["level"].(float64); ok {
		// JSON deserialization stores numbers as float64.
		item.Level = int(v)
	}
	if v, ok := r.Record.Metadata["start_index"].(float64); ok {
		item.StartIndex = int(v)
	}
	if v, ok := r.Record.Metadata["end_index"].(float64); ok {
		item.EndIndex = int(v)
	}
	if v, ok := r.Record.Metadata["actionable_summary"].(string); ok {
		item.Actionable = v
	}
	if v, ok := r.Record.Metadata["workingDir"].(string); ok {
		item.Workspace = v
	}

	item.Summary = strings.TrimSpace(r.Record.Signature)
	if item.Summary == "" {
		// Some legacy records may have an empty Signature; fall back to a
		// short placeholder so the injected block isn't a blank.
		return RecalledItem{}, false
	}

	return item, true
}

// FormatSemanticRecall renders the recall items as a markdown block to
// inject into the system supplement. Returns "" when there's nothing
// to inject so the caller can short-circuit.
//
// maxChars caps the total output size. Use semanticRecallMaxInjectedChars
// (8000) for the default ceiling, or a model-aware value for per-model tuning.
func FormatSemanticRecall(items []RecalledItem, maxChars int) string {
	if len(items) == 0 {
		return ""
	}
	if maxChars <= 0 {
		maxChars = semanticRecallMaxInjectedChars
	}

	var b strings.Builder
	b.WriteString("# Recalled From Session History\n\n")
	b.WriteString("The following past summaries from this session are semantically related to the user's current message. They are read-only context — use them to maintain continuity without re-asking the user.\n\n")

	total := 0
	for _, item := range items {
		block := formatRecalledBlock(item)
		if total+len(block) > maxChars {
			break
		}
		b.WriteString(block)
		total += len(block)
	}
	return b.String()
}

func formatRecalledBlock(item RecalledItem) string {
	var b strings.Builder
	if item.Level > 0 {
		fmt.Fprintf(&b, "## Recalled (level-%d rollup, similarity %.2f, %.0fd ago)\n\n", item.Level, item.Similarity, item.AgeDays)
	} else {
		fmt.Fprintf(&b, "## Recalled (turn, similarity %.2f, %.0fd ago)\n\n", item.Similarity, item.AgeDays)
	}
	b.WriteString(strings.TrimSpace(item.Summary))
	b.WriteString("\n\n")
	if a := strings.TrimSpace(item.Actionable); a != "" && a != strings.TrimSpace(item.Summary) {
		b.WriteString("Actionable: ")
		b.WriteString(a)
		b.WriteString("\n\n")
	}
	return b.String()
}

// Recall runs the semantic-recall pipeline over the conversation store and
// returns the items worth surfacing, capped at `limit`. It is the pure-data
// sibling of InjectSemanticRecall: no formatting, no system-supplement
// mutation, no telemetry publish. Used by:
//   - InjectSemanticRecall (the in-loop wrapper that adds formatting)
//   - the future /recall CLI command (SP-092-2)
//   - the future webui /api/recall endpoint (SP-092-3)
//
// Returns (nil, nil) when:
//   - the agent or its embedding manager is missing
//   - the query is blank after trim
//   - limit <= 0
//
// `limit` replaces the hardcoded semanticRecallTopK (3) for the slice length;
// the same gating constants (recency decay, similarity threshold) still apply
// inside retrieveSemanticRecall.
func (a *Agent) Recall(ctx context.Context, query string, limit int) ([]RecalledItem, error) {
	if a == nil {
		return nil, nil
	}
	if limit <= 0 {
		return nil, nil
	}
	mgr := a.GetEmbeddingManager()
	if mgr == nil {
		return nil, nil
	}

	sessionID := ""
	if a.state != nil {
		sessionID = a.state.GetSessionID()
	}

	presentIDs := a.presentCheckpointIDs()

	items, _, err := retrieveSemanticRecall(ctx, mgr, sessionID, query, presentIDs, limit, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	return items, nil
}

// InjectSemanticRecall runs recall over the current user query and appends
// the formatted block (if any) to the pending system supplement.
// Mirrors InjectProactiveContext — same shape, graceful degradation on
// every failure mode (no embedding manager, no store, embed failure, etc).
func (a *Agent) InjectSemanticRecall(ctx context.Context, query string) {
	if a == nil {
		return
	}
	items, err := a.Recall(ctx, query, semanticRecallTopK)
	if err != nil {
		a.Logger().Debug("[semantic-recall] retrieval failed: %v\n", err)
		return
	}
	if len(items) == 0 {
		return
	}

	// Compute a model-aware char budget: 2% of context window, converted
	// from tokens to chars (~4 chars/token), floored at 2000, ceiling at
	// semanticRecallMaxInjectedChars (8000).
	maxChars := computeRecallMaxChars(a.GetMaxContextTokens())

	formatted := FormatSemanticRecall(items, maxChars)
	if formatted == "" {
		return
	}

	// Publish telemetry (the diagnostic is best-effort — we re-summarize
	// here so the existing PublishRecallDiagnostic signature stays stable).
	//
	// TODO(SP-095 — see TODO.md instrumentation ticket): Recall discards the
	// retrieval-time diag from retrieveSemanticRecall, so we lose the
	// following per-call fields here:
	//   - EmbedDurationMS      (provider.Embed wall time)
	//   - CandidatesConsidered (raw store.Query result count)
	//   - TopScores            (raw cosine similarities for telemetry)
	// Re-introduce them when the instrumentation follow-up lands. Recall's
	// signature may grow to return the diag, or we may wrap it.
	diag := recallRetrievalDiagnostic{
		Injected:      len(items),
		InjectedChars: len(formatted),
	}
	a.PublishRecallDiagnostic(diag)

	// Prepend any existing supplement (proactive context, previous-session
	// continuity) so all sections are preserved in chronological-of-injection
	// order. The semantic-recall block goes last so it sits closest to the
	// user message.
	existing := a.consumePendingSystemSupplement()
	combined := formatted
	if strings.TrimSpace(existing) != "" {
		combined = existing + "\n\n" + formatted
	}
	a.setPendingSystemSupplement(combined)

	debugLogf("[semantic-recall] injected %d items (%d chars)", len(items), len(formatted))
}

// computeRecallMaxChars returns a model-aware character budget for semantic
// recall injection. It targets 2% of the model's context window, converted
// from tokens to chars (~4 chars/token), with a floor of 2000 and a ceiling
// of semanticRecallMaxInjectedChars (8000).
func computeRecallMaxChars(maxTokens int) int {
	if maxTokens <= 0 {
		return semanticRecallMaxInjectedChars
	}
	chars := int(float64(maxTokens) * 0.02 * 4)
	if chars < 2000 {
		chars = 2000
	}
	if chars > semanticRecallMaxInjectedChars {
		chars = semanticRecallMaxInjectedChars
	}
	return chars
}

// presentCheckpointIDs returns the set of checkpoint IDs currently in the
// active TurnCheckpoints slice. Used by recall to skip matches that the
// substitution pass will already emit, so we don't double-render the same
// summary.
func (a *Agent) presentCheckpointIDs() map[string]struct{} {
	if a == nil {
		return nil
	}
	cps := a.copyTurnCheckpoints()
	if len(cps) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(cps))
	for _, cp := range cps {
		if cp.ID != "" {
			out[cp.ID] = struct{}{}
		}
	}
	return out
}
