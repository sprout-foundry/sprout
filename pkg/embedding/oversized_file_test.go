package embedding

// Regression tests for the oversized-file OOM fix.
//
// A 19.8GB .txt corpus in the workspace previously OOM-crashed the index build:
// os.ReadFile materialized the whole file, Extract then made a full string copy
// and a []rune slice (>=4x the input size), and only then truncated to 8KB.
// Peak memory was >100GB on a 62GB machine.
//
// The fix (constants.go / extractor_file.go / ignore.go / index.go):
//   - MaxIndexableFileBytes (1 MiB) caps which files file-level indexing will
//     even consider; larger files are skipped by the walk, BuildIndex, and
//     UpdateFile before any read.
//   - Extract is byte-bounded: newline counting runs on raw bytes BEFORE
//     truncation, truncation snaps to a valid UTF-8 boundary, and the full
//     input is never materialized as a string or rune slice.
//
// These tests pin that behavior: oversized files never produce records, the
// 1 MiB boundary is inclusive, truncation respects UTF-8, and a 100MB input
// survives Extract without blowing up.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestFileExtractorTruncationBounded verifies Extract's byte-bounded
// truncation:
//   - EndLine reflects the WHOLE file (newlines past the truncation point are
//     still counted — the count happens on raw bytes before truncation).
//   - Body is truncated to maxFileBytes BYTES (not runes — the old code
//     truncated to maxFileBytes runes, so multibyte input could produce a body
//     up to 3x larger than the cap).
//   - Truncation snaps back to a valid UTF-8 boundary; the body is never
//     mid-rune.
//   - Small files pass through untouched.
func TestFileExtractorTruncationBounded(t *testing.T) {
	ext := NewFileExtractor(8000)

	t.Run("ascii truncates at maxFileBytes and counts newlines pre-truncation", func(t *testing.T) {
		// 100KB of 'a's with newlines ONLY past the 8000-byte truncation point.
		content := bytes.Repeat([]byte{'a'}, 100*1024)
		content[9000] = '\n'
		content[50000] = '\n'
		content[len(content)-1] = '\n' // 3 newlines total, all beyond byte 8000

		units, err := ext.Extract("corpus.txt", content)
		if err != nil {
			t.Fatalf("Extract() error = %v", err)
		}
		if len(units) != 1 {
			t.Fatalf("Extract() got %d units, want 1", len(units))
		}

		unit := units[0]
		if len(unit.Body) > 8000 {
			t.Errorf("Body length = %d, want <= 8000 (byte-bounded truncation)", len(unit.Body))
		}
		if len(unit.Body) != 8000 {
			t.Errorf("Body length = %d, want exactly 8000 (no UTF-8 boundary to trim for ASCII)", len(unit.Body))
		}
		// Newlines past byte 8000 must still be counted: EndLine = 3 + 1.
		if unit.EndLine != 4 {
			t.Errorf("EndLine = %d, want 4 (newlines counted before truncation)", unit.EndLine)
		}
		// The truncated region (first 8000 bytes) contains no newlines, so the
		// body must be pure 'a's.
		if strings.Contains(unit.Body, "\n") {
			t.Errorf("Body contains a newline; truncation should have cut before the first one at byte 9000")
		}
	})

	t.Run("multibyte truncation stays valid UTF-8 within byte cap", func(t *testing.T) {
		// '世' is 3 bytes (E4 B8 96). 3000 runes = 9000 bytes; truncating at
		// 8000 bytes cuts mid-rune (E4 B8 at the tail).
		content := bytes.Repeat([]byte("世"), 3000)

		units, err := ext.Extract("corpus.txt", content)
		if err != nil {
			t.Fatalf("Extract() error = %v", err)
		}
		if len(units) != 1 {
			t.Fatalf("Extract() got %d units, want 1", len(units))
		}

		body := units[0].Body
		if len(body) > 8000 {
			t.Errorf("Body length = %d, want <= 8000 bytes (old code truncated by runes and returned %d bytes for 8000 3-byte runes)", len(body), 8000*3)
		}
		if !utf8.ValidString(body) {
			t.Error("Body is not valid UTF-8; truncation must snap back to a rune boundary")
		}
		if !strings.HasSuffix(body, "世") {
			t.Error("Body does not end on a complete rune; trailing rune was cut mid-encoding")
		}
	})

	t.Run("complete final rune at the cap boundary is not over-trimmed", func(t *testing.T) {
		// 2666 three-byte runes (7998 bytes) + "ab" = exactly 8000 bytes,
		// ending on complete runes. The boundary trim must keep all 8000:
		// an earlier destructive-slicing version walked back past the ASCII
		// tail into the final '世' and dropped it (7997 bytes).
		content := append(bytes.Repeat([]byte("世"), 2666), 'a', 'b')
		if len(content) != 8000 {
			t.Fatalf("setup: len=%d, want 8000", len(content))
		}

		units, err := ext.Extract("corpus.txt", content)
		if err != nil {
			t.Fatalf("Extract() error = %v", err)
		}
		if len(units[0].Body) != 8000 {
			t.Errorf("Body length = %d, want exactly 8000 (truncation point already ends on complete runes)", len(units[0].Body))
		}
		if !utf8.ValidString(units[0].Body) {
			t.Error("Body is not valid UTF-8")
		}
	})

	t.Run("small file passes through unchanged", func(t *testing.T) {
		content := []byte("hello world\nsecond line\n")
		units, err := ext.Extract("notes.txt", content)
		if err != nil {
			t.Fatalf("Extract() error = %v", err)
		}
		if len(units) != 1 {
			t.Fatalf("Extract() got %d units, want 1", len(units))
		}
		if units[0].Body != string(content) {
			t.Errorf("Body = %q, want %q (small file must not be truncated)", units[0].Body, content)
		}
		if units[0].EndLine != 3 {
			t.Errorf("EndLine = %d, want 3", units[0].EndLine)
		}
	})
}

