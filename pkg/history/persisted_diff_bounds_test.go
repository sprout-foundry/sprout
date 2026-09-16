package history

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnifiedDiffFor_BoundsInput(t *testing.T) {
	// A 25k-line rewrite on each side would previously run an unbounded
	// Myers diff; now each side is head-truncated before diffing.
	var bigBefore, bigAfter strings.Builder
	for i := 0; i < 25000; i++ {
		bigBefore.WriteString("old line\n")
		bigAfter.WriteString("new line\n")
	}

	out := UnifiedDiffFor("big.txt", bigBefore.String(), bigAfter.String())
	// The output cap fired (total would have been ~40k lines without it),
	// and — because the inputs were head-truncated — the rendered diff
	// cannot contain the tail of either side.
	if !strings.Contains(out, "(diff truncated at") {
		t.Fatalf("expected output truncation notice, got suffix: %q", out[max(0, len(out)-200):])
	}
	if strings.Count(out, "\n") > maxHistoryDiffOutputLines+10 {
		t.Fatalf("output diff too large: %d lines", strings.Count(out, "\n"))
	}
}

func TestUnifiedDiffFor_OutputCap(t *testing.T) {
	// Whole-file rewrite: every line differs, so difflib emits ~2N diff
	// lines even for small N. Oversized N must be capped in output.
	var before, after strings.Builder
	for i := 0; i < maxHistoryDiffOutputLines*2; i++ {
		before.WriteString("same-prefix line\n")
		after.WriteString("different-suffix line\n")
	}
	out := UnifiedDiffFor("rewrite.txt", before.String(), after.String())
	if got := strings.Count(out, "\n"); got > maxHistoryDiffOutputLines+10 {
		t.Fatalf("output not capped: %d lines", got)
	}
	if !strings.Contains(out, "(diff truncated at") {
		t.Fatalf("expected output truncation notice, got suffix: %q", out[max(0, len(out)-200):])
	}
}

func TestUnifiedDiffFor_SmallDiffUnchanged(t *testing.T) {
	out := UnifiedDiffFor("small.go", "a\nb\n", "a\nc\n")
	if strings.Contains(out, "truncated") {
		t.Fatalf("small diff should not be truncated: %q", out)
	}
	for _, want := range []string{"-b", "+c", "@@"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
}

func TestFindPersistedOriginal_MetadataFirstLookup(t *testing.T) {
	tmp := t.TempDir()
	cDir := filepath.Join(tmp, "changes")
	rDir := filepath.Join(tmp, "revisions")
	if err := os.MkdirAll(cDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rDir, 0o755); err != nil {
		t.Fatal(err)
	}
	prevC, prevR := GetPathsForTesting()
	SetPathsForTesting(cDir, rDir)
	t.Cleanup(func() { SetPathsForTesting(prevC, prevR) })

	// Fill the store with unrelated files so a naive content-scan would
	// read their payloads; the metadata-first lookup must not need them.
	for i := 0; i < 50; i++ {
		if err := RecordChangeWithDetails(
			"rev-unrelated", "/ws/other"+string(rune('a'+i))+".txt",
			"unrelated old\n", "unrelated new\n", "edit", "", "", "", "m",
		); err != nil {
			t.Fatal(err)
		}
	}

	target := "/ws/target.go"
	if err := RecordChangeWithDetails(
		"rev-target-1", target, "v1\n", "v2\n", "edit", "", "", "", "m",
	); err != nil {
		t.Fatal(err)
	}
	// A later record for the same file must not win (oldest wins).
	if err := RecordChangeWithDetails(
		"rev-target-2", target, "v2\n", "v3\n", "edit", "", "", "", "m",
	); err != nil {
		t.Fatal(err)
	}

	rec, found, err := FindPersistedOriginal(target)
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if rec.Original != "v1\n" || rec.New != "v2\n" {
		t.Errorf("expected oldest record (v1→v2), got %q→%q", rec.Original, rec.New)
	}

	// Unknown path: no payload reads, clean miss.
	if _, found, _ := FindPersistedOriginal("/ws/never-seen.txt"); found {
		t.Error("expected found=false for unknown path")
	}
}
