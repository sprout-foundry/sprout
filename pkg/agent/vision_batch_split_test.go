package agent

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// ---------------------------------------------------------------------------
// BatchSplit — SP-103-D2
// ---------------------------------------------------------------------------

func TestBatchSplit_NoImages(t *testing.T) {
	result := BatchSplit([]int{}, api.VisionCapabilitiesDefault())
	if len(result.InlineIndices) != 0 {
		t.Errorf("expected empty InlineIndices for no images, got %v", result.InlineIndices)
	}
	if len(result.OverflowIndices) != 0 {
		t.Errorf("expected empty OverflowIndices for no images, got %v", result.OverflowIndices)
	}
}

func TestBatchSplit_AllFitByCount(t *testing.T) {
	// 3 images, MaxImageCount=20 → count fits
	// sizes: [100, 200, 300], total=600, maxTotalBytes=5_000_000*20=100_000_000 → all fit
	caps := api.VisionCapabilities{
		MaxImageCount: 20,
		MaxImageBytes: 5_000_000,
	}
	sizes := []int{100, 200, 300}
	result := BatchSplit(sizes, caps)

	if len(result.InlineIndices) != 3 {
		t.Errorf("expected 3 inline indices, got %d: %v", len(result.InlineIndices), result.InlineIndices)
	}
	if len(result.OverflowIndices) != 0 {
		t.Errorf("expected 0 overflow indices, got %d: %v", len(result.OverflowIndices), result.OverflowIndices)
	}
	// Verify all indices present
	verifyIndicesCoverAll(t, 3, result)
}

func TestBatchSplit_AllFitByCountButExceedBytes(t *testing.T) {
	// 3 images, MaxImageCount=20 → count fits
	// sizes: [100_000_000, 1, 1], maxTotalBytes=5_000_000*20=100_000_000
	// total=100_000_002 > 100_000_000 → split needed
	// Greedy: idx0(100M) ≤ 100M → inline (exactly fills budget)
	// idx1(1): accumulated(100M)+1=100_000_001 > 100M → overflow
	// idx2(1): overflow
	caps := api.VisionCapabilities{
		MaxImageCount: 20,
		MaxImageBytes: 5_000_000,
	}
	sizes := []int{100_000_000, 1, 1}
	result := BatchSplit(sizes, caps)

	if len(result.InlineIndices) != 1 {
		t.Errorf("expected 1 inline index (idx0 exactly fills budget), got %d: %v", len(result.InlineIndices), result.InlineIndices)
	}
	if len(result.OverflowIndices) != 2 {
		t.Errorf("expected 2 overflow indices (idx1, idx2), got %d: %v", len(result.OverflowIndices), result.OverflowIndices)
	}
	verifyIndicesCoverAll(t, 3, result)

	if result.InlineIndices[0] != 0 {
		t.Errorf("expected inline [0], got %v", result.InlineIndices)
	}
}

func TestBatchSplit_ExceedsCount(t *testing.T) {
	// 25 images, MaxImageCount=20 → split by count
	// sizes: all 1 byte each, total 25 bytes, maxTotalBytes=100_000_000
	// First 20 inline, last 5 overflow
	caps := api.VisionCapabilities{
		MaxImageCount: 20,
		MaxImageBytes: 5_000_000,
	}
	sizes := make([]int, 25)
	for i := range sizes {
		sizes[i] = 1
	}
	result := BatchSplit(sizes, caps)

	if len(result.InlineIndices) != 20 {
		t.Errorf("expected 20 inline indices, got %d: %v", len(result.InlineIndices), result.InlineIndices)
	}
	if len(result.OverflowIndices) != 5 {
		t.Errorf("expected 5 overflow indices, got %d: %v", len(result.OverflowIndices), result.OverflowIndices)
	}
	verifyIndicesCoverAll(t, 25, result)

	// First 20 should be inline, last 5 overflow
	for i := 0; i < 20; i++ {
		if result.InlineIndices[i] != i {
			t.Errorf("expected inline[%d] = %d, got %d", i, i, result.InlineIndices[i])
		}
	}
	for i := 0; i < 5; i++ {
		if result.OverflowIndices[i] != 20+i {
			t.Errorf("expected overflow[%d] = %d, got %d", i, 20+i, result.OverflowIndices[i])
		}
	}
}

func TestBatchSplit_ExceedsBothCountAndBytes(t *testing.T) {
	// 25 images, MaxImageCount=20
	// sizes: first 10 are 10MB each (10_000_000), rest are small
	// maxTotalBytes = 5_000_000*20 = 100_000_000
	// Greedy: idx0-9 (10M each) accumulate to 100M → inline exactly
	// idx10-24 → overflow (count also exceeded)
	caps := api.VisionCapabilities{
		MaxImageCount: 20,
		MaxImageBytes: 5_000_000,
	}
	sizes := make([]int, 25)
	for i := 0; i < 10; i++ {
		sizes[i] = 10_000_000 // 10 MB
	}
	for i := 10; i < 25; i++ {
		sizes[i] = 1
	}
	result := BatchSplit(sizes, caps)

	if len(result.InlineIndices) != 10 {
		t.Errorf("expected 10 inline indices, got %d: %v", len(result.InlineIndices), result.InlineIndices)
	}
	if len(result.OverflowIndices) != 15 {
		t.Errorf("expected 15 overflow indices, got %d: %v", len(result.OverflowIndices), result.OverflowIndices)
	}
	verifyIndicesCoverAll(t, 25, result)
}

