package tools

// Embedding-based command risk classifier — an augmentation layer over the
// heuristic classifier (security_classifier.go) that reduces false positives.
//
// PROBLEM: The heuristic classifier over-flags common dev commands (npm test,
// rm -rf node_modules, go build) as CAUTION because it relies on prefix
// matching and a catch-all CAUTION default. Users develop prompt fatigue and
// start auto-approving everything, which undermines the security gate.
//
// APPROACH: Embed the command and compare it against a labeled corpus of safe
// and dangerous commands (cmd_corpus.go) using KNN. If the nearest neighbors
// are overwhelmingly SAFE with high similarity, downgrade the heuristic CAUTION
// result to SAFE.
//
// SAFETY CONTRACT: This classifier can ONLY downgrade CAUTION → SAFE. It must
// NEVER downgrade DANGEROUS or CRITICAL. If unavailable (no embedding model,
// init failure, low confidence), it returns the heuristic result unchanged.
// This guarantees the worst case is "no improvement" (heuristic wins), never a
// security regression.

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

// EmbeddingProvider is the minimal interface needed by the classifier. It is
// a subset of embedding.EmbeddingProvider. Defined locally to avoid an import
// cycle: pkg/embedding imports pkg/agent_tools (for the extractor), so
// pkg/agent_tools cannot import pkg/embedding. Go's structural interface
// typing means any embedding.EmbeddingProvider satisfies this interface.
type EmbeddingProvider interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Dimensions() int
	Name() string
}

// corpusVector is a pre-embedded corpus entry with its label.
type corpusVector struct {
	vec      []float32
	label    SecurityRisk
	cmd      string
	category string
}

// knnNeighbor is a corpus entry ranked by similarity to a query command.
type knnNeighbor struct {
	vec        []float32
	similarity float32
	label      SecurityRisk
	cmd        string
	category   string
}

// EmbeddingClassifier augments the heuristic classifier with embedding-based
// similarity matching against a labeled command corpus. Thread-safe: the
// corpus is read-only after Init, so concurrent Classify calls are safe.
type EmbeddingClassifier struct {
	provider EmbeddingProvider

	mu          sync.RWMutex
	ready       bool
	corpus      []corpusVector
	embedErr    error
	embedCtxKey struct{} // reserved for future per-call overrides

	// Configurable thresholds (conservative defaults).
	safeVoteShare     float32 // min fraction of K neighbors that must be SAFE
	topNeighborSim    float32 // min similarity of the single nearest neighbor
	k                 int     // number of neighbors to consider
	embedTimeout      time.Duration
}

// ClassifierOption configures an EmbeddingClassifier at construction.
type ClassifierOption func(*EmbeddingClassifier)

// WithSafeVoteShare sets the minimum SAFE-vote fraction (0-1) required to
// downgrade. Default 0.70.
func WithSafeVoteShare(f float32) ClassifierOption {
	return func(c *EmbeddingClassifier) { c.safeVoteShare = f }
}

// WithTopNeighborSim sets the minimum similarity of the nearest neighbor.
// Default 0.85.
func WithTopNeighborSim(f float32) ClassifierOption {
	return func(c *EmbeddingClassifier) { c.topNeighborSim = f }
}

// WithK sets the number of nearest neighbors to consider. Default 5.
func WithK(k int) ClassifierOption {
	return func(c *EmbeddingClassifier) { c.k = k }
}

// WithEmbedTimeout sets the per-command embed timeout. Default 2s.
func WithEmbedTimeout(d time.Duration) ClassifierOption {
	return func(c *EmbeddingClassifier) { c.embedTimeout = d }
}

