//go:build cgo && ((darwin && arm64) || (linux && ggml && (arm64 || amd64)))

package qwen35

import (
	"fmt"
	"os"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// scaleRMSNorm applies MLX rms_norm (no weight, eps=1e-6) then scales by s.
// Used for the q/k normalization in DeltaNet, which normalizes over the
// head dimension with per-head scale.
func scaleRMSNorm(x tensor.Array, s float32, b tensor.Backend, stream tensor.Stream) (tensor.Array, error) {
	n, err := llm.RMSNorm(x, nil, 1e-6, b, stream)
	if err != nil {
		return nil, err
	}
	defer n.Free()
	scalar, err := b.NewArrayFromFloat32([]float32{s}, []int{1})
	if err != nil {
		return nil, err
	}
	defer scalar.Free()
	return b.Multiply(n, scalar, stream)
}

// decayGate computes g = exp(-exp(A_log) * softplus(a + dt_bias)).
// A_log and dt_bias are [Hv] (per-value-head); a is [B, S, Hv] (per-head).
// When Metal is available it uses a single fused kernel; otherwise falls back
// to the multi-op fp32 path.
func decayGate(aLog, a, dtBias tensor.Array, b tensor.Backend, stream tensor.Stream) (tensor.Array, error) {
	// Fast path: single fused Metal kernel.
	if b.Available() {
		g, err := fusedComputeG(aLog, a, dtBias, b, stream)
		if err == nil {
			return g, nil
		}
	}
	return decayGateOps(aLog, a, dtBias, b, stream)
}

// decayGateOps is decayGate's multi-op fp32 mirror — the same math as the
// fused kernel (softplus via log1p(exp)), for callers that must avoid custom
// Metal kernels (the compiled decode closure traces plain ops only).
func decayGateOps(aLog, a, dtBias tensor.Array, b tensor.Backend, stream tensor.Stream) (tensor.Array, error) {
	aF32, err := b.AsType(a, tensor.Float32, stream)
	if err != nil {
		return nil, err
	}
	defer aF32.Free()

	aLogF32, err := b.AsType(aLog, tensor.Float32, stream)
	if err != nil {
		return nil, err
	}
	defer aLogF32.Free()

	dtBiasF32, err := b.AsType(dtBias, tensor.Float32, stream)
	if err != nil {
		return nil, err
	}
	defer dtBiasF32.Free()

	// softplus(a + dt_bias) in fp32.
	added, err := b.Add(aF32, dtBiasF32, stream)
	if err != nil {
		return nil, err
	}
	defer added.Free()
	sp, err := llm.Softplus(added, b, stream)
	if err != nil {
		return nil, err
	}
	defer sp.Free()

	// exp(A_log) — A_log is [Hv]; broadcasts over [B,S,Hv].
	expALog, err := b.Exp(aLogF32, stream)
	if err != nil {
		return nil, err
	}
	defer expALog.Free()

	// exp(A_log) * softplus(a + dt_bias)
	mul, err := b.Multiply(expALog, sp, stream)
	if err != nil {
		return nil, err
	}
	defer mul.Free()

	// -mul, then exp => g (stays fp32 — the recurrence and kernel consume it).
	neg, err := b.Negative(mul, stream)
	if err != nil {
		return nil, err
	}
	defer neg.Free()
	return b.Exp(neg, stream)
}

// rmsNormGated computes out = rms_norm(y, w, eps) * silu(z), with the silu and
// the product evaluated in fp32, mirroring mlx-lm's Qwen3NextRMSNormGated
// (_precise_swiglu). y and z are [B, S, Hv, Dv]; w is [head_v_dim] and
// broadcasts over the Hv axis. The result is cast back to y's dtype.
func rmsNormGated(y, z, w tensor.Array, b tensor.Backend, stream tensor.Stream) (tensor.Array, error) {
	// x = rms_norm(y, w, eps) — weight [Dv] broadcasts over Hv.
	x, err := b.FastRMSNorm(y, w, 1e-6, stream)
	if err != nil {
		return nil, fmt.Errorf("rms_norm: %w", err)
	}
	defer x.Free()

	// gate = silu(z) computed in fp32.
	zF32, err := b.AsType(z, tensor.Float32, stream)
	if err != nil {
		return nil, fmt.Errorf("z->fp32: %w", err)
	}
	defer zF32.Free()
	gate, err := llm.SiLU(zF32, b, stream)
	if err != nil {
		return nil, fmt.Errorf("silu: %w", err)
	}
	defer gate.Free()

	// x in fp32, product, cast back to y's dtype.
	xF32, err := b.AsType(x, tensor.Float32, stream)
	if err != nil {
		return nil, fmt.Errorf("x->fp32: %w", err)
	}
	defer xF32.Free()
	prod, err := b.Multiply(gate, xF32, stream)
	if err != nil {
		return nil, fmt.Errorf("gated mul: %w", err)
	}
	defer prod.Free()
	return b.AsType(prod, y.Dtype(), stream)
}

// freeAll releases every array in ys. Safe on nil/empty slices.
func freeAll(ys []tensor.Array) {
	for _, a := range ys {
		a.Free()
	}
}

// fusedDeltaNet is the optional fused DeltaNet capability for backends
// that can run the full recurrence in a single kernel (e.g. GGML on Linux).
type fusedDeltaNet interface {
	GatedDeltaUpdate(q, k, v, g, beta, state tensor.Array, s tensor.Stream) (tensor.Array, tensor.Array, error)
}

// gatedDeltaUpdate implements the Gated DeltaNet recurrence over a full
// sequence. On Apple Silicon (Metal available) it dispatches to a single
// fused Metal kernel (fusedGatedDeltaUpdate) that scans the whole sequence in
// one launch; on Linux with GGML it dispatches to the GGML fused kernel if
// available; otherwise it falls back to the exact sequential per-step ops
// loop below (mirroring mlx-lm's gated_delta_ops reference implementation).
//
// Inputs (batch B):
//
//	q, k: [B, S, Hk, Dk]
//	v:    [B, S, Hv, Dv]
//	g:    [B, S, Hv]  (per value head decay)
//	beta: [B, S, Hv]
//	state: [B, Hv, Dv, Dk] or nil for the first call
//
// Returns y [B, S, Hv, Dv] and the final state [B, Hv, Dv, Dk].
//
// Per step the rule is (q/k broadcast from Hk to Hv heads):
//
//	state = state * g_t                        [B,Hv,Dv,Dk]
//	kv_mem = sum_dk(state * k_t)               [B,Hv,Dv]
//	delta = (v_t - kv_mem) * beta_t            [B,Hv,Dv]
//	state = state + k_t * delta                [B,Hv,Dv,Dk]
//	y_t = sum_dk(state * q_t)                  [B,Hv,Dv]
var deltaPathLogged = map[string]bool{}

func gatedDeltaUpdate(q, k, v, g, beta, state tensor.Array, b tensor.Backend, stream tensor.Stream) (tensor.Array, tensor.Array, error) {
	// Fused kernel path for backends that support it (e.g. GGML on Linux).
	// SPROUT_GGML_DELTA_FUSED=0 disables it to A/B against the eager path.
	if os.Getenv("SPROUT_GGML_DELTA_FUSED") != "0" {
		if fd, ok := b.(fusedDeltaNet); ok {
			y, ns, err := fd.GatedDeltaUpdate(q, k, v, g, beta, state, stream)
			if err == nil {
				return y, ns, nil
			}
		}
	}

	// Fast path: one fused Metal kernel launch when available.
	if b.Available() && os.Getenv("SPROUT_DELTA_OPS") == "" {
		y, ns, err := fusedGatedDeltaUpdate(q, k, v, g, beta, state, b, stream)
		if err == nil {
			if os.Getenv("SPROUT_LOCAL_DEBUG") == "1" && !deltaPathLogged["fused"] {
				deltaPathLogged["fused"] = true
				fmt.Printf("delta: using FUSED kernel path (shape q=%v)\n", q.Shape())
			}
			return y, ns, nil
		}
		if os.Getenv("SPROUT_LOCAL_DEBUG") == "1" && !deltaPathLogged["fallback-err"] {
			deltaPathLogged["fallback-err"] = true
			fmt.Printf("delta: fused kernel FAILED (%v), falling back to ops loop\n", err)
		}
		// Fall through to the sequential ops loop on any kernel error
		// (uncompilable shape, Metal hiccup, etc.).
	}
	if os.Getenv("SPROUT_LOCAL_DEBUG") == "1" && !deltaPathLogged["ops"] {
		deltaPathLogged["ops"] = true
		fmt.Printf("delta: using SEQUENTIAL ops-loop path (shape q=%v)\n", q.Shape())
	}
	return gatedDeltaUpdateOps(q, k, v, g, beta, state, b, stream)
}

// gatedDeltaUpdateOps is the exact sequential per-step scan. It mirrors
// mlx-lm's gated_delta_ops reference implementation (matches the fused GPU
// kernel's math) and is used when Metal is unavailable.
func gatedDeltaUpdateOps(q, k, v, g, beta, state tensor.Array, b tensor.Backend, stream tensor.Stream) (tensor.Array, tensor.Array, error) {
	qs := q.Shape()
	B, S, Hk, Dk := qs[0], qs[1], qs[2], qs[3]
	vs := v.Shape()
	Hv, Dv := vs[2], vs[3]

	// Broadcast q/k from Hk to Hv heads (repeat factor Hv/Hk), matching
	// gated_delta_ops.
	repeat := Hv / Hk
	var qR, kR tensor.Array
	var err error
	if repeat > 1 {
		qR, err = b.RepeatAxis(q, repeat, 2, stream)
		if err != nil {
			return nil, nil, fmt.Errorf("repeat q: %w", err)
		}
		defer qR.Free()
		kR, err = b.RepeatAxis(k, repeat, 2, stream)
		if err != nil {
			return nil, nil, fmt.Errorf("repeat k: %w", err)
		}
		defer kR.Free()
	} else {
		qR, kR = q, k
	}

	var cur tensor.Array
	curOwned := false
	if state != nil {
		cur = state // borrowed: caller owns it
	} else {
		// The recurrence accumulates; the MLX reference uses an fp32 state even
		// when q/k/v are bf16. A bf16 state would destroy delta-rule precision.
		cur, err = b.Zeros([]int{B, Hv, Dv, Dk}, tensor.Float32, stream)
		if err != nil {
			return nil, nil, fmt.Errorf("zero state: %w", err)
		}
		curOwned = true
	}

	ys := make([]tensor.Array, 0, S)
	for t := 0; t < S; t++ {
		// qR/kR are [B, S, Hv, Dk] after the repeat — slice with Hv, not Hk.
		qt, err := sliceStep(qR, t, B, Hv, Dk, b, stream)
		if err != nil {
			freeAll(ys)
			if curOwned {
				cur.Free()
			}
			return nil, nil, err
		}
		kt, err := sliceStep(kR, t, B, Hv, Dk, b, stream)
		if err != nil {
			qt.Free()
			freeAll(ys)
			if curOwned {
				cur.Free()
			}
			return nil, nil, err
		}
		vt, err := sliceStep(v, t, B, Hv, Dv, b, stream)
		if err != nil {
			qt.Free()
			kt.Free()
			freeAll(ys)
			if curOwned {
				cur.Free()
			}
			return nil, nil, err
		}
		gt, err := sliceHead(g, t, B, Hv, b, stream)
		if err != nil {
			qt.Free()
			kt.Free()
			vt.Free()
			freeAll(ys)
			if curOwned {
				cur.Free()
			}
			return nil, nil, fmt.Errorf("sliceHead g: t=%d B=%d Hv=%d: %w", t, B, Hv, err)
		}
		bt, err := sliceHead(beta, t, B, Hv, b, stream)
		if err != nil {
			qt.Free()
			kt.Free()
			vt.Free()
			gt.Free()
			freeAll(ys)
			if curOwned {
				cur.Free()
			}
			return nil, nil, err
		}

		yt, next, err := gatedDeltaStep(cur, qt, kt, vt, gt, bt, B, Hv, Dv, Dk, b, stream)
		qt.Free()
		kt.Free()
		vt.Free()
		gt.Free()
		bt.Free()
		if err != nil {
			freeAll(ys)
			if curOwned {
				cur.Free()
			}
			return nil, nil, err
		}
		// Release the previous state (zero-state or prior step's next) now
		// that the step has read it; the final next is returned to the caller.
		if curOwned {
			cur.Free()
		}
		cur = next
		curOwned = true
		ys = append(ys, yt)
	}

	// Concatenate [B,1,Hv,Dv] steps into [B,S,Hv,Dv].
	y, err := b.ConcatenateAxis(ys, 1, stream)
	freeAll(ys)
	if err != nil {
		return nil, nil, fmt.Errorf("concat y: %w", err)
	}
	return y, cur, nil
}

// sliceStep slices a [B, S, H, D] tensor at position t → [B, 1, H, D].
func sliceStep(x tensor.Array, t, B, H, D int, b tensor.Backend, stream tensor.Stream) (tensor.Array, error) {
	return b.Slice(x, []int{0, t, 0, 0}, []int{B, t + 1, H, D}, []int{1, 1, 1, 1}, stream)
}

// sliceHead slices a [B, S, H] tensor at position t → [B, 1, H].
func sliceHead(x tensor.Array, t, B, H int, b tensor.Backend, stream tensor.Stream) (tensor.Array, error) {
	return b.Slice(x, []int{0, t, 0}, []int{B, t + 1, H}, []int{1, 1, 1}, stream)
}

// gatedDeltaStep performs one recurrent step. Inputs are per-step:
//
//	state [B, Hv, Dv, Dk] (borrowed or owned)
//	q, k  [B, 1, Hv, Dk]   (already head-broadcast)
//	v     [B, 1, Hv, Dv]
//	g, beta [B, 1, Hv]
//
// Returns y [B, 1, Hv, Dv] and the updated state [B, Hv, Dv, Dk]. Both are
// freshly allocated and owned by the caller.
func gatedDeltaStep(state, q, k, v, g, beta tensor.Array, B, Hv, Dv, Dk int, b tensor.Backend, stream tensor.Stream) (tensor.Array, tensor.Array, error) {
	// GGML supports at most 4D tensors and broadcasts natively via ne=1
	// dims in mul. Use the 4D path for non-MLX backends; the MLX path uses
	// 5D reshapes to broadcast.
	if !b.NativeQuantization() {
		return gatedDeltaStep4D(state, q, k, v, g, beta, B, Hv, Dv, Dk, b, stream)
	}

	// g_t [B,1,Hv] → [B,1,Hv,1,1] to broadcast with state [B,Hv,Dv,Dk].
	g5, err := b.Reshape(g, []int{B, 1, Hv, 1, 1}, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("g reshape: %w", err)
	}
	defer g5.Free()

	// Reshape state to [B,1,Hv,Dv,Dk] and broadcast g5 over it.
	s5, err := b.Reshape(state, []int{B, 1, Hv, Dv, Dk}, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("state reshape: %w", err)
	}
	defer s5.Free()

	decayed, err := b.Multiply(s5, g5, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("decay mul: %w", err)
	}
	defer decayed.Free()

	// k_t [B,1,Hv,Dk] → [B,1,Hv,1,Dk]
	k5, err := b.Reshape(k, []int{B, 1, Hv, 1, Dk}, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("k reshape: %w", err)
	}
	defer k5.Free()

	// kv_mem = sum_dk(decayed * k_t) → [B,1,Hv,Dv]
	prod, err := b.Multiply(decayed, k5, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("kv_mem prod: %w", err)
	}
	defer prod.Free()
	kvMem, err := b.Sum(prod, []int{4}, false, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("kv_mem sum: %w", err)
	}
	defer kvMem.Free()

	// delta = (v_t - kv_mem) * beta_t → [B,1,Hv,Dv]
	diff, err := b.Subtract(v, kvMem, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("v - kv_mem: %w", err)
	}
	defer diff.Free()
	// beta [B,1,Hv] → [B,1,Hv,1]
	b4, err := b.Reshape(beta, []int{B, 1, Hv, 1}, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("beta reshape: %w", err)
	}
	defer b4.Free()
	delta, err := b.Multiply(diff, b4, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("delta mul: %w", err)
	}
	defer delta.Free()

	// state += k_t * delta → [B,1,Hv,Dv,Dk] (delta [B,1,Hv,Dv] → [B,1,Hv,Dv,1])
	d5, err := b.Reshape(delta, []int{B, 1, Hv, Dv, 1}, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("delta reshape: %w", err)
	}
	defer d5.Free()
	outer, err := b.Multiply(k5, d5, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("outer: %w", err)
	}
	defer outer.Free()
	next5, err := b.Add(decayed, outer, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("state add: %w", err)
	}
	defer next5.Free()

	// y = sum_dk(state * q_t) → [B,1,Hv,Dv]
	// q_t [B,1,Hv,Dk] → [B,1,Hv,1,Dk]
	q5, err := b.Reshape(q, []int{B, 1, Hv, 1, Dk}, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("q reshape: %w", err)
	}
	defer q5.Free()
	yProd, err := b.Multiply(next5, q5, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("y prod: %w", err)
	}
	defer yProd.Free()
	y, err := b.Sum(yProd, []int{4}, false, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("y sum: %w", err)
	}

	// The reference casts y back to q's dtype (bf16) before the gated norm
	// and quantized out_proj — y would otherwise stay fp32 (state dtype).
	yCast, err := b.AsType(y, q.Dtype(), stream)
	y.Free()
	if err != nil {
		return nil, nil, fmt.Errorf("y dtype: %w", err)
	}

	// next (a reshape of next5) and y are fresh arrays; the caller owns both.
	next, err := b.Reshape(next5, []int{B, Hv, Dv, Dk}, stream)
	if err != nil {
		yCast.Free()
		return nil, nil, fmt.Errorf("state unflatten: %w", err)
	}
	return yCast, next, nil
}

// gatedDeltaStep4D is the GGML-compatible recurrence step. GGML tensors are
// at most 4D and ggml_mul broadcasts natively via ne=1 dims, so the 5D
// broadcast reshapes ([B,1,Hv,1,1] etc.) are replaced with 4D shapes that
// carry the same logical layout:
//
//	state [B,Hv,Dv,Dk]; g [B,1,Hv]   → g reshaped [B,Hv,1,1]  broadcasts over Dv,Dk
//	k     [B,1,Hv,Dk];  decayed 4D   → k reshaped [B,Hv,1,Dk]  broadcasts over Dv
//	beta  [B,1,Hv];     diff [B,Hv,Dv] → beta [B,Hv,1]         broadcasts over Dv
//	delta [B,Hv,Dv];    k5 [B,Hv,1,Dk] → delta [B,Hv,Dv,1]     broadcasts over Dk
//	q     [B,1,Hv,Dk];  next [B,Hv,Dv,Dk] → q [B,Hv,1,Dk]      broadcasts over Dv
func gatedDeltaStep4D(state, q, k, v, g, beta tensor.Array, B, Hv, Dv, Dk int, b tensor.Backend, stream tensor.Stream) (tensor.Array, tensor.Array, error) {
	// g [B,1,Hv] → [B,Hv,1,1]; state stays [B,Hv,Dv,Dk].
	g4, err := b.Reshape(g, []int{B, Hv, 1, 1}, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("g reshape: %w", err)
	}
	defer g4.Free()

	decayed, err := b.Multiply(state, g4, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("decay mul: %w", err)
	}
	defer decayed.Free()

	// k [B,1,Hv,Dk] → [B,Hv,1,Dk]; broadcast over Dv in the product.
	k4, err := b.Reshape(k, []int{B, Hv, 1, Dk}, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("k reshape: %w", err)
	}
	defer k4.Free()

	prod, err := b.Multiply(decayed, k4, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("kv_mem prod: %w", err)
	}
	defer prod.Free()
	kvMem, err := b.Sum(prod, []int{3}, false, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("kv_mem sum: %w", err)
	}
	defer kvMem.Free()

	// v [B,1,Hv,Dv] → [B,Hv,Dv] (drop the singleton S dim), subtract kvMem.
	v3, err := b.Reshape(v, []int{B, Hv, Dv}, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("v reshape: %w", err)
	}
	defer v3.Free()
	diff, err := b.Subtract(v3, kvMem, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("v - kv_mem: %w", err)
	}
	defer diff.Free()
	// beta [B,1,Hv] → [B,Hv,1]; broadcast over Dv.
	b4, err := b.Reshape(beta, []int{B, Hv, 1}, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("beta reshape: %w", err)
	}
	defer b4.Free()
	delta, err := b.Multiply(diff, b4, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("delta mul: %w", err)
	}
	defer delta.Free()

	// delta [B,Hv,Dv] → [B,Hv,Dv,1]; the outer product with k4 [B,Hv,1,Dk]
	// is a batched matmul, not an elementwise mul after a Dk-way repeat
	// (the repeat materialised [B,Hv,Dv,Dk] worth of delta, Dk× waste).
	d4, err := b.Reshape(delta, []int{B, Hv, Dv, 1}, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("delta reshape: %w", err)
	}
	defer d4.Free()
	outer, err := b.MatMul(d4, k4, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("outer: %w", err)
	}
	defer outer.Free()
	next4, err := b.Add(decayed, outer, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("state add: %w", err)
	}
	defer next4.Free()

	// y = sum_dk(state * q_t): q [B,1,Hv,Dk] → [B,Hv,1,Dk]; broadcast over Dv.
	q4, err := b.Reshape(q, []int{B, Hv, 1, Dk}, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("q reshape: %w", err)
	}
	defer q4.Free()
	yProd, err := b.Multiply(next4, q4, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("y prod: %w", err)
	}
	defer yProd.Free()
	y, err := b.Sum(yProd, []int{3}, false, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("y sum: %w", err)
	}

	yCast, err := b.AsType(y, q.Dtype(), stream)
	y.Free()
	if err != nil {
		return nil, nil, fmt.Errorf("y dtype: %w", err)
	}

	// The sum drops Dk, leaving [B,Hv,Dv]. Restore the step axis so this path
	// returns [B,1,Hv,Dv] like the 5D one: the caller concatenates the steps
	// along axis 1 and then pairs the result with a [B,S,Hv,Dv] gate.
	yStep, err := b.Reshape(yCast, []int{B, 1, Hv, Dv}, stream)
	yCast.Free()
	if err != nil {
		return nil, nil, fmt.Errorf("y step reshape: %w", err)
	}

	next, err := b.Reshape(next4, []int{B, Hv, Dv, Dk}, stream)
	if err != nil {
		yStep.Free()
		return nil, nil, fmt.Errorf("state unflatten: %w", err)
	}
	return yStep, next, nil
}
