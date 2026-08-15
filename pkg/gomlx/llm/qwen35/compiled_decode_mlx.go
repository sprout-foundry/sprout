//go:build darwin && arm64 && cgo

// Compiled decode: the full single-token decode step (embedding gather,
// 32 decoder layers, final norm, logits head) traced once as an MLX closure
// and compiled via mlx_compile, so every subsequent token
// replays the cached Metal execution plan instead of re-walking the graph
// from Go. Profiling showed the per-token graph walk (FlushPending →
// mlx_async_eval's C-side trace+schedule of ~1500 ops) dominating decode
// CPU time; this removes it.
//
// The KV cache's per-layer state becomes explicit closure I/O:
//
//	inputs  = [ids [1,1], pos [1]] + per-layer (K,V buffers | State,ConvState)
//	outputs = per-layer updated state, then logits last
//
// Full-attention layers get fixed-capacity K/V buffers (capacity rounded
// to compiledGrowStep): the new token's K/V row is written via an in-graph
// Where(Equal(arange(C), pos)) scatter, attention runs over the padded
// buffer with an in-graph additive mask (Where(LessEqual(arange(C), pos),
// 0, -inf)), and the updated whole buffers are the outputs — one
// materialization per token, no host-side write. Both idioms are verified
// bitwise against the eager path in mlx/compiled_decode_spike_test.go.
// Linear-attention layers' recurrent state is fixed-size already.
//
// The closure body uses the same fused Metal kernels as the eager path
// (fusedSwiglu, decayGate, gatedDeltaUpdate) — custom metal kernels DO
// trace and replay correctly inside mlx_compile closures (verified by
// TestSpikeMetalKernelInCompiledClosure), which keeps numerics identical
// to eager AND their speed. Position enters as a dynamic input via
// mlx_fast_rope_dynamic (spike-verified bit-identical to the static offset
// the eager path uses).
package qwen35

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// debugCompiledDecode gates verbose per-step timing logs.
func debugCompiledDecode() bool { return os.Getenv("SPROUT_LOCAL_DEBUG") == "1" }

// compiledGrowStep rounds the fixed K/V capacity up so a single capacity
// serves most generation budgets. Mirrors llm.kvGrowStep's granularity.
const compiledGrowStep = 256

// roundUpTo rounds n up to a multiple of m (m >= 1).
func roundUpTo(n, m int) int {
	if m < 1 {
		m = 1
	}
	return ((n + m - 1) / m) * m
}

// compiledDecode holds one model's compiled decode closure plus its
// per-layer KV state.
type compiledDecode struct {
	closure *mlx.Closure
	plain   *mlx.Closure // traced source; freed with the compiled closure
	// capacity is the per-full-attention-layer sequence capacity. Shapes
	// are constant for the whole generation — a change would recompile.
	capacity int
	// kBufs/vBufs are the exact-length K/V buffers (owned); they grow by
	// one row per step via the in-graph concat. Entries are nil for
	// linear-attention layers.
	kBufs, vBufs []tensor.Array
	// deltaStates/deltaConvs hold the linear-attention layers' recurrent
	// state between steps (owned); nil for full-attention layers.
	deltaStates, deltaConvs []tensor.Array

	backend tensor.Backend
	stream  tensor.Stream
	streamM *mlx.Stream

	traceCount int // diagnostics: how many times the Go body ran (should be 1)
}

