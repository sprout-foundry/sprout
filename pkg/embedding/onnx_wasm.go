//go:build wasm

package embedding

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"syscall/js"
	"time"
)

// In WASM builds the ONNX runtime is available via JS bridge, so
// onnxAvailable — the initONNX fast-path won't reject it.
const onnxAvailable = true

// This file provides the WASM-side ONNX implementation. There are two modes:
//
//  1. **JS bridge active** — when the host page registers a provider on
//     `globalThis.__sproutONNX` (see docs/WASM_API.md for the contract),
//     `NewONNXEmbeddingProvider` returns a working provider whose `Embed`
//     and `EmbedBatch` calls marshal through `syscall/js` to the JS-side
//     provider (typically onnxruntime-web). The tokenizer / index /
//     RRF-merge / dual-store paths stay pure Go.
//
//  2. **No bridge** — `__sproutONNX` is undefined, so `NewONNXEmbeddingProvider`
//     returns `errWASMNotSupported` and `EmbeddingManager` silently falls
//     back to the static provider. Everything else continues working.
//
// Pure-Go pieces of the embedding pipeline — `ModelConfig`, `ModelDownloader`,
// `GemmaTokenizer`, the static provider, the HNSW store, the IndexManager —
// live in their non-tagged files and build for WASM unchanged.

var errWASMNotSupported = errors.New("onnx: native ONNX runtime not available on WASM; install a JS-side onnxruntime-web provider on globalThis.__sproutONNX to enable ONNX-quality embeddings")

// onnxRequiresModelFiles is the WASM counterpart to the helper defined in
// onnx_runtime.go for native builds. Returns false because the JS-side
// provider holds the model bytes; the manager skips its on-disk file check
// when this is false.
func onnxRequiresModelFiles() bool { return false }

// DefaultModelDir mirrors the non-wasm resolver: SPROUT_MODELS_DIR env var
// takes precedence; otherwise we anchor under SPROUT_CONFIG or the user's
// home directory. On WASM this points at the IndexedDB-backed MEMFS path
// that cmd/wasm sets up.
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

// ─── ONNXRuntime (no-op on WASM) ─────────────────────────────────
//
// Native sprout uses ONNXRuntime to hold the global C-runtime environment.
// On WASM the JS-side provider holds its own runtime; the Go-WASM side
// only needs a placeholder so the manager-level wiring (Init → Open store
// → NewONNXEmbeddingProvider) doesn't short-circuit before the bridge is
// asked. Methods are no-ops; Close still flips `closed` so misuse traps.

type ONNXRuntime struct {
	closed bool
}

func NewONNXRuntime() (*ONNXRuntime, error)                { return &ONNXRuntime{}, nil }
func NewONNXRuntimeWithDir(_ string) (*ONNXRuntime, error) { return &ONNXRuntime{}, nil }
func (r *ONNXRuntime) Ready() bool                         { return r != nil && !r.closed }
func (r *ONNXRuntime) Close() error {
	if r != nil {
		r.closed = true
	}
	return nil
}

type SessionOption struct {
	IntraOpNumThreads int
	InterOpNumThreads int
}

func (o SessionOption) Apply(_ interface{}) error { return nil }

// ─── ONNXEmbeddingProvider (JS bridge or stub) ───────────────────

// ONNXEmbeddingProvider on WASM either bridges to the JS-side onnxruntime-web
// provider or returns errors for every operation. Construction picks the
// mode based on whether `globalThis.__sproutONNX` is set.
type ONNXEmbeddingProvider struct {
	bridged    bool
	jsProvider js.Value
	dims       int
	fullDims   int
	modelName  string
	modelHash  string
}

