//go:build wasm

package wasmshell

import (
	"context"
	"errors"
	"fmt"
	"syscall/js"
	"time"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// ErrEmbeddingDisabled is returned when Mode=Off and Embed() is called.
var ErrEmbeddingDisabled = errors.New("embedding: disabled by mode=off")

// ErrEmbeddingUnavailable is returned when the requested mode's
// provider isn't available (e.g. Mode=ONNX but no JS bridge).
var ErrEmbeddingUnavailable = errors.New("embedding: requested provider unavailable")

// jsEmbeddingAPI is the JS-callable API surface registered on
// globalThis.__sproutEmbedding. The WebUI calls these to query status
// or switch mode without going through a Go-side wrapper.
type jsEmbeddingAPI struct{}

// NewJSEmbeddingAPI registers the embedding API on
// globalThis.__sproutEmbedding. Idempotent.
func NewJSEmbeddingAPI() {
	api := &jsEmbeddingAPI{}
	_ = api // keep the type for future expansion
	obj := js.Global().Get("__sproutEmbedding")
	if !obj.IsUndefined() && !obj.IsNull() {
		return
	}
	wrapped := map[string]interface{}{}
	wrapped["status"] = js.FuncOf(func(_ js.Value, _ []js.Value) interface{} {
		return serializeEmbeddingStatus(GetEmbeddingStatus())
	})
	wrapped["setMode"] = js.FuncOf(func(_ js.Value, args []js.Value) interface{} {
		if len(args) < 1 {
			return errors.New("setMode: missing mode argument")
		}
		newMode := EmbeddingMode(args[0].String())
		SetEmbeddingMode(newMode)

		// When switching to ONNX mode, trigger lazy-load of
		// onnxruntime-web from the CDN. The JS global is a
		// fire-and-forget callback; errors are logged on the JS side.
		if newMode == ModeONNX {
			loader := js.Global().Get("__sproutLoadOnnxRuntime")
			if !loader.IsUndefined() && !loader.IsNull() && loader.Type() == js.TypeFunction {
				loader.Invoke()
			}
		}

		return nil
	})
	wrapped["currentMode"] = js.FuncOf(func(_ js.Value, _ []js.Value) interface{} {
		return string(CurrentMode())
	})
	js.Global().Set("__sproutEmbedding", js.ValueOf(wrapped))
}

// serializeEmbeddingStatus returns the status as a JS object.
func serializeEmbeddingStatus(s EmbeddingStatus) map[string]interface{} {
	out := map[string]interface{}{
		"mode":           string(s.Mode),
		"onnx_available": s.ONNXAvailable,
	}
	if s.ONNXModelName != "" {
		out["onnx_model_name"] = s.ONNXModelName
	}
	if s.ONNXDimensions > 0 {
		out["onnx_dimensions"] = s.ONNXDimensions
	}
	if s.StaticProvider != "" {
		out["static_provider"] = s.StaticProvider
	}
	if s.LastError != "" {
		out["last_error"] = s.LastError
	}
	if s.InitAtUnixMS > 0 {
		out["init_at_unix_ms"] = s.InitAtUnixMS
	}
	return out
}

// EmbeddingProvider is the WASM-side wrapper that picks a concrete
// provider based on the current mode. Construction is lazy: the
// underlying provider isn't built until first use. The wrapper is
// safe for concurrent use.
type EmbeddingProvider struct {
	Mode    EmbeddingMode
	runtime *embedding.ONNXRuntime
}

// NewEmbeddingProvider returns a wrapper around the configured
// embedding backend. The Mode field is set from CurrentMode() at
// construction; later SetEmbeddingMode() calls don't change this
// wrapper's behavior — callers should request a new wrapper if they
// need mode-aware behavior.
func NewEmbeddingProvider() *EmbeddingProvider {
	return &EmbeddingProvider{Mode: CurrentMode(), runtime: &embedding.ONNXRuntime{}}
}

// Embed runs the configured backend and returns the embedding vector.
func (p *EmbeddingProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	switch p.Mode {
	case ModeOff:
		return nil, ErrEmbeddingDisabled
	case ModeONNX:
		return p.embedONNX(ctx, text)
	case ModeStatic:
		return p.embedStatic(ctx, text)
	case ModeAuto:
		// Lazy detect: if JS bridge is up, prefer it.
		if GetEmbeddingStatus().ONNXAvailable {
			return p.embedONNX(ctx, text)
		}
		return p.embedStatic(ctx, text)
	}
	return nil, fmt.Errorf("embedding: unknown mode %q", p.Mode)
}

func (p *EmbeddingProvider) embedONNX(ctx context.Context, text string) ([]float32, error) {
	prov, err := embedding.NewONNXEmbeddingProvider(ctx, p.runtime, "", "", 0, 0)
	if err != nil {
		recordError(err)
		return nil, fmt.Errorf("%w: %v", ErrEmbeddingUnavailable, err)
	}
	return prov.Embed(ctx, text)
}

func (p *EmbeddingProvider) embedStatic(ctx context.Context, text string) ([]float32, error) {
	name := embedding.StaticProviderName()
	statusMu.Lock()
	status.StaticProvider = name
	statusMu.Unlock()
	return embedding.StaticEmbed(ctx, text)
}

func recordError(err error) {
	statusMu.Lock()
	status.LastError = err.Error()
	statusMu.Unlock()
}

// init registers the JS API at package init so the WebUI can call it
// as soon as the WASM module loads.
func init() {
	NewJSEmbeddingAPI()
	statusMu.Lock()
	status.Mode = CurrentMode()
	status.InitAtUnixMS = time.Now().UnixMilli()
	statusMu.Unlock()
}
