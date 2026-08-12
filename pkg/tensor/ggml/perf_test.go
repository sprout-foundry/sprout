//go:build (darwin || linux) && arm64 && cgo && ggml

package ggml

import (
	"testing"
)

// ensureInit must pin the thread count rather than leaving it to ggml, which
// scales it with the core count and oversubscribes on small machines.
func TestThreadCountConfigured(t *testing.T) {
	g := &GGMLBackend{}
	if !g.Available() {
		t.Skip("GGML backend not available")
	}
	if g.threads == 0 {
		t.Skip("backend does not expose ggml_backend_set_n_threads")
	}
	if want := pickThreadCount(); g.threads != want {
		t.Errorf("threads = %d, want %d", g.threads, want)
	}
}

// Every op pays a fixed cost: build a temp context, allocate the graph,
// compute, copy the result out, tear it all down. Decode runs on the order
// of a thousand ops per token, so this sets the floor on per-token latency.
func BenchmarkOpOverhead(b *testing.B) {
	g := &GGMLBackend{}
	if !g.Available() {
		b.Skip("GGML backend not available")
	}
	s, _ := g.DefaultGPUStream()
	x, _ := g.NewArrayFromFloat32(make([]float32, 32), []int{1, 32})
	y, _ := g.NewArrayFromFloat32(make([]float32, 32), []int{1, 32})
	defer x.Free()
	defer y.Free()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, err := g.Add(x, y, s)
		if err != nil {
			b.Fatalf("Add: %v", err)
		}
		r.Free()
	}
}

// Decode is bound by how fast Q4_0 weights stream through the matmul kernel.
// Reported in GB/s so it can be compared against the machine's memory
// bandwidth and against the 0.76 s/token measured end to end.
func BenchmarkQ4MatMul(b *testing.B) {
	g := &GGMLBackend{}
	if !g.Available() {
		b.Skip("GGML backend not available")
	}
	s, _ := g.DefaultGPUStream()

	const in, out = 2560, 9216
	w, err := g.NewArrayQ4_0(fill(out*in, 41), []int{out, in})
	if err != nil {
		b.Fatalf("NewArrayQ4_0: %v", err)
	}
	defer w.Free()
	x, _ := g.NewArrayFromFloat32(fill(in, 42), []int{1, 1, in})
	defer x.Free()

	// Q4_0 packs 32 weights per 18-byte block.
	b.SetBytes(int64(out) * int64(in) / 32 * 18)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r, err := g.MatMul(x, w, s)
		if err != nil {
			b.Fatalf("MatMul: %v", err)
		}
		r.Free()
	}
}

// The quantized matmul kernels have ARM fast paths gated on instructions this
// CPU has (asimddp, i8mm). They are selected at runtime from flags baked in
// when libggml-cpu was compiled, so a library built for baseline armv8-a
// silently falls back to the generic NEON path and roughly halves decode
// throughput. Nothing errors — it is only visible here.
func TestCPUQuantFastPaths(t *testing.T) {
	g := &GGMLBackend{}
	if !g.Available() {
		t.Skip("GGML backend not available")
	}
	dotprod, i8mm, neon := g.cpuFeatures()
	t.Logf("ggml cpu fast paths: dotprod=%v i8mm=%v neon=%v", dotprod, i8mm, neon)
	if !neon {
		t.Error("ggml built without NEON; quantized matmul will be scalar")
	}
	if !dotprod || !i8mm {
		t.Skipf("libggml-cpu lacks dotprod/i8mm (built for baseline armv8-a); "+
			"rebuild with -DGGML_NATIVE=ON to enable them. dotprod=%v i8mm=%v", dotprod, i8mm)
	}
}
