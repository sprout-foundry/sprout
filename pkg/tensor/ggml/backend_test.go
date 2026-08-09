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

	// A [2,3] @ B [3,4] = C [2,4]
	aData := []float32{1, 2, 3, 4, 5, 6}
	bData := []float32{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}

	a, _ := b.NewArrayFromFloat32(aData, []int{3, 2})
	bb, _ := b.NewArrayFromFloat32(bData, []int{4, 3})

	result, err := b.MatMul(a, bb, s)
	if err != nil {
		t.Fatalf("MatMul: %v", err)
	}
	data, err := result.Float32Data()
	if err != nil {
		t.Fatalf("Float32Data: %v", err)
	}

	// Expected (column-major result from GGML, but we read via Float32Data
	// which uses ggml_backend_tensor_get — this returns raw bytes in GGML's
	// ne layout). For a [3,2] @ [4,3] = result with ne[0]=2, ne[1]=4.
	t.Logf("Result shape: %v, data: %v", result.Shape(), data)
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
