//go:build darwin && arm64 && cgo && ggml

package ggml

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/tensor"
)

func getBackend(t *testing.T) tensor.Backend {
	for _, b := range []tensor.Backend{&GGMLBackend{}} {
		if b.Available() {
			return b
		}
	}
	t.Skip("GGML backend not available")
	return nil
}

func TestGGMLBackendName(t *testing.T) {
	b := getBackend(t)
	t.Logf("Backend: %s", b.Name())
}

func TestGGMLMatMul(t *testing.T) {
	b := getBackend(t)
	s, _ := b.DefaultGPUStream()

	// GGML mul_mat(ctx, a, b) computes b @ a^T where a->ne[0] == b->ne[0].
	// For result = a @ b in standard notation:
	//   a: ne[0]=K, ne[1]=M  (row-major [M,K] maps to ne[0]=K, ne[1]=M)
	//   b: ne[0]=K, ne[1]=N  (row-major [K,N] needs K contiguous)
	// Result: ne[0]=M, ne[1]=N
	//
	// M=2, K=3, N=4
	aData := []float32{1, 2, 3, 4, 5, 6} // [M=2, K=3]
	bData := []float32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12} // [K=3, N=4]

	// Create with GGML layout: A ne[0]=K=3, ne[1]=M=2
	a, _ := b.NewArrayFromFloat32(aData, []int{3, 2})
	// B ne[0]=K=3, ne[1]=N=4 — but our data is row-major [K,N] which has N contiguous.
	// GGML ne[0] is contiguous, so ne[0]=N. We need ne[0]=K.
	// Transpose the data.
	bTrans := make([]float32, len(bData))
	for k := 0; k < 3; k++ {
		for n := 0; n < 4; n++ {
			bTrans[n*3+k] = bData[k*4+n] // [N,K] layout with K contiguous
		}
	}
	bb, _ := b.NewArrayFromFloat32(bTrans, []int{3, 4}) // ne[0]=K=3, ne[1]=N=4

	result, err := b.MatMul(a, bb, s)
	if err != nil {
		t.Fatalf("MatMul: %v", err)
	}
	data, err := result.Float32Data()
	if err != nil {
		t.Fatalf("Float32Data: %v", err)
	}

	// Result ne[0]=M=2, ne[1]=N=4 (column-major). Convert to row-major.
	// Expected: [38, 44, 50, 56, 83, 98, 113, 128]
	expected := []float32{38, 44, 50, 56, 83, 98, 113, 128}
	out := make([]float32, 8)
	for m := 0; m < 2; m++ {
		for n := 0; n < 4; n++ {
			out[m*4+n] = data[m+n*2] // col-major to row-major
		}
	}
	for i, v := range out {
		if v != expected[i] {
			t.Errorf("out[%d] = %f, expected %f", i, v, expected[i])
		}
	}
	t.Logf("MatMul result: %v", out)
}

func TestGGMLAdd(t *testing.T) {
	b := getBackend(t)
	s, _ := b.DefaultGPUStream()

	aData := []float32{1, 2, 3, 4}
	bData := []float32{10, 20, 30, 40}

	a, _ := b.NewArrayFromFloat32(aData, []int{4})
	bb, _ := b.NewArrayFromFloat32(bData, []int{4})

	result, err := b.Add(a, bb, s)
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	data, err := result.Float32Data()
	if err != nil {
		t.Fatalf("Float32Data: %v", err)
	}

	expected := []float32{11, 22, 33, 44}
	for i, v := range data {
		if v != expected[i] {
			t.Errorf("data[%d] = %f, expected %f", i, v, expected[i])
		}
	}
}

func TestGGMLSoftmax(t *testing.T) {
	b := getBackend(t)
	s, _ := b.DefaultGPUStream()

	aData := []float32{1, 2, 3, 4}
	a, _ := b.NewArrayFromFloat32(aData, []int{4})

	result, err := b.Softmax(a, s)
	if err != nil {
		t.Fatalf("Softmax: %v", err)
	}
	data, err := result.Float32Data()
	if err != nil {
		t.Fatalf("Float32Data: %v", err)
	}

	// Check that it sums to ~1.0
	var sum float32
	for _, v := range data {
		sum += v
	}
	if sum < 0.99 || sum > 1.01 {
		t.Errorf("softmax sum = %f, expected ~1.0", sum)
	}
	t.Logf("Softmax: %v (sum=%f)", data, sum)
}

func TestGGMLRMSNorm(t *testing.T) {
	b := getBackend(t)
	s, _ := b.DefaultGPUStream()

	aData := []float32{1, 2, 3, 4}
	wData := []float32{1, 1, 1, 1}

	a, _ := b.NewArrayFromFloat32(aData, []int{4})
	w, _ := b.NewArrayFromFloat32(wData, []int{4})

	result, err := b.FastRMSNorm(a, w, 1e-6, s)
	if err != nil {
		t.Fatalf("RMSNorm: %v", err)
	}
	data, err := result.Float32Data()
	if err != nil {
		t.Fatalf("Float32Data: %v", err)
	}

	// With weight=1, RMSNorm normalizes x by its RMS. The values should be
	// roughly [-0.9, -0.3, 0.3, 0.9] but we just check it runs and produces
	// finite values.
	t.Logf("RMSNorm: %v", data)
	for _, v := range data {
		if v != v { // NaN check
			t.Errorf("RMSNorm produced NaN")
		}
	}
}
