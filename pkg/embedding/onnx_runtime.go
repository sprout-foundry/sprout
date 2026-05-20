//go:build !wasm

// Package embedding provides ONNX-based embedding infrastructure for sprout.
//
// The ONNX runtime is a shared base that any ONNX-based model can use:
// embedding models (EmbeddingGemma) and future generation models (Gemma 3 2B).
package embedding

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	onnxruntime "github.com/yalue/onnxruntime_go"
)

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
		if err := onnxruntime.InitializeEnvironment(onnxruntime.WithLogLevelWarning()); err != nil {
			return fmt.Errorf("onnx: initialize environment: %w", err)
		}
	}

	r.ready = true
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

// Close destroys the global ONNX environment and releases all resources.
// After calling Close, the runtime cannot be used again; create a new one.
func (r *ONNXRuntime) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.closed {
		return nil
	}
	r.closed = true
	r.ready = false

	// Destroy the global ONNX environment.
	// Note: this affects all ONNXRuntime instances sharing the same process.
	if onnxruntime.IsInitialized() {
		if err := onnxruntime.DestroyEnvironment(); err != nil {
			return fmt.Errorf("onnx: destroy environment: %w", err)
		}
	}

	return nil
}
