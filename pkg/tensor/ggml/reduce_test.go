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

// DeltaNet sums the innermost axis of [B,Hv,Dv,Dk] with keepdims=false and
// immediately subtracts the result from a [B,Hv,Dv] tensor. Checking the
// values alone is not enough: a reduction can produce the right numbers with
// an ne that is offset by one against its logical shape, which only fails
// later when the next op tries to broadcast against it.
func TestSumLastAxisIsUsableDownstream(t *testing.T) {
	g := &GGMLBackend{}
	if !g.Available() {
		t.Skip("GGML backend not available")
	}
	var b tensor.Backend = g
	s, _ := b.DefaultGPUStream()

	const B, Hv, Dv, Dk = 1, 3, 4, 5
	prod := iotaF32(B * Hv * Dv * Dk)
	x, _ := b.NewArrayFromFloat32(prod, []int{B, Hv, Dv, Dk})
	defer x.Free()

	summed, err := b.Sum(x, []int{3}, false, s)
	if err != nil {
		t.Fatalf("Sum: %v", err)
	}
	defer summed.Free()
	if got := summed.Shape(); !equalInts(got, []int{B, Hv, Dv}) {
		t.Fatalf("shape = %v, want [%d %d %d]", got, B, Hv, Dv)
	}

	v := fill(B*Hv*Dv, 7)
	y, _ := b.NewArrayFromFloat32(v, []int{B, Hv, Dv})
	defer y.Free()

	diff, err := b.Subtract(y, summed, s)
	if err != nil {
		t.Fatalf("Subtract after Sum: %v", err)
	}
	defer diff.Free()

	out, err := diff.Float32Data()
	if err != nil {
		t.Fatalf("Float32Data: %v", err)
	}
	want := make([]float32, B*Hv*Dv)
	for i := range want {
		var acc float32
		for k := 0; k < Dk; k++ {
			acc += prod[i*Dk+k]
		}
		want[i] = v[i] - acc
	}
	checkClose(t, out, want, 1e-4, "Subtract(v, Sum(prod))")
}
