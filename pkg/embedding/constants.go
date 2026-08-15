package embedding

import "time"

// Similarity thresholds derived from retrieval_quality_test.go and
// prefix_symmetry_test.go measurements against the active model.
//
// Prior values (0.90 duplicate, 0.85 file-check, 0.75 search) were unreachable:
// near-duplicates top out ~0.84 symmetric, correct hits near 0.5.
const (
	// DefaultDuplicateThreshold gates code-vs-code duplicate detection.
	// Measured through CheckFileForDuplicates with documentPrefix on both sides.
	// 0.65 sits between near-duplicates (0.767) and related code (0.492).
	DefaultDuplicateThreshold = 0.65

	// DefaultRelatedCodeThreshold gates "related code" injection into read_file results.
	// Related code barely separates from unrelated (0.421 vs 0.368), so 0.55
	// keeps injected context closer to genuine near-duplicates.
	DefaultRelatedCodeThreshold = 0.55

	// DefaultSemanticSearchThreshold gates natural-language search over code.
	// Asymmetric query/doc embeddings: correct hits 0.499-0.613, wrong answers ~0.32.
	DefaultSemanticSearchThreshold = 0.40

	// DefaultConversationSearchThreshold gates search over the CONVERSATION store
	// (turns, rollups, memories). No task prefix; scores not comparable to code index.
	// 0.45 matches what semantic recall uses against the same store.
	DefaultConversationSearchThreshold = 0.45

	// Thresholds for Jina Code v2 (sole embedding model).
	// Measured via code_model_eval_test.go:
	// near-duplicate: 0.756, related: 0.459, unrelated: 0.066,
	// NL-code correct: 0.343-0.823, wrong: -0.07-0.20

	// DefaultCodeModelDuplicateThreshold gates duplicate detection.
	DefaultCodeModelDuplicateThreshold = 0.65

	// DefaultCodeModelRelatedThreshold gates "related code" injection.
	DefaultCodeModelRelatedThreshold = 0.35

	// DefaultCodeModelSemanticSearchThreshold gates NL-code search via Jina.
	DefaultCodeModelSemanticSearchThreshold = 0.30
)

// WalkTimeout is the maximum time for WalkCodeFiles to enumerate files.
const WalkTimeout = 30 * time.Second

// MaxDepth limits WalkCodeFiles directory nesting (avoids pathological trees).
const MaxDepth = 15

// MaxFileCount caps files WalkCodeFiles will collect.
const MaxFileCount = 10000

// MaxIndexableFileBytes caps the size of ANY file the index will read —
// code files (via ExtractFromFile) and file-level non-code files alike.
// Files larger than this are skipped entirely: index bodies truncate to
// 8 KB anyway, so reading a multi-GB file (generated code bundles, data
// corpora) would only waste memory and walk budget. The read paths also
// use this as their LimitReader bound, so even a race between stat and
// open can't blow memory.
const MaxIndexableFileBytes int64 = 1 << 20 // 1 MiB

// ProgressInterval is the file count between progress events during walk and embedding.
const ProgressInterval = 500

// BuildTimeout is the max duration for the full index build lifecycle.
// Sized for this repo: ~12k units at ~5.3 units/s ≈ 40 min cold build.
const BuildTimeout = 45 * time.Minute

// BuildLockTimeout is the max time to poll (50ms intervals) for the cross-process
// build lock after the initial non-blocking attempt fails.
const BuildLockTimeout = 2 * time.Second

// EmbedBatchSize is the number of code units per ONNX inference call.
const EmbedBatchSize = 32

// autoBuildTimeout calculates an adaptive timeout for background index builds.
func autoBuildTimeout(fileCount int) time.Duration {
	const (
		perEmbedBudget = 250 * time.Millisecond // measured ~189ms/unit + headroom
		baseOverhead   = 30 * time.Second       // model load + walk + I/O
		minTimeout     = 2 * time.Minute
		maxTimeout     = 45 * time.Minute
	)
	// fileCount maps roughly to fileCount * 4 code units (avg symbols/file).
	// Use perEmbedBudget on the estimated unit count.
	estimatedUnits := fileCount * 4
	d := baseOverhead + time.Duration(estimatedUnits)*perEmbedBudget
	if d < minTimeout {
		return minTimeout
	}
	if d > maxTimeout {
		return maxTimeout
	}
	return d
}
