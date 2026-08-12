//go:build darwin && arm64 && cgo && mlx

// Package qwen3 implements the Qwen3 transformer architecture for the gomlx
// LLM engine. This file contains the MLX-specific compiled-graph SwiGLU path,
// gated behind the mlx build tag so it only compiles when MLX is available.
package qwen3

import (
	"sync"
	"unsafe"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// closureEntry holds the compiled SwiGLU closures for a single Qwen3 instance.
type closureEntry struct {
	closures []*mlx.Closure
	stream   tensor.Stream
}

var (
	closureMu    sync.Mutex
	closureCache = make(map[uintptr]closureEntry)
)

func closureKey(q *Qwen3) uintptr {
	return uintptr(unsafe.Pointer(q))
}

// swigluClosure returns the compiled MLP closure for a layer, compiling it
// lazily on the inference thread when the per-call stream changes. Returns
// nil if compilation is unavailable or fails — the eager path is always the
// fallback.
func (q *Qwen3) swigluClosure(layerIdx int) any {
	closureMu.Lock()
	entry, ok := closureCache[closureKey(q)]
	closureMu.Unlock()

	// Stream changed — discard stale closures for this instance.
	if ok && entry.stream != q.stream {
		closureMu.Lock()
		for _, c := range closureCache[closureKey(q)].closures {
			if c != nil {
				c.Free()
			}
		}
		delete(closureCache, closureKey(q))
		closureMu.Unlock()
		entry = closureEntry{}
		ok = false
	}

	if ok && entry.closures != nil && layerIdx < len(entry.closures) && entry.closures[layerIdx] != nil {
		return entry.closures[layerIdx]
	}

	// Compile the closure for this layer.
	s := q.stream
	lw := &q.weights.layers[layerIdx]
	fn := func(inputs []*mlx.Array) ([]*mlx.Array, error) {
		h := inputs[0]
		gate, err := lw.gateProj.Forward(h, q.backend, s)
		if err != nil {
			return nil, err
		}
		defer gate.Free()
		up, err := lw.upProj.Forward(h, q.backend, s)
		if err != nil {
			return nil, err
		}
		defer up.Free()
		gateSilu, err := llm.SiLU(gate, q.backend, s)
		if err != nil {
			return nil, err
		}
		defer gateSilu.Free()
		gated, err := q.backend.Multiply(gateSilu, up, s)
		if err != nil {
			return nil, err
		}
		defer gated.Free()
		out, err := lw.downProj.Forward(gated, q.backend, s)
		if err != nil {
			return nil, err
		}
		return []*mlx.Array{out.(*mlx.Array)}, nil
	}

	plain, err := mlx.NewClosure(fn)
	if err != nil {
		return nil
	}
	compiled, err := plain.Compile(false)
	if err != nil {
		plain.Free()
		return nil
	}
	// plain must stay registered: the first apply of compiled runs the
	// original body once on placeholder inputs to trace the graph. The
	// compiled closure owns a template ref and frees it when released.

	// Store in the per-instance cache.
	closureMu.Lock()
	e := closureCache[closureKey(q)]
	if e.stream != s || e.closures == nil {
		e = closureEntry{stream: s, closures: make([]*mlx.Closure, q.cfg.NumLayers)}
	}
	e.closures[layerIdx] = compiled
	closureCache[closureKey(q)] = e
	closureMu.Unlock()

	return compiled
}

// applySwigluClosure runs a compiled SwiGLU closure on input h.
func (q *Qwen3) applySwigluClosure(c any, h tensor.Array) (tensor.Array, error) {
	closure := c.(*mlx.Closure)
	results, err := closure.Apply([]*mlx.Array{h.(*mlx.Array)})
	if err != nil {
		return nil, err
	}
	return results[0], nil
}

// freeSwigluClosures releases all compiled closures for a Qwen3 instance.
// Called from FreeWeights to prevent leaked MLX closures.
func freeSwigluClosures(q *Qwen3) {
	closureMu.Lock()
	defer closureMu.Unlock()
	key := closureKey(q)
	entry, ok := closureCache[key]
	if !ok {
		return
	}
	for _, c := range entry.closures {
		if c != nil {
			c.Free()
		}
	}
	delete(closureCache, key)
}
