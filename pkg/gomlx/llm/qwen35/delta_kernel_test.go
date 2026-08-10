//go:build darwin && arm64 && cgo && mlx

package qwen35

import (
	"math"
	"math/rand"
	"runtime"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// TestFusedVsSequentialParity checks that the fused Metal kernel and the
// sequential ops scan produce consistent outputs for a small random case.
// Skipped when Metal is not available.
func TestFusedVsSequentialParity(t *testing.T) {
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

	rng := rand.New(rand.NewSource(42))
	B, S, Hk, Dk, Hv, Dv := 1, 8, 4, 128, 8, 128

	mkF32 := func(shape []int) tensor.Array {
		data := make([]float32, product(shape))
		for i := range data {
			data[i] = float32(rng.NormFloat64())
		}
		a, err := backend.NewArrayFromFloat32(data, shape)
		if err != nil {
			t.Fatalf("mkF32: %v", err)
		}
		return a
	}

	q := mkF32([]int{B, S, Hk, Dk})
	defer q.Free()
	k := mkF32([]int{B, S, Hk, Dk})
	defer k.Free()
	v := mkF32([]int{B, S, Hv, Dv})
	defer v.Free()

	mkHead := func(lo, hi float32) tensor.Array {
		data := make([]float32, B*S*Hv)
		for i := range data {
			data[i] = lo + rng.Float32()*(hi-lo)
		}
		a, err := backend.NewArrayFromFloat32(data, []int{B, S, Hv})
		if err != nil {
			t.Fatalf("mkHead: %v", err)
		}
		return a
	}
	g := mkHead(0.5, 1.0)
	defer g.Free()
	beta := mkHead(0.0, 1.0)
	defer beta.Free()

	// Fused kernel path (q/k un-repeated; kernel broadcasts Hk→Hv).
	yF, stF, err := fusedGatedDeltaUpdate(q, k, v, g, beta, nil, backend, stream)
	if err != nil {
		t.Fatalf("fused: %v", err)
	}
	defer yF.Free()
	defer stF.Free()
	if err := yF.Eval(); err != nil {
		t.Fatalf("eval fused y: %v", err)
	}
	if err := stF.Eval(); err != nil {
		t.Fatalf("eval fused state: %v", err)
	}

	// Sequential ops path.
	yO, stO, err := gatedDeltaUpdateOps(q, k, v, g, beta, nil, backend, stream)
	if err != nil {
		t.Fatalf("ops: %v", err)
	}
	defer yO.Free()
	defer stO.Free()
	if err := yO.Eval(); err != nil {
		t.Fatalf("eval ops y: %v", err)
	}
	if err := stO.Eval(); err != nil {
		t.Fatalf("eval ops state: %v", err)
	}

	dy := maxAbsDiffArr(yF, yO)
	ds := maxAbsDiffArr(stF, stO)
	t.Logf("y_maxdiff=%.6f state_maxdiff=%.6f", dy, ds)
	if dy > 0.5 {
		t.Errorf("y output diverged: maxdiff %.6f", dy)
	}
	if ds > 0.5 {
		t.Errorf("state diverged: maxdiff %.6f", ds)
	}
}

func product(shape []int) int {
	n := 1
	for _, d := range shape {
		n *= d
	}
	return n
}

func maxAbsDiffArr(a, b tensor.Array) float64 {
	da, err := a.Float32Data()
	if err != nil {
		return math.Inf(1)
	}
	db, err := b.Float32Data()
	if err != nil {
		return math.Inf(1)
	}
	if len(da) != len(db) {
		return math.Inf(1)
	}
	m := 0.0
	for i := range da {
		d := math.Abs(float64(da[i]) - float64(db[i]))
		if d > m {
			m = d
		}
	}
	return m
}
