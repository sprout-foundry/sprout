//go:build darwin && arm64 && cgo

package mlx

import (
	"encoding/binary"
	"testing"
)

// TestArgPartitionTopKDirectionSpike pins which end of mlx argpartition's
// output holds the top-k elements for kth=-k. The MoE router slices the
// LAST k indices (see sparseMoeBlock.forward); if an MLX upgrade ever
// changes this ordering, expert routing silently selects the WORST experts
// and generation collapses — this test is the tripwire.
func TestArgPartitionTopKDirectionSpike(t *testing.T) {
	b := &MetalBackend{}
	if !b.Available() {
		t.Skip("no Metal backend")
	}
	stream, err := b.DefaultStream()
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Free()
	s := stream.(*Stream)

	// One token's router scores over 10 experts. Top-3: indices 7, 2, 5.
	data := []float32{0.05, 0.10, 0.60, 0.02, 0.03, 0.25, 0.05, 0.80, 0.01, 0.09}
	a, err := b.NewArrayFromFloat32(data, []int{1, 10})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Free()

	const k = 3
	part, err := ArgPartitionAxis(a.(*Array), -k, -1, s)
	if err != nil {
		t.Fatal(err)
	}
	defer part.Free()
	if err := s.Synchronize(); err != nil {
		t.Fatal(err)
	}

	raw, err := part.RawBytes()
	if err != nil {
		t.Fatal(err)
	}
	idx := make([]int, 0, len(raw)/4)
	for i := 0; i < len(raw); i += 4 {
		idx = append(idx, int(binary.LittleEndian.Uint32(raw[i:i+4])))
	}
	t.Logf("argpartition(-3) full result: %v", idx)

	want := map[int]bool{7: true, 2: true, 5: true}
	lastK := idx[len(idx)-k:]
	for _, id := range lastK {
		if !want[id] {
			t.Fatalf("last-k=%v is not the top-k set {7,2,5} — MoE router slice direction is wrong", lastK)
		}
	}
}
