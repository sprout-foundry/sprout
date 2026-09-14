package localmodel

import (
	"os"
	"path/filepath"
	"testing"
)

// withIsolatedModelsDir points DefaultModelsDir at a fresh temp dir for
// the test's duration. The package var is resolved ONCE at init from the
// environment, so t.Setenv alone has no effect — tests must swap the var
// directly (the model_label_test.go pattern).
func withIsolatedModelsDir(t *testing.T) string {
	t.Helper()
	old := DefaultModelsDir
	root := t.TempDir()
	DefaultModelsDir = root
	t.Cleanup(func() { DefaultModelsDir = old })
	return root
}

// clearDownloadRegistry cancels and drops every registry entry — tests
// asserting the no-job contract must not observe a prior test's leftover
// job (the registry is process-wide).
func clearDownloadRegistry(t *testing.T) {
	t.Helper()
	downloads.mu.Lock()
	for name, job := range downloads.jobs {
		if job.running() {
			job.cancel()
		}
		delete(downloads.jobs, name)
	}
	downloads.mu.Unlock()
}

// TestStartDownloadUnknownModel guards the allow-list: only catalog IDs
// (Names or directory basenames) are downloadable — arbitrary strings are
// refused, never turned into filesystem paths.
func TestStartDownloadUnknownModel(t *testing.T) {
	if _, err := StartDownload(`../../etc/passwd`); err == nil {
		t.Fatal("path traversal ID must be refused")
	}
	if _, err := StartDownload("totally-made-up-model"); err == nil {
		t.Fatal("unknown model ID must be refused")
	}
	// The refusal message names the model, not the filesystem.
	if _, err := StartDownload("nope"); err == nil && filepath.IsAbs(err.Error()) {
		t.Errorf("error must not leak absolute paths: %v", err)
	}
}

// TestStartDownloadAlreadyInstalled guards the double-download guard: an
// installed model refuses to start a new job.
func TestStartDownloadAlreadyInstalled(t *testing.T) {
	withIsolatedModelsDir(t)
	// "Install" a catalog model. hasWeights requires an actual weights
	// marker (.safetensors / .gguf / the sharded index) — a config.json
	// alone does NOT count as installed.
	status, err := ResolveModelID("minicpm5-2b")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if err := writeFile(filepath.Join(status.Dir, "model.safetensors.index.json"), []byte("{}")); err != nil {
		t.Fatal(err)
	}

	if _, err := StartDownload("minicpm5-2b"); err == nil {
		t.Fatal("installed model must refuse a new download job")
	}
}

// TestDownloadStatusNoJob returns nil for a model with no job.
func TestDownloadStatusNoJob(t *testing.T) {
	withIsolatedModelsDir(t)
	clearDownloadRegistry(t)

	if job := DownloadStatus("minicpm5-2b"); job != nil {
		t.Errorf("expected nil job, got %+v", job)
	}
	if jobs := ActiveDownloads(); len(jobs) != 0 {
		t.Errorf("expected no active downloads, got %d", len(jobs))
	}
}

// TestCancelDownloadNoJob errors when nothing runs.
func TestCancelDownloadNoJob(t *testing.T) {
	withIsolatedModelsDir(t)
	clearDownloadRegistry(t)
	if err := CancelDownload("minicpm5-2b"); err == nil {
		t.Fatal("cancel with no active job must error")
	}
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
