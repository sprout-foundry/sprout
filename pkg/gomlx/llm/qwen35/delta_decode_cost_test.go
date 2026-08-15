//go:build darwin && arm64 && cgo

package qwen35

import (
	"fmt"
	"math/rand"
	"runtime"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// TestDeltaDecodeStepCost measures the per-layer cost of the DeltaNet
// recurrence at decode shape (B=1, S=1, real qwen3.5-4B head config),
// fused Metal kernel vs sequential ops loop, with real evaluation.
// 24 of 32 layers pay this cost every token — it bounds decode throughput.
func TestDeltaDecodeStepCost(t *testing.T) {
	if !mlx.Available() {
		t.Skip("Metal not available")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	backend := &mlx.MetalBackend{}
	stream, err := backend.NewGPUStream()
	if err != nil {
		t.Skipf("no GPU stream: %v", err)
	}
	defer stream.Free()

	rng := rand.New(rand.NewSource(7))
	// Real qwen3.5-4B linear-attn config: Hk=16, Dk=128, Hv=8, Dv=128.
	B, S, Hk, Dk, Hv, Dv := 1, 1, 16, 128, 8, 128

	mk := func(shape []int) tensor.Array {
		n := 1
		for _, d := range shape {
			n *= d
		}
		data := make([]float32, n)
		for i := range data {
			data[i] = float32(rng.NormFloat64())
		}
		a, err := backend.NewArrayFromFloat32(data, shape)
		if err != nil {
			t.Fatalf("mk: %v", err)
		}
		return a
	}

	q := mk([]int{B, S, Hk, Dk})
	defer q.Free()
	k := mk([]int{B, S, Hk, Dk})
	defer k.Free()
	v := mk([]int{B, S, Hv, Dv})
	defer v.Free()
	g := mk([]int{B, S, Hv})
	defer g.Free()
	beta := mk([]int{B, S, Hv})
	defer beta.Free()
	state := mk([]int{B, Hv, Dv, Dk})
	defer state.Free()

	bench := func(name string, f func(tensor.Array) (tensor.Array, tensor.Array, error)) {
		// warmup
		y, st, err := f(state)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if err := y.Eval(); err != nil {
			t.Fatal(err)
		}
		if err := st.Eval(); err != nil {
			t.Fatal(err)
		}
		y.Free()
		st.Free()
		if err := stream.Synchronize(); err != nil {
			t.Fatal(err)
		}

		// Launch-only (no eval): measures dispatch cost per call.
		const iters = 50
		start := time.Now()
		var y2, st2 tensor.Array
		for i := 0; i < iters; i++ {
			y, st, err := f(state)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			y2, st2 = y, st
		}
		launchElapsed := time.Since(start) / iters
		if err := y2.Eval(); err != nil {
			t.Fatal(err)
		}
		if err := st2.Eval(); err != nil {
			t.Fatal(err)
		}
		y2.Free()
		st2.Free()
		if err := stream.Synchronize(); err != nil {
			t.Fatal(err)
		}

		// One call, one eval: measures launch + execution + sync.
		start = time.Now()
		for i := 0; i < iters; i++ {
			y, st, err := f(state)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if err := y.Eval(); err != nil {
				t.Fatal(err)
			}
			if err := st.Eval(); err != nil {
				t.Fatal(err)
			}
			y.Free()
			st.Free()
		}
		syncElapsed := time.Since(start) / iters
		fmt.Printf("%s: launch=%v sync=%v per-step (x24 = %.2fms launch / %.2fms sync per token)\n",
			name, launchElapsed, syncElapsed, float64(launchElapsed.Microseconds())/1000*24, float64(syncElapsed.Microseconds())/1000*24)
	}

	bench("fused-kernel", func(st tensor.Array) (tensor.Array, tensor.Array, error) {
		return fusedGatedDeltaUpdate(q, k, v, g, beta, st, backend, stream)
	})
	bench("ops-loop", func(st tensor.Array) (tensor.Array, tensor.Array, error) {
		return gatedDeltaUpdateOps(q, k, v, g, beta, st, backend, stream)
	})
}