// TestWalkSkipsOversizedFiles verifies WalkAllIndexableFiles drops files
// larger than MaxIndexableFileBytes before they can be read. The 1 MiB
// boundary itself is inclusive: exactly 1 MiB is collected, 1 MiB + 1 is not.
func TestWalkSkipsOversizedFiles(t *testing.T) {
	dir := t.TempDir()

	// Normal small file.
	normal := filepath.Join(dir, "normal.txt")
	if err := os.WriteFile(normal, []byte("small file\n"), 0o644); err != nil {
		t.Fatalf("write normal.txt: %v", err)
	}

	// Exactly at the cap — must be INCLUDED (the guard is size > MaxIndexableFileBytes).
	boundary := filepath.Join(dir, "boundary.txt")
	if err := os.WriteFile(boundary, bytes.Repeat([]byte{'a'}, int(MaxIndexableFileBytes)), 0o644); err != nil {
		t.Fatalf("write boundary.txt: %v", err)
	}

	// One byte over the cap — must be SKIPPED.
	oversized := filepath.Join(dir, "oversized.txt")
	if err := os.WriteFile(oversized, bytes.Repeat([]byte{'a'}, int(MaxIndexableFileBytes)+1), 0o644); err != nil {
		t.Fatalf("write oversized.txt: %v", err)
	}

	files, err := WalkAllIndexableFiles(context.Background(), dir)
	if err != nil {
		t.Fatalf("WalkAllIndexableFiles: %v", err)
	}

	got := make(map[string]bool, len(files))
	for _, f := range files {
		rel, err := filepath.Rel(dir, f)
		if err != nil {
			t.Fatalf("filepath.Rel(%q, %q): %v", dir, f, err)
		}
		got[rel] = true
	}

	if len(got) != 2 {
		t.Fatalf("walk returned %d files %v, want exactly 2 (normal + boundary)", len(got), files)
	}
	if !got["normal.txt"] {
		t.Error("normal.txt missing from walk results")
	}
	if !got["boundary.txt"] {
		t.Error("boundary.txt (exactly 1 MiB) missing; the cap is size > MaxIndexableFileBytes, so exactly 1 MiB must be included")
	}
	if got["oversized.txt"] {
		t.Error("oversized.txt (1 MiB + 1) was collected; walk must skip files larger than MaxIndexableFileBytes")
	}
}

