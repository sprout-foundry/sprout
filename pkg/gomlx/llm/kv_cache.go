//go:build darwin && arm64 && cgo && mlx

package llm

import (
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// KVCache stores key and value tensors across decode steps so that each
// token only requires computing attention against cached keys/values rather
// than recomputing the entire sequence.
//
// The cache has one entry per layer. Each entry is [1, num_kv_heads, seq, head_dim].
// On prefill, the full sequence's K/V are stored. On each decode step, the new
// token's K/V are concatenated to the cache.
//
// Memory: at seq=2048, each layer stores 2 tensors of
// [1, 8, 2048, 128] * 4 bytes (fp32) = 16 MB. For 28 layers: ~900 MB.
// With fp16 (future): ~450 MB.
type KVCache struct {
	layers      []*KVCacheLayer
	initialized []bool
	stream      tensor.Stream
	backend     tensor.Backend
}

// KVCacheLayer holds the cached K and V for a single transformer layer.
// For hybrid linear-attention layers (Qwen3.5 DeltaNet), K/V are nil and
// State/ConvState hold the fixed-size recurrent state instead.
type KVCacheLayer struct {
	K tensor.Array // [1, num_kv_heads, cached_len, head_dim] — full attention
	V tensor.Array // [1, num_kv_heads, cached_len, head_dim] — full attention

	// DeltaNet (linear attention) state. State is [B, Hv, Dv, Dk] (the
	// recurrent key/value memory); ConvState is [B, conv_kernel-1, conv_dim]
	// (the trailing conv input rows). Both are fixed-size — they do NOT grow
	// with sequence length.
	State     tensor.Array
	ConvState tensor.Array
}

// NewKVCache creates a cache for the given number of layers.
func NewKVCache(numLayers int, s tensor.Stream, backend tensor.Backend) *KVCache {
	return &KVCache{
		layers:      make([]*KVCacheLayer, numLayers),
		initialized: make([]bool, numLayers),
		stream:      s,
		backend:     backend,
	}
}

// SetBackend sets the backend used for tensor operations in the cache.
func (c *KVCache) SetBackend(b tensor.Backend) {
	c.backend = b
}

// IsInitialized reports whether a layer's cache has been populated.
func (c *KVCache) IsInitialized(layerIdx int) bool {
	if layerIdx < 0 || layerIdx >= len(c.initialized) {
		return false
	}
	return c.initialized[layerIdx]
}

// Store writes the initial K/V tensors for a layer during prefill. The cache
// takes ownership of the arrays — do not Free them after calling Store.
func (c *KVCache) Store(layerIdx int, k, v tensor.Array) error {
	if layerIdx < 0 || layerIdx >= len(c.layers) {
		return fmt.Errorf("kv_cache: layer index %d out of range [0, %d)", layerIdx, len(c.layers))
	}
	if c.layers[layerIdx] != nil {
		l := c.layers[layerIdx]
		if l.K != nil {
			l.K.Free()
		}
		if l.V != nil {
			l.V.Free()
		}
		if l.State != nil {
			l.State.Free()
		}
		if l.ConvState != nil {
			l.ConvState.Free()
		}
	}
	c.layers[layerIdx] = &KVCacheLayer{K: k, V: v}
	c.initialized[layerIdx] = true
	return nil
}

// Append concatenates new K/V (single token) to the cache for a layer.
// newK and newV must be [1, num_kv_heads, 1, head_dim]. The cache frees
// the old arrays and stores the concatenated result.
func (c *KVCache) Append(layerIdx int, newK, newV tensor.Array) error {
	if layerIdx < 0 || layerIdx >= len(c.layers) {
		return fmt.Errorf("kv_cache: layer index %d out of range", layerIdx)
	}

	cached := c.layers[layerIdx]
	if cached == nil {
		// First entry — store directly
		c.layers[layerIdx] = &KVCacheLayer{K: newK, V: newV}
		c.initialized[layerIdx] = true
		return nil
	}
	defer newK.Free()
	defer newV.Free()

	// Concatenate along the sequence axis (axis=2)
	concatK, err := c.backend.ConcatenateAxis([]tensor.Array{cached.K, newK}, 2, c.stream)
	if err != nil {
		return fmt.Errorf("kv_cache: concat K: %w", err)
	}
	concatV, err := c.backend.ConcatenateAxis([]tensor.Array{cached.V, newV}, 2, c.stream)
	if err != nil {
		concatK.Free()
		return fmt.Errorf("kv_cache: concat V: %w", err)
	}

	cached.K.Free()
	cached.V.Free()
	cached.K = concatK
	cached.V = concatV
	return nil
}

// Get returns the cached K/V for a layer, or nil if not cached.
func (c *KVCache) Get(layerIdx int) (*KVCacheLayer, error) {
	if layerIdx < 0 || layerIdx >= len(c.layers) {
		return nil, fmt.Errorf("kv_cache: layer index %d out of range", layerIdx)
	}
	return c.layers[layerIdx], nil
}

// StoreState writes the fixed-size DeltaNet recurrent state for a layer.
// The cache takes ownership of the arrays.
func (c *KVCache) StoreState(layerIdx int, state, convState tensor.Array) error {
	if layerIdx < 0 || layerIdx >= len(c.layers) {
		return fmt.Errorf("kv_cache: layer index %d out of range", layerIdx)
	}
	if c.layers[layerIdx] != nil {
		l := c.layers[layerIdx]
		if l.State != nil {
			l.State.Free()
		}
		if l.ConvState != nil {
			l.ConvState.Free()
		}
		if l.K != nil {
			l.K.Free()
		}
		if l.V != nil {
			l.V.Free()
		}
	}
	c.layers[layerIdx] = &KVCacheLayer{State: state, ConvState: convState}
	c.initialized[layerIdx] = true
	return nil
}

// GetState returns the fixed-size DeltaNet recurrent state for a layer.
// Returns nil, nil if the layer is not initialized.
func (c *KVCache) GetState(layerIdx int) (tensor.Array, tensor.Array, error) {
	if layerIdx < 0 || layerIdx >= len(c.layers) {
		return nil, nil, fmt.Errorf("kv_cache: layer index %d out of range", layerIdx)
	}
	l := c.layers[layerIdx]
	if l == nil {
		return nil, nil, nil
	}
	return l.State, l.ConvState, nil
}

// CachedLen returns the number of cached tokens, or 0 if empty.
// For buffered layers (AppendFast path), uses Offset which tracks the
// number of valid tokens. For raw tensors (prefill-only / concat path),
// reads the sequence dimension from the shape.
func (c *KVCache) CachedLen() int {
	if len(c.layers) == 0 || c.layers[0] == nil {
		return 0
	}
	l := c.layers[0]
	if l.K == nil {
		return 0
	}
	shape := l.K.Shape()
	if len(shape) < 3 {
		return 0
	}
	return shape[2]
}

// SnapshotPrefix returns a new KVCache whose arrays are retained copies of
// this cache's current arrays (same MLX buffers, refcount bumped). The
// returned cache is independent — decoding into this cache will not disturb
// the original. The original still owns its arrays; free the snapshot when
// done. Used by prefix caching to keep a prompt-only snapshot alive while
// the working cache continues decoding.
func (c *KVCache) SnapshotPrefix() *KVCache {
	snap := &KVCache{
		layers:      make([]*KVCacheLayer, len(c.layers)),
		initialized: make([]bool, len(c.initialized)),
		stream:      c.stream,
		backend:     c.backend,
	}
	for i, l := range c.layers {
		if l == nil {
			continue
		}
		cp := &KVCacheLayer{}
		if l.K != nil {
			cp.K = c.backend.RetainArray(l.K)
		}
		if l.V != nil {
			cp.V = c.backend.RetainArray(l.V)
		}
		if l.State != nil {
			cp.State = c.backend.RetainArray(l.State)
		}
		if l.ConvState != nil {
			cp.ConvState = c.backend.RetainArray(l.ConvState)
		}
		snap.layers[i] = cp
		snap.initialized[i] = c.initialized[i]
	}
	return snap
}

// RestorePrefix copies the retained prefix arrays from snap into this cache,
// replacing whatever this cache held. The cache takes ownership (Free on
// replace). snap is not modified.
func (c *KVCache) RestorePrefix(snap *KVCache) error {
	if len(snap.layers) != len(c.layers) {
		return fmt.Errorf("kv_cache: prefix layers %d != cache layers %d", len(snap.layers), len(c.layers))
	}
	for i, l := range snap.layers {
		if l == nil {
			c.layers[i] = nil
			c.initialized[i] = false
			continue
		}
		if c.layers[i] != nil {
			cl := c.layers[i]
			if cl.K != nil {
				cl.K.Free()
			}
			if cl.V != nil {
				cl.V.Free()
			}
			if cl.State != nil {
				cl.State.Free()
			}
			if cl.ConvState != nil {
				cl.ConvState.Free()
			}
		}
		cp := &KVCacheLayer{}
		if l.K != nil {
			cp.K = c.backend.RetainArray(l.K)
		}
		if l.V != nil {
			cp.V = c.backend.RetainArray(l.V)
		}
		if l.State != nil {
			cp.State = c.backend.RetainArray(l.State)
		}
		if l.ConvState != nil {
			cp.ConvState = c.backend.RetainArray(l.ConvState)
		}
		c.layers[i] = cp
		c.initialized[i] = snap.initialized[i]
	}
	return nil
}

// Free releases all arrays held by the cache.
func (c *KVCache) Free() {
	for _, layer := range c.layers {
		if layer != nil {
			if layer.K != nil {
				layer.K.Free()
			}
			if layer.V != nil {
				layer.V.Free()
			}
			if layer.State != nil {
				layer.State.Free()
			}
			if layer.ConvState != nil {
				layer.ConvState.Free()
			}
		}
	}
}
