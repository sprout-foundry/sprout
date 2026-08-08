//go:build darwin && arm64 && cgo && ggml

package ggml

import (
	"testing"
)

func TestMatMulMetal(t *testing.T) {
	b, err := InitMetal()
	if err != nil {
		t.Skipf("Metal backend not available: %v", err)
	}
	defer b.Free()

	t.Logf("Backend: %s", b.Name())

	// 2x3 @ 3x4 = 2x4
	M, K, N := 2, 3, 4
	a := []float32{
		1, 2, 3,
		4, 5, 6,
	}
	bb := []float32{
		1, 2, 3, 4,
		5, 6, 7, 8,
		9, 10, 11, 12,
	}

	result, err := b.MatMulF32(a, bb, M, K, N)
	if err != nil {
		t.Fatalf("MatMulF32: %v", err)
	}

	// Expected: [1*1+2*5+3*9, 1*2+2*6+3*10, ...] = [38, 44, 50, 56, 83, 98, 113, 128]
	expected := []float32{38, 44, 50, 56, 83, 98, 113, 128}
	if len(result) != len(expected) {
		t.Fatalf("result length %d != expected %d", len(result), len(expected))
	}
	for i, v := range result {
		if v != expected[i] {
			t.Errorf("result[%d] = %f, expected %f", i, v, expected[i])
		}
	}
	t.Logf("Result: %v", result)
}
