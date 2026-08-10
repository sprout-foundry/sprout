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
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// TestDeltaNetPrefillSpeed compares the fused Metal kernel against the
// sequential ops scan on a realistic prefill (B=1, S=128, the 0.8B model's
// DeltaNet shape: Hk=16, Hv=16, Dk=128, Dv=128).
//
// Run with:
//
//	go test -tags mlx -run TestDeltaNetPrefillSpeed -v ./llm/qwen35/
func TestDeltaNetPrefillSpeed(t *testing.T) {
	if !mlx.Available() {
		t.Skip("Metal not available")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	backend := &mlx.MetalBackend{}
	stream, err := backend.NewGPUStream()
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Free()

	B, S, Hk, Dk, Hv, Dv := 1, 128, 16, 128, 16, 128

	rng := rand.New(rand.NewSource(1))
	mkF32 := func(shape []int) tensor.Array {
		data := make([]float32, product(shape))
		for i := range data {
			data[i] = 0.2 * (rng.Float32()*2 - 1)
		}
		a, err := backend.NewArrayFromFloat32(data, shape)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}

	q := mkF32([]int{B, S, Hk, Dk})
	defer q.Free()
	k := mkF32([]int{B, S, Hk, Dk})
	defer k.Free()
	v := mkF32([]int{B, S, Hv, Dv})
	defer v.Free()
	g := mkF32([]int{B, S, Hv})
	defer g.Free()
	beta := mkF32([]int{B, S, Hv})
	defer beta.Free()

	// Warm up kernel compilation.
	for i := 0; i < 5; i++ {
		y, st, err := fusedGatedDeltaUpdate(q, k, v, g, beta, nil, backend, stream)
		if err != nil {
			t.Fatal(err)
		}
		_ = y.Eval()
		y.Free()
		st.Free()
	}

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
		y, st, err := fusedGatedDeltaUpdate(q, k, v, g, beta, nil, backend, stream)
		if err != nil {
			return err
		}
		defer y.Free()
		defer st.Free()
		return y.Eval()
	})
	timePath("sequential ops", func() error {
		y, st, err := gatedDeltaUpdateOps(q, k, v, g, beta, nil, backend, stream)
		if err != nil {
			return err
		}
		defer y.Free()
		defer st.Free()
		return y.Eval()
	})
	fusedMs = measureMs(func() error {
		y, st, err := fusedGatedDeltaUpdate(q, k, v, g, beta, nil, backend, stream)
		if err != nil {
			return err
		}
		defer y.Free()
		defer st.Free()
		return y.Eval()
	})
	opsMs = measureMs(func() error {
		y, st, err := gatedDeltaUpdateOps(q, k, v, g, beta, nil, backend, stream)
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

func bf16Encode(f []float32, out []byte) {
	for i, x := range f {
		bits := math.Float32bits(x)
		trunc := (bits + 0x8000) >> 16
		out[i*2] = byte(trunc >> 8)
		out[i*2+1] = byte(trunc)
	}
}
