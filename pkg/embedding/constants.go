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