// PrepareCompiledDecode sizes the fixed-capacity K/V buffers from the
// just-prefilled cache, traces the decode-step closure, and compiles it.
// Runs once per generation, before the decode loop.
func (q *Qwen35) PrepareCompiledDecode(promptLen, maxTokens int, cache *llm.KVCache) error {
	if q.cd != nil {
		q.ReleaseCompiledDecode()
	}
	if !q.backend.Available() {
		return fmt.Errorf("qwen35: compiled decode requires Metal")
	}

	cd := &compiledDecode{
		// Fixed-capacity K/V buffers (capacity rounded to compiledGrowStep):
		// shapes are constant for the whole generation, so one compilation
		// serves every step and every op traces with concrete shapes.
		capacity:    roundUpTo(promptLen+maxTokens, compiledGrowStep),
		kBufs:       make([]tensor.Array, q.cfg.NumLayers),
		vBufs:       make([]tensor.Array, q.cfg.NumLayers),
		deltaStates: make([]tensor.Array, q.cfg.NumLayers),
		deltaConvs:  make([]tensor.Array, q.cfg.NumLayers),
		backend:     q.backend,
		stream:      q.stream,
		streamM:     q.stream.(*mlx.Stream),
	}
	fail := func(err error) error {
		q.freeCompiledState(cd)
		return err
	}

	n := cache.CachedLen()
	for i := 0; i < q.cfg.NumLayers; i++ {
		layer, err := cache.Get(i)
		if err != nil {
			return fail(err)
		}
		if layer == nil {
			return fail(fmt.Errorf("qwen35: compiled decode: layer %d not initialized", i))
		}
		if layer.K != nil {
			shape := layer.K.Shape()
			padShape := []int{shape[0], shape[1], cd.capacity, shape[3]}
			kb, err := q.backend.Zeros(padShape, layer.K.Dtype(), q.stream)
			if err != nil {
				return fail(fmt.Errorf("qwen35: compiled decode: alloc k buf: %w", err))
			}
			vb, err := q.backend.Zeros(padShape, layer.V.Dtype(), q.stream)
			if err != nil {
				kb.Free()
				return fail(fmt.Errorf("qwen35: compiled decode: alloc v buf: %w", err))
			}
			if err := q.copyPrefillWindow(cd, i, kb, vb, layer.K, layer.V, n); err != nil {
				kb.Free()
				vb.Free()
				return fail(err)
			}
			cd.kBufs[i], cd.vBufs[i] = kb, vb
		} else if layer.State != nil && layer.ConvState != nil {
			// Fixed-size recurrent state: retain the prefilled values; the
			// closure rewrites them each step.
			cd.deltaStates[i] = q.backend.RetainArray(layer.State)
			cd.deltaConvs[i] = q.backend.RetainArray(layer.ConvState)
		} else {
			return fail(fmt.Errorf("qwen35: compiled decode: layer %d has neither K/V nor recurrent state", i))
		}
	}

	// Trace inputs: dummy ids/pos plus the real (already materialized)
	// state arrays. The trace only consumes shapes; values are irrelevant.
	inputs, err := q.compiledDecodePlaceholders(cd, cache)
	if err != nil {
		return fail(err)
	}
	plain, err := mlx.NewClosure(func(in []*mlx.Array) ([]*mlx.Array, error) {
		return q.compiledDecodeBody(cd, in)
	})
	if err != nil {
		for _, a := range inputs {
			a.Free()
		}
		return fail(fmt.Errorf("qwen35: compiled decode: new closure: %w", err))
	}
	// NO_SIMPLIFY+NO_FUSE: cache the scheduled execution plan (the CPU-side
	// win) but skip BOTH algebraic simplification (which can reorder
	// associativity and change bf16 rounding vs the eager op sequence) and
	// kernel fusion (which keeps fp32 intermediates where eager rounds to
	// bf16 between kernels). Either transform is enough to flip near-tie
	// argmax tokens and break parity with the eager decode path.
	if err := mlx.SetCompileMode(mlx.CompileModeNoSimplify); err != nil {
		return fail(fmt.Errorf("qwen35: compiled decode: set compile mode: %w", err))
	}
	compiled, err := plain.Compile(false) // fixed shapes by design
	if err != nil {
		plain.Free()
		for _, a := range inputs {
			a.Free()
		}
		return fail(fmt.Errorf("qwen35: compiled decode: compile: %w", err))
	}
	cd.closure = compiled
	cd.plain = plain
	q.cd = cd

	// First apply runs the trace on the placeholder inputs; its outputs are
	// discarded (values may be placeholder garbage) but this must happen
	// before the real loop so the first real token is a pure replay.
	// EXPERIMENT: use dummy state arrays for the trace so the real K/V and
	// recurrent states are never touched at trace time. inputs[2:] hold the
	// real arrays — swap them for zero clones of the same shapes.
	dummies := make([]*mlx.Array, 0, len(inputs)-2)
	for _, a := range inputs[2:] {
		z, err := mlx.Zeros(a.Shape(), a.Dtype(), cd.streamM)
		if err != nil {
			inputs[0].Free()
			inputs[1].Free()
			q.ReleaseCompiledDecode()
			return fmt.Errorf("qwen35: compiled decode: trace dummy: %w", err)
		}
		dummies = append(dummies, z)
	}
	traceInputs := append([]*mlx.Array{inputs[0], inputs[1]}, dummies...)
	outs, err := compiled.Apply(traceInputs)
	for _, z := range dummies {
		z.Free()
	}
	// inputs[0:2] are the dummy ids/pos; inputs[2:] are the real state
	// arrays (owned by cd) and must NOT be freed here.
	inputs[0].Free()
	inputs[1].Free()
	if err != nil {
		q.ReleaseCompiledDecode()
		return fmt.Errorf("qwen35: compiled decode: trace apply: %w", err)
	}
	for _, o := range outs {
		o.Free()
	}
	return nil
}

