package localmodel

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDirSizeBytes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.bin"), make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "b.bin"), make([]byte, 250), 0o644); err != nil {
		t.Fatal(err)
	}

	if got := dirSizeBytes(root); got != 350 {
		t.Errorf("dirSizeBytes() = %d, want 350", got)
	}
}

func TestDirSizeBytesMissingDir(t *testing.T) {
	if got := dirSizeBytes(filepath.Join(t.TempDir(), "does-not-exist")); got != 0 {
		t.Errorf("dirSizeBytes(missing) = %d, want 0", got)
	}
}

// TestPollDownloadProgressReportsGrowth guards the fix replacing
// subprocess-stdout scraping (confirmed non-functional: hf download
// writes nothing to a piped stdout/stderr) with directory-size polling.
// total is always reported as 0 here — the caller doesn't know the full
// download size up front — callers must treat total<=0 as "show bytes
// downloaded so far", not skip reporting entirely.
func TestPollDownloadProgressReportsGrowth(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "partial.bin"), make([]byte, 500), 0o644); err != nil {
		t.Fatal(err)
	}

	var reports []int64
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		pollDownloadProgress(dir, stop, func(downloaded, total int64) {
			if total != 0 {
				t.Errorf("expected total=0 (unknown), got %d", total)
			}
			reports = append(reports, downloaded)
		})
	}()

	time.Sleep(50 * time.Millisecond)
	close(stop)
	<-done

	if len(reports) == 0 {
		t.Fatal("expected at least one progress report (the final one on stop)")
	}
	if last := reports[len(reports)-1]; last != 500 {
		t.Errorf("final report = %d, want 500", last)
	}
}

func TestPollDownloadProgressNilCallbackDoesNotPanic(t *testing.T) {
	stop := make(chan struct{})
	close(stop)
	pollDownloadProgress(t.TempDir(), stop, nil)
}
