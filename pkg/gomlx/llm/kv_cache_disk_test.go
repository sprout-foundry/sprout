//go:build darwin && arm64 && cgo

package llm

import (
	"path/filepath"
	"testing"

	_ "github.com/sprout-foundry/sprout/pkg/gomlx/mlx" // registers a real tensor.Backend for this test
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// TestKVCacheDiskRoundTrip guards the actual bytes-on-disk mechanism behind
// spillIdleSlots/ensureSlotResident: a cache built from real backend arrays
// must come back byte-for-byte identical after SaveToDisk + LoadKVCacheFromDisk,
// covering both a full-attention layer (K/V) and a DeltaNet layer (State/
// ConvState) plus an uninitialized layer in between.
func TestKVCacheDiskRoundTrip(t *testing.T) {
	backend := tensor.DetectBackend()
	if backend == nil || !backend.Available() {
		t.Skip("no tensor backend available on this platform")
	}
	stream, err := backend.DefaultGPUStream()
	if err != nil {
		t.Fatalf("get stream: %v", err)
	}

	c := NewKVCache(3, stream, backend)
	defer c.Free()

	k, err := backend.NewArrayFromFloat32([]float32{1, 2, 3, 4, 5, 6}, []int{1, 1, 3, 2})
	if err != nil {
		t.Fatalf("new K array: %v", err)
	}
	v, err := backend.NewArrayFromFloat32([]float32{7, 8, 9, 10, 11, 12}, []int{1, 1, 3, 2})
	if err != nil {
		t.Fatalf("new V array: %v", err)
	}
	if err := c.Store(0, k, v); err != nil {
		t.Fatalf("store layer 0: %v", err)
	}

	state, err := backend.NewArrayFromFloat32([]float32{0.5, -0.5, 1.5, -1.5}, []int{1, 2, 2, 1})
	if err != nil {
		t.Fatalf("new State array: %v", err)
	}
	convState, err := backend.NewArrayFromFloat32([]float32{3.25, -3.25}, []int{1, 2, 1})
	if err != nil {
		t.Fatalf("new ConvState array: %v", err)
	}
	if err := c.StoreState(2, state, convState); err != nil {
		t.Fatalf("store layer 2 state: %v", err)
	}
	// Layer 1 stays uninitialized on purpose.

	path := filepath.Join(t.TempDir(), "slot.bin")
	if err := c.SaveToDisk(path); err != nil {
		t.Fatalf("SaveToDisk: %v", err)
	}

	loaded, err := LoadKVCacheFromDisk(path, stream, backend)
	if err != nil {
		t.Fatalf("LoadKVCacheFromDisk: %v", err)
	}
	defer loaded.Free()

	if loaded.IsInitialized(1) {
		t.Error("layer 1 should still be uninitialized after round-trip")
	}
	if !loaded.IsInitialized(0) || !loaded.IsInitialized(2) {
		t.Fatal("layers 0 and 2 should be initialized after round-trip")
	}

	l0, err := loaded.Get(0)
	if err != nil {
		t.Fatalf("get layer 0: %v", err)
	}
	gotK, err := l0.K.Float32Data()
	if err != nil {
		t.Fatalf("K.Float32Data: %v", err)
	}
	assertFloat32Equal(t, "K", gotK, []float32{1, 2, 3, 4, 5, 6})
	gotV, err := l0.V.Float32Data()
	if err != nil {
		t.Fatalf("V.Float32Data: %v", err)
	}
	assertFloat32Equal(t, "V", gotV, []float32{7, 8, 9, 10, 11, 12})

	l2, err := loaded.Get(2)
	if err != nil {
		t.Fatalf("get layer 2: %v", err)
	}
	gotState, err := l2.State.Float32Data()
	if err != nil {
		t.Fatalf("State.Float32Data: %v", err)
	}
	assertFloat32Equal(t, "State", gotState, []float32{0.5, -0.5, 1.5, -1.5})
	gotConvState, err := l2.ConvState.Float32Data()
	if err != nil {
		t.Fatalf("ConvState.Float32Data: %v", err)
	}
	assertFloat32Equal(t, "ConvState", gotConvState, []float32{3.25, -3.25})
}

func assertFloat32Equal(t *testing.T, label string, got, want []float32) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: length = %d, want %d", label, len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s[%d] = %v, want %v (full: got=%v want=%v)", label, i, got[i], want[i], got, want)
		}
	}
}
