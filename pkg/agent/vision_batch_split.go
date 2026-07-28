// Package agent — SP-103-D2: batch splitting with fallback.
//
// Provides proactive batch splitting for vision images to avoid provider
// 400 (context overflow) errors. The splitter considers both image count
// and total payload bytes, routing overflow images to the existing OCR
// fallback path so the model still gets text descriptions of images that
// exceed the provider's inline limits.

package agent

import api "github.com/sprout-foundry/sprout/pkg/agent_api"

// BatchSplitResult describes how a set of images should be split between
// inline multimodal processing and OCR fallback.
type BatchSplitResult struct {
	// InlineIndices holds the indices of images that should be sent inline
	// as multimodal content.
	InlineIndices []int
	// OverflowIndices holds the indices of images that should be processed
	// via OCR fallback.
	OverflowIndices []int
}

// BatchSplit proactively determines which images fit within the provider's
// vision context window based on count and total payload size. Unlike a
// simple count-based split, it also considers total payload bytes to avoid
// provider 400 (context overflow) errors when embedding many/large images.
//
// caps should already be resolved through VisionCapabilitiesOrDefault
// before calling this function so that zero-valued fields are replaced
// with safe defaults.
//
// Algorithm: Greedy — images are taken in order until either the count limit
// (MaxImageCount) or the total byte budget (MaxImageBytes × MaxImageCount)
// is reached. The function is proactive: it splits before any provider call
// so the caller can route overflow images through OCR fallback.
func BatchSplit(sizes []int, caps api.VisionCapabilities) BatchSplitResult {
	n := len(sizes)
	if n == 0 {
		return BatchSplitResult{}
	}

	maxImageCount := caps.MaxImageCount
	maxTotalBytes := caps.MaxImageBytes * caps.MaxImageCount

	// Fast path: if count and estimated payload both fit, return all inline.
	if n <= maxImageCount {
		total := 0
		for _, s := range sizes {
			total += s
		}
		if total <= maxTotalBytes {
			return BatchSplitResult{
				InlineIndices: makeRange(0, n),
			}
		}
	}

	// Greedy: take images in order until count or byte limit is hit.
	var inline, overflow []int
	accumulated := 0

	for i, size := range sizes {
		if len(inline) < maxImageCount && accumulated+size <= maxTotalBytes {
			inline = append(inline, i)
			accumulated += size
		} else {
			overflow = append(overflow, i)
		}
	}

	// If all images fit (e.g. byte budget was generous enough), return all.
	if len(overflow) == 0 {
		return BatchSplitResult{
			InlineIndices: makeRange(0, n),
		}
	}

	return BatchSplitResult{
		InlineIndices:   inline,
		OverflowIndices: overflow,
	}
}

// makeRange returns [start, end) as a slice of ints.
func makeRange(start, end int) []int {
	r := make([]int, 0, end-start)
	for i := start; i < end; i++ {
		r = append(r, i)
	}
	return r
}