// TestBuildIndexSkipsOversizedFiles verifies the end-to-end build path: a
// workspace containing a >1 MiB .txt builds successfully, indexes the small
// .md, and produces NO record for the oversized file.
func TestBuildIndexSkipsOversizedFiles(t *testing.T) {
	dir := t.TempDir()

	mdPath := filepath.Join(dir, "README.md")
	if err := os.WriteFile(mdPath, []byte("# Hello\n\nSmall doc.\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}

	bigTxt := filepath.Join(dir, "corpus.txt")
	if err := os.WriteFile(bigTxt, bytes.Repeat([]byte{'a'}, int(MaxIndexableFileBytes)+1), 0o644); err != nil {
		t.Fatalf("write corpus.txt: %v", err)
	}

	provider := newMockProvider(8)
	store := newCountingStore()
	idx := NewIndexManager(provider, store, IndexOptions{
		IndexFileLevel: true,
		BatchSize:      16,
		MaxBodyLen:     500,
	})

	stats, err := idx.BuildIndex(context.Background(), dir)
	if err != nil {
		t.Fatalf("BuildIndex: %v", err)
	}

	if stats.FilesProcessed != 1 {
		t.Errorf("FilesProcessed = %d, want 1 (only README.md; oversized corpus.txt must be skipped)", stats.FilesProcessed)
	}

	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("store.LoadAll: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("store has %d records, want 1 (only README.md)", len(all))
	}
	if all[0].File != mdPath {
		t.Errorf("record File = %q, want %q", all[0].File, mdPath)
	}
	for _, r := range all {
		if r.File == bigTxt {
			t.Errorf("found record for oversized file %q; oversized files must never be indexed", bigTxt)
		}
	}
}

// TestUpdateFileSkipsOversizedFile verifies the single-file update path:
// UpdateFile on a >1 MiB file returns nil (skip, not error) and stores
// nothing, while a subsequent UpdateFile on a small file indexes normally.
func TestUpdateFileSkipsOversizedFile(t *testing.T) {
	dir := t.TempDir()

	bigTxt := filepath.Join(dir, "corpus.txt")
	if err := os.WriteFile(bigTxt, bytes.Repeat([]byte{'a'}, int(MaxIndexableFileBytes)+1), 0o644); err != nil {
		t.Fatalf("write corpus.txt: %v", err)
	}

	mdPath := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(mdPath, []byte("# Notes\n\nBody.\n"), 0o644); err != nil {
		t.Fatalf("write notes.md: %v", err)
	}

	provider := newMockProvider(8)
	store := newCountingStore()
	idx := NewIndexManager(provider, store, IndexOptions{
		IndexFileLevel: true,
		BatchSize:      16,
		MaxBodyLen:     500,
	})
	ctx := context.Background()

	// Oversized file: skipped, not an error, no record.
	if err := idx.UpdateFile(ctx, bigTxt); err != nil {
		t.Fatalf("UpdateFile on oversized file returned error (want nil skip): %v", err)
	}
	if store.Size() != 0 {
		t.Fatalf("store has %d records after UpdateFile on oversized file, want 0", store.Size())
	}

	// Small file: indexed normally.
	if err := idx.UpdateFile(ctx, mdPath); err != nil {
		t.Fatalf("UpdateFile on small file: %v", err)
	}
	all, err := store.LoadAll()
	if err != nil {
		t.Fatalf("store.LoadAll: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("store has %d records after UpdateFile on small file, want 1", len(all))
	}
	if all[0].File != mdPath {
		t.Errorf("record File = %q, want %q", all[0].File, mdPath)
	}
}

// TestExtractDoesNotMaterializeFullString guards the memory-bound property:
// a 100MB input must flow through Extract allocating only kilobytes, not the
// ~500MB the old implementation spent on a full string copy plus a []rune
// slice (>=4x input size). TotalAlloc is monotonic per-run and unaffected by
// GC, so the delta is a stable assertion; the bound is generous (64MB) to
// stay robust across allocator behavior while still failing hard on a
// regression to full materialization.
func TestExtractDoesNotMaterializeFullString(t *testing.T) {
	ext := NewFileExtractor(8000)

	content := bytes.Repeat([]byte{'a'}, 100<<20) // 100 MiB

	// Baseline AFTER the input allocation so the delta isolates Extract's
	// own allocations from the test's setup.
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	units, err := ext.Extract("corpus.txt", content)

	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(units) != 1 {
		t.Fatalf("Extract() got %d units, want 1", len(units))
	}
	if len(units[0].Body) > 8000 {
		t.Errorf("Body length = %d, want <= 8000", len(units[0].Body))
	}
	if units[0].EndLine != 1 {
		t.Errorf("EndLine = %d, want 1 (no newlines in input)", units[0].EndLine)
	}

	allocated := after.TotalAlloc - before.TotalAlloc
	if allocated > 64<<20 {
		t.Errorf("Extract allocated %d bytes for a 100MB input, want <= 64MB (byte-bounded path regressed)", allocated)
	}
}

// TestExtractTruncationEdgeCases pins the degenerate-input paths of the
// UTF-8 boundary trim: invalid continuation-byte runs and caps smaller than
// one rune must produce an empty (or minimal) body without panicking.
func TestExtractTruncationEdgeCases(t *testing.T) {
	t.Run("all continuation bytes yields empty body without panic", func(t *testing.T) {
		ext := NewFileExtractor(8000)
		content := bytes.Repeat([]byte{0x80}, 10000) // invalid UTF-8

		units, err := ext.Extract("corpus.txt", content)
		if err != nil {
			t.Fatalf("Extract() error = %v", err)
		}
		if len(units) != 1 {
			t.Fatalf("Extract() got %d units, want 1", len(units))
		}
		if units[0].Body != "" {
			t.Errorf("Body = %q, want empty (no rune-start byte exists to trim back to)", units[0].Body)
		}
	})

	t.Run("maxFileBytes smaller than one rune yields empty body", func(t *testing.T) {
		ext := NewFileExtractor(1)
		content := []byte("世") // 3-byte rune, cap is 1 byte

		units, err := ext.Extract("corpus.txt", content)
		if err != nil {
			t.Fatalf("Extract() error = %v", err)
		}
		if len(units) != 1 {
			t.Fatalf("Extract() got %d units, want 1", len(units))
		}
		if units[0].Body != "" {
			t.Errorf("Body = %q, want empty (1-byte cap cannot hold a 3-byte rune)", units[0].Body)
		}
	})

	t.Run("four-byte emoji truncated mid-rune", func(t *testing.T) {
		ext := NewFileExtractor(4)
		// 2 emoji = 8 bytes; cap 4 keeps the first emoji complete and the
		// second would start at byte 4 — exactly at the cap, so it is not
		// included. Body = one complete 4-byte emoji.
		content := []byte("😀😀")

		units, err := ext.Extract("corpus.txt", content)
		if err != nil {
			t.Fatalf("Extract() error = %v", err)
		}
		if len(units) != 1 {
			t.Fatalf("Extract() got %d units, want 1", len(units))
		}
		if units[0].Body != "😀" {
			t.Errorf("Body = %q, want a single complete emoji", units[0].Body)
		}
		if !utf8.ValidString(units[0].Body) {
			t.Error("Body is not valid UTF-8")
		}
	})
}

// TestExtractFromFileSkipsOversizedCodeFile pins the chokepoint size guard:
// a >1 MiB code file yields no units (via nil, nil), covering the direct
// UpdateFile / ExtractFromFile paths that bypass the walk-level skip.
func TestExtractFromFileSkipsOversizedCodeFile(t *testing.T) {
	dir := t.TempDir()

	small := filepath.Join(dir, "small.go")
	smallSrc := "package main\n\nfunc main() {}\n"
	if err := os.WriteFile(small, []byte(smallSrc), 0o644); err != nil {
		t.Fatalf("write small.go: %v", err)
	}

	// >1 MiB .go file (invalid Go, but extraction only parses — size is the point).
	big := filepath.Join(dir, "big.go")
	if err := os.WriteFile(big, bytes.Repeat([]byte("// padding\n"), (int(MaxIndexableFileBytes)/10)+1), 0o644); err != nil {
		t.Fatalf("write big.go: %v", err)
	}
	if fi, err := os.Stat(big); err != nil || fi.Size() <= MaxIndexableFileBytes {
		t.Fatalf("setup: big.go must exceed %d bytes", MaxIndexableFileBytes)
	}

	units, err := ExtractFromFile(small)
	if err != nil {
		t.Fatalf("ExtractFromFile(small.go) error = %v", err)
	}
	if len(units) == 0 {
		t.Error("small.go produced no units; small code files must still be extracted")
	}

	units, err = ExtractFromFile(big)
	if err != nil {
		t.Fatalf("ExtractFromFile(big.go) error = %v (oversized skip must be nil, nil)", err)
	}
	if len(units) != 0 {
		t.Errorf("ExtractFromFile(big.go) returned %d units, want 0 (oversized code file must be skipped)", len(units))
	}
}
