//go:build darwin && arm64 && cgo && mlx

package qwen35

import (
	"fmt"
	"math"
	"math/rand"
	"runtime"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
)

// TestDeltaNetPrefillSpeed compares the fused Metal kernel against the
// sequential ops scan on a realistic prefill (B=1, S=128, the 0.8B model's
// DeltaNet shape: Hk=16, Hv=16, Dk=128, Dv=128). The fused kernel scans the
// whole sequence in one launch; the ops path launches S per-step graphs.
//
// Run with:
//
//	go test -tags mlx -run TestDeltaNetPrefillSpeed -v ./qwen35/
func TestDeltaNetPrefillSpeed(t *testing.T) {
	if !mlx.Available() {
		t.Skip("Metal not available")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	stream, err := mlx.NewGPUStream()
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Free()

	// Model-like shapes (0.8B: Hk=Hv=16, Dk=Dv=128). Use B=1, S=128 prefill.
	B, S, Hk, Dk, Hv, Dv := 1, 128, 16, 128, 16, 128

	rng := rand.New(rand.NewSource(1))
	f32 := func(n int, scale float32) []float32 {
		d := make([]float32, n)
		for i := range d {
			d[i] = scale * (rng.Float32()*2 - 1)
		}
		return d
	}
	q, _ := mlx.NewArrayFromFloat32(f32(B*S*Hk*Dk, 0.2), []int{B, S, Hk, Dk})
	defer q.Free()
	k, _ := mlx.NewArrayFromFloat32(f32(B*S*Hk*Dk, 0.2), []int{B, S, Hk, Dk})
	defer k.Free()
	vf := f32(B*S*Hv*Dv, 0.2)
	vb := make([]byte, len(vf)*2)
	bf16Encode(vf, vb)
	v, _ := mlx.NewArrayFromBytes(vb, []int{B, S, Hv, Dv}, mlx.BFloat16)
	defer v.Free()
	g, _ := mlx.NewArrayFromFloat32(f32(B*S*Hv, 0.5), []int{B, S, Hv})
	defer g.Free()
	bf := f32(B*S*Hv, 0.5)
	bb := make([]byte, len(bf)*2)
	bf16Encode(bf, bb)
	beta, _ := mlx.NewArrayFromBytes(bb, []int{B, S, Hv}, mlx.BFloat16)
	defer beta.Free()

	// Warm up kernel compilation AND let the GPU pipeline settle before
	// timing (first calls include Metal shader compile + first allocations).
	for i := 0; i < 5; i++ {
		y, st, err := fusedGatedDeltaUpdate(q, k, v, g, beta, nil, stream)
		if err != nil {
			t.Fatal(err)
		}
		_ = y.Eval()
		y.Free()
		st.Free()
	}
	{
		y, st, err := fusedGatedDeltaUpdate(q, k, v, g, beta, nil, stream)
		if err != nil {
			t.Fatal(err)
		}
		_ = y.Eval()
		y.Free()
		st.Free()
	}

	// Time each path: run N iterations, average. All work on this locked
	// thread so the thread-local GPU stream stays valid.
	timePath := func(name string, fn func() error) {
		const n = 10
		var total time.Duration
		for i := 0; i < n; i++ {
			start := time.Now()
			if err := fn(); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			total += time.Since(start)
		}
		fmt.Printf("  %-16s avg %.2f ms/run\n", name, total.Seconds()*1000/float64(n))
	}

	var fusedMs, opsMs float64
	timePath("fused kernel", func() error {
		y, st, err := fusedGatedDeltaUpdate(q, k, v, g, beta, nil, stream)
		if err != nil {
			return err
		}
		defer y.Free()
		defer st.Free()
		return y.Eval()
	})
	timePath("sequential ops", func() error {
		y, st, err := gatedDeltaUpdateOps(q, k, v, g, beta, nil, stream)
		if err != nil {
			return err
		}
		defer y.Free()
		defer st.Free()
		return y.Eval()
	})
	fusedMs, opsMs = measureMs(func() error {
		y, st, err := fusedGatedDeltaUpdate(q, k, v, g, beta, nil, stream)
		if err != nil {
			return err
		}
		defer y.Free()
		defer st.Free()
		return y.Eval()
	}), measureMs(func() error {
		y, st, err := gatedDeltaUpdateOps(q, k, v, g, beta, nil, stream)
		if err != nil {
			return err
		}
		defer y.Free()
		defer st.Free()
		return y.Eval()
	})
	t.Logf("B=1 S=%d prefill: fused=%.2fms ops=%.2fms speedup=%.1fx",
		S, fusedMs, opsMs, opsMs/fusedMs)
}

func measureMs(fn func() error) float64 {
	start := time.Now()
	if err := fn(); err != nil {
		return -1
	}
	return time.Since(start).Seconds() * 1000
}

// bf16Encode converts float32 values to bfloat16 big-endian bytes.
func bf16Encode(f []float32, out []byte) {
	for i, x := range f {
		bits := math.Float32bits(x)
		trunc := (bits + 0x8000) >> 16
		out[i*2] = byte(trunc >> 8)
		out[i*2+1] = byte(trunc)
	}
}
