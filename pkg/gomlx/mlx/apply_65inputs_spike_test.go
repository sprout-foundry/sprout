//go:build darwin && arm64 && cgo

package mlx

import (
	"fmt"
	"testing"
	"time"
)

// TestSpikeApply65Inputs mirrors the compiled-decode replay's real shape:
// 65 closure inputs (2 scalars + 16 K/V buffer pairs + 24 state pairs),
// one trivial op each, measuring per-apply dispatch cost. If apply scales
// with input count/size, that overhead rides every decode token.
func TestSpikeApply65Inputs(t *testing.T) {
	s := spikeStream(t)

	Hkv, C, D := 4, 20544, 256
	bufShape := []int{1, Hkv, C, D}

	mk := func(n int) []float32 {
		d := make([]float32, n)
		for i := range d {
			d[i] = float32(i%13) - 6
		}
		return d
	}

	big := Hkv * C * D
	stHv, stDv, stDk := 8, 128, 128
	stShape := []int{1, stHv, stDv, stDk}

	// Build the trace-time inputs: 2 scalars + 16 buffer pairs + 24 state pairs.
	var inputs []*Array
	ids, err := NewArrayFromInt64([]int64{0}, []int{1, 1})
	if err != nil {
		t.Fatal(err)
	}
	defer ids.Free()
	inputs = append(inputs, ids)
	pos, err := NewArrayFromInt32([]int32{0}, []int{1})
	if err != nil {
		t.Fatal(err)
	}
	defer pos.Free()
	inputs = append(inputs, pos)

	for i := 0; i < 16; i++ {
		for j := 0; j < 2; j++ {
			f, err := NewArrayFromFloat32(mk(big), bufShape)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Free()
			b, err := AsType(f, BFloat16, s)
			if err != nil {
				t.Fatal(err)
			}
			defer b.Free()
			inputs = append(inputs, b)
		}
	}
	for i := 0; i < 24; i++ {
		for j := 0; j < 2; j++ {
			f, err := NewArrayFromFloat32(mk(stHv*stDv*stDk), stShape)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Free()
			inputs = append(inputs, f)
		}
	}

	zero, err := NewArrayFromFloat32([]float32{0}, []int{1})
	if err != nil {
		t.Fatal(err)
	}
	defer zero.Free()

	fn := func(in []*Array) ([]*Array, error) {
		// One tiny op per input, outputs same shapes (like the real body's
		// state pass-through), plus one "logits".
		outs := make([]*Array, 0, len(in)-1)
		for _, a := range in[2:] {
			o, err := Add(a, a, s)
			if err != nil {
				return nil, err
			}
			outs = append(outs, o)
		}
		return outs, nil
	}

	plain, err := NewClosure(fn)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := plain.Compile(false)
	if err != nil {
		t.Fatal(err)
	}
	defer compiled.Free()
	defer plain.Free()

	// Trace + warmup.
	outs, err := compiled.Apply(inputs)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range outs {
		o.Free()
	}

	const iters = 10
	var last []*Array
	start := time.Now()
	for i := 0; i < iters; i++ {
		outs, err := compiled.Apply(inputs)
		if err != nil {
			t.Fatal(err)
		}
		if last != nil {
			for _, o := range last {
				o.Free()
			}
		}
		last = outs
	}
	applyEl := time.Since(start) / iters
	for _, o := range last {
		if err := o.Eval(); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Synchronize(); err != nil {
		t.Fatal(err)
	}
	totalEl := time.Since(start) / iters
	for _, o := range last {
		o.Free()
	}
	fmt.Printf("apply(65 inputs): dispatch=%v dispatch+eval=%v\n", applyEl, totalEl)
}
