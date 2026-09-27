package agent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent/changes"
)

// change_tracking_recover_bulk_test.go — the agent-integration pieces of
// the old change_tracking_shell_test.go (SP-141 phase 2). These need a
// real *Agent (the recover_file handler, the read-only shell
// classifier), so they stay in pkg/agent; the pure tracker tests live in
// pkg/agent/changes.

// mustWriteFile is a local copy of the changes-package test helper
// (which is unexported there).
func mustWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

// TestRecoverBulk_RestoresAllPackedFiles confirms recover_file(scope=bulk)
// walks BulkItems and restores each per-file payload.
func TestRecoverBulk_RestoresAllPackedFiles(t *testing.T) {
	dir := t.TempDir()
	const fileCount = 3
	abs := make([]string, fileCount)
	for i := range abs {
		abs[i] = filepath.Join(dir, "f"+strconv.Itoa(i)+".txt")
		mustWriteFile(t, abs[i], []byte("after-"+strconv.Itoa(i)))
	}

	items := make([]TrackedBulkItem, fileCount)
	for i := range items {
		items[i] = TrackedBulkItem{
			FilePath:     abs[i],
			OriginalCode: "before-" + strconv.Itoa(i),
			NewCode:      "after-" + strconv.Itoa(i),
			Operation:    "edit",
		}
	}
	tracker := changes.NewChangeTracker(nil, "")
	tracker.Enable()
	tracker.MergeChild([]TrackedFileChange{{
		FilePath:  "git checkout .",
		Operation: "bulk",
		BulkCount: fileCount,
		BulkItems: items,
	}}, "test")

	// Sanity: files are in the "after" state before recovery.
	for i, p := range abs {
		got, _ := os.ReadFile(p)
		if !bytes.Equal(got, []byte("after-"+strconv.Itoa(i))) {
			t.Fatalf("pre-recovery state wrong for %s: got %q, want 'after-%d'", p, got, i)
		}
	}

	a := trackerOnlyAgent(tracker)
	out, err := handleRecoverFile(context.Background(), a, map[string]any{"path": "git checkout .", "scope": "bulk"})
	if err != nil {
		t.Fatalf("handleRecoverFile(scope=bulk): %v", err)
	}
	if !strings.Contains(out, `"restored": 3`) {
		t.Errorf("expected restored:3 in payload, got %s", out)
	}
	for i, p := range abs {
		got, _ := os.ReadFile(p)
		want := []byte("before-" + strconv.Itoa(i))
		if !bytes.Equal(got, want) {
			t.Errorf("post-recovery content wrong for %s: got %q, want %q", p, got, want)
		}
	}
}

// BenchmarkShellLooksReadOnly measures the cost of the read-only
// classifier. This runs on every shell_command before deciding whether
// to snapshot — needs to be cheap (microseconds) so the short-circuit
// itself isn't a bottleneck.
func BenchmarkShellLooksReadOnly(b *testing.B) {
	cmds := []string{
		"ls -la",
		"grep -r foo .",
		"git status",
		"cat README.md",
		"sed -i 's/foo/bar/' file.txt",      // unsafe path
		"go build ./...",                    // unsafe path
		"find . -name '*.go' | xargs wc -l", // pipe path
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = shellLooksReadOnly(cmds[i%len(cmds)])
	}
}
