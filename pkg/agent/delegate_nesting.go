package agent

import (
	"os"
	"strconv"
)

// getMaxDelegateNestingDepth returns the maximum allowed delegate nesting depth.
// It reads from SPROUT_MAX_DELEGATE_DEPTH env var, defaulting to MaxDelegateNestingDepth (3).
func getMaxDelegateNestingDepth() int {
	envVal := os.Getenv("SPROUT_MAX_DELEGATE_DEPTH")
	if envVal == "" {
		return MaxDelegateNestingDepth
	}
	depth, err := strconv.Atoi(envVal)
	if err != nil || depth < 1 {
		return MaxDelegateNestingDepth
	}
	return depth
}
