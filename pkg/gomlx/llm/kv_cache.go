//go:build cgo && ((darwin && arm64) || (linux && ggml && (arm64 || amd64)))

package llm

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"os"

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

	// capacity/used implement geometric growth for AppendWindow. K and V are
	// allocated with room for `capacity` positions, of which the first `used`
	// hold real data. capacity == 0 means the layer is still in exact-length
	// mode (K/V sized exactly to the sequence), which is how prefill leaves it.
	capacity int
	used     int
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
	// Materialize before storing: a later chunk's attention reads these back
	// as inputs, so an unevaluated k/v here becomes a dependency every
	// subsequent chunk's graph carries until something forces evaluation —
	// compounding across chunks the same way an un-evaluated layer chain did
	// (see the eval call in qwen35's prefillInternalChunk).
	if err := k.Eval(); err != nil {
		return fmt.Errorf("kv_cache: eval stored K: %w", err)
	}
	if err := v.Eval(); err != nil {
		return fmt.Errorf("kv_cache: eval stored V: %w", err)
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
	// Same reasoning as Store/AppendWindow: a later chunk's DeltaNet forward
	// reads this state back as its recurrence's starting point, so leaving
	// it fully lazy carries this chunk's whole computation forward
	// uncomputed. AsyncEval (not Eval) dispatches it without blocking: this
	// is called once per DeltaNet layer on every single decode step (24 of
	// 32 layers), and a blocking Eval here forces a synchronous CPU-GPU
	// round trip per layer per token — confirmed via CPU profiling to be
	// the dominant decode-time cost (~29% of all samples, ~80% of the
	// decode loop itself), serializing 32 layers' worth of Metal dispatch
	// into 32 blocking round trips instead of one continuous async stream
	// ending in a single sync at the argmax readback. AsyncEval still
	// prevents unbounded graph growth (the whole point of evaluating here
	// at all) without paying for the wait.
	// Callers (e.g. LFM2's shortConv) may pass a nil state or convState.
	if state != nil {
		if err := state.AsyncEval(); err != nil {
			return fmt.Errorf("kv_cache: eval stored state: %w", err)
		}
	}
	if convState != nil {
		if err := convState.AsyncEval(); err != nil {
			return fmt.Errorf("kv_cache: eval stored conv state: %w", err)
		}
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
// For layers grown by AppendWindow, K/V are over-allocated, so the valid
// length is `used` rather than the sequence dimension of the buffer.
func (c *KVCache) CachedLen() int {
	if len(c.layers) == 0 || c.layers[0] == nil {
		return 0
	}
	l := c.layers[0]
	if l.K == nil {
		return 0
	}
	if l.capacity > 0 {
		return l.used
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
		// Carry the window bookkeeping: the retained buffers may be
		// over-allocated, so used/capacity are what distinguish real tokens
		// from padding.
		cp.capacity = l.capacity
		cp.used = l.used
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
		cp.capacity = l.capacity
		cp.used = l.used
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

// kvGrowStep is how many positions AppendWindow adds when the buffer fills.
// Growth is the only O(seq) copy; per-token appends write a single position.
const kvGrowStep = 256

// AppendWindow appends one or more positions of K/V and returns the populated
// prefix. Unlike Append, which concatenates (and therefore copies the whole
// cache every call), this writes into a geometrically grown buffer, so
// per-token decode cost is independent of sequence length.
//
// newK/newV are [1, num_kv_heads, n, head_dim] and are consumed. n > 1 occurs
// when prefill is chunked. The returned arrays are caller-owned.
func (c *KVCache) AppendWindow(layerIdx int, newK, newV tensor.Array) (tensor.Array, tensor.Array, error) {
	if layerIdx < 0 || layerIdx >= len(c.layers) {
		return nil, nil, fmt.Errorf("kv_cache: layer index %d out of range", layerIdx)
	}
	l := c.layers[layerIdx]
	if l == nil || l.K == nil {
		c.layers[layerIdx] = &KVCacheLayer{K: newK, V: newV}
		c.initialized[layerIdx] = true
		shape := newK.Shape()
		c.layers[layerIdx].used = shape[2]
		c.layers[layerIdx].capacity = shape[2]
		return newK, newV, nil
	}
	defer newK.Free()
	defer newV.Free()

	// Adopt the prefill-shaped arrays as the initial buffer.
	if l.capacity == 0 {
		l.used = l.K.Shape()[2]
		l.capacity = l.used
	}

	n := newK.Shape()[2]
	if l.used+n > l.capacity {
		if err := c.growLayer(l, newK.Shape(), newK.Dtype(), l.used+n-l.capacity); err != nil {
			return nil, nil, err
		}
	}

	shape := l.K.Shape()
	start := []int{0, 0, l.used, 0}
	stop := []int{shape[0], shape[1], l.used + n, shape[3]}

	updK, err := c.backend.SliceUpdate(l.K, newK, start, stop, c.stream)
	if err != nil {
		return nil, nil, fmt.Errorf("kv_cache: slice update K: %w", err)
	}
	updV, err := c.backend.SliceUpdate(l.V, newV, start, stop, c.stream)
	if err != nil {
		updK.Free()
		return nil, nil, fmt.Errorf("kv_cache: slice update V: %w", err)
	}
	// Dispatch the updated buffer now rather than leaving it fully lazy —
	// see the eval call in Store. A subsequent chunk's SliceUpdate reads
	// this buffer back as its base, so an undispatched buffer here is a
	// dependency every later chunk's graph carries forward uncomputed.
	// AsyncEval (not Eval): this runs once per full-attention layer on
	// every decode step, and a blocking Eval here — like StoreState's,
	// see the comment there — forces a synchronous round trip per layer
	// per token instead of letting all layers dispatch as one continuous
	// async stream.
	if err := updK.AsyncEval(); err != nil {
		updK.Free()
		updV.Free()
		return nil, nil, fmt.Errorf("kv_cache: eval updated K: %w", err)
	}
	if err := updV.AsyncEval(); err != nil {
		updK.Free()
		updV.Free()
		return nil, nil, fmt.Errorf("kv_cache: eval updated V: %w", err)
	}
	l.K.Free()
	l.V.Free()
	l.K = updK
	l.V = updV
	l.used += n

	return c.window(l)
}

// window returns the populated prefix of a layer's buffers. The results are
// always caller-owned (retained when the buffer happens to be exactly full),
// so callers can unconditionally Free them.
func (c *KVCache) window(l *KVCacheLayer) (tensor.Array, tensor.Array, error) {
	if l.used == l.capacity {
		return c.backend.RetainArray(l.K), c.backend.RetainArray(l.V), nil
	}
	shape := l.K.Shape()
	start := []int{0, 0, 0, 0}
	stop := []int{shape[0], shape[1], l.used, shape[3]}
	strides := []int{1, 1, 1, 1}
	kv, err := c.backend.Slice(l.K, start, stop, strides, c.stream)
	if err != nil {
		return nil, nil, fmt.Errorf("kv_cache: window K: %w", err)
	}
	vv, err := c.backend.Slice(l.V, start, stop, strides, c.stream)
	if err != nil {
		kv.Free()
		return nil, nil, fmt.Errorf("kv_cache: window V: %w", err)
	}
	return kv, vv, nil
}

// growLayer extends a layer's K/V buffers by at least `need` positions,
// rounded up to kvGrowStep so that growth is amortized across many tokens.
func (c *KVCache) growLayer(l *KVCacheLayer, tokShape []int, dt tensor.Dtype, need int) error {
	grow := kvGrowStep
	if need > grow {
		grow = ((need + kvGrowStep - 1) / kvGrowStep) * kvGrowStep
	}
	padShape := []int{tokShape[0], tokShape[1], grow, tokShape[3]}
	padK, err := c.backend.Zeros(padShape, dt, c.stream)
	if err != nil {
		return fmt.Errorf("kv_cache: grow alloc: %w", err)
	}
	defer padK.Free()

	grownK, err := c.backend.ConcatenateAxis([]tensor.Array{l.K, padK}, 2, c.stream)
	if err != nil {
		return fmt.Errorf("kv_cache: grow K: %w", err)
	}
	grownV, err := c.backend.ConcatenateAxis([]tensor.Array{l.V, padK}, 2, c.stream)
	if err != nil {
		grownK.Free()
		return fmt.Errorf("kv_cache: grow V: %w", err)
	}
	l.K.Free()
	l.V.Free()
	l.K = grownK
	l.V = grownV
	l.capacity += grow
	return nil
}

// kvSpillMagic tags spilled-cache files so a mismatched/corrupted file is
// detected and rejected instead of misread.
var kvSpillMagic = [4]byte{'S', 'P', 'K', 'V'}

const kvSpillVersion = uint32(1)

// SaveToDisk serializes the cache's raw arrays to path (created or
// truncated). Used to spill an inactive conversation's KV cache out of GPU
// memory without losing it — see Model's prefix-slot spill/restore. Does
// not modify or free c's arrays.
func (c *KVCache) SaveToDisk(path string) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("kv_cache: create %s: %w", path, err)
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()

	w := bufio.NewWriter(f)
	if _, err := w.Write(kvSpillMagic[:]); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, kvSpillVersion); err != nil {
		return err
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(len(c.layers))); err != nil {
		return err
	}
	for i, l := range c.layers {
		init := i < len(c.initialized) && c.initialized[i] && l != nil
		if err := binary.Write(w, binary.LittleEndian, init); err != nil {
			return err
		}
		if !init {
			continue
		}
		hasKV := l.K != nil && l.V != nil
		hasState := l.State != nil && l.ConvState != nil
		if err := binary.Write(w, binary.LittleEndian, hasKV); err != nil {
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, hasState); err != nil {
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, int32(l.capacity)); err != nil {
			return err
		}
		if err := binary.Write(w, binary.LittleEndian, int32(l.used)); err != nil {
			return err
		}
		if hasKV {
			if err := writeSpillArray(w, l.K); err != nil {
				return fmt.Errorf("kv_cache: write layer %d K: %w", i, err)
			}
			if err := writeSpillArray(w, l.V); err != nil {
				return fmt.Errorf("kv_cache: write layer %d V: %w", i, err)
			}
		}
		if hasState {
			if err := writeSpillArray(w, l.State); err != nil {
				return fmt.Errorf("kv_cache: write layer %d State: %w", i, err)
			}
			if err := writeSpillArray(w, l.ConvState); err != nil {
				return fmt.Errorf("kv_cache: write layer %d ConvState: %w", i, err)
			}
		}
	}
	return w.Flush()
}

func writeSpillArray(w io.Writer, a tensor.Array) error {
	shape := a.Shape()
	if err := binary.Write(w, binary.LittleEndian, uint32(len(shape))); err != nil {
		return err
	}
	for _, d := range shape {
		if err := binary.Write(w, binary.LittleEndian, int32(d)); err != nil {
			return err
		}
	}
	if err := binary.Write(w, binary.LittleEndian, int32(a.Dtype())); err != nil {
		return err
	}
	data, err := a.RawBytes()
	if err != nil {
		return fmt.Errorf("raw bytes: %w", err)
	}
	if err := binary.Write(w, binary.LittleEndian, uint64(len(data))); err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// LoadKVCacheFromDisk reconstructs a cache previously written by SaveToDisk.
// The returned cache's arrays are freshly allocated on s/backend — safe to
// use from any thread that owns a valid stream registration (see
// Model.runOnGPUThread).
func LoadKVCacheFromDisk(path string, s tensor.Stream, backend tensor.Backend) (_ *KVCache, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("kv_cache: open %s: %w", path, err)
	}
	defer f.Close()

	r := bufio.NewReader(f)
	var magic [4]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil {
		return nil, fmt.Errorf("kv_cache: read magic: %w", err)
	}
	if magic != kvSpillMagic {
		return nil, fmt.Errorf("kv_cache: %s is not a spilled cache file", path)
	}
	var version, numLayers uint32
	if err := binary.Read(r, binary.LittleEndian, &version); err != nil {
		return nil, err
	}
	if version != kvSpillVersion {
		return nil, fmt.Errorf("kv_cache: unsupported spill version %d", version)
	}
	if err := binary.Read(r, binary.LittleEndian, &numLayers); err != nil {
		return nil, err
	}

	c := NewKVCache(int(numLayers), s, backend)
	// On any error partway through, free whatever arrays were already
	// reconstructed so a bad file can't leak GPU memory.
	defer func() {
		if err != nil {
			c.Free()
		}
	}()

	for i := range int(numLayers) {
		var init bool
		if err := binary.Read(r, binary.LittleEndian, &init); err != nil {
			return nil, err
		}
		if !init {
			continue
		}
		var hasKV, hasState bool
		var capacity, used int32
		if err := binary.Read(r, binary.LittleEndian, &hasKV); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.LittleEndian, &hasState); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.LittleEndian, &capacity); err != nil {
			return nil, err
		}
		if err := binary.Read(r, binary.LittleEndian, &used); err != nil {
			return nil, err
		}
		l := &KVCacheLayer{capacity: int(capacity), used: int(used)}
		if hasKV {
			if l.K, err = readSpillArray(r, backend); err != nil {
				return nil, fmt.Errorf("kv_cache: read layer %d K: %w", i, err)
			}
			if l.V, err = readSpillArray(r, backend); err != nil {
				return nil, fmt.Errorf("kv_cache: read layer %d V: %w", i, err)
			}
		}
		if hasState {
			if l.State, err = readSpillArray(r, backend); err != nil {
				return nil, fmt.Errorf("kv_cache: read layer %d State: %w", i, err)
			}
			if l.ConvState, err = readSpillArray(r, backend); err != nil {
				return nil, fmt.Errorf("kv_cache: read layer %d ConvState: %w", i, err)
			}
		}
		c.layers[i] = l
		c.initialized[i] = true
	}
	return c, nil
}

func readSpillArray(r io.Reader, backend tensor.Backend) (tensor.Array, error) {
	var ndim uint32
	if err := binary.Read(r, binary.LittleEndian, &ndim); err != nil {
		return nil, err
	}
	shape := make([]int, ndim)
	for i := range shape {
		var d int32
		if err := binary.Read(r, binary.LittleEndian, &d); err != nil {
			return nil, err
		}
		shape[i] = int(d)
	}
	var dtype int32
	if err := binary.Read(r, binary.LittleEndian, &dtype); err != nil {
		return nil, err
	}
	var byteLen uint64
	if err := binary.Read(r, binary.LittleEndian, &byteLen); err != nil {
		return nil, err
	}
	data := make([]byte, byteLen)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}
	return backend.NewArrayFromBytes(data, shape, tensor.Dtype(dtype))
}
