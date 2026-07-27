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
const BuildTimeout = 10 * time.Minute

// EmbedBatchSize is the number of code units embedded per ONNX inference
// call during a full index build. The batch is what flows through
// provider.EmbedBatch — a single session.Run with a [batch, seq] tensor.
const EmbedBatchSize = 32

// autoBuildTimeout calculates an adaptive timeout for the background index
// build based on the number of files to process. Each batch of
// EmbedBatchSize units takes roughly 50ms of ONNX inference on a desktop
// CPU; on a constrained mobile device that can be 5–10x slower. The formula
// budgets generously (perEmbedBudget per unit) plus a fixed base for model
// loading and file I/O.
//
// The result is clamped to [2min, 15min] so tiny workspaces don't get an
// absurdly short budget and large ones don't run indefinitely.
func autoBuildTimeout(fileCount int) time.Duration {
	const (
		perEmbedBudget = 100 * time.Millisecond // generous per-unit budget
		baseOverhead   = 30 * time.Second       // model load + walk + I/O
		minTimeout     = 2 * time.Minute
		maxTimeout     = 15 * time.Minute
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
