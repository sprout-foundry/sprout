//go:build darwin && arm64 && cgo

package qwen35

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// DebugDumpLayers runs the prefill forward pass and writes each layer's
// hidden state to dir/layer-NN.bin as raw float32. Used by the parity tool
// to compare layer-by-layer with mlx-lm and isolate which layer kind
// (linear DeltaNet vs full attention) diverges. Debug-only.
func (q *Qwen35) DebugDumpLayers(ids tensor.Array, seqLen int, cache *llm.KVCache, s tensor.Stream, dir string) error {
	q.stream = s

	h, err := q.weights.embed.Lookup(ids, q.backend, s)
	if err != nil {
		return fmt.Errorf("embedding lookup: %w", err)
	}
	defer h.Free()
	h, err = q.backend.SqueezeAxis(h, 2, s)
	if err != nil {
		return fmt.Errorf("squeeze embedding: %w", err)
	}

	for i := 0; i < q.cfg.NumLayers; i++ {
		out, err := q.forwardLayer(h, i, seqLen, 0, cache)
		if err != nil {
			return fmt.Errorf("layer %d: %w", i, err)
		}
		h.Free()
		h = out

		// Cast to fp32 for the dump (Float32Data requires Float32 dtype).
		f32Arr, err := q.backend.AsType(h, tensor.Float32, s)
		if err != nil {
			return fmt.Errorf("layer %d cast: %w", i, err)
		}
		f32, err := f32Arr.Float32Data()
		f32Arr.Free()
		if err != nil {
			return fmt.Errorf("layer %d read: %w", i, err)
		}
		path := fmt.Sprintf("%s/layer-%02d.bin", dir, i)
		if err := writeFloat32(path, f32); err != nil {
			return err
		}
	}
	return nil
}

func writeFloat32(path string, data []float32) error {
	buf := make([]byte, 4*len(data))
	for i, v := range data {
		binary.LittleEndian.PutUint32(buf[4*i:], math.Float32bits(v))
	}
	return os.WriteFile(path, buf, 0o644)
}
