//go:build !wasm && cgo

// Package embedding provides ONNX-based embedding infrastructure for sprout.
//
// The ONNX runtime is a shared base that any ONNX-based model can use:
// embedding models (EmbeddingGemma) and future generation models (Gemma 3 2B).
package embedding

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	onnxruntime "github.com/yalue/onnxruntime_go"
)

// onnxAvailable is true when the ONNX runtime backend is available.
// True in CGO builds (native onnxruntime_go) and WASM builds (JS bridge).
// False when CGO is disabled and no alternative backend exists.
const onnxAvailable = true

// ONNXRuntime provides shared ONNX Runtime infrastructure.
// It initializes the ONNX environment and creates dynamic inference sessions
// for models. Designed to be shared between embedding providers (EmbeddingGemma)
// and future local LLM providers (Gemma 3 2B) — they all use the same
// runtime environment and shared library.
type ONNXRuntime struct {
	mu         sync.Mutex
	ready      bool
	modelDir   string // ~/.config/sprout/models/
	runtimeDir string // ~/.config/sprout/models/onnxruntime/
	closed     bool
}

// onnxRequiresModelFiles reports whether the active ONNX backend expects
// the model + tokenizer to exist on local disk before NewONNXEmbeddingProvider
// can succeed. Native sprout uses yalue/onnxruntime_go which loads .onnx
// files directly, so the answer is yes; the WASM build delegates to a
// JS-side provider that handles model loading itself, so the WASM
// counterpart returns false.
func onnxRequiresModelFiles() bool { return true }

// DefaultModelDir returns the default model directory path.
// Priority: SPROUT_MODELS_DIR env > SPROUT_CONFIG/LEDIT_CONFIG env > ~/.config/sprout
func DefaultModelDir() string {
	if dir := os.Getenv("SPROUT_MODELS_DIR"); dir != "" {
		return dir
	}
	configDir := os.Getenv("SPROUT_CONFIG")
	if configDir == "" {
		configDir = os.Getenv("LEDIT_CONFIG")
	}
	if configDir == "" {
		home, _ := os.UserHomeDir()
		configDir = filepath.Join(home, ".config", "sprout")
	}
	return filepath.Join(configDir, "models")
}

// NewONNXRuntime creates a new ONNX runtime with the default model directory.
// The ONNX environment is initialized globally on first creation.
func NewONNXRuntime() (*ONNXRuntime, error) {
	return NewONNXRuntimeWithDir(DefaultModelDir())
}

// NewONNXRuntimeWithDir creates a new ONNX runtime with a specific model
// directory. Useful for testing with isolated temp directories.
func NewONNXRuntimeWithDir(modelDir string) (*ONNXRuntime, error) {
	r := &ONNXRuntime{
		modelDir:   modelDir,
		runtimeDir: filepath.Join(modelDir, "onnxruntime"),
	}
	if err := r.init(); err != nil {
		return nil, err
	}
	return r, nil
}

// init initializes the ONNX environment. The global environment is shared
// across all ONNXRuntime instances via the onnxruntime_go library.
func (r *ONNXRuntime) init() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return fmt.Errorf("onnx: runtime is closed")
	}

	// Ensure the runtime directory exists for shared library extraction.
	if err := os.MkdirAll(r.runtimeDir, 0o755); err != nil {
		return fmt.Errorf("onnx: create runtime dir: %w", err)
	}

	// Initialize the global ONNX environment if not already done.
	// onnxruntime_go uses a global singleton; this is idempotent.
	if !onnxruntime.IsInitialized() {
		// Resolve and set the shared library path BEFORE InitializeEnvironment.
		// The yalue/onnxruntime_go library defaults to looking for plain
		// "onnxruntime.so" in CWD/LD_LIBRARY_PATH, which is rarely correct on
		// developer machines. We probe a few stable locations and bootstrap
		// from the yalue module's bundled library when needed.
		libPath := r.resolveSharedLibraryPath()
		if libPath != "" {
			onnxruntime.SetSharedLibraryPath(libPath)
		}
		if err := onnxruntime.InitializeEnvironment(onnxruntime.WithLogLevelWarning()); err != nil {
			return r.installGuidanceError(err)
		}
	}

	r.ready = true
	return nil
}

