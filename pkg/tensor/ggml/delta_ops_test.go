//go:build (darwin || linux) && arm64 && cgo && ggml

package ggml

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// refSlice extracts a row-major sub-box.
func refSlice(data []float32, shape, start, stop []int) ([]float32, []int) {
	outShape := make([]int, len(shape))
	copy(outShape, shape)
	for i := range start {
		outShape[i] = stop[i] - start[i]
	}
	inSt, outSt := strides(shape), strides(outShape)
	total := 1
	for _, d := range outShape {
		total *= d
	}
	out := make([]float32, total)
	idx := make([]int, len(outShape))
	for n := 0; n < total; n++ {
		rem := n
		for i := range idx {
			idx[i] = rem / outSt[i]
			rem %= outSt[i]
		}
		src := 0
		for i := range idx {
			off := idx[i]
			if i < len(start) {
				off += start[i]
			}
			src += off * inSt[i]
		}
		out[n] = data[src]
		_ = inSt
	}
	return out, outShape
}

// DeltaNet slices one timestep out of [B,S,H,D] and [B,S,H] every step, and
// the conv tail out of [B,S+k,C].
func TestSliceSteps(t *testing.T) {
	g := &GGMLBackend{}
	if !g.Available() {
		t.Skip("GGML backend not available")
	}
	var b tensor.Backend = g
	s, _ := b.DefaultGPUStream()

	cases := []struct {
		name        string
		shape       []int
		start, stop []int
	}{
		{"step-4d", []int{1, 5, 3, 4}, []int{0, 2, 0, 0}, []int{1, 3, 3, 4}},
		{"step-4d-last", []int{1, 5, 3, 4}, []int{0, 4, 0, 0}, []int{1, 5, 3, 4}},
		{"head-3d", []int{1, 5, 3}, []int{0, 2, 0}, []int{1, 3, 3}},
		{"conv-tail", []int{1, 8, 6}, []int{0, 5, 0}, []int{1, 8, 6}},
		{"inner-3d", []int{2, 4, 6}, []int{0, 1, 2}, []int{2, 3, 5}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			total := 1
			for _, d := range tc.shape {
				total *= d
			}
			data := iotaF32(total)
			x, _ := b.NewArrayFromFloat32(data, tc.shape)
			strideOnes := make([]int, len(tc.start))
			for i := range strideOnes {
				strideOnes[i] = 1
			}
			got, err := b.Slice(x, tc.start, tc.stop, strideOnes, s)
			if err != nil {
				t.Fatalf("Slice: %v", err)
			}
			want, wantShape := refSlice(data, tc.shape, tc.start, tc.stop)
			if gs := got.Shape(); !equalInts(gs, wantShape) {
				t.Errorf("shape = %v, want %v", gs, wantShape)
			}
			out, err := got.Float32Data()
			if err != nil {
				t.Fatalf("Float32Data: %v", err)
			}
			checkClose(t, out, want, 0, "Slice "+tc.name)
		})
	}
}

// The DeltaNet outer product broadcasts [B,Hv,Dv,1] up to [B,Hv,Dv,Dk].
func TestRepeatTo(t *testing.T) {
	g := &GGMLBackend{}
	if !g.Available() {
		t.Skip("GGML backend not available")
	}
	var b tensor.Backend = g
	s, _ := b.DefaultGPUStream()

	rt, ok := b.(interface {
		RepeatTo(tensor.Array, []int, tensor.Stream) (tensor.Array, error)
	})
	if !ok {
		t.Skip("backend has no RepeatTo")
	}

	shape := []int{1, 2, 3, 1}
	data := iotaF32(6)
	x, _ := b.NewArrayFromFloat32(data, shape)
	got, err := rt.RepeatTo(x, []int{1, 2, 3, 4}, s)
	if err != nil {
		t.Fatalf("RepeatTo: %v", err)
	}
	if gs := got.Shape(); !equalInts(gs, []int{1, 2, 3, 4}) {
		t.Errorf("shape = %v, want [1 2 3 4]", gs)
	}
	out, err := got.Float32Data()
	if err != nil {
		t.Fatalf("Float32Data: %v", err)
	}
	want := make([]float32, 24)
	for i := 0; i < 6; i++ {
		for k := 0; k < 4; k++ {
			want[i*4+k] = data[i]
		}
	}
	checkClose(t, out, want, 0, "RepeatTo")
}

