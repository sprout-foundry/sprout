//go:build cgo && ((darwin && arm64 && (mlx || ggml)) || (linux && ggml && (arm64 || amd64)))

package llm

import (
	"encoding/binary"
	"github.com/sprout-foundry/sprout/pkg/tensor"
	"math"
	"testing"
)

// quantizeAffineRef is a reference quantizer for testing dequantizeAffineGo.
// It packs values in the same unsigned bit-interleaved uint32 format as MLX.
//
// MLX affine quantization: q[i] = round(x[i] / scale[g] - bias[g])
// The packed integer is unsigned: 0 .. 2^bits-1.
// Dequantization: x[i] = (q[i] + bias[g]) * scale[g]
func quantizeAffineRef(values []float32, scales []float32, biases []float32, bits, groupSize int) ([]byte, []byte, []byte, int) {
	numValues := len(values)
	numGroups := len(scales)
	mask := uint32(1)<<uint(bits) - 1
	valuesPerInt := 32 / bits
	packedDim := (numValues + valuesPerInt - 1) / valuesPerInt

	packedInts := make([]uint32, packedDim)
	for i, v := range values {
		group := i / groupSize
		qVal := (v - biases[group]) / scales[group]
		iv := int32(math.Round(float64(qVal)))

		// Clip to unsigned range [0, 2^bits - 1]
		if iv < 0 {
			iv = 0
		}
		if iv > int32(mask) {
			iv = int32(mask)
		}

		idx := i / valuesPerInt
		pos := (i % valuesPerInt) * bits
		packedInts[idx] |= uint32(iv) << uint(pos)
	}

	wBytes := make([]byte, packedDim*4)
	for i, v := range packedInts {
		binary.LittleEndian.PutUint32(wBytes[i*4:], v)
	}

	sBytes := make([]byte, numGroups*4)
	for i, v := range scales {
		binary.LittleEndian.PutUint32(sBytes[i*4:], math.Float32bits(v))
	}

	var bBytes []byte
	if len(biases) > 0 {
		bBytes = make([]byte, numGroups*4)
		for i, v := range biases {
			binary.LittleEndian.PutUint32(bBytes[i*4:], math.Float32bits(v))
		}
	}

	return wBytes, sBytes, bBytes, packedDim
}

func TestDequantizeAffineGo4Bit(t *testing.T) {
	// 8 values, 1 group, scale=0.5, no bias
	values := []float32{1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0}
	scales := []float32{0.5}
	biases := []float32{0.0}
	bits := 4
	groupSize := 8

	wBytes, sBytes, bBytes, packedDim := quantizeAffineRef(values, scales, biases, bits, groupSize)

	result, shape, err := dequantizeAffineGo(wBytes, []int{1, packedDim}, sBytes, bBytes, bits, groupSize, tensor.Float32, tensor.Float32)
	if err != nil {
		t.Fatalf("dequantizeAffineGo failed: %v", err)
	}
	if len(shape) != 2 || shape[0] != 1 || shape[1] != 8 {
		t.Fatalf("unexpected shape: %v", shape)
	}

	for i, v := range values {
		diff := math.Abs(float64(result[i] - v))
		if diff > 1.0 {
			t.Errorf("value[%d]: got %f, want %f (diff=%f)", i, result[i], v, diff)
		}
	}
}

func TestDequantizeAffineGo5Bit(t *testing.T) {
	// 16 values, 2 groups of 8
	scales := []float32{1.0, 2.0}
	biases := []float32{0.0, 0.0}
	bits := 5
	groupSize := 8

	// Build values from unsigned quantization: q * scale + bias
	values := []float32{}
	for i := 0; i < 16; i++ {
		group := i / groupSize
		q := float32(i % 15) // 0..14 (within 5-bit range)
		values = append(values, q*scales[group]+biases[group])
	}

	wBytes, sBytes, bBytes, packedDim := quantizeAffineRef(values, scales, biases, bits, groupSize)

	result, shape, err := dequantizeAffineGo(wBytes, []int{1, packedDim}, sBytes, bBytes, bits, groupSize, tensor.Float32, tensor.Float32)
	if err != nil {
		t.Fatalf("dequantizeAffineGo failed: %v", err)
	}
	if len(shape) != 2 || shape[0] != 1 || shape[1] != 16 {
		t.Fatalf("unexpected shape: %v", shape)
	}

	for i, v := range values {
		diff := math.Abs(float64(result[i] - v))
		if diff > 1.0 {
			t.Errorf("value[%d]: got %f, want %f (diff=%f)", i, result[i], v, diff)
		}
	}
}

