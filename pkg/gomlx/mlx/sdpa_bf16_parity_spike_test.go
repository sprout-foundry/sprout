//go:build darwin && arm64 && cgo

package mlx

import (
	"fmt"
	"math"
	"runtime"
	"testing"
)

// TestSpikeSDPABF16MaskedVsExact checks bitwise equality at REAL decode
// shapes in bf16: SDPA over a zero-padded buffer with an additive mask vs
// exact-length unmasked SDPA, across lengths from 1 to near-capacity.
func TestSpikeSDPABF16MaskedVsExact(t *testing.T) {
	if !Available() {
		t.Skip("Metal not available")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	s, err := NewGPUStream()
	if err != nil {
		t.Skipf("no stream: %v", err)
	}
	defer s.Free()

	const B, Hq, Hkv, D = 1, 16, 4, 256
	const cap = 256

	mk := func(n int, seed float32) []float32 {
		d := make([]float32, n)
		for i := range d {
			d[i] = float32(math.Sin(float64(i+int(seed))*0.101)) * 3
		}
		return d
	}

	kPadF, err := NewArrayFromFloat32(mk(B*Hkv*cap*D, 1), []int{B, Hkv, cap, D})
	if err != nil {
		t.Fatal(err)
	}
	defer kPadF.Free()
	kPad, err := AsType(kPadF, BFloat16, s)
	if err != nil {
		t.Fatal(err)
	}
	defer kPad.Free()
	vPadF, err := NewArrayFromFloat32(mk(B*Hkv*cap*D, 2), []int{B, Hkv, cap, D})
	if err != nil {
		t.Fatal(err)
	}
	defer vPadF.Free()
	vPad, err := AsType(vPadF, BFloat16, s)
	if err != nil {
		t.Fatal(err)
	}
	defer vPad.Free()
	qF, err := NewArrayFromFloat32(mk(B*Hq*D, 3), []int{B, Hq, 1, D})
	if err != nil {
		t.Fatal(err)
	}
	defer qF.Free()
	qB, err := AsType(qF, BFloat16, s)
	if err != nil {
		t.Fatal(err)
	}
	defer qB.Free()

	scale := float32(1.0 / math.Sqrt(float64(D)))

	rg, err := Arange(0, cap, 1, Int32, s)
	if err != nil {
		t.Fatal(err)
	}
	defer rg.Free()
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

	read := func(a *Array) []float32 {
		f, err := AsType(a, Float32, s)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Free()
		d, err := f.Float32Data()
		if err != nil {
			t.Fatal(err)
		}
		return d
	}

	for _, exact := range []int{1, 2, 3, 5, 13, 100, 250} {
		sl := []int{1, 1, 1, 1}
		kRef, err := Slice(kPad, []int{0, 0, 0, 0}, []int{B, Hkv, exact, D}, sl, s)
		if err != nil {
			t.Fatal(err)
		}
		vRef, err := Slice(vPad, []int{0, 0, 0, 0}, []int{B, Hkv, exact, D}, sl, s)
		if err != nil {
			kRef.Free()
			t.Fatal(err)
		}
		ref, err := FastScaledDotProductAttention(qB, kRef, vRef, scale, "", nil, nil, s)
		kRef.Free()
		vRef.Free()
		if err != nil {
			t.Fatal(err)
		}

		posArr, err := NewArrayFromInt32([]int32{int32(exact - 1)}, []int{1})
		if err != nil {
			t.Fatal(err)
		}
		le, err := LessEqual(rg, posArr, s)
		posArr.Free()
		if err != nil {
			ref.Free()
			t.Fatal(err)
		}
		maskVals, err := Where(le, zeroF, negInfF, s)
		le.Free()
		if err != nil {
			ref.Free()
			t.Fatal(err)
		}
		maskBF, err := AsType(maskVals, BFloat16, s)
		maskVals.Free()
		if err != nil {
			ref.Free()
			t.Fatal(err)
		}
		mask4, err := Reshape(maskBF, []int{1, 1, 1, cap}, s)
		maskBF.Free()
		if err != nil {
			ref.Free()
			t.Fatal(err)
		}
		gotBF, err := FastScaledDotProductAttention(qB, kPad, vPad, scale, "array", mask4, nil, s)
		mask4.Free()
		if err != nil {
			ref.Free()
			t.Fatal(err)
		}

		w := read(ref)
		g := read(gotBF)
		ref.Free()
		gotBF.Free()
		bitwise := true
		maxDiff := float64(0)
		for i := range w {
			d := math.Abs(float64(w[i] - g[i]))
			if d > maxDiff {
				maxDiff = d
			}
			if w[i] != g[i] {
				bitwise = false
			}
		}
		if !bitwise {
			t.Errorf("exact=%d: masked-padded SDPA NOT bitwise-identical to exact-length (maxAbsDiff=%g)", exact, maxDiff)
		} else {
			fmt.Printf("exact=%d: bitwise identical (maxAbsDiff=0)\n", exact)
		}
	}
}
