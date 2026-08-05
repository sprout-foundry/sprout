package embedding

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// End-to-end duplicate detection against a real index, at the thresholds the
// product actually ships. Reading the threshold constants tells you what the
// gate is; only running the pipeline tells you whether anything gets through.
//
// Opt-in: SPROUT_RETRIEVAL_EVAL=1.
func TestDuplicateDetectionFiresOnRealNearDuplicate(t *testing.T) {
	if os.Getenv("SPROUT_RETRIEVAL_EVAL") != "1" {
		t.Skip("SPROUT_RETRIEVAL_EVAL unset")
	}

	ctx := context.Background()

	workspace := t.TempDir()
	// Indexed corpus: the "original" plus filler so the store is not trivial.
	writeFile(t, filepath.Join(workspace, "sum.go"), `package p

// SumInts adds every value in the slice and returns the total.
func SumInts(values []int) int {
	total := 0
	for _, v := range values {
		total += v
	}
	return total
}
`)
	// Same domain, different job — must NOT be flagged as a duplicate.
	writeFile(t, filepath.Join(workspace, "maxint.go"), `package p

// MaxInt returns the largest value in the slice.
func MaxInt(values []int) int {
	best := values[0]
	for _, v := range values[1:] {
		if v > best {
			best = v
		}
	}
	return best
}
`)
	for i := 0; i < 6; i++ {
		writeFile(t, filepath.Join(workspace, fmt.Sprintf("filler%d.go", i)), fmt.Sprintf(`package p

// Handler%d processes a request and returns a formatted label.
func Handler%d(name string, count int) string {
	if count <= 0 {
		return "none"
	}
	return name
}
`, i, i))
	}

	mgr := NewEmbeddingManager(&configuration.EmbeddingIndexConfig{IndexDir: t.TempDir()}, workspace)
	t.Cleanup(func() { _ = mgr.Close() })
	if err := mgr.Init(ctx); err != nil {
		t.Skipf("embedding init unavailable: %v", err)
	}
	idx, err := mgr.snapshotIndexMgr()
	if err != nil {
		t.Fatalf("index manager: %v", err)
	}
	if _, err := idx.BuildIndex(ctx, workspace); err != nil {
		t.Fatalf("build: %v", err)
	}

	// The candidate a developer would be warned about: identical logic,
	// renamed identifiers, different function name.
	candidate := `package p

// AddNumbers totals the numbers in the slice.
func AddNumbers(nums []int) int {
	sum := 0
	for _, n := range nums {
		sum += n
	}
	return sum
}
`

	// Path A: CheckFileForDuplicates, the write-time gate (default threshold).
	res, err := CheckFileForDuplicates(ctx, idx, filepath.Join(workspace, "new.go"), candidate, workspace, 0, 5)
	if err != nil {
		t.Fatalf("CheckFileForDuplicates: %v", err)
	}
	t.Logf("CheckFileForDuplicates at default threshold: %d match(es)", len(res.Duplicates))
	for _, d := range res.Duplicates {
		t.Logf("    %.3f  %s", d.Similarity, d.Record.ID)
	}

	// Path B: the SAME production path with the gate opened, so the number is
	// the one CheckFileForDuplicates actually computes (per extracted unit),
	// not a whole-file approximation.
	ungated, err := CheckFileForDuplicates(ctx, idx, filepath.Join(workspace, "new.go"), candidate, workspace, 0.001, 5)
	if err != nil {
		t.Fatalf("CheckFileForDuplicates ungated: %v", err)
	}
	t.Logf("production path, gate opened — all three bands in one regime:")
	for _, d := range ungated.Duplicates {
		band := "unrelated"
		switch {
		case strings.Contains(d.Record.ID, "SumInts"):
			band = "NEAR-DUPLICATE"
		case strings.Contains(d.Record.ID, "MaxInt"):
			band = "related"
		}
		t.Logf("    %.3f  %-14s %s", d.Similarity, band, filepath.Base(d.Record.ID))
	}

	scored, err := idx.CheckDuplicates(ctx, candidate, 5, 0.001)
	if err != nil {
		t.Fatalf("CheckDuplicates: %v", err)
	}
	var best float32
	var bestID string //nolint:unused
	for _, s := range scored {
		if s.Similarity > best {
			best, bestID = s.Similarity, s.Record.ID
		}
	}
	t.Logf("whole-file query (not the production path): %.3f  %s", best, filepath.Base(bestID))
	t.Logf("duplicate gate: %.2f", float32(DefaultDuplicateThreshold))

	if len(res.Duplicates) == 0 {
		t.Errorf("duplicate detection did not flag an obvious near-duplicate — "+
			"the feature cannot fire in production (gate %.2f)", float32(DefaultDuplicateThreshold))
	}
	// Precision matters as much as recall: a gate that fires on everything
	// interrupts every write.
	for _, d := range res.Duplicates {
		if !strings.Contains(d.Record.ID, "SumInts") {
			t.Errorf("flagged non-duplicate %s at %.3f — gate is too permissive",
				filepath.Base(d.Record.ID), d.Similarity)
		}
	}
}
