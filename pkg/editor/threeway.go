package editor

import (
	"fmt"

	"github.com/sergi/go-diff/diffmatchpatch"
)

// ApplyThreeWayMerge attempts to apply changes from base→proposed onto current.
// It creates a patch between base and proposed, then applies it to current.
// Returns the merged text and whether any hunks failed to apply (hadConflicts).
func ApplyThreeWayMerge(base, current, proposed string) (merged string, hadConflicts bool, err error) {
	if base == current {
		// Fast path: no divergence, accept proposed
		return proposed, false, nil
	}
	if proposed == current {
		// Already applied
		return current, false, nil
	}
	dmp := diffmatchpatch.New()
	patches := dmp.PatchMake(base, proposed)
	mergedText, results := dmp.PatchApply(patches, current)
	// Detect any failed hunks
	for _, ok := range results {
		if !ok {
			hadConflicts = true
			break
		}
	}
	if hadConflicts {
		return "", true, fmt.Errorf("three-way merge conflict: could not apply some hunks cleanly")
	}
	return mergedText, false, nil
}
