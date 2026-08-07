//go:build darwin && arm64 && cgo && mlx

package qwen3

import (
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
)

// linear is a projection weight with two representations:
//
//   - Full precision: wT is the pre-transposed [in, out] weight; Forward does
//     a plain MatMul (the fast path, no dequant).
//   - Quantized: qW is the packed int32 weight [out, in*bits/32] (PyTorch
//     layout, NOT pre-transposed), qScales/qBiases are the per-group scale
//     and bias. Forward calls mlx_quantized_matmul which dequantizes on the
//     fly in the Metal kernel.
//
// Quantized weights stay in [out, in] layout because packing is bit-interleaved
// along the in axis; a transpose would corrupt the layout. mlx_quantized_matmul
// takes transpose=true to compensate.
type linear struct {
	wT *mlx.Array // [in, out], nil when quantized

	qW        *mlx.Array // packed int32 [out, in*bits/32]
	qScales   *mlx.Array // [out, in/group_size]
	qBiases   *mlx.Array // [out, in/group_size], optional
	qGroupSize int
	qBits      int
	qMode      string
}

func (l *linear) isQuantized() bool { return l.qW != nil }

// Forward computes x @ W (x is [..., in], result [..., out]).
func (l *linear) Forward(x *mlx.Array, s *mlx.Stream) (*mlx.Array, error) {
	if l.qW != nil {
		return mlx.QuantizedMatMul(x, l.qW, l.qScales, l.qBiases, true, l.qGroupSize, l.qBits, l.qMode, s)
	}
	return mlx.MatMul(x, l.wT, s)
}

func (l *linear) Free() {
	freeArr(l.wT)
	freeArr(l.qW)
	freeArr(l.qScales)
	freeArr(l.qBiases)
}

// loadLinear loads a projection weight. When quant is nil it loads and
// pre-transposes the float weight (the existing full-precision path). When
// quant is non-nil it either loads a pre-quantized triplet from the file
// (keys `{name}.weight`/`.scales`/`.biases`, as mlx-community models store)
// or quantizes the loaded float weight at load time.
func loadLinear(sf *llm.SafetensorsFile, name string, s *mlx.Stream, quant *llm.QuantConfig) (*linear, error) {
	if quant == nil {
		wT, err := loadLinearT(sf, name, s)
		if err != nil {
			return nil, err
		}
		return &linear{wT: wT}, nil
	}

	if sf.Has(name + ".scales") {
		// Pre-quantized triplet in the safetensors file.
		return loadQuantizedTriplet(sf, name, s, quant)
	}

	// Quantize a full-precision weight at load time.
	w, err := sf.Get(name, s)
	if err != nil {
		return nil, err
	}
	defer w.Free()
	return quantizeLinear(w, s, quant)
}

// loadQuantizedTriplet loads the packed weight + scales + biases tensors that
// mlx-community stores for a pre-quantized model.
func loadQuantizedTriplet(sf *llm.SafetensorsFile, name string, s *mlx.Stream, quant *llm.QuantConfig) (*linear, error) {
	w, err := sf.Get(name, s)
	if err != nil {
		return nil, err
	}
	scales, err := sf.Get(name+".scales", s)
	if err != nil {
		w.Free()
		return nil, err
	}
	var biases *mlx.Array
	if sf.Has(name + ".biases") {
		biases, err = sf.Get(name+".biases", s)
		if err != nil {
			w.Free()
			scales.Free()
			return nil, err
		}
	}
	return &linear{
		qW:        w,
		qScales:   scales,
		qBiases:   biases,
		qGroupSize: quant.GroupSize,
		qBits:      quant.Bits,
		qMode:      quant.Mode,
	}, nil
}

// quantizeLinear runs MLX quantization on a full-precision weight and
// materializes the triplet on the loading thread (lazy arrays bind to the
// thread-local stream, so evals must happen here — see loadLinearT).
func quantizeLinear(w *mlx.Array, s *mlx.Stream, quant *llm.QuantConfig) (*linear, error) {
	parts, err := mlx.Quantize(w, quant.GroupSize, quant.Bits, quant.Mode, s)
	if err != nil {
		return nil, fmt.Errorf("quantize %s: %w", "weight", err)
	}
	if len(parts) < 2 {
		for _, p := range parts {
			p.Free()
		}
		return nil, fmt.Errorf("quantize: expected [weight, scales] got %d arrays", len(parts))
	}
	for _, p := range parts {
		if err := p.Eval(); err != nil {
			for _, q := range parts {
				q.Free()
			}
			return nil, fmt.Errorf("eval quantized weight: %w", err)
		}
	}
	l := &linear{
		qW:        parts[0],
		qScales:   parts[1],
		qGroupSize: quant.GroupSize,
		qBits:      quant.Bits,
		qMode:      quant.Mode,
	}
	if len(parts) > 2 {
		l.qBiases = parts[2]
	}
	return l, nil
}