// NewEmbeddingClassifier creates a classifier with conservative defaults.
// The corpus is embedded lazily on first Init call (takes ~1-2s for 200+
// commands). Until Init succeeds, Classify returns the heuristic result
// unchanged (IsReady() == false).
func NewEmbeddingClassifier(provider EmbeddingProvider, opts ...ClassifierOption) *EmbeddingClassifier {
	c := &EmbeddingClassifier{
		provider:       provider,
		safeVoteShare:   0.70,
		topNeighborSim:  0.85,
		k:               5,
		embedTimeout:    2 * time.Second,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Init embeds the corpus. Safe to call multiple times (idempotent after first
// success). Returns an error if the provider is nil or embedding fails. After
// success, IsReady() returns true and Classify will use embeddings.
func (c *EmbeddingClassifier) Init(ctx context.Context) error {
	if c == nil {
		return fmt.Errorf("nil classifier")
	}

	c.mu.RLock()
	if c.ready {
		c.mu.RUnlock()
		return nil
	}
	c.mu.RUnlock()

	if c.provider == nil {
		err := fmt.Errorf("embedding classifier: nil provider")
		c.mu.Lock()
		c.embedErr = err
		c.mu.Unlock()
		return err
	}

	corpusEntries := DefaultCommandCorpus()
	vectors := make([]corpusVector, 0, len(corpusEntries))

	for _, entry := range corpusEntries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		vec, err := c.provider.Embed(ctx, entry.Command)
		if err != nil {
			c.mu.Lock()
			c.embedErr = fmt.Errorf("embed corpus entry %q: %w", entry.Command, err)
			c.mu.Unlock()
			return c.embedErr
		}
		vectors = append(vectors, corpusVector{
			vec:      vec,
			label:    entry.Label,
			cmd:      entry.Command,
			category: entry.Category,
		})
	}

	c.mu.Lock()
	c.corpus = vectors
	c.ready = true
	c.embedErr = nil
	c.mu.Unlock()
	return nil
}

// IsReady reports whether the corpus has been embedded and the classifier is
// ready to make decisions.
func (c *EmbeddingClassifier) IsReady() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ready
}

// Classify augments the heuristic result with embedding evidence.
//
// Parameters:
//   - heuristicResult: the result from ClassifyToolCall (the existing heuristic)
//   - command: the shell command string
//
// Returns (newRisk, reasoning). reasoning is "" when the result is unchanged;
// otherwise it explains why the risk was downgraded (e.g. "downgraded
// CAUTION→SAFE: 5/5 nearest neighbors SAFE (vote share 100%), nearest
// 'npm test' (sim 0.95)").
//
// Decision logic (ONLY applies when heuristicResult == SecurityCaution):
//  1. Embed the command. On any error or timeout, return CAUTION unchanged.
//  2. Find the K nearest corpus neighbors by cosine similarity.
//  3. If ANY neighbor is DANGEROUS, refuse to downgrade (negative-evidence guard).
//  4. Compute SAFE-vote share (fraction of K neighbors that are SAFE).
//  5. Downgrade to SAFE only if: top-neighbor-similarity >= threshold AND
//     safe-vote-share >= threshold.
//  6. Otherwise return CAUTION unchanged.
//
// Never called for DANGEROUS/CRITICAL/SAFE heuristic results — the explicit
// gate at the top enforces this regardless of caller behavior.
func (c *EmbeddingClassifier) Classify(ctx context.Context, heuristicResult SecurityRisk, command string) (SecurityRisk, string) {
	// SAFETY: refuse to downgrade anything that isn't CAUTION. This is the
	// core safety contract — DANGEROUS and CRITICAL are never touched.
	if heuristicResult != SecurityCaution {
		return heuristicResult, ""
	}
	if c == nil || !c.IsReady() {
		return heuristicResult, ""
	}

	// Embed the command with a bounded timeout.
	embedCtx, cancel := context.WithTimeout(ctx, c.embedTimeout)
	defer cancel()
	vec, err := c.provider.Embed(embedCtx, command)
	if err != nil {
		return heuristicResult, ""
	}

	// Snapshot the corpus under a read lock.
	c.mu.RLock()
	corpus := c.corpus
	c.mu.RUnlock()

	// Compute similarity to every corpus entry, collect top-K.
	neighbors := c.knn(vec, corpus)
	if len(neighbors) == 0 {
		return heuristicResult, ""
	}

	// Negative-evidence guard: if ANY of the top-K neighbors is DANGEROUS,
	// refuse to downgrade — UNLESS the query is an exact (or near-exact)
	// match to a SAFE corpus entry. An exact match is the strongest signal
	// (the command is literally in our allowlist), so it overrides the
	// proximity-to-dangerous concern. This also handles the common case
	// where a safe cleanup command (rm -rf node_modules) shares tokens
	// with dangerous variants (rm -rf /) but is itself explicitly safe.
	top := neighbors[0]
	exactSafeMatch := top.similarity >= 0.99 && top.label == SecuritySafe
	if !exactSafeMatch {
		for _, n := range neighbors {
			if n.label == SecurityDangerous {
				return heuristicResult, ""
			}
		}
	}

	// Count SAFE votes among the top-K.
	safeCount := 0
	for _, n := range neighbors {
		if n.label == SecuritySafe {
			safeCount++
		}
	}
	safeShare := float32(safeCount) / float32(len(neighbors))

	// Downgrade only if both thresholds are met.
	if top.similarity >= c.topNeighborSim && safeShare >= c.safeVoteShare {
		var topCmds []string
		limit := 3
		for i, n := range neighbors {
			if i >= limit {
				break
			}
			topCmds = append(topCmds, n.cmd)
		}
		reason := fmt.Sprintf(
			"downgraded CAUTION→SAFE: %d/%d nearest neighbors SAFE (vote share %.0f%%), nearest %q (sim %.3f). Safe neighbors: %s",
			safeCount, len(neighbors), safeShare*100, top.cmd, top.similarity, joinStrings(topCmds),
		)
		return SecuritySafe, reason
	}

	return heuristicResult, ""
}

// knn returns the K nearest corpus entries to the query vector by cosine
// similarity, sorted descending. Returns at most K entries.
func (c *EmbeddingClassifier) knn(query []float32, corpus []corpusVector) []knnNeighbor {
	all := make([]knnNeighbor, 0, len(corpus))
	for _, cv := range corpus {
		sim := cosineSimilarity(query, cv.vec)
		all = append(all, knnNeighbor{
			vec:        cv.vec,
			similarity: sim,
			label:      cv.label,
			cmd:        cv.cmd,
			category:   cv.category,
		})
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].similarity > all[j].similarity
	})
	k := c.k
	if k > len(all) {
		k = len(all)
	}
	return all[:k]
}

