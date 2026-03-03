package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alantheprice/ledit/pkg/configuration"
)

func TestCaptureWebText_WritesFileAndLog(t *testing.T) {
	workDir := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWD) })

	t.Setenv("LEDIT_RESOURCE_DIRECTORY", "captures")
	dir := filepath.Join(workDir, "captures")

	a := &Agent{}
	a.captureWebText("fetch_url", "https://example.com/menu", "hello world")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed reading dir: %v", err)
	}
	foundText := false
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".txt") {
			foundText = true
			break
		}
	}
	if !foundText {
		t.Fatalf("expected captured text file in %s", dir)
	}

	logBytes, err := os.ReadFile(filepath.Join(dir, "resource_capture.log"))
	if err != nil {
		t.Fatalf("expected resource_capture.log: %v", err)
	}
	if !strings.Contains(string(logBytes), `"action":"saved_text"`) {
		t.Fatalf("expected saved_text action in log, got: %s", string(logBytes))
	}
}

func TestCaptureVisionAsset_SkipsLargeLocalFile(t *testing.T) {
	workDir := t.TempDir()
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWD) })

	t.Setenv("LEDIT_RESOURCE_DIRECTORY", "captures")
	dir := filepath.Join(workDir, "captures")

	large := filepath.Join(t.TempDir(), "large.pdf")
	f, err := os.Create(large)
	if err != nil {
		t.Fatalf("failed creating large file: %v", err)
	}
	if err := f.Truncate(resourceCaptureMaxSizeBytes + 1); err != nil {
		_ = f.Close()
		t.Fatalf("failed truncating large file: %v", err)
	}
	_ = f.Close()

	a := &Agent{}
	path, size, err := a.captureVisionAsset(large, dir)
	if err != nil {
		t.Fatalf("captureVisionAsset returned error: %v", err)
	}
	if path != "" {
		t.Fatalf("expected no saved path for oversized file, got %s", path)
	}
	if size <= resourceCaptureMaxSizeBytes {
		t.Fatalf("expected oversized size, got %d", size)
	}

	logBytes, err := os.ReadFile(filepath.Join(dir, "resource_capture.log"))
	if err != nil {
		t.Fatalf("expected resource_capture.log: %v", err)
	}
	if !strings.Contains(string(logBytes), `"action":"skipped_large"`) {
		t.Fatalf("expected skipped_large action in log, got: %s", string(logBytes))
	}
}

func TestResourceDirectory_UsesConfigFallbackAndIsRelativeToWorkingDir(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	t.Setenv("LEDIT_RESOURCE_DIRECTORY", "")

	workDir := filepath.Join(t.TempDir(), "wd")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("failed creating work dir: %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWD) })

	manager, err := configuration.NewManager()
	if err != nil {
		t.Fatalf("failed to create config manager: %v", err)
	}
	manager.GetConfig().ResourceDirectory = "captures"

	a := &Agent{configManager: manager}
	got := a.resourceDirectory()
	want := filepath.Join(workDir, "captures")
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestResourceDirectory_NormalizesAbsoluteEnvToRelativeWorkingDir(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "wd2")
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		t.Fatalf("failed creating work dir: %v", err)
	}
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(originalWD) })

	abs := filepath.Join(string(filepath.Separator), "tmp", "captures")
	t.Setenv("LEDIT_RESOURCE_DIRECTORY", abs)

	a := &Agent{}
	got := a.resourceDirectory()
	want := filepath.Join(workDir, "tmp", "captures")
	if got != want {
		t.Fatalf("expected normalized relative path %s, got %s", want, got)
	}
}
