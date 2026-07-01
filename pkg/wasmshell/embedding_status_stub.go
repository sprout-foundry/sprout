//go:build !wasm

package wasmshell

import (
	"sync"
	"sync/atomic"
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
// queryable from both Go and JS.
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
	statusMu.Lock()
	status.Mode = ModeAuto
	statusMu.Unlock()
}

// GetEmbeddingStatus returns a snapshot of the current embedding status.
func GetEmbeddingStatus() EmbeddingStatus {
	statusMu.RLock()
	defer statusMu.RUnlock()
	return status
}

// SetEmbeddingMode switches the mode at runtime.
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