func TestBatchSplit_SingleImageExceedsByteBudget(t *testing.T) {
	// 1 image, size = 200_000_000 > 100_000_000 → even single image exceeds
	caps := api.VisionCapabilities{
		MaxImageCount: 20,
		MaxImageBytes: 5_000_000,
	}
	sizes := []int{200_000_000}
	result := BatchSplit(sizes, caps)

	if len(result.InlineIndices) != 0 {
		t.Errorf("expected 0 inline indices for oversized single image, got %d: %v", len(result.InlineIndices), result.InlineIndices)
	}
	if len(result.OverflowIndices) != 1 {
		t.Errorf("expected 1 overflow index for oversized single image, got %d: %v", len(result.OverflowIndices), result.OverflowIndices)
	}
}

func TestBatchSplit_EmptyCapsDefaults(t *testing.T) {
	// Empty caps (zero values) — caller should resolve with VisionCapabilitiesOrDefault
	caps := api.VisionCapabilitiesOrDefault(api.VisionCapabilities{})
	// MaxImageCount=20, MaxImageBytes=5_000_000, maxTotalBytes=100_000_000
	sizes := []int{1_000_000, 2_000_000, 3_000_000} // total 6M ≤ 100M, count 3 ≤ 20
	result := BatchSplit(sizes, caps)

	if len(result.InlineIndices) != 3 {
		t.Errorf("expected 3 inline indices with default caps, got %d: %v", len(result.InlineIndices), result.InlineIndices)
	}
	if len(result.OverflowIndices) != 0 {
		t.Errorf("expected 0 overflow indices with default caps, got %d: %v", len(result.OverflowIndices), result.OverflowIndices)
	}
}

func TestBatchSplit_AllFitWithDefaultCaps(t *testing.T) {
	// All zeros caps resolved to defaults
	caps := api.VisionCapabilitiesOrDefault(api.VisionCapabilities{})
	sizes := []int{100, 200, 300}
	result := BatchSplit(sizes, caps)

	if len(result.InlineIndices) != 3 {
		t.Errorf("expected 3 inline indices, got %d: %v", len(result.InlineIndices), result.InlineIndices)
	}
	if len(result.OverflowIndices) != 0 {
		t.Errorf("expected 0 overflow indices, got %d: %v", len(result.OverflowIndices), result.OverflowIndices)
	}
}

func TestBatchSplit_VerifyIndicesCorrect(t *testing.T) {
	// Verify indices are correct, non-overlapping, and cover all inputs
	caps := api.VisionCapabilities{
		MaxImageCount: 20,
		MaxImageBytes: 5_000_000,
	}
	// maxTotalBytes = 100_000_000
	// Greedy: idx0(10) inline, idx1(100M) exceeds 100M → overflow
	// idx2(20) inline (10+20=30 ≤ 100M), idx3(30) inline (30+30=60 ≤ 100M)
	// idx4(50M) inline (60+50M=50_000_060 ≤ 100M) → all remaining fit
	sizes := []int{10, 100_000_000, 20, 30, 50_000_000}
	result := BatchSplit(sizes, caps)

	verifyIndicesCoverAll(t, 5, result)

	// idx1 (100M) should be overflow, everything else inline
	if len(result.InlineIndices) != 4 {
		t.Errorf("expected 4 inline indices, got %d: %v", len(result.InlineIndices), result.InlineIndices)
	}
	if len(result.OverflowIndices) != 1 {
		t.Errorf("expected 1 overflow index, got %d: %v", len(result.OverflowIndices), result.OverflowIndices)
	}
	if result.OverflowIndices[0] != 1 {
		t.Errorf("expected overflow index 1, got %v", result.OverflowIndices)
	}
}

func TestBatchSplit_GreedyPreservesOrder(t *testing.T) {
	// Verify that inline images maintain input order
	caps := api.VisionCapabilities{
		MaxImageCount: 3,
		MaxImageBytes: 100_000_000,
	}
	sizes := []int{1000, 2000, 3000, 4000, 5000}
	result := BatchSplit(sizes, caps)

	// maxTotalBytes = 300_000_000, count limited to 3
	// First 3 inline [0,1,2], last 2 overflow [3,4]
	if len(result.InlineIndices) != 3 {
		t.Errorf("expected 3 inline indices, got %d: %v", len(result.InlineIndices), result.InlineIndices)
	}
	if len(result.OverflowIndices) != 2 {
		t.Errorf("expected 2 overflow indices, got %d: %v", len(result.OverflowIndices), result.OverflowIndices)
	}

	// Inline order should be preserved
	expectedInline := []int{0, 1, 2}
	for i, idx := range result.InlineIndices {
		if idx != expectedInline[i] {
			t.Errorf("expected inline[%d] = %d, got %d", i, expectedInline[i], idx)
		}
	}
	// Overflow order should be preserved
	expectedOverflow := []int{3, 4}
	for i, idx := range result.OverflowIndices {
		if idx != expectedOverflow[i] {
			t.Errorf("expected overflow[%d] = %d, got %d", i, expectedOverflow[i], idx)
		}
	}
}

