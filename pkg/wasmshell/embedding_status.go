//go:build wasm

package wasmshell

import (
	"sync"
	"sync/atomic"
	"syscall/js"
)

// EmbeddingMode is the high-level switch for the WASM embedding
// pipeline. Defaults to ModeAuto: detect at first embed call.
type EmbeddingMode string

const (
	ModeAuto   EmbeddingMode = "auto"   // detect ONNX bridge at first call; fall back to static if absent
	ModeOff    EmbeddingMode = "off"    // no embeddings; Embed() returns an error
	ModeONNX   EmbeddingMode = "onnx"   // require JS-side __sproutONNX bridge; error if absent
	ModeStatic EmbeddingMode = "static" // use the Go-side static provider only
)

// EmbeddingStatus is the snapshot of the embedding pipeline's state
// queryable from both Go and JS. The same struct is JSON-serialized
// to globalThis.__sproutEmbeddingStatus for the WebUI to read.
type EmbeddingStatus struct {
	Mode           EmbeddingMode `json:"mode"`
	ONNXAvailable  bool          `json:"onnx_available"`
	ONNXModelName  string        `json:"onnx_model_name,omitempty"`
	ONNXDimensions int           `json:"onnx_dimensions,omitempty"`
	StaticProvider string        `json:"static_provider,omitempty"`
	LastError      string        `json:"last_error,omitempty"`
	InitAtUnixMS   int64         `json:"init_at_unix_ms,omitempty"`
}

var (
	statusMu  sync.RWMutex
	status    EmbeddingStatus
	modeStore atomic.Value // EmbeddingMode
)

func init() {
	modeStore.Store(ModeAuto)
	// Lazy detection: scan for __sproutONNX once at init. Cheap and
	// doesn't trigger a download.
	jsProvider := js.Global().Get("__sproutONNX")
	statusMu.Lock()
	if !jsProvider.IsUndefined() && !jsProvider.IsNull() {
		status.ONNXAvailable = true
		if nv := jsProvider.Get("modelName"); nv.Type() == js.TypeString {
			status.ONNXModelName = nv.String()
		}
		if dv := jsProvider.Get("dimensions"); dv.Type() == js.TypeNumber {
			status.ONNXDimensions = dv.Int()
		}
	}
	statusMu.Unlock()
}

// GetEmbeddingStatus returns a snapshot of the current embedding status.
func GetEmbeddingStatus() EmbeddingStatus {
	statusMu.RLock()
	defer statusMu.RUnlock()
	return status
}

// SetEmbeddingMode switches the mode at runtime. The change takes
// effect on the next Embed() call — in-flight calls complete under
// the previous mode.
func SetEmbeddingMode(m EmbeddingMode) {
	modeStore.Store(m)
	statusMu.Lock()
	status.Mode = m
	statusMu.Unlock()
}

// CurrentMode returns the active mode.
func CurrentMode() EmbeddingMode {
	if v := modeStore.Load(); v != nil {
		return v.(EmbeddingMode)
	}
	return ModeAuto
}
