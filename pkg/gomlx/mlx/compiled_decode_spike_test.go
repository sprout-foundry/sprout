//go:build darwin && arm64 && cgo

package mlx

import (
	"math"
	"runtime"
	"testing"
)

// spikeStream returns a GPU stream for the compiled-decode spike tests.
func spikeStream(t *testing.T) *Stream {
	t.Helper()
	if !Available() {
		t.Skip("Metal not available")
	}
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)
	stream, err := DefaultGPUStream()
	if err != nil {
		t.Skipf("no GPU stream: %v", err)
	}
	t.Cleanup(stream.Free)
	return stream
}

func spikeF32(t *testing.T, data []float32, shape []int) *Array {
	t.Helper()
	a, err := NewArrayFromFloat32(data, shape)
	if err != nil {
		t.Fatalf("NewArrayFromFloat32: %v", err)
	}
	t.Cleanup(a.Free)
	return a
}

func spikeI32(t *testing.T, v int32) *Array {
	t.Helper()
	a, err := NewArrayFromInt32([]int32{v}, []int{1})
	if err != nil {
		t.Fatalf("NewArrayFromInt32: %v", err)
	}
	t.Cleanup(a.Free)
	return a
}

func spikeRead(t *testing.T, s *Stream, a *Array) []float32 {
	t.Helper()
	if a.Dtype() != Float32 {
		f, err := AsType(a, Float32, s)
		if err != nil {
			t.Fatalf("AsType: %v", err)
		}
		defer f.Free()
		a = f
	}
	d, err := a.Float32Data()
	if err != nil {
		t.Fatalf("Float32Data: %v", err)
	}
	return d
}

// TestSpikeRoPEDynamicParity verifies that fast.rope with an ARRAY offset
// (mlx_fast_rope_dynamic) matches the int-offset fast.rope the eager path
// uses. If bit-identical, position can become a closure input (dynamic per
// call) instead of a captured constant (frozen at trace time) in a compiled
// decode step.
func TestSpikeRoPEDynamicParity(t *testing.T) {
	s := spikeStream(t)

	B, H, D := 1, 2, 8
	n := B * H * D
	data := make([]float32, n)
	for i := range data {
		data[i] = float32(i%7) - 3 + 0.25*float32(i%3)
	}
	x := spikeF32(t, data, []int{B, 1, H, D})

	const offset = 17
	dynOff := spikeI32(t, offset)

	want, err := FastRoPE(x, D, false, 10000.0, 1.0, offset, nil, s)
	if err != nil {
		t.Fatalf("FastRoPE: %v", err)
	}
	t.Cleanup(want.Free)

	got, err := FastRoPEDynamic(x, D, false, 10000.0, 1.0, dynOff, nil, s)
	if err != nil {
		t.Fatalf("FastRoPEDynamic: %v", err)
	}
	t.Cleanup(got.Free)

	w := spikeRead(t, s, want)
	g := spikeRead(t, s, got)
	if len(w) == 0 || len(w) != len(g) {
		t.Fatalf("shape mismatch: want %d got %d elems", len(w), len(g))
	}
	for i := range w {
		if w[i] != g[i] {
			t.Fatalf("rope dynamic mismatch at %d: want %v got %v (not bit-identical; dynamic-offset kernel differs — position input would change numerics vs eager)", i, w[i], g[i])
		}
	}
	t.Logf("rope dynamic offset bit-identical to static offset (%d elems)", len(w))
}

