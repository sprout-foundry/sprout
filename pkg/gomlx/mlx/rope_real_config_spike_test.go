//go:build darwin && arm64 && cgo

package mlx

import (
	"testing"
)

// TestSpikeRoPEDynamicRealConfig repeats TestSpikeRoPEDynamicParity at the
// real Qwen3.5-4B decode configuration: bf16 activations, dims=64
// (partial_rotary_factor 0.25 of head_dim 256), rope_theta 1e7, GQA shapes.
func TestSpikeRoPEDynamicRealConfig(t *testing.T) {
	s := spikeStream(t)

	B, Hq, Hkv, D, dims := 1, 16, 4, 256, 64
	const theta = 1e7

	mk := func(n int, seed float32) []float32 {
		d := make([]float32, n)
		for i := range d {
			d[i] = float32(int(seed)*7+i%13) / 17.0
		}
		return d
	}

	qF, err := NewArrayFromFloat32(mk(B*Hq*D, 1), []int{B, Hq, 1, D})
	if err != nil {
		t.Fatal(err)
	}
	defer qF.Free()
	qB, err := AsType(qF, BFloat16, s)
	if err != nil {
		t.Fatal(err)
	}
	defer qB.Free()

	kF, err := NewArrayFromFloat32(mk(B*Hkv*D, 2), []int{B, Hkv, 1, D})
	if err != nil {
		t.Fatal(err)
	}
	defer kF.Free()
	kB, err := AsType(kF, BFloat16, s)
	if err != nil {
		t.Fatal(err)
	}
	defer kB.Free()

	for _, off := range []int{0, 1, 2, 5, 23} {
		offArr := spikeI32(t, int32(off))

		qStatic, err := FastRoPE(qB, dims, false, theta, 1.0, off, nil, s)
		if err != nil {
			t.Fatal(err)
		}
		qDyn, err := FastRoPEDynamic(qB, dims, false, theta, 1.0, offArr, nil, s)
		if err != nil {
			t.Fatal(err)
		}
		qs := spikeRead(t, s, qStatic)
		qd := spikeRead(t, s, qDyn)
		qStatic.Free()
		qDyn.Free()
		for i := range qs {
			if qs[i] != qd[i] {
				t.Fatalf("offset=%d Q mismatch at %d: static=%v dynamic=%v", off, i, qs[i], qd[i])
			}
		}

		kStatic, err := FastRoPE(kB, dims, false, theta, 1.0, off, nil, s)
		if err != nil {
			t.Fatal(err)
		}
		kDyn, err := FastRoPEDynamic(kB, dims, false, theta, 1.0, offArr, nil, s)
		if err != nil {
			t.Fatal(err)
		}
		ks := spikeRead(t, s, kStatic)
		kd := spikeRead(t, s, kDyn)
		kStatic.Free()
		kDyn.Free()
		for i := range ks {
			if ks[i] != kd[i] {
				t.Fatalf("offset=%d K mismatch at %d: static=%v dynamic=%v", off, i, ks[i], kd[i])
			}
		}
	}
	t.Logf("real-config rope dynamic==static bitwise (bf16, dims=64, theta=1e7, offsets 0/1/2/5/23)")
}