// cosineSimilarity computes the cosine similarity between two vectors.
// Returns 0 for zero-length or mismatched vectors.
func cosineSimilarity(a, b []float32) float32 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		af := float64(a[i])
		bf := float64(b[i])
		dot += af * bf
		normA += af * af
		normB += bf * bf
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(normA) * math.Sqrt(normB)))
}

// joinStrings joins strings with ", ".
func joinStrings(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	out := ss[0]
	for _, s := range ss[1:] {
		out += ", " + s
	}
	return out
}

// ---------------------------------------------------------------------------
// Package-level wiring (for future integration from pkg/agent)
// ---------------------------------------------------------------------------

var (
	globalEmbeddingClassifier   *EmbeddingClassifier
	globalEmbeddingClassifierMu sync.RWMutex
)

// SetEmbeddingClassifier registers the package-level classifier used by the
// security pipeline. Pass nil to disable embedding-based augmentation.
func SetEmbeddingClassifier(c *EmbeddingClassifier) {
	globalEmbeddingClassifierMu.Lock()
	globalEmbeddingClassifier = c
	globalEmbeddingClassifierMu.Unlock()
}

// GetEmbeddingClassifier returns the package-level classifier, or nil if none
// is registered.
func GetEmbeddingClassifier() *EmbeddingClassifier {
	globalEmbeddingClassifierMu.RLock()
	defer globalEmbeddingClassifierMu.RUnlock()
	return globalEmbeddingClassifier
}
