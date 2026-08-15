//go:build darwin && arm64 && cgo

package mlx

import (
	"fmt"
	"math"
	"testing"
	"time"
)

// TestSpikeSDPAMaskCostReordered re-measures masked vs unmasked decode-shape
// SDPA at 20K with strict warmup of BOTH paths before timing (the earlier
// bench's "unmasked=838µs > masked=227µs" inversion smelled like
// first-call compile pollution).
func TestSpikeSDPAMaskCostReordered(t *testing.T) {
	s := spikeStream(t)

	B, Hq, Hkv, D := 1, 16, 4, 256
	C := 20544

	mk := func(n int, seed int) []float32 {
		d := make([]float32, n)
		for i := range d {
			d[i] = float32(math.Sin(float64(i%1024+seed))) * 2
		}
		return d
	}

	kB, err := NewArrayFromFloat32(mk(B*Hkv*C*D, 1), []int{B, Hkv, C, D})
	if err != nil {
		t.Fatal(err)
	}
	defer kB.Free()
	kBf, err := AsType(kB, BFloat16, s)
	if err != nil {
		t.Fatal(err)
	}
	defer kBf.Free()
	vB, err := NewArrayFromFloat32(mk(B*Hkv*C*D, 2), []int{B, Hkv, C, D})
	if err != nil {
		t.Fatal(err)
	}
	defer vB.Free()
	vBf, err := AsType(vB, BFloat16, s)
	if err != nil {
		t.Fatal(err)
	}
	defer vBf.Free()
	qB, err := NewArrayFromFloat32(mk(B*Hq*D, 3), []int{B, Hq, 1, D})
	if err != nil {
		t.Fatal(err)
	}
	defer qB.Free()
	qBf, err := AsType(qB, BFloat16, s)
	if err != nil {
		t.Fatal(err)
	}
	defer qBf.Free()

	scale := float32(1.0 / math.Sqrt(float64(D)))

	rg, err := Arange(0, float64(C), 1, Int32, s)
	if err != nil {
		t.Fatal(err)
	}
	defer rg.Free()
	posArr, err := NewArrayFromInt32([]int32{int32(C - 10)}, []int{1})
	if err != nil {
		t.Fatal(err)
	}
	defer posArr.Free()
	le, err := LessEqual(rg, posArr, s)
	if err != nil {
		t.Fatal(err)
	}
	defer le.Free()
	zeroF, err := NewArrayFromFloat32([]float32{0}, []int{1})
	if err != nil {
		t.Fatal(err)
	}
	defer zeroF.Free()
	negInfF, err := NewArrayFromFloat32([]float32{float32(math.Inf(-1))}, []int{1})
	if err != nil {
		t.Fatal(err)
	}
	defer negInfF.Free()
	maskVals, err := Where(le, zeroF, negInfF, s)
	if err != nil {
		t.Fatal(err)
	}
	defer maskVals.Free()
	maskBF, err := AsType(maskVals, BFloat16, s)
	if err != nil {
		t.Fatal(err)
	}
	defer maskBF.Free()
	mask4, err := Reshape(maskBF, []int{1, 1, 1, C}, s)
	if err != nil {
		t.Fatal(err)
	}
	defer mask4.Free()

	run := func(masked bool) time.Duration {
		const iters = 30
		var acc *Array
		start := time.Now()
		for i := 0; i < iters; i++ {
			var out *Array
			var err error
			if masked {
				out, err = FastScaledDotProductAttention(qBf, kBf, vBf, scale, "array", mask4, nil, s)
			} else {
				out, err = FastScaledDotProductAttention(qBf, kBf, vBf, scale, "", nil, nil, s)
			}
			if err != nil {
				t.Fatal(err)
			}
			if acc != nil {
				acc.Free()
			}
			acc = out
		}
		if err := acc.Eval(); err != nil {
			t.Fatal(err)
		}
		if err := s.Synchronize(); err != nil {
			t.Fatal(err)
		}
		el := time.Since(start) / iters
		acc.Free()
		return el
	}

	// Warm BOTH kernel specializations before timing either.
	run(true)
	run(false)
	run(false)
	run(true)

	m1 := run(true)
	m2 := run(false)
	fmt.Printf("masked(first)=%v masked(second)=%v\n", m1, m2)
	fmt.Printf("unmasked=%v\n", m2)
	m3 := run(true)
	fmt.Printf("masked(third)=%v\n", m3)
}