// platformLibName returns the conventional ONNX Runtime shared library
// filename for the running platform. Returns "" for unsupported platforms;
// the caller should fall back to letting yalue try its default.
func platformLibName() string {
	switch runtime.GOOS {
	case "linux":
		if runtime.GOARCH == "arm64" {
			return "onnxruntime_arm64.so"
		}
		return "onnxruntime.so"
	case "darwin":
		if runtime.GOARCH == "arm64" {
			return "onnxruntime_arm64.dylib"
		}
		return "onnxruntime.dylib"
	case "windows":
		return "onnxruntime.dll"
	default:
		return ""
	}
}

// resolveSharedLibraryPath locates the ONNX Runtime shared library, returning
// an absolute path or "" if none was found. Resolution order:
//
//  1. SPROUT_ONNX_RUNTIME_LIB env var (absolute path; user override).
//  2. ~/.config/sprout/models/onnxruntime/<platform-lib-name> (pre-staged).
//  3. Auto-download from the official microsoft/onnxruntime release matching
//     the yalue ABI, extract, and stage into (2). This is the production-
//     grade fallback — files come from a known source, hashes are pinned
//     per onnxRuntimeReleaseConfig, and the writes are atomic.
//  4. Bootstrap from yalue/onnxruntime_go's bundled test_data shared library.
//     Strictly a dev convenience; SPROUT_DISABLE_YALUE_BOOTSTRAP=1 turns
//     this step off so production builds can rely on (3) alone.
//
// Returning "" leaves SetSharedLibraryPath uncalled, so yalue falls back to
// its compiled-in default ("onnxruntime.so" in CWD/LD_LIBRARY_PATH). That
// default usually fails on dev machines but is preserved so custom
// deployments that pre-stage the library on the system loader path still
// work without sprout intervention.
func (r *ONNXRuntime) resolveSharedLibraryPath() string {
	libName := platformLibName()
	if libName == "" {
		return ""
	}

	if env := os.Getenv("SPROUT_ONNX_RUNTIME_LIB"); env != "" {
		return env
	}

	staged := filepath.Join(r.runtimeDir, libName)
	if _, err := os.Stat(staged); err == nil {
		return staged
	}

	// Production-grade fallback: download from upstream. Use a bounded
	// context so a stuck CDN can't hold up sprout startup indefinitely.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	if path, err := downloadAndStageONNXRuntime(ctx, r.runtimeDir, libName); err == nil {
		log.Printf("onnx: downloaded ONNX Runtime v%s to %s (from microsoft/onnxruntime release)", onnxRuntimeVersion, path)
		return path
	} else {
		log.Printf("onnx: upstream download failed (will try dev fallback): %v", err)
	}

	if os.Getenv("SPROUT_DISABLE_YALUE_BOOTSTRAP") != "" {
		// Production deployments lock to (1)-(3). Don't even try the yalue
		// test_data path — fail closed so the install error surfaces.
		return ""
	}
	if bundled := findYalueBundledLib(libName); bundled != "" {
		if err := copyFileTo(bundled, staged); err == nil {
			log.Printf("onnx: bootstrapped shared library to %s from yalue/onnxruntime_go test_data — DEV CONVENIENCE ONLY. Production deployments should rely on the upstream download path or pre-stage the library. See docs/ONNX_RUNTIME.md.", staged)
			return staged
		} else {
			log.Printf("onnx: failed to bootstrap shared library: %v", err)
		}
	}

	return ""
}

// installGuidanceError wraps a raw yalue InitializeEnvironment error with
// concrete next-steps so the user doesn't have to guess where to drop the
// shared library. The original error is kept via %w for debugging.
func (r *ONNXRuntime) installGuidanceError(cause error) error {
	libName := platformLibName()
	platform := fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
	if libName == "" {
		return fmt.Errorf("onnx: initialize environment on unsupported platform %s: %w", platform, cause)
	}
	return fmt.Errorf("onnx: initialize environment: %w\n"+
		"ONNX Runtime shared library not found for %s. Resolve by either:\n"+
		"  - setting SPROUT_ONNX_RUNTIME_LIB to an absolute path containing %s, or\n"+
		"  - placing %s at %s\n"+
		"See docs/ONNX_RUNTIME.md for distribution guidance.",
		cause, platform, libName, libName, filepath.Join(r.runtimeDir, libName))
}