func TestDequantizeAffineGo6Bit(t *testing.T) {
	// 10 values, 2 groups of 5, no bias
	values := []float32{1.0, 2.0, 3.0, 4.0, 5.0, 10.0, 20.0, 30.0, 40.0, 50.0}
	scales := []float32{1.0, 10.0}
	biases := []float32{0.0, 0.0}
	bits := 6
	groupSize := 5

	wBytes, sBytes, bBytes, packedDim := quantizeAffineRef(values, scales, biases, bits, groupSize)

	result, shape, err := dequantizeAffineGo(wBytes, []int{1, packedDim}, sBytes, bBytes, bits, groupSize, tensor.Float32, tensor.Float32)
	if err != nil {
		t.Fatalf("dequantizeAffineGo failed: %v", err)
	}
	if len(shape) != 2 || shape[0] != 1 || shape[1] != 10 {
		t.Fatalf("unexpected shape: %v", shape)
	}

	for i, v := range values {
		diff := math.Abs(float64(result[i] - v))
		if diff > 1.0 {
			t.Errorf("value[%d]: got %f, want %f (diff=%f)", i, result[i], v, diff)
		}
	}
}

func TestDequantizeAffineGoWithBias(t *testing.T) {
	// 8 values, 1 group, with bias offset
	values := []float32{10.0, 12.0, 14.0, 16.0, 18.0, 20.0, 22.0, 24.0}
	scales := []float32{2.0}
	biases := []float32{5.0} // bias=5, so q values will be (val/2.0 - 5.0) = 0,1,2,3,4,5,6,7
	bits := 4
	groupSize := 8

	wBytes, sBytes, bBytes, packedDim := quantizeAffineRef(values, scales, biases, bits, groupSize)

	result, shape, err := dequantizeAffineGo(wBytes, []int{1, packedDim}, sBytes, bBytes, bits, groupSize, tensor.Float32, tensor.Float32)
	if err != nil {
		t.Fatalf("dequantizeAffineGo failed: %v", err)
	}
	if len(shape) != 2 || shape[0] != 1 || shape[1] != 8 {
		t.Fatalf("unexpected shape: %v", shape)
	}

	for i, v := range values {
		diff := math.Abs(float64(result[i] - v))
		if diff > 1.0 {
			t.Errorf("value[%d]: got %f, want %f (diff=%f)", i, result[i], v, diff)
		}
	}
}

func TestDequantizeAffineGoMultiRow(t *testing.T) {
	row1 := []float32{2.0, 4.0, 6.0, 8.0, 10.0, 12.0, 14.0, 16.0}
	row2 := []float32{20.0, 40.0, 60.0, 80.0, 100.0, 120.0, 140.0, 160.0}
	bits := 4
	groupSize := 8

	// Row 1: scale=2, bias=0 → q = 1,2,3,4,5,6,7,8
	w1, s1, _, p1 := quantizeAffineRef(row1, []float32{2.0}, []float32{0.0}, bits, groupSize)
	// Row 2: scale=20, bias=0 → q = 1,2,3,4,5,6,7,8
	w2, s2, _, _ := quantizeAffineRef(row2, []float32{20.0}, []float32{0.0}, bits, groupSize)

	allW := make([]byte, len(w1)+len(w2))
	allS := make([]byte, len(s1)+len(s2))
	copy(allW, w1)
	copy(allW[len(w1):], w2)
	copy(allS, s1)
	copy(allS[len(s1):], s2)

	result, shape, err := dequantizeAffineGo(allW, []int{2, p1}, allS, nil, bits, groupSize, tensor.Float32, tensor.Float32)
	if err != nil {
		t.Fatalf("dequantizeAffineGo failed: %v", err)
	}
	if len(shape) != 2 || shape[0] != 2 || shape[1] != 8 {
		t.Fatalf("unexpected shape: %v", shape)
	}

	for i, v := range row1 {
		diff := math.Abs(float64(result[i] - v))
		if diff > 2.0 {
			t.Errorf("row1[%d]: got %f, want %f (diff=%f)", i, result[i], v, diff)
		}
	}
	for i, v := range row2 {
		diff := math.Abs(float64(result[8+i] - v))
		if diff > 20.0 {
			t.Errorf("row2[%d]: got %f, want %f (diff=%f)", i, result[8+i], v, diff)
		}
	}
}

func TestDequantizeAffineGoErrors(t *testing.T) {
	_, _, err := dequantizeAffineGo([]byte{}, []int{1}, nil, nil, 4, 64, tensor.Float32, tensor.Float32)
	if err == nil {
		t.Error("expected error for 1D shape")
	}

	_, _, err = dequantizeAffineGo([]byte{}, []int{1, 1}, nil, nil, 4, 64, tensor.Float32, tensor.Float32)
	if err == nil {
		t.Error("expected error for empty scales")
	}

	_, _, err = dequantizeAffineGo([]byte{}, []int{1, 1}, []byte{0, 0, 0, 0}, nil, 0, 64, tensor.Float32, tensor.Float32)
	if err == nil {
		t.Error("expected error for bits=0")
	}

	_, _, err = dequantizeAffineGo([]byte{}, []int{1, 1}, []byte{0, 0, 0, 0}, nil, 4, 0, tensor.Float32, tensor.Float32)
	if err == nil {
		t.Error("expected error for groupSize=0")
	}
}
