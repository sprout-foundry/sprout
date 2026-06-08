//go:build cgo

package embedding

import (
	"errors"
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

func TestIsVersionMismatchError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "real ORT version mismatch error",
			err:  errors.New("The requested API version [25] is not available, only API versions [1, 20] are supported in this build. Current ORT Version is: 1.20.1"),
			want: true,
		},
		{
			name: "short API version mismatch",
			err:  errors.New("API version [30] is not available"),
			want: true,
		},
		{
			name: "unrelated error",
			err:  errors.New("some other error"),
			want: false,
		},
		{
			name: "empty error message",
			err:  errors.New(""),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isVersionMismatchError(tt.err)
			if got != tt.want {
				t.Errorf("isVersionMismatchError() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRemoveStagedLibrary(t *testing.T) {
	t.Run("removes existing file", func(t *testing.T) {
		tmpDir := t.TempDir()
		runtimeDir := filepath.Join(tmpDir, "onnxruntime")
		if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
			t.Fatal(err)
		}
		libName := platformLibName()
		if libName == "" {
			t.Skip("no platform lib name on this platform")
		}
		staged := filepath.Join(runtimeDir, libName)
		if err := os.WriteFile(staged, []byte("fake"), 0o644); err != nil {
			t.Fatal(err)
		}

		r := &ONNXRuntime{runtimeDir: runtimeDir}
		r.removeStagedLibrary()

		if _, err := os.Stat(staged); !os.IsNotExist(err) {
			t.Errorf("staged library should have been removed, got err=%v", err)
		}
	})

	t.Run("no error when file does not exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		runtimeDir := filepath.Join(tmpDir, "onnxruntime")
		if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
			t.Fatal(err)
		}

		r := &ONNXRuntime{runtimeDir: runtimeDir}
		// Should not panic or error.
		r.removeStagedLibrary()
	})
}

func TestResolveSharedLibraryPath_SkipStaged(t *testing.T) {
	// Ensure env override is clear so we test the staged-file logic.
	os.Unsetenv("SPROUT_ONNX_RUNTIME_LIB")
	os.Unsetenv("SPROUT_DISABLE_YALUE_BOOTSTRAP")

	libName := platformLibName()
	if libName == "" {
		t.Skip("no platform lib name on this platform")
	}

	t.Run("skipStaged=false returns staged file", func(t *testing.T) {
		tmpDir := t.TempDir()
		runtimeDir := filepath.Join(tmpDir, "onnxruntime")
		if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
			t.Fatal(err)
		}
		staged := filepath.Join(runtimeDir, libName)
		if err := os.WriteFile(staged, []byte("fake"), 0o644); err != nil {
			t.Fatal(err)
		}

		r := &ONNXRuntime{runtimeDir: runtimeDir}
		got := r.resolveSharedLibraryPath(false)
		if got != staged {
			t.Errorf("expected %s, got %s", staged, got)
		}
	})

	t.Run("skipStaged=true skips staged file", func(t *testing.T) {
		tmpDir := t.TempDir()
		runtimeDir := filepath.Join(tmpDir, "onnxruntime")
		if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
			t.Fatal(err)
		}
		staged := filepath.Join(runtimeDir, libName)
		// Write a tiny fake file so we can tell if it was replaced by a download.
		if err := os.WriteFile(staged, []byte("fake_stale_library"), 0o644); err != nil {
			t.Fatal(err)
		}

		r := &ONNXRuntime{runtimeDir: runtimeDir}
		got := r.resolveSharedLibraryPath(true)
		// With skipStaged=true, the pre-existing staged file is bypassed.
		// The download step may write a fresh library to the SAME path.
		// Verify the original fake content is gone (replaced or deleted).
		if got == staged {
			content, err := os.ReadFile(staged)
			if err != nil {
				t.Fatalf("failed to read staged file: %v", err)
			}
			if string(content) == "fake_stale_library" {
				t.Error("skipStaged=true returned path to the original fake staged file — staged file was not bypassed")
			}
			// Download succeeded and replaced the fake with a real library — that's correct.
		}
		// If got != staged, the staged file was bypassed and something else
		// (yalue bootstrap, etc.) was returned, or "" if nothing was found. Both are fine.
	})

	t.Run("env override ignores skipStaged", func(t *testing.T) {
		tmpDir := t.TempDir()
		runtimeDir := filepath.Join(tmpDir, "onnxruntime")
		if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
			t.Fatal(err)
		}
		envLib := filepath.Join(tmpDir, "custom_lib.so")
		t.Setenv("SPROUT_ONNX_RUNTIME_LIB", envLib)

		r := &ONNXRuntime{runtimeDir: runtimeDir}
		got := r.resolveSharedLibraryPath(true)
		if got != envLib {
			t.Errorf("env override should take precedence, expected %s, got %s", envLib, got)
		}
	})
}