// findYalueBundledLib returns the filesystem path of the bundled .so/.dll/.dylib
// shipped in the yalue/onnxruntime_go module's test_data directory, or "" if
// no match exists. We consult runtime/debug.ReadBuildInfo to find the exact
// module version, then construct the canonical GOPATH/pkg/mod path.
func findYalueBundledLib(libName string) string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	var version string
	for _, dep := range info.Deps {
		if dep == nil {
			continue
		}
		if dep.Path == "github.com/yalue/onnxruntime_go" {
			version = dep.Version
			break
		}
	}
	if version == "" {
		return ""
	}

	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		home, _ := os.UserHomeDir()
		gopath = filepath.Join(home, "go")
	}

	candidate := filepath.Join(gopath, "pkg", "mod", "github.com", "yalue",
		"onnxruntime_go@"+version, "test_data", libName)
	if _, err := os.Stat(candidate); err != nil {
		return ""
	}
	return candidate
}

func copyFileTo(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".lib-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, dst); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// Ready returns true if the runtime has been successfully initialized.
func (r *ONNXRuntime) Ready() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ready && !r.closed
}

// NewDynamicSession creates a dynamic inference session for the given ONNX
// model file. Dynamic sessions allow flexible input/output tensors per Run()
// call, which is useful when batch sizes vary.
//
// The inputNames and outputNames can be nil to use the model's defaults.
// Session options (threading, GPU) can be passed via opts.
func (r *ONNXRuntime) NewDynamicSession(modelPath string, inputNames, outputNames []string, opts ...SessionOption) (*onnxruntime.DynamicAdvancedSession, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.ready {
		return nil, fmt.Errorf("onnx: runtime not initialized")
	}
	if r.closed {
		return nil, fmt.Errorf("onnx: runtime is closed")
	}

	so, err := r.newSessionOptions(opts)
	if err != nil {
		return nil, err
	}
	defer so.Destroy()

	session, err := onnxruntime.NewDynamicAdvancedSession(modelPath, inputNames, outputNames, so)
	if err != nil {
		return nil, fmt.Errorf("onnx: create session for %s: %w", modelPath, err)
	}

	return session, nil
}

// SessionOption configures an inference session.
type SessionOption struct {
	// IntraOpNumThreads sets the number of threads for intra-op parallelism.
	// 0 means use default.
	IntraOpNumThreads int
	// InterOpNumThreads sets the number of threads for inter-op parallelism.
	// 0 means use default.
	InterOpNumThreads int
}

// newSessionOptions creates a SessionOptions with the given options applied.
// The caller must call Destroy() on the returned options when done.
func (r *ONNXRuntime) newSessionOptions(opts []SessionOption) (*onnxruntime.SessionOptions, error) {
	so, err := onnxruntime.NewSessionOptions()
	if err != nil {
		return nil, fmt.Errorf("onnx: create session options: %w", err)
	}

	// Merge options (last wins).
	option := SessionOption{}
	for _, o := range opts {
		if o.IntraOpNumThreads != 0 {
			option.IntraOpNumThreads = o.IntraOpNumThreads
		}
		if o.InterOpNumThreads != 0 {
			option.InterOpNumThreads = o.InterOpNumThreads
		}
	}

	if option.IntraOpNumThreads > 0 {
		if err := so.SetIntraOpNumThreads(option.IntraOpNumThreads); err != nil {
			so.Destroy()
			return nil, fmt.Errorf("onnx: set intra-op threads: %w", err)
		}
	}
	if option.InterOpNumThreads > 0 {
		if err := so.SetInterOpNumThreads(option.InterOpNumThreads); err != nil {
			so.Destroy()
			return nil, fmt.Errorf("onnx: set inter-op threads: %w", err)
		}
	}

	return so, nil
}

// Close marks this runtime instance as closed. The underlying yalue ONNX
// environment is a PROCESS-WIDE singleton shared by every ONNXRuntime in the
// program, so we deliberately do NOT call DestroyEnvironment here: doing so
// invalidates in-flight sessions in OTHER managers and crashes their lingering
// init goroutines inside CGO (observed as SIGSEGV during the test suite when
// multiple managers were created and destroyed in quick succession).
//
// The environment is freed at process exit; that's enough.
func (r *ONNXRuntime) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}
	r.closed = true
	r.ready = false
	return nil
}
