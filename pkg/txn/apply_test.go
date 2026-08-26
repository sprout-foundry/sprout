//go:build !js

package txn

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Cap, mode and failure-path behaviour of ApplyDelta. The three manifest
// caps are package vars tightened per-test (withCap) so a 100 MiB total or
// a 2000-file manifest never has to be materialized to be exercised.

// capSet snapshots the three manifest caps so a test can tighten them
// without allocating a real 100 MiB fixture.
type capSet struct {
	file, count, total int
}

// withCap tightens the caps for the duration of the test and restores them
// on cleanup; it returns the restore func so callers write
// `defer withCap(t, ...)()`. Tests in this package never opt into
// parallelism, so a plain save/restore is sufficient.
func withCap(t *testing.T, mutate func(*capSet)) func() {
	t.Helper()
	saved := capSet{file: MaxFileBytes, count: MaxFileCount, total: MaxTotalBytes}
	t.Cleanup(func() {
		MaxFileBytes, MaxFileCount, MaxTotalBytes = saved.file, saved.count, saved.total
	})
	next := saved
	mutate(&next)
	MaxFileBytes, MaxFileCount, MaxTotalBytes = next.file, next.count, next.total
	return func() {
		MaxFileBytes, MaxFileCount, MaxTotalBytes = saved.file, saved.count, saved.total
	}
}

func TestApplyDelta_PerFileCap(t *testing.T) {
	defer withCap(t, func(c *capSet) { c.file = 8 })()

	dir := t.TempDir()
	result, err := ApplyDelta(context.Background(), dir, manifestOf(
		DeltaFile{Path: "big.bin", ContentBase64: b64(strings.Repeat("a", 9))},
		DeltaFile{Path: "ok.txt", ContentBase64: b64("x")},
	))
	if err != nil {
		t.Fatalf("ApplyDelta: %v", err)
	}
	if result.Applied != 1 {
		t.Fatalf("applied = %d, want 1", result.Applied)
	}
	if len(result.Skipped) != 1 || result.Skipped[0].Reason != SkipReasonExceedsPerFile {
		t.Fatalf("skipped = %+v, want exceeds_per_file_cap", result.Skipped)
	}
	if result.Status != StatusPartial {
		t.Fatalf("status = %q, want partial", result.Status)
	}
}

func TestApplyDelta_ExactlyAtPerFileCapIsApplied(t *testing.T) {
	defer withCap(t, func(c *capSet) { c.file = 8 })()

	dir := t.TempDir()
	result, err := ApplyDelta(context.Background(), dir, manifestOf(
		DeltaFile{Path: "exact.bin", ContentBase64: b64(strings.Repeat("a", 8))},
	))
	if err != nil {
		t.Fatalf("ApplyDelta: %v", err)
	}
	if result.Applied != 1 || result.Status != StatusOK {
		t.Fatalf("result = %+v, want a clean apply at exactly the cap", result)
	}
}

func TestApplyDelta_TotalCap(t *testing.T) {
	defer withCap(t, func(c *capSet) { c.total = 10 })()

	dir := t.TempDir()
	// Two files that individually pass but jointly exceed the total cap.
	chunk := strings.Repeat("b", 6)
	result, err := ApplyDelta(context.Background(), dir, manifestOf(
		DeltaFile{Path: "first.bin", ContentBase64: b64(chunk)},
		DeltaFile{Path: "second.bin", ContentBase64: b64(chunk)},
	))
	if err != nil {
		t.Fatalf("ApplyDelta: %v", err)
	}
	if result.Applied != 1 {
		t.Fatalf("applied = %d, want 1", result.Applied)
	}
	if len(result.Skipped) != 1 || result.Skipped[0].Reason != SkipReasonExceedsTotal {
		t.Fatalf("skipped = %+v, want exceeds_total_cap", result.Skipped)
	}
	if result.Skipped[0].Path != "second.bin" {
		t.Fatalf("the second entry must be the one refused, got %q", result.Skipped[0].Path)
	}
}

