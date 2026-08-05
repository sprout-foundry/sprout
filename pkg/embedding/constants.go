package embedding

import "time"

// Performance protection constants for the embedding index pipeline.
// These limits prevent UI hangs and runaway CPU usage on large workspaces.

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