// Depthwise causal conv over [B,S,C] with weight [C,K,1], groups=C.
func TestConv1DDepthwise(t *testing.T) {
	g := &GGMLBackend{}
	if !g.Available() {
		t.Skip("GGML backend not available")
	}
	var b tensor.Backend = g
	s, _ := b.DefaultGPUStream()

	const B, S, C, K = 1, 7, 5, 4
	inData := fill(B*S*C, 21)
	wData := fill(C*K, 22)

	x, _ := b.NewArrayFromFloat32(inData, []int{B, S, C})
	w, _ := b.NewArrayFromFloat32(wData, []int{C, K, 1})

	got, err := b.Conv1D(x, w, 1, 0, 1, C, s)
	if err != nil {
		t.Fatalf("Conv1D: %v", err)
	}
	sout := S - K + 1
	if gs := got.Shape(); !equalInts(gs, []int{B, sout, C}) {
		t.Errorf("shape = %v, want [%d %d %d]", gs, B, sout, C)
	}
	out, err := got.Float32Data()
	if err != nil {
		t.Fatalf("Float32Data: %v", err)
	}

	want := make([]float32, B*sout*C)
	for so := 0; so < sout; so++ {
		for c := 0; c < C; c++ {
			var acc float32
			for k := 0; k < K; k++ {
				acc += inData[(so+k)*C+c] * wData[c*K+k]
			}
			want[so*C+c] = acc
		}
	}
	checkClose(t, out, want, 1e-5, "Conv1D depthwise")
}

// DeltaNet splits the conv output into q|k|v along the channel axis. Only the
// first chunk starts at offset 0, so a walk that ignores the chunk origin
// corrupts k and v while leaving q correct.
func TestSplitAxisChannels(t *testing.T) {
	g := &GGMLBackend{}
	if !g.Available() {
		t.Skip("GGML backend not available")
	}
	var b tensor.Backend = g
	s, _ := b.DefaultGPUStream()

	const B, S, C = 1, 4, 12
	shape := []int{B, S, C}
	data := iotaF32(B * S * C)
	x, _ := b.NewArrayFromFloat32(data, shape)

	parts, err := b.SplitAxis(x, []int{4, 8}, 2, s)
	if err != nil {
		t.Fatalf("SplitAxis: %v", err)
	}
	bounds := [][2]int{{0, 4}, {4, 8}, {8, 12}}
	if len(parts) != len(bounds) {
		t.Fatalf("got %d parts, want %d", len(parts), len(bounds))
	}
	for i, part := range parts {
		lo, hi := bounds[i][0], bounds[i][1]
		if gs := part.Shape(); !equalInts(gs, []int{B, S, hi - lo}) {
			t.Errorf("part %d shape = %v, want [%d %d %d]", i, gs, B, S, hi-lo)
		}
		out, err := part.Float32Data()
		if err != nil {
			t.Fatalf("part %d: Float32Data: %v", i, err)
		}
		var want []float32
		for si := 0; si < S; si++ {
			want = append(want, data[si*C+lo:si*C+hi]...)
		}
		checkClose(t, out, want, 0, "SplitAxis part")
	}
}

// The conv window is built by concatenating the cached tail with the new
// tokens along the sequence axis of [B,S,C].
func TestConcatenateAxis1_3D(t *testing.T) {
	g := &GGMLBackend{}
	if !g.Available() {
		t.Skip("GGML backend not available")
	}
	var b tensor.Backend = g
	s, _ := b.DefaultGPUStream()

	aData := iotaF32(3 * 5)
	bData := iotaF32(4 * 5)
	for i := range bData {
		bData[i] += 100
	}
	x, _ := b.NewArrayFromFloat32(aData, []int{1, 3, 5})
	y, _ := b.NewArrayFromFloat32(bData, []int{1, 4, 5})

	got, err := b.ConcatenateAxis([]tensor.Array{x, y}, 1, s)
	if err != nil {
		t.Fatalf("ConcatenateAxis: %v", err)
	}
	if gs := got.Shape(); !equalInts(gs, []int{1, 7, 5}) {
		t.Errorf("shape = %v, want [1 7 5]", gs)
	}
	out, err := got.Float32Data()
	if err != nil {
		t.Fatalf("Float32Data: %v", err)
	}
	checkClose(t, out, append(append([]float32{}, aData...), bData...), 0, "concat axis1")
}
