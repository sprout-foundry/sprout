//go:build (darwin || linux) && arm64 && cgo && ggml

package ggml

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// DeltaNet reduces the key dim of [B,Hv,Dv,Dk]; the MoE router reduces -1
// with keepdims. Both must sum the whole axis, not a single element.
func TestSumAxes(t *testing.T) {
	g := &GGMLBackend{}
	if !g.Available() {
		t.Skip("GGML backend not available")
	}
	var b tensor.Backend = g
	s, _ := b.DefaultGPUStream()

	shape := []int{2, 3, 4}
	data := iotaF32(24)

	cases := []struct {
		name      string
		axes      []int
		keepdims  bool
		wantShape []int
	}{
		{"last", []int{2}, false, []int{2, 3}},
		{"last-keepdims", []int{2}, true, []int{2, 3, 1}},
		{"negative", []int{-1}, true, []int{2, 3, 1}},
		{"middle", []int{1}, false, []int{2, 4}},
		{"first", []int{0}, false, []int{3, 4}},
		{"two-axes", []int{0, 2}, false, []int{3}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x, _ := b.NewArrayFromFloat32(data, shape)
			got, err := b.Sum(x, tc.axes, tc.keepdims, s)
			if err != nil {
				t.Fatalf("Sum: %v", err)
			}
			if gs := got.Shape(); !equalInts(gs, tc.wantShape) {
				t.Errorf("shape = %v, want %v", gs, tc.wantShape)
			}
			out, err := got.Float32Data()
			if err != nil {
				t.Fatalf("Float32Data: %v", err)
			}
			checkClose(t, out, refSum(data, shape, tc.axes, tc.keepdims), 1e-4, "Sum "+tc.name)
		})
	}
}

// refSum reduces row-major data over the given (possibly negative) axes.
func refSum(data []float32, shape, axes []int, keepdims bool) []float32 {
	reduce := make([]bool, len(shape))
	for _, ax := range axes {
		if ax < 0 {
			ax += len(shape)
		}
		reduce[ax] = true
	}
	var outShape []int
	for i, d := range shape {
		if !reduce[i] {
			outShape = append(outShape, d)
		} else if keepdims {
			outShape = append(outShape, 1)
		}
	}
	total := 1
	for _, d := range outShape {
		total *= d
	}
	outSt := strides(outShape)
	out := make([]float32, total)
	inSt := strides(shape)
	for n := range data {
		o, oi := 0, 0
		for d := range shape {
			if reduce[d] {
				if keepdims {
					oi++
				}
				continue
			}
			o += (n / inSt[d] % shape[d]) * outSt[oi]
			oi++
		}
		out[o] += data[n]
	}
	return out
}
