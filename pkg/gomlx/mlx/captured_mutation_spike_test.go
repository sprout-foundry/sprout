//go:build darwin && arm64 && cgo

package mlx

import (
	"fmt"
	"testing"
)

// TestSpikeCapturedBufferMutation answers THE design question for compiled
// decode: if a Go-held array is CAPTURED by the closure body (not passed as
// an input), does a later in-place-style mutation of its buffer contents
// (via SliceUpdate into the same underlying storage) get SEEN by replays?
//
// MLX arrays are immutable values, so SliceUpdate normally produces a new
// array — but the allocator may reuse the SAME buffer when the source's ref
// drops. This test checks the semantics that would make the "buffer as
// captured constant + host-side SliceUpdate" design work: whether replay
// re-reads the constant's memory each apply (by-reference) or bakes a copy
// of the values (by-value).
func TestSpikeCapturedBufferMutation(t *testing.T) {
	s := spikeStream(t)

	const N = 8
	buf, err := NewArrayFromFloat32([]float32{1, 2, 3, 4, 5, 6, 7, 8}, []int{N})
	if err != nil {
		t.Fatal(err)
	}
	defer buf.Free()

	// 4-D alias of the same storage for the SliceUpdate probe below.
	buf4, err := Reshape(buf, []int{1, 1, 1, N}, s)
	if err != nil {
		t.Fatal(err)
	}
	defer buf4.Free()

	// fn() captures buf as a constant (no inputs) and sums it.
	fn := func(inputs []*Array) ([]*Array, error) {
		out, err := Sum(buf, []int{0}, false, s)
		if err != nil {
			return nil, err
		}
		return []*Array{out}, nil
	}

	plain, err := NewClosure(fn)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := plain.Compile(false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(compiled.Free)
	t.Cleanup(plain.Free)

	// Trace + first replay: sum = 36.
	outs, err := compiled.Apply(nil)
	if err != nil {
		t.Fatal(err)
	}
	v1 := spikeRead(t, s, outs[0])[0]
	outs[0].Free()
	fmt.Printf("first sum = %v (want 36)\n", v1)

	// Mutate the buffer contents in place via a new array written into the
	// same storage — emulate by SliceUpdate producing a new array and then
	// checking if the CAPTURED constant tracks it. (It cannot: constants
	// are captured by mlx_array handle, and SliceUpdate makes a new array.)
	// So instead: verify the semantics directly — replay again.
	outs = outs[:0]
	outs, err = compiled.Apply(nil)
	if err != nil {
		t.Fatal(err)
	}
	v2 := spikeRead(t, s, outs[0])[0]
	outs[0].Free()
	fmt.Printf("second sum = %v (constant semantics: still 36)\n", v2)

	// Now the REAL question in its practical form: if the Go side keeps ONE
	// persistent buffer array and SliceUpdate "replaces" it (new array,
	// possibly same storage), can the compiled graph ever see new values
	// without being re-traced? Test storage reuse (4-D to match the
	// package SliceUpdate's fixed 4-stride form):
	upd, err := SliceUpdate(buf4, spikeF32(t, []float32{100}, []int{1, 1, 1, 1}), []int{0, 0, 0, 0}, []int{1, 1, 1, 1}, s)
	if err != nil {
		t.Fatal(err)
	}
	if err := upd.Eval(); err != nil {
		t.Fatal(err)
	}
	defer upd.Free()
	// Read the ORIGINAL buf: if the allocator let SliceUpdate write into
	// shared storage, buf would now start with 100.
	orig := spikeRead(t, s, buf)
	fmt.Printf("after SliceUpdate, original buf[0] = %v (if 1: arrays are value semantics; if 100: storage was shared)\n", orig[0])
}
