//go:build darwin && arm64 && cgo && mlx

package llm

import (
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// Linear is a projection weight with two representations:
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
type Linear struct {
	wT tensor.Array // [in, out], nil when quantized

	qW         tensor.Array // packed int32 [out, in*bits/32]
	qScales    tensor.Array // [out, in/group_size]
	qBiases    tensor.Array // [out, in/group_size], optional
	qGroupSize int
	qBits      int
	qMode      string
}

// IsQuantized reports whether this projection holds packed quantized weights.
func (l *Linear) IsQuantized() bool { return l.qW != nil }

// Accessors for MoE gather_qmm (need raw quantized weight tensors).
func (l *Linear) QW() tensor.Array         { return l.qW }
func (l *Linear) QScales() tensor.Array    { return l.qScales }
func (l *Linear) QBiases() tensor.Array    { return l.qBiases }
func (l *Linear) QGroupSize() int          { return l.qGroupSize }
func (l *Linear) QBits() int               { return l.qBits }
func (l *Linear) QMode() string            { return l.qMode }
func (l *Linear) WT() tensor.Array         { return l.wT }

// NumExperts returns the number of experts from the weight's first dimension.
// For non-MoE (2D) weights, returns 0.
func (l *Linear) NumExperts() int {
	if l.qW != nil {
		shape := l.qW.Shape()
		if len(shape) == 3 {
			return shape[0]
		}
	} else if l.wT != nil {
		shape := l.wT.Shape()
		if len(shape) == 3 {
			return shape[0]
		}
	}
	return 0
}

// Forward computes x @ W (x is [..., in], result [..., out]).
// Calls through the backend interface.
func (l *Linear) Forward(x tensor.Array, b tensor.Backend, s tensor.Stream) (tensor.Array, error) {
	if l.qW != nil {
		return b.QuantizedMatMul(x, l.qW, l.qScales, l.qBiases, true, l.qGroupSize, l.qBits, l.qMode, s)
	}
	return b.MatMul(x, l.wT, s)
}

func (l *Linear) Free() {
	freeArr(l.wT)
	freeArr(l.qW)
	freeArr(l.qScales)
	freeArr(l.qBiases)
}

// NewLinearFull creates a full-precision linear from a pre-transposed weight.
func NewLinearFull(wT tensor.Array) *Linear { return &Linear{wT: wT} }

// LoadLinear loads a projection weight. When quant is nil it loads and
// pre-transposes the float weight (the existing full-precision path). When
// quant is non-nil it either loads a pre-quantized triplet from the file
// (keys `{base}.weight`/`.scales`/`.biases`, as mlx-community models store)
// or quantizes the loaded float weight at load time.
//
// `name` may be the full weight key ("...q_proj.weight") or the base
// ("...q_proj") — the pre-quantized triplet check strips a trailing
// ".weight" so `{base}.scales` resolves.
func LoadLinear(sf *SafetensorsFile, name string, b tensor.Backend, s tensor.Stream, quant *QuantConfig) (*Linear, error) {
	base := strings.TrimSuffix(name, ".weight")

	if quant == nil {
		wT, err := loadLinearT(sf, name, b, s)
		if err != nil {
			return nil, err
		}
		return &Linear{wT: wT}, nil
	}

	if sf.Has(base + ".scales") {
		// Pre-quantized triplet in the safetensors file.
		return loadQuantizedTriplet(sf, base, b, s, quant)
	}

	// Quantize a full-precision weight at load time.
	w, err := sf.Get(name, s)
	if err != nil {
		return nil, err
	}
	defer w.Free()
	return quantizeLinear(w, b, s, quant)
}

// loadLinearT loads a float weight and pre-transposes it from [out, in]
// (PyTorch layout) to [in, out] so decode-time matmuls skip the transpose.
func loadLinearT(sf *SafetensorsFile, name string, b tensor.Backend, s tensor.Stream) (tensor.Array, error) {
	w, err := sf.Get(name, s)
	if err != nil {
		return nil, err
	}
	defer w.Free()

	wT, err := b.Transpose(w, s)
	if err != nil {
		return nil, fmt.Errorf("transpose %s: %w", name, err)
	}
	if err := wT.Eval(); err != nil {
		wT.Free()
		return nil, fmt.Errorf("eval transpose %s: %w", name, err)
	}
	return wT, nil
}

// loadQuantizedTriplet loads the packed weight + scales + biases tensors that
// mlx-community stores for a pre-quantized model. `name` is the base key
// ("...q_proj"); the triplet keys are `{name}.weight`/`.scales`/`.biases`.
func loadQuantizedTriplet(sf *SafetensorsFile, name string, b tensor.Backend, s tensor.Stream, quant *QuantConfig) (*Linear, error) {
	w, err := sf.Get(name+".weight", s)
	if err != nil {
		return nil, err
	}
	scales, err := sf.Get(name+".scales", s)
	if err != nil {
		w.Free()
		return nil, err
	}
	var biases tensor.Array
	if sf.Has(name + ".biases") {
		biases, err = sf.Get(name+".biases", s)
		if err != nil {
			w.Free()
			scales.Free()
			return nil, err
		}
	}

	// Infer actual packed bits from weight/scales shape ratio. MLX models
	// may mix packing widths (e.g. 5-bit config but embedding stored as
	// 6-bit). MLX ops validate bits against the shape, so we must pass the
	// correct value, not the config's nominal bits.
	bits := inferQuantBits(w.Shape(), scales.Shape(), quant.GroupSize, quant.Bits)

	return &Linear{
		qW:         w,
		qScales:    scales,
		qBiases:    biases,
		qGroupSize: quant.GroupSize,
		qBits:      bits,
		qMode:      quant.Mode,
	}, nil
}

// inferQuantBits derives the actual packed bits from the weight and scales
// shapes. MLX stores quantized weights as uint32 with packed_dim =
// in_dim * bits / 32. When the config's nominal bits doesn't match the
// actual packing (common with 5-bit models where the embedding uses 6-bit
// packing), use the shape-derived value.
func inferQuantBits(weightShape, scalesShape []int, groupSize, configBits int) int {
	if len(weightShape) < 2 || len(scalesShape) < 2 || groupSize == 0 {
		return configBits
	}
	packedDim := weightShape[1]
	numGroups := scalesShape[1]
	inDim := numGroups * groupSize
	if inDim == 0 {
		return configBits
	}
	shapeBits := packedDim * 32 / inDim
	if shapeBits <= 0 {
		return configBits
	}
	return shapeBits
}

// quantizeLinear runs MLX quantization on a full-precision weight and
// materializes the triplet on the loading thread (lazy arrays bind to the
// thread-local stream, so evals must happen here — see loadLinearT).
func quantizeLinear(w tensor.Array, b tensor.Backend, s tensor.Stream, quant *QuantConfig) (*Linear, error) {
	parts, err := b.Quantize(w, quant.GroupSize, quant.Bits, quant.Mode, s)
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
	l := &Linear{
		qW:         parts[0],
		qScales:    parts[1],
		qGroupSize: quant.GroupSize,
		qBits:      quant.Bits,
		qMode:      quant.Mode,
	}
	if len(parts) > 2 {
		l.qBiases = parts[2]
	}
	return l, nil
}

// _ ensures mlx.Dtype is used via the alias (mlx.Dtype = tensor.Dtype).
var _ = mlx.Dtype(0)