// TestSpikeSDPAPaddedBufferParity checks whether SDPA over a zero-padded
// fixed-capacity K/V buffer, masked so positions > pos are excluded, gives
// the same result as SDPA over the exact-length slice. This decides whether
// a compiled decode step with fixed-shape K/V buffers matches the eager
// path's growing-window numerics bitwise or only approximately.
func TestSpikeSDPAPaddedBufferParity(t *testing.T) {
	s := spikeStream(t)

	B, H, D := 1, 2, 8
	pos := 5 // real keys
	cap := 8 // buffer capacity (3 padded slots)
	n := B * H * cap * D
	kData := make([]float32, n)
	vData := make([]float32, n)
	for i := range kData {
		kData[i] = float32(i%11) - 5 + 0.5*float32(i%4)
		vData[i] = float32(i%9) - 4 + 0.25*float32(i%5)
	}
	kPad := spikeF32(t, kData, []int{B, H, cap, D})
	vPad := spikeF32(t, vData, []int{B, H, cap, D})
	qData := make([]float32, B*H*D)
	for i := range qData {
		qData[i] = float32(i%7) - 3
	}
	q := spikeF32(t, qData, []int{B, H, 1, D})

	scale := float32(1.0 / math.Sqrt(float64(D)))

	// Reference: exact-length slice. The mask allows arange(cap) <= pos,
	// i.e. keys 0..pos — that's pos+1 keys.
	sl := []int{1, 1, 1, 1}
	kRef, err := Slice(kPad, []int{0, 0, 0, 0}, []int{B, H, pos + 1, D}, sl, s)
	if err != nil {
		t.Fatalf("slice k: %v", err)
	}
	t.Cleanup(kRef.Free)
	vRef, err := Slice(vPad, []int{0, 0, 0, 0}, []int{B, H, pos + 1, D}, sl, s)
	if err != nil {
		t.Fatalf("slice v: %v", err)
	}
	t.Cleanup(vRef.Free)
	ref, err := FastScaledDotProductAttention(q, kRef, vRef, scale, "", nil, nil, s)
	if err != nil {
		t.Fatalf("sdpa ref: %v", err)
	}
	t.Cleanup(ref.Free)

	// Padded buffer + ADDITIVE mask (positions > pos get -inf). MLX's C++
	// fast-SDPA treats an array mask as additive, so the bool form is wrong
	// there (adds 0/1 instead of masking) — build it the way the compiled
	// graph would: Where(arange(cap) <= pos, 0, -inf), reshaped to
	// [1,1,1,cap] for broadcast against [B,H,1,cap] scores.
	posArr := spikeI32(t, int32(pos))
	rg, err := Arange(0, float64(cap), 1, Int32, s)
	if err != nil {
		t.Fatalf("arange: %v", err)
	}
	t.Cleanup(rg.Free)
	valid, err := LessEqual(rg, posArr, s)
	if err != nil {
		t.Fatalf("less_equal: %v", err)
	}
	t.Cleanup(valid.Free)
	zero, err := NewArrayFromFloat32([]float32{0}, []int{1})
	if err != nil {
		t.Fatalf("zero: %v", err)
	}
	t.Cleanup(zero.Free)
	negInf, err := NewArrayFromFloat32([]float32{float32(math.Inf(-1))}, []int{1})
	if err != nil {
		t.Fatalf("negInf: %v", err)
	}
	t.Cleanup(negInf.Free)
	additive, err := Where(valid, zero, negInf, s)
	if err != nil {
		t.Fatalf("where additive: %v", err)
	}
	t.Cleanup(additive.Free)
	// In-graph-expressible form: [1,1,1,cap] broadcast against [B,H,1,cap]
	// scores. (A host-materialized full mask would not be expressible inside
	// a compiled graph.)
	mask4, err := Reshape(additive, []int{1, 1, 1, cap}, s)
	if err != nil {
		t.Fatalf("reshape mask: %v", err)
	}
	t.Cleanup(mask4.Free)

	got, err := FastScaledDotProductAttention(q, kPad, vPad, scale, "array", mask4, nil, s)
	if err != nil {
		t.Fatalf("sdpa padded: %v", err)
	}
	t.Cleanup(got.Free)

	w := spikeRead(t, s, ref)
	g := spikeRead(t, s, got)
	if len(w) != len(g) {
		t.Fatalf("shape mismatch: want %d got %d elems", len(w), len(g))
	}
	bitwise := true
	for i := range w {
		if w[i] != g[i] {
			bitwise = false
		}
		if math.Abs(float64(w[i]-g[i])) > 2e-3 {
			t.Errorf("padded+masked SDPA diverges from exact-length at %d: want %v got %v", i, w[i], g[i])
		}
	}
	if !bitwise {
		t.Errorf("padded+masked SDPA close but NOT bitwise-identical to exact-length (want %v got %v)", w, g)
	}
	t.Logf("padded+masked SDPA vs exact-length: bitwise=%v (%d elems)", bitwise, len(w))
}

// TestSpikeWhereScatter verifies the Where(Equal(arange(C), pos), new, buf)
// idiom for writing one position into a fixed-capacity buffer without a
// dynamic slice offset.
func TestSpikeWhereScatter(t *testing.T) {
	s := spikeStream(t)

	const cap = 8
	pos := 3
	bufData := make([]float32, cap)
	for i := range bufData {
		bufData[i] = float32(i + 1)
	}
	buf := spikeF32(t, bufData, []int{cap})
	newVal := spikeF32(t, []float32{99.5}, []int{1})
	posArr := spikeI32(t, int32(pos))
	rg, err := Arange(0, cap, 1, Int32, s)
	if err != nil {
		t.Fatalf("arange: %v", err)
	}
	t.Cleanup(rg.Free)
	eq, err := Equal(rg, posArr, s)
	if err != nil {
		t.Fatalf("equal: %v", err)
	}
	t.Cleanup(eq.Free)
	out, err := Where(eq, newVal, buf, s)
	if err != nil {
		t.Fatalf("where: %v", err)
	}
	t.Cleanup(out.Free)

	got := spikeRead(t, s, out)
	want := append([]float32(nil), bufData...)
	want[pos] = 99.5
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("scatter mismatch at %d: want %v got %v", i, want[i], got[i])
		}
	}
	t.Logf("Where-scatter exact (%d elems)", len(got))
}

