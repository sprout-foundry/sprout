//go:build (darwin || linux) && arm64 && cgo && ggml

package ggml

import (
	"math"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// deterministic pseudo-random fill, so failures reproduce exactly.
func fill(n int, seed uint32) []float32 {
	out := make([]float32, n)
	x := seed*2654435761 + 1
	for i := range out {
		x = x*1664525 + 1013904223
		out[i] = float32(int32(x>>8)%2000)/1000.0 - 1.0
	}
	return out
}

func idx4(shape []int, b, h, s, d int) int {
	return ((b*shape[1]+h)*shape[2]+s)*shape[3] + d
}

// refSDPA computes softmax(QK^T*scale + causalMask) @ V on [B,H,S,D] /
// [B,H,Skv,D] row-major data, matching the MLX convention the model layer uses.
func refSDPA(q, k, v []float32, qShape, kvShape []int, scale float32, causal bool) []float32 {
	B, H, S, D := qShape[0], qShape[1], qShape[2], qShape[3]
	Skv, Dv := kvShape[2], kvShape[3]
	out := make([]float32, B*H*S*Dv)
	offset := Skv - S
	for b := 0; b < B; b++ {
		for h := 0; h < H; h++ {
			for i := 0; i < S; i++ {
				scores := make([]float64, Skv)
				maxScore := math.Inf(-1)
				limit := Skv - 1
				if causal {
					limit = offset + i
				}
				for j := 0; j <= limit; j++ {
					var dot float64
					for d := 0; d < D; d++ {
						dot += float64(q[idx4(qShape, b, h, i, d)]) * float64(k[idx4(kvShape, b, h, j, d)])
					}
					scores[j] = dot * float64(scale)
					if scores[j] > maxScore {
						maxScore = scores[j]
					}
				}
				var sum float64
				for j := 0; j <= limit; j++ {
					scores[j] = math.Exp(scores[j] - maxScore)
					sum += scores[j]
				}
				for d := 0; d < Dv; d++ {
					var acc float64
					for j := 0; j <= limit; j++ {
						acc += scores[j] / sum * float64(v[idx4(kvShape, b, h, j, d)])
					}
					out[idx4([]int{B, H, S, Dv}, b, h, i, d)] = float32(acc)
				}
			}
		}
	}
	return out
}

func checkClose(t *testing.T, got, want []float32, tol float32, label string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: length %d, want %d", label, len(got), len(want))
	}
	worst := float32(0)
	worstAt := -1
	for i := range want {
		d := got[i] - want[i]
		if d < 0 {
			d = -d
		}
		if d > worst {
			worst, worstAt = d, i
		}
	}
	if worst > tol {
		t.Errorf("%s: max abs diff %g at index %d (got %g, want %g)",
			label, worst, worstAt, got[worstAt], want[worstAt])
	}
}

func TestSDPACausalMatchesReference(t *testing.T) {
	b := getBackend(t)
	s, _ := b.DefaultGPUStream()

	const B, H, S, D = 1, 4, 6, 8
	qShape := []int{B, H, S, D}
	qData := fill(B*H*S*D, 1)
	kData := fill(B*H*S*D, 2)
	vData := fill(B*H*S*D, 3)
	scale := float32(1.0 / math.Sqrt(float64(D)))

	q, _ := b.NewArrayFromFloat32(qData, qShape)
	k, _ := b.NewArrayFromFloat32(kData, qShape)
	v, _ := b.NewArrayFromFloat32(vData, qShape)

	got, err := b.FastScaledDotProductAttention(q, k, v, scale, "causal", nil, nil, s)
	if err != nil {
		t.Fatalf("SDPA: %v", err)
	}
	if shape := got.Shape(); len(shape) != 4 || shape[1] != H || shape[2] != S || shape[3] != D {
		t.Fatalf("SDPA shape = %v, want [%d %d %d %d]", shape, B, H, S, D)
	}
	data, err := got.Float32Data()
	if err != nil {
		t.Fatalf("Float32Data: %v", err)
	}
	checkClose(t, data, refSDPA(qData, kData, vData, qShape, qShape, scale, true), 1e-4, "causal SDPA")
}

// Decode: one query attending over a cached prefix, no mask.
func TestSDPADecodeMatchesReference(t *testing.T) {
	b := getBackend(t)
	s, _ := b.DefaultGPUStream()

	const B, H, D, Skv = 1, 4, 8, 7
	qShape := []int{B, H, 1, D}
	kvShape := []int{B, H, Skv, D}
	qData := fill(B*H*D, 4)
	kData := fill(B*H*Skv*D, 5)
	vData := fill(B*H*Skv*D, 6)
	scale := float32(1.0 / math.Sqrt(float64(D)))

	q, _ := b.NewArrayFromFloat32(qData, qShape)
	k, _ := b.NewArrayFromFloat32(kData, kvShape)
	v, _ := b.NewArrayFromFloat32(vData, kvShape)

	got, err := b.FastScaledDotProductAttention(q, k, v, scale, "", nil, nil, s)
	if err != nil {
		t.Fatalf("SDPA: %v", err)
	}
	data, err := got.Float32Data()
	if err != nil {
		t.Fatalf("Float32Data: %v", err)
	}
	checkClose(t, data, refSDPA(qData, kData, vData, qShape, kvShape, scale, false), 1e-4, "decode SDPA")
}