// copyPrefillWindow copies a layer's populated window into the fresh
// fixed-capacity buffers via SliceUpdate (eager, once per generation).
func (q *Qwen35) copyPrefillWindow(cd *compiledDecode, layerIdx int, kb, vb, srcK, srcV tensor.Array, n int) error {
	shape := srcK.Shape()
	if n <= 0 || n > shape[2] {
		n = shape[2]
	}
	start := []int{0, 0, 0, 0}
	stop := []int{shape[0], shape[1], n, shape[3]}
	wk, err := q.backend.SliceUpdate(kb, srcK, start, stop, q.stream)
	if err != nil {
		return fmt.Errorf("qwen35: prefill copy k: %w", err)
	}
	wv, err := q.backend.SliceUpdate(vb, srcV, start, stop, q.stream)
	if err != nil {
		wk.Free()
		return fmt.Errorf("qwen35: prefill copy v: %w", err)
	}
	if err := wk.Eval(); err != nil {
		wk.Free()
		wv.Free()
		return fmt.Errorf("qwen35: prefill copy k eval: %w", err)
	}
	if err := wv.Eval(); err != nil {
		wk.Free()
		wv.Free()
		return fmt.Errorf("qwen35: prefill copy v eval: %w", err)
	}
	wk.Free()
	wv.Free()
	return nil
}

// compiledDecodePlaceholders allocates the trace inputs: dummy ids/pos plus
// the real state arrays (already evaluated by prefill).
func (q *Qwen35) compiledDecodePlaceholders(cd *compiledDecode, cache *llm.KVCache) ([]*mlx.Array, error) {
	inputs := make([]*mlx.Array, 0, 2+2*q.cfg.NumLayers)
	ids, err := q.backend.NewArrayFromInt64([]int64{0}, []int{1, 1})
	if err != nil {
		return nil, err
	}
	inputs = append(inputs, ids.(*mlx.Array))
	pos, err := q.backend.NewArrayFromInt32([]int32{0}, []int{1})
	if err != nil {
		ids.Free()
		return nil, err
	}
	inputs = append(inputs, pos.(*mlx.Array))
	for i := 0; i < q.cfg.NumLayers; i++ {
		switch {
		case cd.kBufs[i] != nil:
			inputs = append(inputs, cd.kBufs[i].(*mlx.Array), cd.vBufs[i].(*mlx.Array))
		case cd.deltaStates[i] != nil:
			inputs = append(inputs, cd.deltaStates[i].(*mlx.Array), cd.deltaConvs[i].(*mlx.Array))
		default:
			for _, a := range inputs {
				a.Free()
			}
			return nil, fmt.Errorf("qwen35: compiled decode: layer %d has no state", i)
		}
	}
	return inputs, nil
}

