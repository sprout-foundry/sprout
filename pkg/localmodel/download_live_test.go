package localmodel

import (
	"os"
	"testing"
	"time"
)

// TestLiveStartDownloadCompletes runs a real (small) catalog download
// through the in-process engine: job goes downloading → completed, the
// weights land in DefaultModelsDir, and the model flips to Installed.
// Uses gemma4-e2b (the smallest catalog entry). Skips unless
// SPROUT_LIVE_DOWNLOAD=1.
func TestLiveStartDownloadCompletes(t *testing.T) {
	if os.Getenv("SPROUT_LIVE_DOWNLOAD") != "1" {
		t.Skip("SPROUT_LIVE_DOWNLOAD not set")
	}
	// DefaultModelsDir is resolved at package init; t.Setenv alone would
	// leave downloads targeting the real home dir.
	withIsolatedModelsDir(t)

	job, err := StartDownload("gemma4-e2b")
	if err != nil {
		t.Fatalf("StartDownload: %v", err)
	}
	if job.Status != "downloading" {
		t.Fatalf("initial status = %q, want downloading", job.Status)
	}

	deadline := time.Now().Add(10 * time.Minute)
	var last *DownloadStatusPayload
	for time.Now().Before(deadline) {
		last = DownloadStatus("gemma4-e2b")
		if last == nil {
			t.Fatal("job vanished from registry")
		}
		if last.Status != "downloading" {
			break
		}
		if last.Bytes > 0 && last.Bytes%(50*1024*1024) < 2*1024*1024 {
			t.Logf("progress: %d bytes", last.Bytes)
		}
		time.Sleep(2 * time.Second)
	}

	if last == nil || last.Status != "completed" {
		t.Fatalf("download did not complete: %+v", last)
	}
	t.Logf("completed: %d bytes in %s", last.Bytes, time.Since(time.Now().Add(-time.Minute)).Truncate(time.Millisecond))

	status, err := ResolveModelID("gemma4-e2b")
	if err != nil {
		t.Fatalf("resolve after download: %v", err)
	}
	if !status.Installed {
		t.Errorf("model not installed after completed download (dir %s)", status.Dir)
	}

	// Re-downloading an installed model must refuse.
	if _, err := StartDownload("gemma4-e2b"); err == nil {
		t.Error("second download of installed model must refuse")
	}
}
