package embedding

import (
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const ortVersion = "1.25.1"

// resolveORTLibrary finds or provisions the ONNX Runtime shared library.
// Priority:
//
//  1. Explicit config (explicitPath)
//  2. ONNXRUNTIME_LIB env var
//  3. Cached in user config dir (our download — takes priority over system)
//  4. Well-known system paths
//  5. Extracted embedded library (requires embed_ort build tag)
//  6. Auto-download from pre-built releases (unless SPROUT_NO_DOWNLOAD=1)
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

	// 3. Cached in user config dir (our download — before system paths so our
	// compatible version wins over any potentially incompatible system install).
	cacheDir := ortCacheDir()
	libName := ortLibraryName()
	cached := filepath.Join(cacheDir, libName)
	if _, err := os.Stat(cached); err == nil {
		return cached, nil
	}

	// 4. Well-known system paths
	for _, searchPath := range ortSearchPaths() {
		candidate := filepath.Join(searchPath, libName)
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
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

	// 6. Auto-download from pre-built releases
	if os.Getenv("SPROUT_NO_DOWNLOAD") != "1" {
		downloaded, err := downloadORTLibrary(cacheDir, libName)
		if err == nil {
			return downloaded, nil
		}
		// Log download failure but continue to error below
		_ = err
	}

	return "", fmt.Errorf(
		"embedding: ONNX Runtime library not found. "+
			"Install onnxruntime %s, set ONNXRUNTIME_LIB env var, or ensure network access for auto-download", ortVersion)
}

// downloadORTLibrary downloads the ONNX Runtime shared library from a
// pre-built release and saves it to cacheDir with the expected libName.
//
// It downloads the appropriate platform ZIP from csukuangfj/onnxruntime-libs,
// extracts the shared library, and saves it to the cache directory.
func downloadORTLibrary(cacheDir, libName string) (string, error) {
	downloadURL, err := getORTDownloadURL()
	if err != nil {
		return "", fmt.Errorf("embedding: determine ORT download URL: %w", err)
	}

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return "", fmt.Errorf("embedding: create ORT cache dir: %w", err)
	}

	// Download to temp file
	tmpFile, err := os.CreateTemp("", "onnxruntime-*.zip")
	if err != nil {
		return "", fmt.Errorf("embedding: create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()

	req, err := http.NewRequest("GET", downloadURL, nil)
	if err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("embedding: create download request: %w", err)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("embedding: download ORT library: %w", err)
	}

	_, err = io.Copy(tmpFile, resp.Body)
	resp.Body.Close()
	tmpFile.Close()
	if err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("embedding: write download: %w", err)
	}

	// Extract the shared library from the ZIP
	outPath := filepath.Join(cacheDir, libName)
	if err := extractORTFromZIP(tmpPath, outPath); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("embedding: extract ORT library: %w", err)
	}

	// Set executable permissions
	if err := os.Chmod(outPath, 0755); err != nil {
		os.Remove(tmpPath)
		os.Remove(outPath)
		return "", fmt.Errorf("embedding: set permissions: %w", err)
	}

	// Clean up temp file
	os.Remove(tmpPath)

	return outPath, nil
}

// getORTDownloadURL returns the download URL for the ORT shared library
// for the current platform.
func getORTDownloadURL() (string, error) {
	baseURL := "https://github.com/csukuangfj/onnxruntime-libs/releases/download/v" + ortVersion

	switch runtime.GOOS {
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			return baseURL + "/onnxruntime-linux-x64-glibc2_17-Release-" + ortVersion + ".zip", nil
		case "arm64":
			return baseURL + "/onnxruntime-linux-aarch64-glibc2_17-Release-" + ortVersion + ".zip", nil
		default:
			return "", fmt.Errorf("unsupported architecture %s for Linux", runtime.GOARCH)
		}
	case "darwin":
		switch runtime.GOARCH {
		case "amd64":
			return baseURL + "/onnxruntime-osx-x64-" + ortVersion + ".zip", nil
		case "arm64":
			return baseURL + "/onnxruntime-osx-arm64-" + ortVersion + ".zip", nil
		default:
			return "", fmt.Errorf("unsupported architecture %s for macOS", runtime.GOARCH)
		}
	case "windows":
		switch runtime.GOARCH {
		case "amd64":
			return baseURL + "/onnxruntime-win-x64-" + ortVersion + ".zip", nil
		default:
			return "", fmt.Errorf("unsupported architecture %s for Windows", runtime.GOARCH)
		}
	default:
		return "", fmt.Errorf("unsupported OS %s", runtime.GOOS)
	}
}

// extractORTFromZIP extracts the shared library from a downloaded ZIP file.
// The ZIPs from csukuangfj have different internal structures per platform:
//   - Linux: lib/libonnxruntime.so.1.N.N (with symlink libonnxruntime.so.1)
//   - macOS: libonnxruntime.N.N.dylib at root
//   - Windows: onnxruntime.dll at root
func extractORTFromZIP(zipPath, outPath string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer r.Close()

	libName := filepath.Base(outPath)

	// First try to find the exact filename we need
	for _, f := range r.File {
		if filepath.Base(f.Name) == libName {
			return copyFromZIP(f, outPath)
		}
	}

	// For Linux, the ZIP contains lib/libonnxruntime.so.1.N.N but we want
	// libonnxruntime.so.1.N.N at root level. Find any .so file.
	if runtime.GOOS == "linux" {
		for _, f := range r.File {
			if strings.Contains(f.Name, "libonnxruntime.so") && !f.FileInfo().IsDir() {
				return copyFromZIP(f, outPath)
			}
		}
	}

	// For macOS, look for any dylib file
	if runtime.GOOS == "darwin" {
		for _, f := range r.File {
			if strings.HasSuffix(f.Name, ".dylib") && !f.FileInfo().IsDir() {
				return copyFromZIP(f, outPath)
			}
		}
	}

	// For Windows, look for the DLL
	if runtime.GOOS == "windows" {
		for _, f := range r.File {
			if strings.HasSuffix(strings.ToLower(f.Name), ".dll") && !f.FileInfo().IsDir() {
				return copyFromZIP(f, outPath)
			}
		}
	}

	return fmt.Errorf("could not find %s in downloaded archive", libName)
}

// copyFromZIP copies a single file from a ZIP entry to the output path.
func copyFromZIP(f *zip.File, outPath string) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("open zip entry: %w", err)
	}
	defer rc.Close()

	out, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("create output file: %w", err)
	}

	_, err = io.Copy(out, rc)
	closeErr := out.Close()
	if err != nil {
		return fmt.Errorf("copy content: %w", err)
	}
	return closeErr
}

// ortLibraryName returns the platform-specific library filename.
func ortLibraryName() string {
	switch runtime.GOOS {
	case "linux":
		return "libonnxruntime.so." + ortVersion
	case "darwin":
		return "libonnxruntime." + ortVersion + ".dylib"
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