// refRoPE applies NEOX-style (half-split) rotary embedding to the first
// `dims` entries of each head vector in [B,H,S,D] data, passing the rest
// through unchanged (Qwen3.5 rotates only partial_rotary_factor of head_dim).
func refRoPE(x []float32, shape []int, dims int, base float64, offset int) []float32 {
	B, H, S := shape[0], shape[1], shape[2]
	out := append([]float32(nil), x...)
	half := dims / 2
	for b := 0; b < B; b++ {
		for h := 0; h < H; h++ {
			for si := 0; si < S; si++ {
				pos := float64(offset + si)
				for i := 0; i < half; i++ {
					theta := pos / math.Pow(base, 2*float64(i)/float64(dims))
					cos, sin := math.Cos(theta), math.Sin(theta)
					lo := x[idx4(shape, b, h, si, i)]
					hi := x[idx4(shape, b, h, si, i+half)]
					out[idx4(shape, b, h, si, i)] = float32(float64(lo)*cos - float64(hi)*sin)
					out[idx4(shape, b, h, si, i+half)] = float32(float64(lo)*sin + float64(hi)*cos)
				}
			}
		}
	}
	return out
}

// numHeads > seqLen is the case the old ne[1]<ne[2] heuristic silently skipped.
func TestRoPEMatchesReference(t *testing.T) {
	b := getBackend(t)
	s, _ := b.DefaultGPUStream()

	const B, H, S, D = 1, 8, 3, 16
	shape := []int{B, H, S, D}
	data := fill(B*H*S*D, 7)
	const offset = 5

	// 1e6/1e7 are Qwen3 / Qwen3.5 rope_theta values; 10000 is ggml_rope's
	// hardcoded default, so testing only that value would hide the base being
	// dropped. dims<D is Qwen3.5's partial rotary factor.
	for _, base := range []float64{10000.0, 1000000.0, 10000000.0} {
		for _, dims := range []int{D, D / 4} {
			x, _ := b.NewArrayFromFloat32(data, shape)
			got, err := b.FastRoPE(x, dims, false, base, 1.0, offset, nil, s)
			if err != nil {
				t.Fatalf("base %g dims %d: FastRoPE: %v", base, dims, err)
			}
			if gs := got.Shape(); len(gs) != 4 || gs[1] != H || gs[2] != S || gs[3] != D {
				t.Fatalf("base %g dims %d: RoPE shape = %v, want [%d %d %d %d]", base, dims, gs, B, H, S, D)
			}
			out, err := got.Float32Data()
			if err != nil {
				t.Fatalf("base %g dims %d: Float32Data: %v", base, dims, err)
			}
			checkClose(t, out, refRoPE(data, shape, dims, base, offset), 1e-4, "RoPE")
		}
	}
}

func TestRMSNormMatchesReference(t *testing.T) {
	b := getBackend(t)
	s, _ := b.DefaultGPUStream()

	const rows, cols = 3, 16
	data := fill(rows*cols, 8)
	weight := fill(cols, 9)
	const eps = 1e-6

	x, _ := b.NewArrayFromFloat32(data, []int{1, rows, cols})
	w, _ := b.NewArrayFromFloat32(weight, []int{cols})
	got, err := b.FastRMSNorm(x, w, eps, s)
	if err != nil {
		t.Fatalf("FastRMSNorm: %v", err)
	}
	out, err := got.Float32Data()
	if err != nil {
		t.Fatalf("Float32Data: %v", err)
	}

	want := make([]float32, rows*cols)
	for r := 0; r < rows; r++ {
		var ss float64
		for c := 0; c < cols; c++ {
			ss += float64(data[r*cols+c]) * float64(data[r*cols+c])
		}
		scale := 1.0 / math.Sqrt(ss/float64(cols)+eps)
		for c := 0; c < cols; c++ {
			want[r*cols+c] = float32(float64(data[r*cols+c]) * scale * float64(weight[c]))
		}
	}
	checkClose(t, out, want, 1e-4, "RMSNorm")
}

var _ tensor.Backend = (*GGMLBackend)(nil)

// The model layer no longer materialises KV heads (ExpandKVHeads) — the
// backend must broadcast Hkv key/value heads across their query-head group.
func TestSDPAGroupedQueryAttention(t *testing.T) {
	b := getBackend(t)
	s, _ := b.DefaultGPUStream()

	const B, H, HKV, S, D = 1, 8, 2, 5, 8
	qShape := []int{B, H, S, D}
	kvShape := []int{B, HKV, S, D}
	qData := fill(B*H*S*D, 31)
	kData := fill(B*HKV*S*D, 32)
	vData := fill(B*HKV*S*D, 33)
	scale := float32(1.0 / math.Sqrt(float64(D)))

	q, _ := b.NewArrayFromFloat32(qData, qShape)
	k, _ := b.NewArrayFromFloat32(kData, kvShape)
	v, _ := b.NewArrayFromFloat32(vData, kvShape)

	got, err := b.FastScaledDotProductAttention(q, k, v, scale, "causal", nil, nil, s)
	if err != nil {
		t.Fatalf("SDPA: %v", err)
	}
	if gs := got.Shape(); len(gs) != 4 || gs[1] != H || gs[2] != S || gs[3] != D {
		t.Fatalf("shape = %v, want [%d %d %d %d]", gs, B, H, S, D)
	}
	data, err := got.Float32Data()
	if err != nil {
		t.Fatalf("Float32Data: %v", err)
	}

	// Reference: expand each KV head across its group of H/HKV query heads.
	expK := make([]float32, B*H*S*D)
	expV := make([]float32, B*H*S*D)
	group := H / HKV
	for h := 0; h < H; h++ {
		src := (h / group) * S * D
		copy(expK[h*S*D:(h+1)*S*D], kData[src:src+S*D])
		copy(expV[h*S*D:(h+1)*S*D], vData[src:src+S*D])
	}
	checkClose(t, data, refSDPA(qData, expK, expV, qShape, qShape, scale, true), 1e-4, "GQA SDPA")
}