// NewONNXEmbeddingProvider on WASM detects the JS-side provider at
// construction time. The model/tokenizer path arguments are ignored
// because the JS-side handles model loading itself.
func NewONNXEmbeddingProvider(
	_ context.Context,
	_ *ONNXRuntime,
	_, _ string,
	dims, fullDims int,
) (*ONNXEmbeddingProvider, error) {
	jsProvider := js.Global().Get("__sproutONNX")
	if jsProvider.IsUndefined() || jsProvider.IsNull() {
		return nil, errWASMNotSupported
	}
	if dims <= 0 {
		dims = fullDims
	}
	if fullDims <= 0 {
		fullDims = 768
	}
	hash := "browser-bridge"
	if hv := jsProvider.Get("modelHash"); hv.Type() == js.TypeString {
		hash = hv.String()
	}
	name := "onnx-embeddinggemma-300m-web-bridge"
	if nv := jsProvider.Get("modelName"); nv.Type() == js.TypeString {
		name = nv.String()
	}
	if dv := jsProvider.Get("dimensions"); dv.Type() == js.TypeNumber {
		dims = dv.Int()
	}
	return &ONNXEmbeddingProvider{
		bridged:    true,
		jsProvider: jsProvider,
		dims:       dims,
		fullDims:   fullDims,
		modelName:  name,
		modelHash:  hash,
	}, nil
}

func (p *ONNXEmbeddingProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	if p == nil || !p.bridged {
		return nil, errWASMNotSupported
	}
	promise := p.jsProvider.Call("embed", text)
	result, err := awaitPromise(ctx, promise)
	if err != nil {
		return nil, fmt.Errorf("onnx bridge: embed: %w", err)
	}
	return jsValueToFloat32Slice(result, p.dims)
}

func (p *ONNXEmbeddingProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if p == nil || !p.bridged {
		return nil, errWASMNotSupported
	}
	// Build a JS string array. js.ValueOf([]interface{}) works but requires
	// the slice be []interface{}, so coerce element-by-element.
	jsTexts := make([]interface{}, len(texts))
	for i, t := range texts {
		jsTexts[i] = t
	}
	promise := p.jsProvider.Call("embedBatch", js.ValueOf(jsTexts))
	result, err := awaitPromise(ctx, promise)
	if err != nil {
		return nil, fmt.Errorf("onnx bridge: embedBatch: %w", err)
	}
	if result.Type() != js.TypeObject {
		return nil, fmt.Errorf("onnx bridge: embedBatch expected an array, got %s", result.Type())
	}
	n := result.Length()
	out := make([][]float32, n)
	for i := 0; i < n; i++ {
		vec, err := jsValueToFloat32Slice(result.Index(i), p.dims)
		if err != nil {
			return nil, fmt.Errorf("onnx bridge: embedBatch[%d]: %w", i, err)
		}
		out[i] = vec
	}
	return out, nil
}

// EmbedWithPrefix / EmbedBatchWithPrefix mirror the native signatures.
// On WASM, prefix wiring is the JS provider's responsibility — see the
// EmbeddingGemma task-prefix table in BrowserONNXProvider. We pass the
// prefixed text through to keep the contract symmetric.
func (p *ONNXEmbeddingProvider) EmbedWithPrefix(ctx context.Context, text, prefix string) ([]float32, error) {
	return p.Embed(ctx, prefix+text)
}

func (p *ONNXEmbeddingProvider) EmbedBatchWithPrefix(ctx context.Context, texts []string, prefix string) ([][]float32, error) {
	prefixed := make([]string, len(texts))
	for i, t := range texts {
		prefixed[i] = prefix + t
	}
	return p.EmbedBatch(ctx, prefixed)
}

func (p *ONNXEmbeddingProvider) Dimensions() int {
	if p == nil {
		return 0
	}
	return p.dims
}

func (p *ONNXEmbeddingProvider) Name() string {
	if p == nil || p.modelName == "" {
		return "onnx-wasm-bridge"
	}
	return p.modelName
}

func (p *ONNXEmbeddingProvider) ModelHash() string {
	if p == nil {
		return ""
	}
	return p.modelHash
}

func (p *ONNXEmbeddingProvider) Close() error { return nil }

// ─── syscall/js helpers ──────────────────────────────────────────

