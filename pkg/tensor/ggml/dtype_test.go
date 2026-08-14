//go:build (darwin || linux) && (arm64 || amd64) && cgo && ggml

package ggml

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// Weights, scales and biases are loaded via NewArrayFromBytes and read back
// with RawBytes/Dtype. A dtype that maps to a wider ggml type silently pads
// the tensor with uninitialised bytes and misreports Dtype(), which the
// affine dequantiser then decodes as garbage — so pin the round trip for
// every type stored as-is. (BF16/F16 are widened to F32 on load; see
// TestBF16ScalesDecodeExactly.)
func TestNewArrayFromBytesRoundTrip(t *testing.T) {
	g := &GGMLBackend{}
	if !g.Available() {
		t.Skip("GGML backend not available")
	}

	cases := []struct {
		name      string
		dtype     tensor.Dtype
		elemBytes int
	}{
		{"f32", tensor.Float32, 4},
		{"i32", tensor.Int32, 4},
		{"i64", tensor.Int64, 8},
	}
	const n = 64
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data := make([]byte, n*tc.elemBytes)
			for i := range data {
				data[i] = byte(i*7 + 1)
			}
			arr, err := g.NewArrayFromBytes(data, []int{1, n}, tc.dtype)
			if err != nil {
				t.Fatalf("NewArrayFromBytes: %v", err)
			}
			if got := arr.Dtype(); got != tc.dtype {
				t.Errorf("Dtype() = %v, want %v", got, tc.dtype)
			}
			raw, err := arr.RawBytes()
			if err != nil {
				t.Fatalf("RawBytes: %v", err)
			}
			if len(raw) != len(data) {
				t.Fatalf("RawBytes returned %d bytes, want %d (element size mismatch)", len(raw), len(data))
			}
			for i := range data {
				if raw[i] != data[i] {
					t.Fatalf("byte %d = %#x, want %#x", i, raw[i], data[i])
				}
			}
		})
	}
}

// BF16 scales must survive the load with their values intact: decoding them
// as float32 was what produced Inf weights and NaN activations. They are
// widened to F32 so ggml's F32-only CPU kernels can consume them, and the
// affine dequantiser reads them back through Dtype().
func TestBF16ScalesDecodeExactly(t *testing.T) {
	g := &GGMLBackend{}
	if !g.Available() {
		t.Skip("GGML backend not available")
	}

	want := []float32{0.04345703, -0.036865234, 0.017333984, -0.1484375}
	raw := make([]byte, len(want)*2)
	for i, v := range want {
		binary.LittleEndian.PutUint16(raw[i*2:], uint16(math.Float32bits(v)>>16))
	}

	arr, err := g.NewArrayFromBytes(raw, []int{1, len(want)}, tensor.BFloat16)
	if err != nil {
		t.Fatalf("NewArrayFromBytes: %v", err)
	}
	if arr.Dtype() != tensor.Float32 {
		t.Fatalf("Dtype() = %v, want Float32 (BF16 is widened on load)", arr.Dtype())
	}
	got, err := arr.Float32Data()
	if err != nil {
		t.Fatalf("Float32Data: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d values, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("scale %d = %v, want %v", i, got[i], want[i])
		}
	}
}

// F16 widening must handle subnormals and the exponent bias, not just the
// common normal case.
func TestFloat16Widening(t *testing.T) {
	cases := []struct {
		half uint16
		want float32
	}{
		{0x0000, 0},
		{0x8000, float32(math.Copysign(0, -1))},
		{0x3c00, 1},
		{0xc000, -2},
		{0x3555, 0.33325195},
		{0x0001, 5.9604645e-08}, // smallest subnormal
		{0x03ff, 6.0975552e-05}, // largest subnormal
		{0x7bff, 65504},         // largest normal
	}
	for _, tc := range cases {
		if got := float16ToFloat32(tc.half); got != tc.want {
			t.Errorf("float16ToFloat32(%#04x) = %v, want %v", tc.half, got, tc.want)
		}
	}
	if got := float16ToFloat32(0x7c00); !math.IsInf(float64(got), 1) {
		t.Errorf("float16ToFloat32(0x7c00) = %v, want +Inf", got)
	}
	if got := float16ToFloat32(0x7e00); !math.IsNaN(float64(got)) {
		t.Errorf("float16ToFloat32(0x7e00) = %v, want NaN", got)
	}
}
