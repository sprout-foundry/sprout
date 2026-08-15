//go:build darwin && arm64 && cgo

package mlx

import (
	"runtime"
	"testing"
)

// TestSpikeMetalKernelInCompiledClosure checks whether a custom Metal kernel
// (mx.fast.metal_kernel) can live inside an mlx_compile'd closure: the body
// runs once on placeholders to trace, and the replay must re-execute the
// kernel with the NEW inputs rather than frozen trace-time outputs. If this
// works, compiled decode can use the fused kernels (swiglu/compute_g/delta)
// instead of the slower multi-op mirrors.
func TestSpikeMetalKernelInCompiledClosure(t *testing.T) {
	s := spikeStream(t)

	const src = `
    auto idx = thread_position_in_grid.z;
    auto xv = static_cast<float>(x[idx]);
    auto yv = static_cast<float>(y[idx]);
    out[idx] = static_cast<InT>(xv * yv + 1.0f);
`
	k, err := NewMetalKernel(
		"spike_muladd",
		[]string{"x", "y"},
		[]string{"out"},
		src,
		true,
		false,
	)
	if err != nil {
		t.Fatalf("NewMetalKernel: %v", err)
	}
	t.Cleanup(k.Free)

	const N = 16
	fn := func(inputs []*Array) ([]*Array, error) {
		x, y := inputs[0], inputs[1]
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
		outs, err := k.Apply([]*Array{x, y}, cfg, s)
		if err != nil {
			return nil, err
		}
		return outs, nil
	}

	x0 := spikeF32(t, make([]float32, N), []int{N})
	y0 := spikeF32(t, make([]float32, N), []int{N})

	plain, err := NewClosure(fn)
	if err != nil {
		t.Fatalf("NewClosure: %v", err)
	}
	compiled, err := plain.Compile(false)
	if err != nil {
		t.Fatalf("Compile (metal kernel NOT traceable): %v", err)
	}
	t.Cleanup(compiled.Free)
	t.Cleanup(plain.Free)

	// Trace.
	if _, err := compiled.Apply([]*Array{x0, y0}); err != nil {
		t.Fatalf("trace apply: %v", err)
	}

	// Replay with fresh values — must recompute, not replay frozen outputs.
	xv := make([]float32, N)
	yv := make([]float32, N)
	for i := range xv {
		xv[i] = float32(i + 1)
		yv[i] = float32(2 * (i + 1))
	}
	x1 := spikeF32(t, xv, []int{N})
	y1 := spikeF32(t, yv, []int{N})
	outs, err := compiled.Apply([]*Array{x1, y1})
	if err != nil {
		t.Fatalf("replay apply: %v", err)
	}
	if len(outs) != 1 {
		t.Fatalf("got %d outputs", len(outs))
	}
	t.Cleanup(outs[0].Free)
	got := spikeRead(t, s, outs[0])
	for i := range xv {
		want := xv[i]*yv[i] + 1.0
		if got[i] != want {
			t.Fatalf("metal kernel in compiled closure replay WRONG at %d: want %v got %v (kernel output frozen at trace time)", i, want, got[i])
		}
	}
	t.Logf("metal kernel traced+replayed correctly inside compiled closure")
	_ = runtime.NumCPU
}