// awaitPromise blocks the calling goroutine until the JS promise resolves
// or rejects, or until ctx is cancelled. Implemented with then/catch
// callbacks that signal a Go channel — the standard pattern for awaiting
// a JS Promise from Go-WASM.
//
// Both callbacks are released after firing so we don't leak js.Func
// instances. The select on ctx.Done() lets a cancelled call return
// promptly even if the JS side never resolves.
func awaitPromise(ctx context.Context, promise js.Value) (js.Value, error) {
	if promise.IsUndefined() || promise.IsNull() {
		return js.Value{}, fmt.Errorf("promise was %s", promise.Type())
	}
	// Some callers may pass a plain value (sync result) rather than a
	// Promise. Detect by looking for a .then method; if absent, return
	// the value directly.
	then := promise.Get("then")
	if then.Type() != js.TypeFunction {
		return promise, nil
	}

	resultCh := make(chan js.Value, 1)
	errCh := make(chan error, 1)

	var thenCb, catchCb js.Func
	thenCb = js.FuncOf(func(_ js.Value, args []js.Value) interface{} {
		thenCb.Release()
		catchCb.Release()
		if len(args) > 0 {
			resultCh <- args[0]
		} else {
			resultCh <- js.Undefined()
		}
		return nil
	})
	catchCb = js.FuncOf(func(_ js.Value, args []js.Value) interface{} {
		thenCb.Release()
		catchCb.Release()
		msg := "promise rejected"
		if len(args) > 0 {
			rv := args[0]
			// .Get panics on primitives, so check type first. Errors
			// (objects with `.message`) take priority over plain strings.
			switch rv.Type() {
			case js.TypeObject:
				if m := rv.Get("message"); m.Type() == js.TypeString {
					msg = m.String()
				}
			case js.TypeString:
				msg = rv.String()
			}
		}
		errCh <- errors.New(msg)
		return nil
	})

	promise.Call("then", thenCb).Call("catch", catchCb)

	// Bound the wait so a deadlocked JS provider can't pin the goroutine
	// forever even when the caller forgets to set a context deadline.
	const fallbackTimeout = 60 * time.Second
	var deadline <-chan time.Time
	if _, ok := ctx.Deadline(); !ok {
		t := time.NewTimer(fallbackTimeout)
		defer t.Stop()
		deadline = t.C
	}

	select {
	case v := <-resultCh:
		return v, nil
	case err := <-errCh:
		return js.Value{}, err
	case <-ctx.Done():
		thenCb.Release()
		catchCb.Release()
		return js.Value{}, ctx.Err()
	case <-deadline:
		thenCb.Release()
		catchCb.Release()
		return js.Value{}, fmt.Errorf("onnx bridge: timed out after %s waiting for promise", fallbackTimeout)
	}
}

// jsValueToFloat32Slice converts a JS Float32Array, Array, or any indexable
// object of numbers into a Go []float32. When the value is a Float32Array,
// we copy through its backing ArrayBuffer in one shot; for plain Arrays we
// fall back to per-element conversion (slower but correct).
//
// expectedDims is a soft check — values whose Length differs are still
// converted, with a warning surfaced as an error only when length is 0 or
// negative. Some hosts may return MRL-truncated vectors.
func jsValueToFloat32Slice(v js.Value, expectedDims int) ([]float32, error) {
	if v.IsUndefined() || v.IsNull() {
		return nil, fmt.Errorf("expected Float32Array, got %s", v.Type())
	}
	n := v.Length()
	if n <= 0 {
		return nil, fmt.Errorf("empty vector returned (expected ~%d dims)", expectedDims)
	}

	// Fast path: if the value has a .buffer property, treat it as a typed
	// array and copy through the buffer.
	buffer := v.Get("buffer")
	if !buffer.IsUndefined() && !buffer.IsNull() {
		byteOffset := 0
		if bo := v.Get("byteOffset"); bo.Type() == js.TypeNumber {
			byteOffset = bo.Int()
		}
		byteLen := n * 4
		// Make a Uint8Array view of the same bytes so js.CopyBytesToGo can
		// read them. ArrayBuffer doesn't implement the copy contract directly.
		u8 := js.Global().Get("Uint8Array").New(buffer, byteOffset, byteLen)
		raw := make([]byte, byteLen)
		copied := js.CopyBytesToGo(raw, u8)
		if copied != byteLen {
			return nil, fmt.Errorf("typed array copy: got %d bytes, expected %d", copied, byteLen)
		}
		out := make([]float32, n)
		for i := 0; i < n; i++ {
			bits := binary.LittleEndian.Uint32(raw[i*4 : (i+1)*4])
			out[i] = math.Float32frombits(bits)
		}
		return out, nil
	}

	// Slow path: per-element conversion.
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		el := v.Index(i)
		if el.Type() != js.TypeNumber {
			return nil, fmt.Errorf("element %d is %s, expected number", i, el.Type())
		}
		out[i] = float32(el.Float())
	}
	return out, nil
}
