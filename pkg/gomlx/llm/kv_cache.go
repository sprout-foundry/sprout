//go:build darwin && arm64 && cgo && mlx

package llm

import (
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
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
// CacheState tracks whether a layer has been initialized in the cache.
// During prefill, each layer stores its K/V in sequence. Using CachedLen()
// (which checks layer 0) to decide Store vs Append is wrong — layers 1+ see
// layer 0's length and incorrectly take the Append path.
type KVCache struct {
	layers    []*KVCacheLayer
	initialized []bool
	stream    *mlx.Stream
}

// KVCacheLayer holds the cached K and V for a single transformer layer.
// For hybrid linear-attention layers (Qwen3.5 DeltaNet), K/V are nil and
// State/ConvState hold the fixed-size recurrent state instead.
type KVCacheLayer struct {
	K *mlx.Array // [1, num_kv_heads, cached_len, head_dim] — full attention
	V *mlx.Array // [1, num_kv_heads, cached_len, head_dim] — full attention

	// DeltaNet (linear attention) state. State is [B, Hv, Dv, Dk] (the
	// recurrent key/value memory); ConvState is [B, conv_kernel-1, conv_dim]
	// (the trailing conv input rows). Both are fixed-size — they do NOT grow
	// with sequence length.
	State     *mlx.Array
	ConvState *mlx.Array
}

// NewKVCache creates a cache for the given number of layers.
func NewKVCache(numLayers int, s *mlx.Stream) *KVCache {
	return &KVCache{
		layers:      make([]*KVCacheLayer, numLayers),
		initialized: make([]bool, numLayers),
		stream:      s,
	}
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
func (c *KVCache) Store(layerIdx int, k, v *mlx.Array) error {
	if layerIdx < 0 || layerIdx >= len(c.layers) {
		return fmt.Errorf("kv_cache: layer index %d out of range [0, %d)", layerIdx, len(c.layers))
	}
	if c.layers[layerIdx] != nil {
		c.layers[layerIdx].K.Free()
		c.layers[layerIdx].V.Free()
		c.layers[layerIdx].State.Free()
		c.layers[layerIdx].ConvState.Free()
	}
	c.layers[layerIdx] = &KVCacheLayer{K: k, V: v}
	c.initialized[layerIdx] = true
	return nil
}

// Append concatenates new K/V (single token) to the cache for a layer.
// newK and newV must be [1, num_kv_heads, 1, head_dim]. The cache frees
// the old arrays and stores the concatenated result.
func (c *KVCache) Append(layerIdx int, newK, newV *mlx.Array) error {
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
	concatK, err := mlx.ConcatenateAxis([]*mlx.Array{cached.K, newK}, 2, c.stream)
	if err != nil {
		return fmt.Errorf("kv_cache: concat K: %w", err)
	}
	concatV, err := mlx.ConcatenateAxis([]*mlx.Array{cached.V, newV}, 2, c.stream)
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
func (c *KVCache) StoreState(layerIdx int, state, convState *mlx.Array) error {
	if layerIdx < 0 || layerIdx >= len(c.layers) {
		return fmt.Errorf("kv_cache: layer index %d out of range", layerIdx)
	}
	if c.layers[layerIdx] != nil {
		c.layers[layerIdx].State.Free()
		c.layers[layerIdx].ConvState.Free()
		c.layers[layerIdx].K.Free()
		c.layers[layerIdx].V.Free()
	}
	c.layers[layerIdx] = &KVCacheLayer{State: state, ConvState: convState}
	c.initialized[layerIdx] = true
	return nil
}

// GetState returns the fixed-size DeltaNet recurrent state for a layer.
// Returns nil, nil if the layer is not initialized.
func (c *KVCache) GetState(layerIdx int) (*mlx.Array, *mlx.Array, error) {
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
func (c *KVCache) CachedLen() int {
	if len(c.layers) == 0 || c.layers[0] == nil {
		return 0
	}
	shape := c.layers[0].K.Shape() // [1, num_kv_heads, seq, head_dim]
	if len(shape) < 3 {
		return 0
	}
	return shape[2]
}

// Free releases all MLX arrays held by the cache.
func (c *KVCache) Free() {
	for _, layer := range c.layers {
		if layer != nil {
			layer.K.Free()
			layer.V.Free()
			layer.State.Free()
			layer.ConvState.Free()
		}
	}
}