func TestApplyDelta_FileCountCap(t *testing.T) {
	defer withCap(t, func(c *capSet) { c.count = 3 })()

	dir := t.TempDir()
	files := []DeltaFile{
		{Path: "f0.txt", ContentBase64: b64("x")},
		{Path: "f1.txt", ContentBase64: b64("x")},
		{Path: "f2.txt", ContentBase64: b64("x")},
		{Path: "f3.txt", ContentBase64: b64("x")},
		{Path: "f4.txt", ContentBase64: b64("x")},
	}
	result, err := ApplyDelta(context.Background(), dir, manifestOf(files...))
	if err != nil {
		t.Fatalf("ApplyDelta: %v", err)
	}
	if result.Applied != 3 {
		t.Fatalf("applied = %d, want 3", result.Applied)
	}
	if len(result.Skipped) != 2 {
		t.Fatalf("skipped = %d entries, want 2", len(result.Skipped))
	}
	for _, s := range result.Skipped {
		if s.Reason != SkipReasonExceedsFileCount {
			t.Errorf("skipped %q reason = %q, want exceeds_file_count_cap", s.Path, s.Reason)
		}
	}
}

// TestContractCapsArePinned guards the contract's own numbers against
// accidental drift when someone "tunes" the vars.
func TestContractCapsArePinned(t *testing.T) {
	if MaxFileBytes != 5<<20 {
		t.Fatalf("MaxFileBytes = %d, want %d", MaxFileBytes, 5<<20)
	}
	if MaxFileCount != 2000 {
		t.Fatalf("MaxFileCount = %d, want 2000", MaxFileCount)
	}
	if MaxTotalBytes != 100<<20 {
		t.Fatalf("MaxTotalBytes = %d, want %d", MaxTotalBytes, 100<<20)
	}
}

func TestApplyDelta_InvalidModeSkipped(t *testing.T) {
	dir := t.TempDir()
	result, err := ApplyDelta(context.Background(), dir, manifestOf(
		DeltaFile{Path: "a.txt", ContentBase64: b64("x"), Mode: "not-a-mode"},
		DeltaFile{Path: "b.txt", ContentBase64: b64("x"), Mode: "0999"},
		DeltaFile{Path: "c.txt", ContentBase64: b64("x"), Mode: "0o644"},
	))
	if err != nil {
		t.Fatalf("ApplyDelta: %v", err)
	}
	if result.Applied != 1 {
		t.Fatalf("applied = %d, want 1 (only the 0o-prefixed form)", result.Applied)
	}
	if len(result.Skipped) != 2 {
		t.Fatalf("skipped = %+v, want two invalid_mode entries", result.Skipped)
	}
	if perm := modeOf(t, dir, "c.txt"); perm != 0o644 {
		t.Fatalf("c.txt mode = %o, want 644", perm)
	}
}

func TestApplyDelta_EmptyManifestIsOK(t *testing.T) {
	result, err := ApplyDelta(context.Background(), t.TempDir(), manifestOf())
	if err != nil {
		t.Fatalf("ApplyDelta: %v", err)
	}
	if result.Applied != 0 || result.Deleted != 0 || result.Status != StatusOK {
		t.Fatalf("result = %+v, want an empty ok", result)
	}
	if result.Skipped == nil || len(result.Skipped) != 0 {
		t.Fatalf("skipped must be an empty array, got %#v", result.Skipped)
	}
}

func TestApplyDelta_UnwritableTargetSkipped(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root — a 0555 dir would still be writable")
	}
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	if err := os.MkdirAll(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	result, err := ApplyDelta(context.Background(), dir, manifestOf(
		DeltaFile{Path: "locked/x.txt", ContentBase64: b64("x")},
		DeltaFile{Path: "fine.txt", ContentBase64: b64("y")},
	))
	if err != nil {
		t.Fatalf("ApplyDelta: %v", err)
	}
	if result.Applied != 1 {
		t.Fatalf("applied = %d, want 1", result.Applied)
	}
	if len(result.Skipped) != 1 || result.Skipped[0].Reason != SkipReasonWriteFailed {
		t.Fatalf("skipped = %+v, want write_failed", result.Skipped)
	}
}
