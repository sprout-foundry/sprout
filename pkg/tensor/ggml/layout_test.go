//go:build (darwin || linux) && (arm64 || amd64) && cgo && ggml

package ggml

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// iota-filled data makes a misplaced element obvious: every value encodes its
// own source offset.
func iotaF32(n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = float32(i)
	}
	return out
}

func strides(shape []int) []int {
	st := make([]int, len(shape))
	acc := 1
	for i := len(shape) - 1; i >= 0; i-- {
		st[i] = acc
		acc *= shape[i]
	}
	return st
}

// refTranspose permutes row-major data so out[i0,i1,...] = in[...axes...],
// matching the MLX convention out.shape[i] = in.shape[axes[i]].
func refTranspose(in []float32, shape, axes []int) ([]float32, []int) {
	outShape := make([]int, len(axes))
	for i, ax := range axes {
		outShape[i] = shape[ax]
	}
	inSt, outSt := strides(shape), strides(outShape)
	out := make([]float32, len(in))
	idx := make([]int, len(axes))
	for flat := range out {
		rem := flat
		for i := range idx {
			idx[i] = rem / outSt[i]
			rem %= outSt[i]
		}
		src := 0
		for i, ax := range axes {
			src += idx[i] * inSt[ax]
		}
		out[flat] = in[src]
	}
	return out, outShape
}

func TestTransposeAxes4D(t *testing.T) {
	b := getBackend(t)
	s, _ := b.DefaultGPUStream()

	shape := []int{1, 3, 5, 4}
	data := iotaF32(1 * 3 * 5 * 4)

	// [0,2,1,3] is the attention transpose; [0,2,3,1] is non-involutive and
	// catches an inverted source/destination mapping.
	for _, axes := range [][]int{{0, 2, 1, 3}, {0, 2, 3, 1}, {0, 3, 1, 2}} {
		x, _ := b.NewArrayFromFloat32(data, shape)
		got, err := b.TransposeAxes(x, axes, s)
		if err != nil {
			t.Fatalf("axes %v: %v", axes, err)
		}
		want, wantShape := refTranspose(data, shape, axes)
		if gs := got.Shape(); !equalInts(gs, wantShape) {
			t.Errorf("axes %v: shape = %v, want %v", axes, gs, wantShape)
		}
		out, err := got.Float32Data()
		if err != nil {
			t.Fatalf("axes %v: Float32Data: %v", axes, err)
		}
		checkClose(t, out, want, 0, "transpose "+itoa(axes))
	}
}

// ExpandKVHeads repeats each KV head to cover its query-head group, so the
// repeats must be adjacent along axis 1 (head-major), not interleaved.
func TestRepeatAxisHeads(t *testing.T) {
	b := getBackend(t)
	s, _ := b.DefaultGPUStream()

	shape := []int{1, 2, 3, 4} // [B, KVheads, S, D]
	data := iotaF32(1 * 2 * 3 * 4)
	x, _ := b.NewArrayFromFloat32(data, shape)

	got, err := b.RepeatAxis(x, 2, 1, s)
	if err != nil {
		t.Fatalf("RepeatAxis: %v", err)
	}
	if gs := got.Shape(); !equalInts(gs, []int{1, 4, 3, 4}) {
		t.Errorf("shape = %v, want [1 4 3 4]", gs)
	}
	out, err := got.Float32Data()
	if err != nil {
		t.Fatalf("Float32Data: %v", err)
	}
	// head h of the output reads KV head h/2.
	want := make([]float32, 1*4*3*4)
	for h := 0; h < 4; h++ {
		copy(want[h*12:(h+1)*12], data[(h/2)*12:(h/2+1)*12])
	}
	checkClose(t, out, want, 0, "RepeatAxis")
}

// The KV cache concatenates along the sequence axis of [B,H,S,D].
func TestConcatenateSeqAxis(t *testing.T) {
	b := getBackend(t)
	s, _ := b.DefaultGPUStream()

	aShape := []int{1, 2, 3, 4}
	bShape := []int{1, 2, 1, 4}
	aData := iotaF32(24)
	bData := iotaF32(8)
	for i := range bData {
		bData[i] += 100
	}
	x, _ := b.NewArrayFromFloat32(aData, aShape)
	y, _ := b.NewArrayFromFloat32(bData, bShape)

	got, err := b.ConcatenateAxis([]tensor.Array{x, y}, 2, s)
	if err != nil {
		t.Fatalf("ConcatenateAxis: %v", err)
	}
	if gs := got.Shape(); !equalInts(gs, []int{1, 2, 4, 4}) {
		t.Errorf("shape = %v, want [1 2 4 4]", gs)
	}
	out, err := got.Float32Data()
	if err != nil {
		t.Fatalf("Float32Data: %v", err)
	}
	var want []float32
	for h := 0; h < 2; h++ {
		want = append(want, aData[h*12:(h+1)*12]...)
		want = append(want, bData[h*4:(h+1)*4]...)
	}
	checkClose(t, out, want, 0, "ConcatenateAxis")
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func itoa(v []int) string {
	s := ""
	for _, x := range v {
		s += string(rune('0' + x))
	}
	return s
}

// A caller passing a float weight in the quantized [out, in] layout used to
// trip GGML_ASSERT and abort the process. It must surface as an error.
func TestMatMulShapeMismatchIsAnError(t *testing.T) {
	g := &GGMLBackend{}
	if !g.Available() {
		t.Skip("GGML backend not available")
	}
	var b tensor.Backend = g
	s, _ := b.DefaultGPUStream()

	const in, out = 64, 128
	// Float weights must be pre-transposed to [in, out]; pass [out, in].
	w, _ := b.NewArrayFromFloat32(fill(out*in, 1), []int{out, in})
	defer w.Free()
	x, _ := b.NewArrayFromFloat32(fill(in, 2), []int{1, 1, in})
	defer x.Free()

	if _, err := b.MatMul(x, w, s); err == nil {
		t.Fatal("MatMul with a wrongly-laid-out float weight returned nil error")
	}
}