// ForwardDecodeCompiled runs one compiled decode step: applies the closure
// on the current state, writes the updated state back, and returns the
// argmax next token as a [1,1] int64 array with AsyncEval already called
// (same contract as ForwardDecodeArgmaxArray). tokenArr is borrowed — the
// caller owns it.
func (q *Qwen35) ForwardDecodeCompiled(tokenArr tensor.Array, pos int) (tensor.Array, error) {
	t0 := time.Now()
	cd := q.cd
	if cd == nil || cd.closure == nil {
		return nil, fmt.Errorf("qwen35: compiled decode not prepared")
	}
	posArr, err := q.backend.NewArrayFromInt32([]int32{int32(pos)}, []int{1})
	if err != nil {
		return nil, fmt.Errorf("qwen35: compiled decode: pos array: %w", err)
	}
	defer posArr.Free()
	inputs := make([]*mlx.Array, 0, 2+2*q.cfg.NumLayers)
	inputs = append(inputs, tokenArr.(*mlx.Array), posArr.(*mlx.Array))
	for i := 0; i < q.cfg.NumLayers; i++ {
		if cd.kBufs[i] != nil {
			inputs = append(inputs, cd.kBufs[i].(*mlx.Array), cd.vBufs[i].(*mlx.Array))
		} else {
			inputs = append(inputs, cd.deltaStates[i].(*mlx.Array), cd.deltaConvs[i].(*mlx.Array))
		}
	}

	outs, err := cd.closure.Apply(inputs)
	posArr.Free()
	if err != nil {
		return nil, fmt.Errorf("qwen35: compiled decode apply: %w", err)
	}
	applyDone := time.Now()
	if debugCompiledDecode() {
		log.Printf("qwen35: compiled apply done (%d outputs) in %.3fs", len(outs), applyDone.Sub(t0).Seconds())
	}
	if len(outs) != 2*q.cfg.NumLayers+1 {
		for _, o := range outs {
			o.Free()
		}
		return nil, fmt.Errorf("qwen35: compiled decode: got %d outputs, want %d", len(outs), 2*q.cfg.NumLayers+1)
	}

	// Write the updated state back. The closure outputs the GROWN K/V
	// buffers (Where-scatter result) and the new fixed-size recurrent
	// states; swap them in, freeing the previous arrays. The lazy outputs
	// hold their own refs, so dropping our wrapper is safe.
	idx := 0
	for i := 0; i < q.cfg.NumLayers; i++ {
		if cd.kBufs[i] != nil {
			cd.kBufs[i].Free()
			cd.vBufs[i].Free()
			cd.kBufs[i] = outs[idx]
			cd.vBufs[i] = outs[idx+1]
		} else {
			cd.deltaStates[i].Free()
			cd.deltaConvs[i].Free()
			cd.deltaStates[i] = outs[idx]
			cd.deltaConvs[i] = outs[idx+1]
		}
		idx += 2
	}
	logits := outs[idx]

	// The compiled replay evaluates its outputs before returning (verified:
	// Apply is 1ms while the next blocking readback waits on real GPU work),
	// so no separate state dispatch is needed. Keeping an AsyncEvalBatch
	// here walked the whole output graph a SECOND time — measured 45ms/token
	// at 20K context, the entire long-context regression.

	// [1,1,vocab] argmax → [1,1] int64, dispatched without readback — same
	// tail as ForwardDecodeArgmaxArray.
	idxArr, err := mlx.ArgMax(logits, false, cd.streamM)
	logits.Free()
	if err != nil {
		return nil, fmt.Errorf("qwen35: compiled decode argmax: %w", err)
	}
	idx64, err := mlx.AsType(idxArr, mlx.Int64, cd.streamM)
	idxArr.Free()
	if err != nil {
		return nil, fmt.Errorf("qwen35: compiled decode argmax cast: %w", err)
	}
	next, err := mlx.Reshape(idx64, []int{1, 1}, cd.streamM)
	idx64.Free()
	if err != nil {
		return nil, fmt.Errorf("qwen35: compiled decode argmax reshape: %w", err)
	}
	if err := next.AsyncEval(); err != nil {
		next.Free()
		return nil, fmt.Errorf("qwen35: compiled decode async eval: %w", err)
	}
	if debugCompiledDecode() {
		log.Printf("qwen35: compiled step pos=%d total=%.3fs (apply=%.3f tail=%.3f)",
			pos, time.Since(t0).Seconds(), applyDone.Sub(t0).Seconds(), time.Since(applyDone).Seconds())
	}
	return next, nil
}

// ReleaseCompiledDecode frees the compiled closure and all owned state.
func (q *Qwen35) ReleaseCompiledDecode() {
	if q.cd == nil {
		return
	}
	q.freeCompiledState(q.cd)
	q.cd = nil
}

func (q *Qwen35) freeCompiledState(cd *compiledDecode) {
	if cd.closure != nil {
		cd.closure.Free()
		cd.closure = nil
	}
	if cd.plain != nil {
		cd.plain.Free()
		cd.plain = nil
	}
	for i := range cd.kBufs {
		if cd.kBufs[i] != nil {
			cd.kBufs[i].Free()
			cd.kBufs[i] = nil
		}
		if cd.vBufs[i] != nil {
			cd.vBufs[i].Free()
			cd.vBufs[i] = nil
		}
		if cd.deltaStates[i] != nil {
			cd.deltaStates[i].Free()
			cd.deltaStates[i] = nil
		}
		if cd.deltaConvs[i] != nil {
			cd.deltaConvs[i].Free()
			cd.deltaConvs[i] = nil
		}
	}
}
