//go:build darwin && arm64 && cgo

package mlx

import (
	"runtime"
	"testing"
)

// TestSpikeMetalKernelStatefulReplay models the compiled-decode recurrence
// exactly: a closure whose metal kernel consumes a closure-INPUT state and
// returns an updated state, applied twice with the step-1 output fed back
// as the step-2 input. If the kernel froze its trace-time inputs, step 2
// would silently compute from the stale state.
func TestSpikeMetalKernelStatefulReplay(t *testing.T) {
	s := spikeStream(t)

	// out = x + state (elementwise on [1] arrays).
	const src = `
    auto idx = thread_position_in_grid.z;
    auto xv = static_cast<float>(x[idx]);
    auto sv = static_cast<float>(state[idx]);
    out[idx] = static_cast<InT>(xv + sv);
`
	k, err := NewMetalKernel(
		"spike_stateful",
		[]string{"x", "state"},
		[]string{"out"},
		src,
		true,
		false,
	)
	if err != nil {
		t.Fatalf("NewMetalKernel: %v", err)
	}
	t.Cleanup(k.Free)

	const N = 1
	fn := func(inputs []*Array) ([]*Array, error) {
		x, state := inputs[0], inputs[1]
		// Traced intermediate feeding the kernel (models q/k/v projections).
		dbl, err := Multiply(x, x, s)
		if err != nil {
			return nil, err
		}
		cfg := NewMetalKernelConfig()
		if err := cfg.AddOutputArg(x.Shape(), x.Dtype()); err != nil {
			return nil, err
		}
		defer cfg.Free()
		if err := cfg.SetGrid(1, 1, N); err != nil {
			return nil, err
		}
		if err := cfg.SetThreadGroup(1, 1, 256); err != nil {
			return nil, err
		}
		if err := cfg.AddTemplateArgDtype("InT", x.Dtype()); err != nil {
			return nil, err
		}
		outs, err := k.Apply([]*Array{dbl, state}, cfg, s)
		if err != nil {
			return nil, err
		}
		return outs, nil
	}

	mk := func(v float32) *Array {
		a, err := NewArrayFromFloat32([]float32{v}, []int{N})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(a.Free)
		return a
	}

	x0, s0 := mk(0), mk(100) // trace-time values (state=100 = poison marker)
	plain, err := NewClosure(fn)
	if err != nil {
		t.Fatalf("NewClosure: %v", err)
	}
	compiled, err := plain.Compile(false)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	t.Cleanup(compiled.Free)
	t.Cleanup(plain.Free)

	// Trace.
	if _, err := compiled.Apply([]*Array{x0, s0}); err != nil {
		t.Fatalf("trace: %v", err)
	}

	// Step 1: x=3, state=1 → out = 9 + 1 = 10.
	o1, err := compiled.Apply([]*Array{mk(3), mk(1)})
	if err != nil {
		t.Fatalf("apply 1: %v", err)
	}
	v1 := spikeRead(t, s, o1[0])
	if v1[0] != 10 {
		t.Fatalf("step 1: want 10 got %v", v1[0])
	}

	// Step 2: x=2, state=step-1 output (10) → out = 4 + 10 = 14.
	// If the kernel froze the trace-time state (100), this gives 104; if it
	// froze step-1's arrays, 14 still requires the dynamic path.
	o2, err := compiled.Apply([]*Array{mk(2), o1[0]})
	o1[0].Free()
	if err != nil {
		t.Fatalf("apply 2: %v", err)
	}
	v2 := spikeRead(t, s, o2[0])
	if v2[0] != 14 {
		t.Fatalf("step 2: want 14 got %v (state input not honored on replay — kernel froze trace-time inputs)", v2[0])
	}
	o2[0].Free()
	t.Logf("stateful metal-kernel replay correct (14)")
	_ = runtime.NumCPU
}
