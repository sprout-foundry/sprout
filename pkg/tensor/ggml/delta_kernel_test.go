//go:build (darwin || linux) && (arm64 || amd64) && cgo && ggml

package ggml

import (
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// refGatedDelta computes the reference DeltaNet recurrence in Go, matching
// the GGML eager path (gatedDeltaStep4D) exactly: sequential left-to-right
// double accumulation for the Dk reductions, elementwise outer product for
// the state update, and F32 output. This is the ground truth for byte-identity.
func refGatedDelta(q, k, v, g, beta, state []float32, B, S, Hk, Hv, Dk, Dv int) ([]float32, []float32) {
	repeat := Hv / Hk

	// stateOut initialized from state (or zero).
	stateOut := make([]float32, len(state))
	copy(stateOut, state)

	y := make([]float32, B*S*Hv*Dv)

	for b := 0; b < B; b++ {
		for hv := 0; hv < Hv; hv++ {
			for dv := 0; dv < Dv; dv++ {
				hk := hv / repeat

				// Thread-local state for this (b,hv,dv) row, [B,Hv,Dv,Dk] row-major.
				locState := make([]float32, Dk)
				sIdx := b*Hv*Dv*Dk + hv*Dv*Dk + dv*Dk
				for dk := 0; dk < Dk; dk++ {
					locState[dk] = stateOut[sIdx+dk]
				}

				for t := 0; t < S; t++ {
					gt := g[b*S*Hv+t*Hv+hv]
					bt := beta[b*S*Hv+t*Hv+hv]

					// Decay.
					for dk := 0; dk < Dk; dk++ {
						locState[dk] *= gt
					}

					// kv_mem = sum_dk(state * k) — sequential double accumulation.
					kIdx := b*S*Hk*Dk + t*Hk*Dk + hk*Dk
					qIdx := b*S*Hk*Dk + t*Hk*Dk + hk*Dk
					vVal := v[b*S*Hv*Dv+t*Hv*Dv+hv*Dv+dv]

					var kvSum float64
					for dk := 0; dk < Dk; dk++ {
						kvSum += float64(locState[dk] * k[kIdx+dk])
					}
					kvMem := float32(kvSum)

					delta := (vVal - kvMem) * bt

					var ySum float64
					for dk := 0; dk < Dk; dk++ {
						locState[dk] += k[kIdx+dk] * delta
						ySum += float64(locState[dk] * q[qIdx+dk])
					}

					y[b*S*Hv*Dv+t*Hv*Dv+hv*Dv+dv] = float32(ySum)
				}

				// Write state_out.
				for dk := 0; dk < Dk; dk++ {
					stateOut[sIdx+dk] = locState[dk]
				}
			}
		}
	}
	return y, stateOut
}

// TestDeltaKernelByteIdentity verifies the fused GGML kernel matches the
// reference (gatedDeltaStep4D eager path) byte-for-byte.
func TestDeltaKernelByteIdentity(t *testing.T) {
	g := &GGMLBackend{}
	if !g.Available() {
		t.Skip("GGML backend not available")
	}
	var b tensor.Backend = g
	s, _ := b.DefaultGPUStream()

	type shape struct {
		B, S, Hk, Hv, Dk, Dv int
	}

	cases := []shape{
		{1, 1, 2, 4, 8, 8}, // single step, Hv/Hk=2
		{1, 4, 2, 4, 8, 8}, // multi-step, Hv/Hk=2
		{1, 2, 4, 4, 8, 8}, // Hk==Hv (no broadcast)
		{1, 3, 2, 6, 8, 8}, // Hv/Hk=3
	}

	for _, c := range cases {
		name := fmt.Sprintf("B%dS%dHk%dHv%dDk%dDv%d", c.B, c.S, c.Hk, c.Hv, c.Dk, c.Dv)
		t.Run(name, func(t *testing.T) {
			B, S, Hk, Hv, Dk, Dv := c.B, c.S, c.Hk, c.Hv, c.Dk, c.Dv

			qData := fill(B*S*Hk*Dk, 42)
			kData := fill(B*S*Hk*Dk, 137)
			vData := fill(B*S*Hv*Dv, 251)
			gData := fill(B*S*Hv, 311)
			betaData := fill(B*S*Hv, 521)
			stateData := fill(B*Hv*Dv*Dk, 673)

			// Reference Go computation.
			refY, refStateOut := refGatedDelta(qData, kData, vData, gData, betaData, stateData, B, S, Hk, Hv, Dk, Dv)

			// Fused GGML kernel.
			q := mustArray(b, qData, []int{B, S, Hk, Dk})
			k := mustArray(b, kData, []int{B, S, Hk, Dk})
			v := mustArray(b, vData, []int{B, S, Hv, Dv})
			gt := mustArray(b, gData, []int{B, S, Hv})
			beta := mustArray(b, betaData, []int{B, S, Hv})
			state := mustArray(b, stateData, []int{B, Hv, Dv, Dk})
			defer func() {
				q.Free()
				k.Free()
				v.Free()
				gt.Free()
				beta.Free()
				state.Free()
			}()

			candY, candStateOut, err := g.GatedDeltaUpdate(q, k, v, gt, beta, state, s)
			if err != nil {
				t.Fatalf("GatedDeltaUpdate: %v", err)
			}
			defer func() { candY.Free(); candStateOut.Free() }()

			// Compare shapes.
			shapeMatch(t, "y", candY.Shape(), []int{B, S, Hv, Dv})
			shapeMatch(t, "state", candStateOut.Shape(), []int{B, Hv, Dv, Dk})

			// Byte-identity: y (in q's dtype = F32) and state (F32).
			yRefB := f32toBytes(refY)
			yCandB, err := candY.RawBytes()
			if err != nil {
				t.Fatalf("cand y RawBytes: %v", err)
			}
			compareBytes(t, "y", yRefB, yCandB)

			sCand, err := candStateOut.Float32Data()
			if err != nil {
				t.Fatalf("cand state Float32Data: %v", err)
			}
			sRefB := f32toBytes(refStateOut)
			sCandB := f32toBytes(sCand)
			compareBytes(t, "state", sRefB, sCandB)
		})
	}
}

// TestDeltaKernelNilState verifies the fused kernel with state == nil
// (zero-initialized state) matches the reference.
func TestDeltaKernelNilState(t *testing.T) {
	g := &GGMLBackend{}
	if !g.Available() {
		t.Skip("GGML backend not available")
	}
	var b tensor.Backend = g
	s, _ := b.DefaultGPUStream()

	B, S, Hk, Hv, Dk, Dv := 1, 2, 2, 4, 8, 8

	qData := fill(B*S*Hk*Dk, 999)
	kData := fill(B*S*Hk*Dk, 888)
	vData := fill(B*S*Hv*Dv, 777)
	gData := fill(B*S*Hv, 666)
	betaData := fill(B*S*Hv, 555)
	stateData := make([]float32, B*Hv*Dv*Dk) // all zeros

	// Reference Go computation.
	refY, refStateOut := refGatedDelta(qData, kData, vData, gData, betaData, stateData, B, S, Hk, Hv, Dk, Dv)

	// Fused GGML kernel with nil state.
	q := mustArray(b, qData, []int{B, S, Hk, Dk})
	k := mustArray(b, kData, []int{B, S, Hk, Dk})
	v := mustArray(b, vData, []int{B, S, Hv, Dv})
	gt := mustArray(b, gData, []int{B, S, Hv})
	beta := mustArray(b, betaData, []int{B, S, Hv})
	defer func() { q.Free(); k.Free(); v.Free(); gt.Free(); beta.Free() }()

	candY, candStateOut, err := g.GatedDeltaUpdate(q, k, v, gt, beta, nil, s)
	if err != nil {
		t.Fatalf("GatedDeltaUpdate nil state: %v", err)
	}
	defer func() { candY.Free(); candStateOut.Free() }()

	shapeMatch(t, "y", candY.Shape(), []int{B, S, Hv, Dv})
	shapeMatch(t, "state", candStateOut.Shape(), []int{B, Hv, Dv, Dk})

	yRefB := f32toBytes(refY)
	yCandB, err := candY.RawBytes()
	if err != nil {
		t.Fatalf("cand y RawBytes: %v", err)
	}
	compareBytes(t, "y", yRefB, yCandB)

	sCand, err := candStateOut.Float32Data()
	if err != nil {
		t.Fatalf("cand state Float32Data: %v", err)
	}
	sRefB := f32toBytes(refStateOut)
	sCandB := f32toBytes(sCand)
	compareBytes(t, "state", sRefB, sCandB)
}

// shapeMatch fails if the two shapes differ.
func shapeMatch(t *testing.T, label string, got, want []int) {
	t.Helper()
	if !equalInts(got, want) {
		t.Errorf("%s shape = %v, want %v", label, got, want)
	}
}

// compareBytes checks byte-for-byte equality of two slices.
func compareBytes(t *testing.T, label string, a, b []byte) {
	t.Helper()
	if len(a) != len(b) {
		t.Fatalf("%s: length %d, want %d", label, len(a), len(b))
	}
	for i := 0; i+4 <= len(a); i += 4 {
		if a[i] != b[i] || a[i+1] != b[i+1] || a[i+2] != b[i+2] || a[i+3] != b[i+3] {
			vA := math.Float32frombits(binary.LittleEndian.Uint32(a[i : i+4]))
			vB := math.Float32frombits(binary.LittleEndian.Uint32(b[i : i+4]))
			t.Errorf("%s float32[%d] differs: got %g, want %g", label, i/4, vA, vB)
			return
		}
	}
}

// f32toBytes converts a float32 slice to its raw byte representation (LE).
func f32toBytes(f []float32) []byte {
	out := make([]byte, len(f)*4)
	for i, v := range f {
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(v))
	}
	return out
}

// mustArray creates a new array from float32 data, panicking on error.
func mustArray(b tensor.Backend, data []float32, shape []int) tensor.Array {
	arr, err := b.NewArrayFromFloat32(data, shape)
	if err != nil {
		panic(err)
	}
	return arr
}
