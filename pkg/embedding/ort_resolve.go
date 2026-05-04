package embedding

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

const ortVersion = "1.22.0"

// resolveORTLibrary finds or provisions the ONNX Runtime shared library.
// Priority:
//
//  1. Explicit config (explicitPath)
//  2. ONNXRUNTIME_LIB env var
//  3. Well-known system paths
//  4. Cached in user config dir
//  5. Extracted embedded library (requires embed_ort build tag)
//
// Returns an error if no library can be found.
func resolveORTLibrary(explicitPath string) (string, error) {
	// 1. Explicit config
	if explicitPath != "" {
		if _, err := os.Stat(explicitPath); err == nil {
			return explicitPath, nil
		}
		return "", fmt.Errorf("embedding: configured ORT library not found: %s", explicitPath)
	}

	// 2. Environment variable
	if envPath := os.Getenv("ONNXRUNTIME_LIB"); envPath != "" {
		if _, err := os.Stat(envPath); err == nil {
			return envPath, nil
		}
	}

	// 3. Well-known system paths
	libName := ortLibraryName()
	for _, searchPath := range ortSearchPaths() {
		candidate := filepath.Join(searchPath, libName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	// 4. Cached in user config dir
	cacheDir := ortCacheDir()
	cached := filepath.Join(cacheDir, libName)
	if _, err := os.Stat(cached); err == nil {
		return cached, nil
	}

	// 5. Try to extract embedded library (if compiled with embed_ort build tag)
	if embedded := getEmbeddedORT(); len(embedded) > 0 {
		if err := os.MkdirAll(cacheDir, 0755); err != nil {
			return "", fmt.Errorf("embedding: create ORT cache dir: %w", err)
		}
		if err := os.WriteFile(cached, embedded, 0755); err != nil {
			return "", fmt.Errorf("embedding: extract ORT library: %w", err)
		}
		return cached, nil
	}

	return "", fmt.Errorf(
		"embedding: ONNX Runtime library not found. "+
			"Install onnxruntime %s or set ONNXRUNTIME_LIB env var", ortVersion)
}

// ortLibraryName returns the platform-specific library filename.
func ortLibraryName() string {
	switch runtime.GOOS {
	case "linux":
		return "libonnxruntime.so.1.22.0"
	case "darwin":
		return "libonnxruntime.1.22.0.dylib"
	case "windows":
		return "onnxruntime.dll"
	default:
		return "libonnxruntime.so"
	}
}

// ortSearchPaths returns platform-specific paths where ORT might be installed.
func ortSearchPaths() []string {
	switch runtime.GOOS {
	case "linux":
		return []string{
			"/usr/lib/x86_64-linux-gnu",
			"/usr/lib/aarch64-linux-gnu",
			"/usr/local/lib",
			"/usr/lib",
		}
	case "darwin":
		home, _ := os.UserHomeDir()
		return []string{
			"/usr/local/lib",
			filepath.Join(home, ".local/lib"),
		}
	case "windows":
		return []string{} // Windows doesn't have standard shared-library paths
	default:
		return []string{"/usr/local/lib", "/usr/lib"}
	}
}

// ortCacheDir returns the directory for cached ORT libraries.
func ortCacheDir() string {
	configDir := os.Getenv("SPROUT_CONFIG")
	if configDir == "" {
		configDir = os.Getenv("LEDIT_CONFIG")
	}
	if configDir == "" {
		home, _ := os.UserHomeDir()
		configDir = filepath.Join(home, ".config", "sprout")
	}
	return filepath.Join(configDir, "ort")
}
