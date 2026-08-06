package embedding

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

// The embedding model (EmbeddingGemma-300M) is ~180MB of weights loaded into an
// ONNX inference session, plus the runtime's shared library and arena. Each
// EmbeddingManager used to create its own runtime+provider, so a process that
// builds multiple agents — most notably the WebUI daemon, which constructs a
// fresh agent per chat session — loaded one full copy of the model per agent.
//
// The model is identical across managers (same modelDir, same dims), and the
// provider's Embed path is read-locked for state (the embedding cache, model
// weights, session handle), so a single process-wide instance can back every
// manager. This cache holds one (provider, runtime) per (modelDir, dims) key
// for the life of the process. Instances are intentionally never closed: the
// model stays resident so the daemon can reuse it, and one-shot CLI runs free
// it at process exit anyway. Managers that obtain a shared provider mark
// themselves providerShared and skip closing it.

type sharedONNXEntry struct {
	provider EmbeddingProvider
	runtime  *ONNXRuntime
}

var (
	sharedONNXMu      sync.Mutex
	sharedONNXByModel = map[string]*sharedONNXEntry{}
)

// sharedONNXKey returns a cache key for a (modelDir, model name, dims) tuple.
// The model name is load-bearing: Gemma and Jina are both 768-dim and live in
// the same modelDir, so keying on (modelDir, dims) alone makes the second
// provider return the first one's cached entry — a silent wrong-model bug.
func sharedONNXKey(modelDir, modelName string, dims int) string {
	return modelDir + "|" + modelName + "|" + strconv.Itoa(dims)
}

// acquireSharedONNXProvider returns a process-wide shared ONNX provider+runtime
// for the given model, creating (and downloading, if necessary) it once on the
// first call for each (modelDir, dims) pair. The returned instances are owned
// by the cache and must not be closed by the caller.
//
// Errors are not cached: a failed download/init leaves the slot empty so a
// later call can retry. The cache lock is held across creation so concurrent
// first-time callers serialize and then all observe the same instance rather
// than racing to each load their own copy.
func acquireSharedONNXProvider(ctx context.Context, modelDir string, modelConfig ModelConfig) (EmbeddingProvider, *ONNXRuntime, error) {
	if !onnxAvailable {
		return nil, nil, fmt.Errorf("ONNX runtime not available in this build (requires CGO or WASM bridge)")
	}

	key := sharedONNXKey(modelDir, modelConfig.Name, modelConfig.Dims)

	// Fast path: check cache under the lock.
	sharedONNXMu.Lock()
	if e, ok := sharedONNXByModel[key]; ok {
		sharedONNXMu.Unlock()
		return e.provider, e.runtime, nil
	}
	sharedONNXMu.Unlock()

	// Cache miss: build (and possibly download ~180MB) OUTSIDE the lock so
	// other embedding operations and agent reasoning are not blocked during
	// network I/O. The first caller to succeed wins; concurrent callers may
	// build duplicates, but only one is stored — the rest are closed.
	provider, runtime, err := buildONNXProvider(ctx, modelDir, modelConfig)
	if err != nil {
		return nil, nil, err
	}

	// Store under the lock with a double-check: another goroutine may have
	// built and cached the same key while we were building.
	sharedONNXMu.Lock()
	defer sharedONNXMu.Unlock()

	if e, ok := sharedONNXByModel[key]; ok {
		// Another caller cached a provider first — use theirs, close ours.
		provider.Close()
		runtime.Close()
		return e.provider, e.runtime, nil
	}

	sharedONNXByModel[key] = &sharedONNXEntry{provider: provider, runtime: runtime}
	return provider, runtime, nil
}

// acquireSharedJinaProvider returns a process-wide shared Jina Code v2
// provider+runtime, creating (and downloading, if necessary) it once. Like
// acquireSharedONNXProvider, the returned instances are owned by the cache and
// must not be closed by the caller. The key includes the model name so it
// cannot collide with a Gemma entry in the same modelDir.
func acquireSharedJinaProvider(ctx context.Context, modelDir string, modelConfig ModelConfig) (EmbeddingProvider, *ONNXRuntime, error) {
	if !onnxAvailable {
		return nil, nil, fmt.Errorf("ONNX runtime not available in this build (requires CGO)")
	}

	key := sharedONNXKey(modelDir, modelConfig.Name, modelConfig.Dims)

	sharedONNXMu.Lock()
	if e, ok := sharedONNXByModel[key]; ok {
		sharedONNXMu.Unlock()
		return e.provider, e.runtime, nil
	}
	sharedONNXMu.Unlock()

	runtime, err := NewONNXRuntimeWithDir(modelDir)
	if err != nil {
		return nil, nil, fmt.Errorf("jina: create runtime: %w", err)
	}

	modelName := modelConfig.Name
	modelPath := filepath.Join(modelDir, modelName, modelConfig.ModelFilenameOrDefault())
	tokenizerPath := filepath.Join(modelDir, modelName, "tokenizer.json")

	if onnxRequiresModelFiles() {
		if _, err := os.Stat(modelPath); err != nil {
			log.Printf("embedding: downloading code model %s...", modelName)
			if err := DownloadModel(ctx, modelDir, modelConfig); err != nil {
				runtime.Close()
				return nil, nil, fmt.Errorf("jina: download model: %w", err)
			}
			log.Printf("embedding: code model %s downloaded", modelName)
		}
	}

	provider, err := NewJinaONNXEmbeddingProvider(ctx, runtime, modelPath, tokenizerPath)
	if err != nil {
		runtime.Close()
		return nil, nil, fmt.Errorf("jina: create provider: %w", err)
	}

	sharedONNXMu.Lock()
	defer sharedONNXMu.Unlock()
	if e, ok := sharedONNXByModel[key]; ok {
		provider.Close()
		runtime.Close()
		return e.provider, e.runtime, nil
	}

	sharedONNXByModel[key] = &sharedONNXEntry{provider: provider, runtime: runtime}
	return provider, runtime, nil
}

// buildONNXProvider creates a fresh ONNX runtime + provider, downloading the
// model weights if they are not yet present on disk. It is the unshared
// creation path used by acquireSharedONNXProvider.
func buildONNXProvider(ctx context.Context, modelDir string, modelConfig ModelConfig) (EmbeddingProvider, *ONNXRuntime, error) {
	runtime, err := NewONNXRuntimeWithDir(modelDir)
	if err != nil {
		return nil, nil, fmt.Errorf("onnx: create runtime: %w", err)
	}

	modelName := modelConfig.Name
	modelPath := filepath.Join(modelDir, modelName, modelConfig.ModelFilenameOrDefault())
	tokenizerPath := filepath.Join(modelDir, modelName, "tokenizer.json")

	// Native builds load .onnx from disk; download it if missing. The WASM
	// build delegates to a JS-side provider that owns its own model loading, so
	// we skip the on-disk file check there.
	if onnxRequiresModelFiles() {
		if _, err := os.Stat(modelPath); err != nil {
			log.Printf("embedding: downloading ONNX model %s...", modelName)
			if err := DownloadModel(ctx, modelDir, modelConfig); err != nil {
				runtime.Close()
				return nil, nil, fmt.Errorf("onnx: download model: %w", err)
			}
			log.Printf("embedding: ONNX model %s downloaded", modelName)
		}
	}

	provider, err := NewONNXEmbeddingProvider(ctx, runtime, modelPath, tokenizerPath, modelConfig.Dims, modelConfig.FullDims)
	if err != nil {
		runtime.Close()
		return nil, nil, fmt.Errorf("onnx: create provider: %w", err)
	}

	return provider, runtime, nil
}
