package localmodel

import (
	"os"
	"path/filepath"
	"testing"
)

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
	if _, err := StartDownload("nope"); err != nil && filepath.IsAbs(err.Error()) {
		t.Errorf("error must not leak absolute paths: %v", err)
	}
}

// TestStartDownloadAlreadyInstalled guards the double-download guard: an
// installed model refuses to start a new job.
func TestStartDownloadAlreadyInstalled(t *testing.T) {
	t.Setenv("SPROUT_LLM_MODELS_DIR", t.TempDir())
	// "Install" a catalog model by creating its directory with weights.
	status, err := ResolveModelID("minicpm5-2b")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if err := writeFile(filepath.Join(status.Dir, "config.json"), []byte("{}")); err != nil {
		t.Fatal(err)
	}

	if _, err := StartDownload("minicpm5-2b"); err == nil {
		t.Fatal("installed model must refuse a new download job")
	}
}

// TestDownloadStatusNoJob returns nil for a model with no job.
func TestDownloadStatusNoJob(t *testing.T) {
	t.Setenv("SPROUT_LLM_MODELS_DIR", t.TempDir())
	if job := DownloadStatus("minicpm5-2b"); job != nil {
		t.Errorf("expected nil job, got %+v", job)
	}
	if jobs := ActiveDownloads(); len(jobs) != 0 {
		t.Errorf("expected no active downloads, got %d", len(jobs))
	}
}

// TestCancelDownloadNoJob errors when nothing runs.
func TestCancelDownloadNoJob(t *testing.T) {
	t.Setenv("SPROUT_LLM_MODELS_DIR", t.TempDir())
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
