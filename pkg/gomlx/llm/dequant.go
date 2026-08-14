//go:build cgo && ((darwin && arm64) || (linux && ggml && (arm64 || amd64)))

package llm

import (
	"encoding/binary"
	"fmt"
	"math"
	"runtime"

	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// dequantizeAffineGo dequantizes MLX affine-quantized weights entirely in Go.
// Used when the backend doesn't support native quantization (e.g. GGML).
//
// MLX affine format:
//   - weights: packed uint32, each holding 32/bits values (little-endian bit packing)
//   - scales: float32 or bfloat16 [row, hidden/groupSize]
//   - biases: float32 or bfloat16 [row, hidden/groupSize], optional
//   - dequant[i] = unpack(weight_bits[i]) * scale[group(i)] + bias[group(i)]
//
// Note the order: MLX quantizes as q = round((w - bias) / scale) with a signed
// scale, so the bias is added AFTER scaling, not before.
//
// For bits=4: each uint32 holds 8 values.
// For bits=5: each uint32 holds 6 values (5*6=30 bits, 2 padding).
// For bits=6: each uint32 holds 5 values (6*5=30 bits, 2 padding).
// For bits=8: each uint32 holds 4 values.
//
// wShape is [row, packedDim], scalesShape is [row, numGroups].
// scaleDtype/biasDtype is the tensor.Dtype of the scales/biases tensors
// (BF16 = 12, Float32 = 10). Use 0 to auto-detect from byte count.
// Returns float32 data of shape [row, hidden] where hidden = numGroups * groupSize.
func dequantizeAffineGo(wBytes []byte, wShape []int, scalesBytes []byte, biasesBytes []byte, bits, groupSize int, scaleDtype, biasDtype tensor.Dtype) ([]float32, []int, error) {
	if len(wShape) < 2 {
		return nil, nil, fmt.Errorf("dequantizeAffineGo: weight shape must be >= 2D, got %v", wShape)
	}
	if bits < 2 || bits > 8 {
		return nil, nil, fmt.Errorf("dequantizeAffineGo: unsupported bit width %d", bits)
	}
	if groupSize <= 0 {
		return nil, nil, fmt.Errorf("dequantizeAffineGo: group size must be positive, got %d", groupSize)
	}

	// Weights may arrive with leading batch dims (e.g. [1, 1, rows, cols]).
	// The row count is the second-to-last dim; the packed dim is the last.
	vocab := wShape[len(wShape)-2]
	packedDim := wShape[len(wShape)-1]

	// Determine element size for scales/biases based on dtype.
	// MLX stores these as BF16 (2 bytes) or F32 (4 bytes).
	scaleBytesPerElem := 4
	if scaleDtype == tensor.BFloat16 {
		scaleBytesPerElem = 2
	}
	biasBytesPerElem := 4
	if biasDtype == tensor.BFloat16 {
		biasBytesPerElem = 2
	}

	// Derive numGroups from the scales byte count.
	numGroups := len(scalesBytes) / (vocab * scaleBytesPerElem)
	if numGroups == 0 {
		return nil, nil, fmt.Errorf("dequantizeAffineGo: could not determine numGroups from scales size")
	}
	hidden := numGroups * groupSize

	if len(scalesBytes) == 0 {
		return nil, nil, fmt.Errorf("dequantizeAffineGo: scales data is empty")
	}
	// Validate scales size matches expectations
	expectedScalesSize := vocab * numGroups * scaleBytesPerElem
	if len(scalesBytes) < expectedScalesSize {
		return nil, nil, fmt.Errorf("dequantizeAffineGo: scales size mismatch: have %d bytes, need %d", len(scalesBytes), expectedScalesSize)
	}
	// Validate weight size
	expectedWSize := vocab * packedDim * 4
	if len(wBytes) < expectedWSize {
		return nil, nil, fmt.Errorf("dequantizeAffineGo: weight size mismatch: have %d bytes, need %d", len(wBytes), expectedWSize)
	}
	// Validate biases size if present
	hasBias := len(biasesBytes) > 0
	if hasBias {
		expectedBiasesSize := vocab * numGroups * biasBytesPerElem
		if len(biasesBytes) < expectedBiasesSize {
			return nil, nil, fmt.Errorf("dequantizeAffineGo: biases size mismatch: have %d bytes, need %d", len(biasesBytes), expectedBiasesSize)
		}
	}

	// readScaled reads a float32 from either BF16 (2 bytes) or F32 (4 bytes) data.
	readScaled := func(data []byte, offset int, dtype tensor.Dtype) float32 {
		if dtype == tensor.BFloat16 {
			// BF16 is the top 16 bits of a float32 — just read 2 bytes and shift left 16.
			bits := uint32(binary.LittleEndian.Uint16(data[offset : offset+2]))
			return math.Float32frombits(bits << 16)
		}
		return math.Float32frombits(binary.LittleEndian.Uint32(data[offset : offset+4]))
	}

	out := make([]float32, vocab*hidden)

	// Bits per uint32: how many values fit in one packed uint32
	valuesPerInt := 32 / bits
	// Bit mask for extracting each value
	mask := uint32(1)<<uint(bits) - 1

	for row := 0; row < vocab; row++ {
		// Read scales for this row
		scales := make([]float32, numGroups)
		scaleOff := row * numGroups * scaleBytesPerElem
		for g := 0; g < numGroups; g++ {
			scales[g] = readScaled(scalesBytes, scaleOff+g*scaleBytesPerElem, scaleDtype)
		}

		// Read biases for this row if present
		var biases []float32
		if hasBias {
			biases = make([]float32, numGroups)
			biasOff := row * numGroups * biasBytesPerElem
			for g := 0; g < numGroups; g++ {
				biases[g] = readScaled(biasesBytes, biasOff+g*biasBytesPerElem, biasDtype)
			}
		}

		// Unpack weights for this row
		wOff := row * packedDim * 4
		valIdx := 0
		for p := 0; p < packedDim && valIdx < hidden; p++ {
			packed := binary.LittleEndian.Uint32(wBytes[wOff+p*4 : wOff+p*4+4])
			for v := 0; v < valuesPerInt && valIdx < hidden; v++ {
				raw := int32(packed & mask)
				packed >>= uint(bits)

				group := valIdx / groupSize
				val := float32(raw) * scales[group]
				if hasBias {
					val += biases[group]
				}
				out[row*hidden+valIdx] = val
				valIdx++
			}
		}
	}

	return out, []int{vocab, hidden}, nil
}

// dequantizeToFull loads raw bytes from the quantized tensors, dequantizes
// in Go, and returns a tensor. On GGML backends, re-quantizes to Q4_0 for
// ARM-optimized matmul kernels. On other backends, returns F32.
func dequantizeToFull(b tensor.Backend, s tensor.Stream, w, scales, biases tensor.Array, bits, groupSize int) (tensor.Array, error) {
	return dequantizeToQ4_0(b, s, w, scales, biases, bits, groupSize)
}

// Q4_0Quantizer is an optional backend capability for converting F32 weights
// to GGML Q4_0 format at load time. This enables ARM-optimized quantized
// matmul kernels (8x memory reduction vs F32, NEON/i8mm acceleration).
// Backends that don't support it (MLX, stub) simply don't implement it.
type Q4_0Quantizer interface {
	NewArrayQ4_0(data []float32, shape []int) (tensor.Array, error)
}

// dequantizeToQ4_0 dequantizes MLX affine weights and re-quantizes to Q4_0
// in one step. Uses 4x less memory than F32 dequantization, enabling larger
// models on RAM-constrained machines. Falls back to F32 if the backend
// doesn't support Q4_0.
func dequantizeToQ4_0(b tensor.Backend, s tensor.Stream, w, scales, biases tensor.Array, bits, groupSize int) (tensor.Array, error) {
	// Dequantize MLX affine → F32 in Go.
	f32Data, shape, err := readQuantizedWeights(w, scales, biases, bits, groupSize)
	if err != nil {
		return nil, err
	}
	// Free quantized tensors.
	w.Free()
	scales.Free()
	if biases != nil {
		biases.Free()
	}

	// Q4_0 re-quantization: store weights in GGML's native quantized format
	// so ggml_mul_mat uses the ARM-optimized NEON/i8mm kernels (~8x less
	// memory than F32, fused dequant on the fly). Falls back to F32 if the
	// backend doesn't implement Q4_0Quantizer.
	if qz, ok := b.(Q4_0Quantizer); ok {
		arr, err := qz.NewArrayQ4_0(f32Data, shape)
		runtime.GC()
		return arr, err
	}

	// Fallback: store as F32.
	arr, err := b.NewArrayFromFloat32(f32Data, shape)
	runtime.GC()
	return arr, err
}

// readQuantizedWeights reads raw bytes from quantized tensors and dequantizes
// to F32 in Go. Shared by dequantizeToFull and dequantizeToQ4_0.
func readQuantizedWeights(w, scales, biases tensor.Array, bits, groupSize int) ([]float32, []int, error) {
	if err := w.Eval(); err != nil {
		return nil, nil, fmt.Errorf("eval weights: %w", err)
	}
	if err := scales.Eval(); err != nil {
		return nil, nil, fmt.Errorf("eval scales: %w", err)
	}
	wBytes, err := w.RawBytes()
	if err != nil {
		return nil, nil, fmt.Errorf("read weight bytes: %w", err)
	}
	scalesBytes, err := scales.RawBytes()
	if err != nil {
		return nil, nil, fmt.Errorf("read scale bytes: %w", err)
	}
	var biasesBytes []byte
	biasDtype := tensor.Float32
	if biases != nil {
		if err := biases.Eval(); err != nil {
			return nil, nil, fmt.Errorf("eval biases: %w", err)
		}
		biasesBytes, err = biases.RawBytes()
		if err != nil {
			return nil, nil, fmt.Errorf("read bias bytes: %w", err)
		}
		biasDtype = biases.Dtype()
	}
	return dequantizeAffineGo(wBytes, w.Shape(), scalesBytes, biasesBytes, bits, groupSize, scales.Dtype(), biasDtype)
}
