package embedding

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestIndexThroughputProbe measures real end-to-end embedding throughput
// against this repository's own code units, so the auto-build timeout can be
// checked against observed speed rather than an assumed per-unit budget.
//
// Opt-in: SPROUT_THROUGHPUT_PROBE=1 (loads the real ~180MB model).
func TestIndexThroughputProbe(t *testing.T) {
	if os.Getenv("SPROUT_THROUGHPUT_PROBE") != "1" {
		t.Skip("SPROUT_THROUGHPUT_PROBE unset")
	}

	root := os.Getenv("SPROUT_THROUGHPUT_ROOT")
	if root == "" {
		root = "../.."
	}

	ctx := context.Background()

	walkStart := time.Now()
	files, err := WalkAllIndexableFiles(ctx, root)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	t.Logf("walk: %d files in %s", len(files), time.Since(walkStart).Round(time.Millisecond))
	t.Logf("autoBuildTimeout for %d files: %s", len(files), autoBuildTimeout(len(files)))

	extractStart := time.Now()
	var units []CodeUnit
	fileExtractor := NewFileExtractor(8000)
	for _, path := range files {
		var got []CodeUnit
		switch {
		case hasCodeExtension(path):
			got, err = ExtractFromFile(path, WithIncludeTests(false))
		case IsSupportedIndexableFile(path):
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				continue
			}
			got, err = fileExtractor.Extract(path, content)
		default:
			continue
		}
		if err != nil {
			continue
		}
		units = append(units, got...)
	}
	extractElapsed := time.Since(extractStart)
	t.Logf("extract: %d units in %s", len(units), extractElapsed.Round(time.Millisecond))

	provider, _, err := acquireSharedONNXProvider(ctx, DefaultModelDir(), EmbeddingGemma300MConfig())
	if err != nil {
		t.Skipf("ONNX provider unavailable: %v", err)
	}

	// Embed a bounded sample and extrapolate — embedding every unit is the
	// thing under measurement, so the probe must not itself take that long.
	sample := len(units)
	if max := 512; sample > max {
		sample = max
	}
	idx := NewIndexManager(provider, newCountingStore(), IndexOptions{
		BatchSize:  EmbedBatchSize,
		MaxBodyLen: 2000,
	})

	embedStart := time.Now()
	records, err := idx.embedUnits(ctx, units[:sample], "", nil)
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	embedElapsed := time.Since(embedStart)
	if len(records) != sample {
		t.Fatalf("embedded %d of %d units", len(records), sample)
	}

	perUnit := embedElapsed / time.Duration(sample)
	projected := extractElapsed + perUnit*time.Duration(len(units))

	t.Logf("embed: %d units in %s (%s/unit, %.1f units/s)",
		sample, embedElapsed.Round(time.Millisecond), perUnit.Round(time.Microsecond),
		float64(sample)/embedElapsed.Seconds())
	t.Logf("PROJECTED full build for %d units: %s", len(units), projected.Round(time.Second))
	t.Logf("budget: autoBuild=%s BuildTimeout=%s", autoBuildTimeout(len(files)), BuildTimeout)

	if projected > autoBuildTimeout(len(files)) {
		t.Logf("WARNING: projected build exceeds the auto-build budget — builds will be cancelled mid-way")
	}
}
