//go:build (darwin || linux) && (arm64 || amd64) && cgo && ggml

package ggml

import (
	"math"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// Weights arrive as [out, in] and MatMul must compute x @ w^T. Q4_0 rounds to
// 16 levels per 32-element block, so the tolerance is relative to the row norm
// rather than absolute.
func TestMatMulQ4_0(t *testing.T) {
	g := &GGMLBackend{}
	if !g.Available() {
		t.Skip("GGML backend not available")
	}
	var b tensor.Backend = g
	s, _ := b.DefaultGPUStream()

	const in, out, rows = 64, 8, 3
	wData := fill(out*in, 11)
	xData := fill(rows*in, 12)

	w, err := g.NewArrayQ4_0(wData, []int{out, in})
	if err != nil {
		t.Fatalf("NewArrayQ4_0: %v", err)
	}
	x, _ := b.NewArrayFromFloat32(xData, []int{1, rows, in})

	got, err := b.MatMul(x, w, s)
	if err != nil {
		t.Fatalf("MatMul: %v", err)
	}
	if gs := got.Shape(); !equalInts(gs, []int{1, rows, out}) {
		t.Errorf("shape = %v, want [1 %d %d]", gs, rows, out)
	}
	data, err := got.Float32Data()
	if err != nil {
		t.Fatalf("Float32Data: %v", err)
	}

	var allZero = true
	for _, v := range data {
		if v != 0 {
			allZero = false
		}
		if math.IsNaN(float64(v)) {
			t.Fatalf("Q4_0 matmul produced NaN")
		}
	}
	if allZero {
		t.Fatalf("Q4_0 matmul produced all zeros")
	}

	want := make([]float32, rows*out)
	for r := 0; r < rows; r++ {
		for o := 0; o < out; o++ {
			var acc float64
			for i := 0; i < in; i++ {
				acc += float64(xData[r*in+i]) * float64(wData[o*in+i])
			}
			want[r*out+o] = float32(acc)
		}
	}
	// Q4_0 error scales with the magnitudes involved; ~5% of the typical
	// product magnitude is comfortably above quantization noise but well
	// below the error from a wrong layout.
	checkClose(t, data, want, 1.0, "Q4_0 matmul")
}

// The embedding table is stored quantized, so the gather must dequantize.
func TestGatherAxisQ4_0(t *testing.T) {
	g := &GGMLBackend{}
	if !g.Available() {
		t.Skip("GGML backend not available")
	}
	var b tensor.Backend = g
	s, _ := b.DefaultGPUStream()

	const vocab, hidden = 6, 64
	table := fill(vocab*hidden, 13)
	w, err := g.NewArrayQ4_0(table, []int{vocab, hidden})
	if err != nil {
		t.Fatalf("NewArrayQ4_0: %v", err)
	}
	ids := []int32{3, 0, 5}
	idx, _ := b.NewArrayFromInt32(ids, []int{1, len(ids)})

	got, err := b.GatherAxis(w, idx, 0, []int{1, hidden}, s)
	if err != nil {
		t.Fatalf("GatherAxis: %v", err)
	}
	data, err := got.Float32Data()
	if err != nil {
		t.Fatalf("Float32Data: %v", err)
	}
	if len(data) != len(ids)*hidden {
		t.Fatalf("got %d values, want %d", len(data), len(ids)*hidden)
	}
	for r, id := range ids {
		row := data[r*hidden : (r+1)*hidden]
		wantRow := table[int(id)*hidden : (int(id)+1)*hidden]
		checkClose(t, row, wantRow, 0.2, "gather row")
	}
}
