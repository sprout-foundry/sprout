package embedding

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveORTLibraryExplicitPath(t *testing.T) {
	// Create a temporary file to act as a fake ORT library.
	dir := t.TempDir()
	fakeLib := filepath.Join(dir, "fake-ort.so")
	err := os.WriteFile(fakeLib, []byte("fake"), 0755)
	if err != nil {
		t.Fatalf("create fake lib: %v", err)
	}

	path, err := resolveORTLibrary(fakeLib)
	if err != nil {
		t.Fatalf("resolveORTLibrary(%q) = error %v, want success", fakeLib, err)
	}
	if path != fakeLib {
		t.Errorf("resolveORTLibrary(%q) = %q, want %q", fakeLib, path, fakeLib)
	}
}

func TestResolveORTLibraryExplicitPathNotFound(t *testing.T) {
	_, err := resolveORTLibrary("/nonexistent/fake-ort.so")
	if err == nil {
		t.Fatal("expected error for nonexistent explicit path, got nil")
	}
	if got := err.Error(); got != "embedding: configured ORT library not found: /nonexistent/fake-ort.so" {
		t.Errorf("unexpected error message: %s", got)
	}
}

func TestResolveORTLibraryEnvVar(t *testing.T) {
	if os.Getenv("ONNXRUNTIME_LIB") != "" {
		t.Skip("ONNXRUNTIME_LIB is already set; cannot test env var fallback")
	}

	dir := t.TempDir()
	fakeLib := filepath.Join(dir, "libonnxruntime.so.1.22.0")
	err := os.WriteFile(fakeLib, []byte("fake"), 0755)
	if err != nil {
		t.Fatalf("create fake lib: %v", err)
	}

	os.Setenv("ONNXRUNTIME_LIB", fakeLib)
	defer os.Unsetenv("ONNXRUNTIME_LIB")

	path, err := resolveORTLibrary("")
	if err != nil {
		t.Fatalf("resolveORTLibrary(\"\") with env = error %v", err)
	}
	if path != fakeLib {
		t.Errorf("resolveORTLibrary(\"\") = %q, want %q", path, fakeLib)
	}
}

func TestResolveORTLibraryExplicitPathOverridesEnv(t *testing.T) {
	// Set env var to a nonexistent path
	os.Setenv("ONNXRUNTIME_LIB", "/nonexistent/fake-ort.so")
	defer os.Unsetenv("ONNXRUNTIME_LIB")

	// Create an explicit path
	dir := t.TempDir()
	explicitLib := filepath.Join(dir, "explicit-ort.so")
	err := os.WriteFile(explicitLib, []byte("explicit"), 0755)
	if err != nil {
		t.Fatalf("create explicit lib: %v", err)
	}

	path, err := resolveORTLibrary(explicitLib)
	if err != nil {
		t.Fatalf("resolveORTLibrary with explicit path = error %v", err)
	}
	if path != explicitLib {
		t.Errorf("resolveORTLibrary = %q, want %q", path, explicitLib)
	}
}

func TestResolveORTLibraryNotFound(t *testing.T) {
	// Clear env to ensure no fallback
	prevEnv := os.Getenv("ONNXRUNTIME_LIB")
	os.Unsetenv("ONNXRUNTIME_LIB")
	defer func() {
		if prevEnv != "" {
			os.Setenv("ONNXRUNTIME_LIB", prevEnv)
		}
	}()

	// Only run the "not found" test if we're sure no system paths exist
	// (this may skip on systems that have ORT installed)
	_, err := resolveORTLibrary("")
	if err == nil {
		t.Skip("ORT library found via system/cache; skipping not-found test")
	}
	if got := err.Error(); got == "" {
		t.Error("expected non-empty error message")
	}
}

func TestORTLibraryName(t *testing.T) {
	switch runtime.GOOS {
	case "linux":
		if got := ortLibraryName(); got != "libonnxruntime.so.1.22.0" {
			t.Errorf("ortLibraryName() = %q, want %q", got, "libonnxruntime.so.1.22.0")
		}
	case "darwin":
		if got := ortLibraryName(); got != "libonnxruntime.1.22.0.dylib" {
			t.Errorf("ortLibraryName() = %q, want %q", got, "libonnxruntime.1.22.0.dylib")
		}
	case "windows":
		if got := ortLibraryName(); got != "onnxruntime.dll" {
			t.Errorf("ortLibraryName() = %q, want %q", got, "onnxruntime.dll")
		}
	}
}

func TestORTSearchPaths(t *testing.T) {
	paths := ortSearchPaths()
	if len(paths) == 0 && runtime.GOOS != "windows" {
		t.Error("expected at least one search path")
	}
}

func TestORTCacheDir(t *testing.T) {
	dir := ortCacheDir()
	if dir == "" {
		t.Error("ortCacheDir() returned empty string")
	}
	if runtime.GOOS != "windows" {
		// On Unix-like systems, the default should be ~/.config/sprout/ort
		home, _ := os.UserHomeDir()
		expected := filepath.Join(home, ".config", "sprout", "ort")
		if dir != expected {
			t.Logf("ortCacheDir() = %q (expected %q with default env)", dir, expected)
		}
	}
}

func TestORTCacheDirEnvOverride(t *testing.T) {
	prevSprout := os.Getenv("SPROUT_CONFIG")
	prevLedit := os.Getenv("LEDIT_CONFIG")
	os.Setenv("SPROUT_CONFIG", "/tmp/sprout-config")
	defer func() {
		if prevSprout != "" {
			os.Setenv("SPROUT_CONFIG", prevSprout)
		} else {
			os.Unsetenv("SPROUT_CONFIG")
		}
		if prevLedit != "" {
			os.Setenv("LEDIT_CONFIG", prevLedit)
		}
	}()

	dir := ortCacheDir()
	expected := filepath.Join("/tmp/sprout-config", "ort")
	if dir != expected {
		t.Errorf("ortCacheDir() = %q, want %q", dir, expected)
	}
}

func TestGetEmbeddedORT(t *testing.T) {
	// Without embed_ort build tag, this should return nil
	data := getEmbeddedORT()
	if data != nil && len(data) > 0 {
		// This is fine — means we were built with embed_ort
		t.Log("embedded ORT library is available (embed_ort build tag active)")
	}
	// If nil, also fine — just no embedded library
}
