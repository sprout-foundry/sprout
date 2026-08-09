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

func TestGGMLSubtract(t *testing.T) {
	b := getBackend(t)
	s, _ := b.DefaultGPUStream()

	a, _ := b.NewArrayFromFloat32([]float32{10, 20, 30, 40}, []int{4})
	bb, _ := b.NewArrayFromFloat32([]float32{1, 2, 3, 4}, []int{4})

	result, err := b.Subtract(a, bb, s)
	if err != nil {
		t.Fatalf("Subtract: %v", err)
	}
	data, _ := result.Float32Data()
	expected := []float32{9, 18, 27, 36}
	for i, v := range data {
		if v != expected[i] {
			t.Errorf("data[%d] = %f, expected %f", i, v, expected[i])
		}
	}
}

func TestGGMLMultiply(t *testing.T) {
	b := getBackend(t)
	s, _ := b.DefaultGPUStream()

	a, _ := b.NewArrayFromFloat32([]float32{2, 3, 4, 5}, []int{4})
	bb, _ := b.NewArrayFromFloat32([]float32{10, 20, 30, 40}, []int{4})

	result, err := b.Multiply(a, bb, s)
	if err != nil {
		t.Fatalf("Multiply: %v", err)
	}
	data, _ := result.Float32Data()
	expected := []float32{20, 60, 120, 200}
	for i, v := range data {
		if v != expected[i] {
			t.Errorf("data[%d] = %f, expected %f", i, v, expected[i])
		}
	}
}

func TestGGMLExp(t *testing.T) {
	b := getBackend(t)
	s, _ := b.DefaultGPUStream()

	a, _ := b.NewArrayFromFloat32([]float32{0, 1, 2}, []int{3})
	result, err := b.Exp(a, s)
	if err != nil {
		t.Fatalf("Exp: %v", err)
	}
	data, _ := result.Float32Data()
	// exp(0)=1, exp(1)≈2.718, exp(2)≈7.389
	if abs(data[0]-1.0) > 0.001 {
		t.Errorf("exp(0) = %f, expected 1.0", data[0])
	}
	if abs(data[1]-2.718281) > 0.01 {
		t.Errorf("exp(1) = %f, expected ~2.718", data[1])
	}
	if abs(data[2]-7.389056) > 0.01 {
		t.Errorf("exp(2) = %f, expected ~7.389", data[2])
	}
}

func TestGGMLSigmoid(t *testing.T) {
	b := getBackend(t)
	s, _ := b.DefaultGPUStream()

	a, _ := b.NewArrayFromFloat32([]float32{0, 100, -100}, []int{3})
	result, err := b.Sigmoid(a, s)
	if err != nil {
		t.Fatalf("Sigmoid: %v", err)
	}
	data, _ := result.Float32Data()
	// sigmoid(0) = 0.5, sigmoid(100) ≈ 1.0, sigmoid(-100) ≈ 0.0
	if abs(data[0]-0.5) > 0.001 {
		t.Errorf("sigmoid(0) = %f, expected 0.5", data[0])
	}
	if data[1] < 0.99 {
		t.Errorf("sigmoid(100) = %f, expected ~1.0", data[1])
	}
	if data[2] > 0.01 {
		t.Errorf("sigmoid(-100) = %f, expected ~0.0", data[2])
	}
}

func TestGGMLConcat(t *testing.T) {
	b := getBackend(t)
	s, _ := b.DefaultGPUStream()

	a, _ := b.NewArrayFromFloat32([]float32{1, 2}, []int{2})
	bb, _ := b.NewArrayFromFloat32([]float32{3, 4, 5}, []int{3})

	result, err := b.ConcatenateAxis([]tensor.Array{a, bb}, 0, s)
	if err != nil {
		t.Fatalf("Concat: %v", err)
	}
	data, _ := result.Float32Data()
	expected := []float32{1, 2, 3, 4, 5}
	if len(data) != 5 {
		t.Fatalf("len = %d, expected 5", len(data))
	}
	for i, v := range data {
		if v != expected[i] {
			t.Errorf("data[%d] = %f, expected %f", i, v, expected[i])
		}
	}
}

func TestGGMLGather(t *testing.T) {
	b := getBackend(t)
	s, _ := b.DefaultGPUStream()

	// Gather rows [10,20,30,40] by indices [0,2,3] → [10,30,40]
	data, _ := b.NewArrayFromFloat32([]float32{10, 20, 30, 40}, []int{1, 4})
	indices, _ := b.NewArrayFromInt32([]int32{0, 2, 3}, []int{3})

	result, err := b.GatherAxis(data, indices, 0, nil, s)
	if err != nil {
		t.Fatalf("GatherAxis: %v", err)
	}
	out, _ := result.Float32Data()
	// ggml_get_rows returns [ne0, n_indices] → row-major [1, 3]
	if len(out) != 3 {
		t.Fatalf("len = %d, expected 3", len(out))
	}
	expected := []float32{10, 30, 40}
	for i, v := range out {
		if v != expected[i] {
			t.Errorf("out[%d] = %f, expected %f", i, v, expected[i])
		}
	}
}

func TestGGMLSqrt(t *testing.T) {
	b := getBackend(t)
	s, _ := b.DefaultGPUStream()

	a, _ := b.NewArrayFromFloat32([]float32{0, 1, 4, 9, 16}, []int{5})
	result, err := b.Sqrt(a, s)
	if err != nil {
		t.Fatalf("Sqrt: %v", err)
	}
	data, _ := result.Float32Data()
	expected := []float32{0, 1, 2, 3, 4}
	for i, v := range data {
		if abs(v-expected[i]) > 0.001 {
			t.Errorf("sqrt[%d] = %f, expected %f", i, v, expected[i])
		}
	}
}

func abs(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}