func TestBatchSplit_ZeroSizedImages(t *testing.T) {
	// Some images may have size 0 (from stat failures in the caller)
	caps := api.VisionCapabilities{
		MaxImageCount: 3,
		MaxImageBytes: 5_000_000,
	}
	// maxTotalBytes = 15_000_000
	// idx0(0) inline, idx1(0) inline, idx2(0) inline, idx3(100M) overflow
	sizes := []int{0, 0, 0, 100_000_000}
	result := BatchSplit(sizes, caps)

	if len(result.InlineIndices) != 3 {
		t.Errorf("expected 3 inline indices (idx0,1,2), got %d: %v", len(result.InlineIndices), result.InlineIndices)
	}
	if len(result.OverflowIndices) != 1 {
		t.Errorf("expected 1 overflow index (idx3), got %d: %v", len(result.OverflowIndices), result.OverflowIndices)
	}
	verifyIndicesCoverAll(t, 4, result)

	if result.OverflowIndices[0] != 3 {
		t.Errorf("expected overflow index 3, got %v", result.OverflowIndices)
	}
}

func TestBatchSplit_ExactByteBudgetFit(t *testing.T) {
	// Edge case: total bytes exactly equals max budget
	caps := api.VisionCapabilities{
		MaxImageCount: 5,
		MaxImageBytes: 10_000_000,
	}
	// maxTotalBytes = 50_000_000
	sizes := []int{10_000_000, 10_000_000, 10_000_000, 10_000_000, 10_000_000} // total = 50M, exactly at limit
	result := BatchSplit(sizes, caps)

	if len(result.InlineIndices) != 5 {
		t.Errorf("expected 5 inline indices (exact budget fit), got %d: %v", len(result.InlineIndices), result.InlineIndices)
	}
	if len(result.OverflowIndices) != 0 {
		t.Errorf("expected 0 overflow indices, got %d: %v", len(result.OverflowIndices), result.OverflowIndices)
	}
}

func TestBatchSplit_OneByteOverBudget(t *testing.T) {
	// Edge case: total bytes exceeds max budget by 1
	caps := api.VisionCapabilities{
		MaxImageCount: 5,
		MaxImageBytes: 10_000_000,
	}
	// maxTotalBytes = 50_000_000
	sizes := []int{10_000_000, 10_000_000, 10_000_000, 10_000_000, 10_000_001} // total = 50_000_001 > 50M
	result := BatchSplit(sizes, caps)

	// First 4 fit exactly at 40M, 5th exceeds → idx4 overflow
	if len(result.InlineIndices) != 4 {
		t.Errorf("expected 4 inline indices, got %d: %v", len(result.InlineIndices), result.InlineIndices)
	}
	if len(result.OverflowIndices) != 1 {
		t.Errorf("expected 1 overflow index, got %d: %v", len(result.OverflowIndices), result.OverflowIndices)
	}
	verifyIndicesCoverAll(t, 5, result)
	if result.OverflowIndices[0] != 4 {
		t.Errorf("expected overflow index 4, got %v", result.OverflowIndices)
	}
}

func TestMakeRange(t *testing.T) {
	tests := []struct {
		start, end int
		expected   []int
	}{
		{0, 0, []int{}},
		{0, 5, []int{0, 1, 2, 3, 4}},
		{3, 7, []int{3, 4, 5, 6}},
		{5, 5, []int{}},
	}
	for _, tt := range tests {
		got := makeRange(tt.start, tt.end)
		if len(got) != len(tt.expected) {
			t.Errorf("makeRange(%d,%d) length = %d, want %d", tt.start, tt.end, len(got), len(tt.expected))
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("makeRange(%d,%d)[%d] = %d, want %d", tt.start, tt.end, i, got[i], tt.expected[i])
			}
		}
	}
}

// verifyIndicesCoverAll checks that InlineIndices and OverflowIndices together
// contain exactly [0..n-1] with no duplicates.
func verifyIndicesCoverAll(t *testing.T, n int, result BatchSplitResult) {
	t.Helper()
	seen := make(map[int]bool)
	for _, idx := range result.InlineIndices {
		if seen[idx] {
			t.Errorf("duplicate index %d in InlineIndices", idx)
		}
		seen[idx] = true
	}
	for _, idx := range result.OverflowIndices {
		if seen[idx] {
			t.Errorf("duplicate index %d in OverflowIndices", idx)
		}
		seen[idx] = true
	}
	for i := 0; i < n; i++ {
		if !seen[i] {
			t.Errorf("index %d not covered by either inline or overflow", i)
		}
	}
}