// TestSpikeCompiledClosureReplay compiles a small multi-input/multi-output
// closure with shapeless=false and verifies:
//   - replay after the trace uses NEW input arrays (no frozen references),
//   - per-call scalar inputs (the pos analogue) change the output,
//   - results match the eager computation.
//
// This is the load-bearing assumption of compiled decode: the closure body
// runs exactly once on placeholders, and every later Apply replays the traced
// graph on the actual inputs.
func TestSpikeCompiledClosureReplay(t *testing.T) {
	s := spikeStream(t)

	const C = 8
	// fn(x [C], pos [1]) -> (y [C], buf2 [C]) where y = x*2 + pos,
	// buf2 = Where(x==pos, 1, 0). Both outputs, one capturing a scalar
	// comparison — the two mechanisms compiled decode needs.
	fn := func(inputs []*Array) ([]*Array, error) {
		x := inputs[0]
		pos := inputs[1]
		two, err := NewArrayFromFloat32([]float32{2}, []int{1})
		if err != nil {
			return nil, err
		}
		defer two.Free()
		x2, err := Multiply(x, two, s)
		if err != nil {
			return nil, err
		}
		defer x2.Free()
		y, err := Add(x2, pos, s)
		if err != nil {
			return nil, err
		}
		one, err := NewArrayFromFloat32([]float32{1}, []int{1})
		if err != nil {
			return nil, err
		}
		defer one.Free()
		zero, err := NewArrayFromFloat32([]float32{0}, []int{1})
		if err != nil {
			return nil, err
		}
		defer zero.Free()
		eq, err := Equal(x, pos, s)
		if err != nil {
			return nil, err
		}
		defer eq.Free()
		buf2, err := Where(eq, one, zero, s)
		if err != nil {
			return nil, err
		}
		return []*Array{y, buf2}, nil
	}

	// Placeholder inputs for the trace — values must NOT leak into replays.
	x0, err := NewArrayFromFloat32(make([]float32, C), []int{C})
	if err != nil {
		t.Fatalf("x0: %v", err)
	}
	t.Cleanup(x0.Free)
	p0 := spikeI32(t, 0)

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

	// First apply triggers the trace with the placeholder inputs.
	o1, err := compiled.Apply([]*Array{x0, p0})
	if err != nil {
		t.Fatalf("apply 1: %v", err)
	}
	for _, o := range o1 {
		t.Cleanup(o.Free)
	}

	// Replay with FRESH arrays and a different pos — the decisive check.
	x1, err := NewArrayFromFloat32([]float32{1, 2, 3, 4, 5, 6, 7, 8}, []int{C})
	if err != nil {
		t.Fatalf("x1: %v", err)
	}
	t.Cleanup(x1.Free)
	p1 := spikeI32(t, 3)
	o2, err := compiled.Apply([]*Array{x1, p1})
	if err != nil {
		t.Fatalf("apply 2: %v", err)
	}
	for _, o := range o2 {
		t.Cleanup(o.Free)
	}

	y := spikeRead(t, s, o2[0])
	wantY := []float32{1*2 + 3, 2*2 + 3, 3*2 + 3, 4*2 + 3, 5*2 + 3, 6*2 + 3, 7*2 + 3, 8*2 + 3}
	for i := range wantY {
		if y[i] != wantY[i] {
			t.Fatalf("replay y mismatch at %d: want %v got %v (compiled graph froze the trace-time inputs — replay is broken)", i, wantY[i], y[i])
		}
	}
	buf2 := spikeRead(t, s, o2[1])
	// x1 = [1..8], pos = 3 → x == pos at index 2 (x[2] == 3).
	for i := range buf2 {
		want := float32(0)
		if i == 2 {
			want = 1
		}
		if buf2[i] != want {
			t.Fatalf("replay buf2 mismatch at %d: want %v got %v", i, want, buf2[i])
		}
	}
	t.Logf("compiled closure replay honors fresh inputs and scalar pos")
}
