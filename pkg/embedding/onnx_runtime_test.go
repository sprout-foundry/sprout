//go:build cgo

package embedding

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultModelDir(t *testing.T) {
	tmpDir := t.TempDir()
	_ = os.Setenv("SPROUT_MODELS_DIR", tmpDir)
	defer os.Unsetenv("SPROUT_MODELS_DIR")
	got := DefaultModelDir()
	if got != tmpDir {
		t.Errorf("expected %s, got %s", tmpDir, got)
	}
}

func TestDefaultModelDir_Fallback(t *testing.T) {
	// When no env vars are set, should fall back to ~/.config/sprout/models
	os.Unsetenv("SPROUT_MODELS_DIR")
	os.Unsetenv("SPROUT_CONFIG")
	os.Unsetenv("LEDIT_CONFIG")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	expected := filepath.Join(home, ".config", "sprout", "models")
	got := DefaultModelDir()
	if got != expected {
		t.Errorf("expected %s, got %s", expected, got)
	}
}

func TestNewONNXRuntime(t *testing.T) {
	tmpDir := t.TempDir()
	r, err := NewONNXRuntimeWithDir(tmpDir)
	if err != nil {
		t.Skipf("ONNX runtime not available (skip gracefully): %v", err)
	}
	defer r.Close()
	if !r.Ready() {
		t.Error("runtime should be ready after creation")
	}
}
