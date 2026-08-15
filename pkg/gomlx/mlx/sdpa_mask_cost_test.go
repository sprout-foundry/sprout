//go:build darwin && arm64 && cgo

package mlx

import (
	"fmt"
	"math"
	"runtime"
	"testing"
	"time"
)

// TestSpikeSDPADecodeMaskCost measures decode-shaped SDPA (q_len=1) over a
// 20K-key buffer, unmasked vs array-masked, plus the Where-scatter update
// cost — to attribute the compiled-decode slowdown.
func TestSpikeSDPADecodeMaskCost(t *testing.T) {
	s := spikeStream(t)

	B, Hq, Hkv, D, C := 1, 16, 4, 256, 20544

	mkBF16 := func(n int) []float32 {
		d := make([]float32, n)
		for i := range d {
			d[i] = float32(i%13) - 6
		}
		return d
	}
	kData := mkBF16(B * Hkv * C * D)
	vData := mkBF16(B * Hkv * C * D)
	qData := mkBF16(B * Hq * D)

	kPad, err := NewArrayFromFloat32(kData, []int{B, Hkv, C, D})
	if err != nil {
		t.Fatal(err)
	}
	defer kPad.Free()
	kPadB, err := AsType(kPad, BFloat16, s)
	if err != nil {
		t.Fatal(err)
	}
	defer kPadB.Free()
	vPad, err := NewArrayFromFloat32(vData, []int{B, Hkv, C, D})
	if err != nil {
		t.Fatal(err)
	}
	defer vPad.Free()
	vPadB, err := AsType(vPad, BFloat16, s)
	if err != nil {
		t.Fatal(err)
	}
	defer vPadB.Free()
	q, err := NewArrayFromFloat32(qData, []int{B, Hq, 1, D})
	if err != nil {
		t.Fatal(err)
	}
	defer q.Free()
	qB, err := AsType(q, BFloat16, s)
	if err != nil {
		t.Fatal(err)
	}
	defer qB.Free()

	scale := float32(1.0 / math.Sqrt(float64(D)))

	// Additive mask [1,1,1,C].
	maskData := make([]float32, C)
	for i := range maskData {
		maskData[i] = float32(math.Inf(-1))
	}
	mask, err := NewArrayFromFloat32(maskData, []int{1, 1, 1, C})
	if err != nil {
		t.Fatal(err)
	}
	defer mask.Free()
	maskB, err := AsType(mask, BFloat16, s)
	if err != nil {
		t.Fatal(err)
	}
	defer maskB.Free()

	var out *Array
	bench := func(name string, f func() error) {
		// warmup
		if err := f(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := out.Eval(); err != nil {
			t.Fatal(err)
		}
		if err := s.Synchronize(); err != nil {
			t.Fatal(err)
		}
		const iters = 20
		start := time.Now()
		for i := 0; i < iters; i++ {
			if err := f(); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if err := out.Eval(); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.Synchronize(); err != nil {
			t.Fatal(err)
		}
		el := time.Since(start) / iters
		t.Logf("%s: %v/op", name, el)
	}

	bench("sdpa-unmasked", func() error {
		o, err := FastScaledDotProductAttention(qB, kPadB, vPadB, scale, "", nil, nil, s)
		if err != nil {
			return err
		}
		out = o
		return nil
	})
	out.Free()

	bench("sdpa-arraymask", func() error {
		o, err := FastScaledDotProductAttention(qB, kPadB, vPadB, scale, "array", maskB, nil, s)
		if err != nil {
			return err
		}
		out = o
		return nil
	})
	out.Free()

	// Where-scatter: eq [1,1,C,1] bf16 broadcast over [1,Hkv,C,D].
	rg, err := Arange(0, float64(C), 1, Int32, s)
	if err != nil {
		t.Fatal(err)
	}
	defer rg.Free()
	posA := spikeI32(t, 20000)
	eq, err := Equal(rg, posA, s)
	if err != nil {
		t.Fatal(err)
	}
	defer eq.Free()
	eq4, err := Reshape(eq, []int{1, 1, C, 1}, s)
	if err != nil {
		t.Fatal(err)
	}
	defer eq4.Free()
	kRow, err := NewArrayFromFloat32(mkBF16(B*Hkv*D), []int{B, Hkv, 1, D})
	if err != nil {
		t.Fatal(err)
	}
	defer kRow.Free()
	kRowB, err := AsType(kRow, BFloat16, s)
	if err != nil {
		t.Fatal(err)
	}
	defer kRowB.Free()
	bench("where-scatter", func() error {
		o, err := Where(eq4, kRowB, kPadB, s)
		if err != nil {
			return err
		}
		out = o
		return nil
	})
	out.Free()

	// Concatenate [buf, row] along the seq axis (the compiled graph's
	// current design).
	bench("concat-row", func() error {
		o, err := ConcatenateAxis([]*Array{kPadB, kRowB}, 2, s)
		if err != nil {
			return err
		}
		out = o
		return nil
	})
	out.Free()

	// SliceUpdate (the eager path's AppendWindow cost).
	start := []int{0, 0, 20000, 0}
	stop := []int{1, 4, 20001, 256}
	bench("slice-update", func() error {
		o, err := SliceUpdate(kPadB, kRowB, start, stop, s)
		if err != nil {
			return err
		}
		out = o
		return nil
	})
	out.Free()
	fmt.Println("done")
	_ = runtime.NumCPU
}
