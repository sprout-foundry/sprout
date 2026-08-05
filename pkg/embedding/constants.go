package embedding

import "time"

// Performance protection constants for the embedding index pipeline.
// These limits prevent UI hangs and runaway CPU usage on large workspaces.

// Similarity thresholds. These gate every consumer of the index, and they are
// only meaningful against the score distribution the model actually produces —
// which depends on the task prefixes used on each side of the comparison.
// Both values below are derived from measurements in retrieval_quality_test.go
// and prefix_symmetry_test.go, not from intuition.
//
// The previous values (0.90 for duplicates, 0.85 for the write-time file check,
// 0.75 for the semantic_search tool) were unreachable: a textbook near-duplicate
// tops out around 0.84 symmetric, and correct search hits land near 0.5. Every
// one of those consumers retrieved the right record and then discarded it, so
// the features silently did nothing.
const (
	// DefaultDuplicateThreshold gates code-vs-code duplicate detection.
	//
	// Measured end-to-end through CheckFileForDuplicates itself — not through
	// an isolated embed of two snippets, which sits ~0.13 higher and is what
	// made an earlier revision of this constant too strict again. Bands from
	// TestDuplicateDetectionFiresOnRealNearDuplicate:
	//
	//	near-duplicate (renamed identifiers) : 0.715
	//	related (same domain, different job) : 0.421
	//	unrelated                            : 0.368
	//
	// Both sides are embedded with documentPrefix — code compared against code.
	// Re-measured through the production path on that prefix:
	//
	//	near-duplicate (renamed identifiers) : 0.767
	//	related (same domain, different job) : 0.492
	//	unrelated                            : 0.355
	//
	// 0.65 sits in the wide gap, nearer the duplicate end: it fires on real
	// near-duplicates with margin to spare while leaving merely-related code
	// well clear, so a write is not interrupted by a false positive.
	DefaultDuplicateThreshold = 0.65

	// DefaultRelatedCodeThreshold gates "related code" injection into read_file
	// results — the same symmetric comparison, wanting the band below
	// duplicates.
	//
	// Caveat worth knowing before trusting this one: in the measurements above,
	// related code (0.421) barely separates from unrelated (0.368). The model
	// does not give this feature much signal to work with, so no threshold
	// makes it reliably precise. 0.55 keeps injected context closer to genuine
	// near-duplicates, on the reasoning that a wrong injection costs
	// context-window budget on every read_file. If this feature is ever
	// evaluated on its merits, that 0.05 gap — not the threshold — is the
	// thing to argue about.
	DefaultRelatedCodeThreshold = 0.55

	// DefaultSemanticSearchThreshold gates natural-language search over code.
	// Query and document embeddings are deliberately asymmetric, so correct
	// hits score far lower than duplicate pairs. Measured with codeQueryPrefix
	// on the benchmark in retrieval_quality_test.go: correct top-1 results run
	// 0.499-0.613 (10/10 recall@3), and wrong answers sit near 0.32. 0.40
	// leaves margin below the observed floor for queries harder than the
	// benchmark while still trimming noise — ranking plus top-K does the real
	// filtering here.
	DefaultSemanticSearchThreshold = 0.40

	// DefaultConversationSearchThreshold gates search over the CONVERSATION
	// store (turns, rollups, memories) rather than the code index.
	//
	// That store is a different embedding space: it is written and queried
	// with no task prefix at all, so its scores are not comparable to the code
	// index's and thresholds must not be copied between the two. Measured
	// pairwise over real stored turns and memories: p25 0.40, median 0.50,
	// p75 0.61, max 0.81. A 0.75 gate — inherited from the code-search side —
	// admits only the top 4% of pairs, which for a *search* is effectively off.
	// 0.45 matches what semantic recall already uses against the same store.
	DefaultConversationSearchThreshold = 0.45
)

// WalkTimeout is the absolute maximum time allowed for WalkCodeFiles to
// enumerate files across the workspace. After this duration the walk is
// cancelled and a partial result is returned.
const WalkTimeout = 30 * time.Second

// MaxDepth is the maximum directory nesting depth WalkCodeFiles will
// descend into. Deeper directories are pruned to avoid pathological
// directory trees (e.g., deeply nested generated code).
const MaxDepth = 15

// MaxFileCount is the maximum number of source files WalkCodeFiles will
// collect before stopping. Once this limit is reached, the walk exits
// early and returns the files collected so far.
const MaxFileCount = 10000

// ProgressInterval controls how many files must be processed before a
// progress event is emitted (both during walk and batch embedding).
const ProgressInterval = 500

// BuildTimeout is the absolute maximum for the full index build lifecycle
// (ONNX init + directory walk + file parsing + batch embedding). This is
// used by BuildIndexBackground so a background build on a large workspace
// does not get killed after WalkTimeout (30s), which only covers the
// directory-walk phase.
//
// Sized against measurement, not intuition: this repository extracts ~12k
// units and embeds at ~5.3 units/s (TestIndexThroughputProbe), so a cold full
// build is ~40 minutes. At the previous 10 minutes a first build could not
// finish on any real repository. Overrunning is no longer destructive — an
// interrupted build now records only the files it finished and resumes — but
// finishing in one pass is the difference between an index that is usable
// today and one that is usable in three days.
const BuildTimeout = 45 * time.Minute

// EmbedBatchSize is the number of code units embedded per ONNX inference
// call during a full index build. The batch is what flows through
// provider.EmbedBatch — a single session.Run with a [batch, seq] tensor.
const EmbedBatchSize = 32

// autoBuildTimeout calculates an adaptive timeout for the background index
// build based on the number of files to process.
//
// perEmbedBudget is measured, not estimated. TestIndexThroughputProbe on this
// repository records ~189ms per unit end-to-end (5.3 units/s) on an M1 Pro
// after length-sorted batching; the previous 100ms figure was derived from an
// assumed "50ms per 32-unit batch" that is off by two orders of magnitude, so
// every non-trivial workspace silently overran its budget. 250ms leaves
// headroom for slower CPUs without being unbounded.
//
// The result is clamped to [2min, 45min] so tiny workspaces don't get an
// absurdly short budget and large ones don't run indefinitely. Overrun is
// survivable — BuildIndex records only the files it finished, so the next
// build resumes rather than freezing the index at a partial count.
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
