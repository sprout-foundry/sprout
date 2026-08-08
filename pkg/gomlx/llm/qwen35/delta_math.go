//go:build darwin && arm64 && cgo && mlx

package qwen35

import (
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
)

// scaleRMSNorm applies MLX rms_norm (no weight, eps=1e-6) then scales by s.
// Used for the q/k normalization in DeltaNet, which normalizes over the
// head dimension with per-head scale.
func scaleRMSNorm(x *mlx.Array, s float32, stream *mlx.Stream) (*mlx.Array, error) {
	n, err := llm.RMSNorm(x, nil, 1e-6, stream)
	if err != nil {
		return nil, err
	}
	defer n.Free()
	scalar, err := mlx.NewArrayFromFloat32([]float32{s}, []int{1})
	if err != nil {
		return nil, err
	}
	defer scalar.Free()
	return mlx.Multiply(n, scalar, stream)
}

// decayGate computes g = exp(-exp(A_log) * softplus(a + dt_bias)).
// A_log and dt_bias are [Hv] (per-value-head); a is [B, S, Hv] (per-head).
func decayGate(aLog, a, dtBias *mlx.Array, stream *mlx.Stream) (*mlx.Array, error) {
	// softplus(a + dt_bias)
	added, err := mlx.Add(a, dtBias, stream)
	if err != nil {
		return nil, err
	}
	defer added.Free()
	sp, err := llm.Softplus(added, stream)
	if err != nil {
		return nil, err
	}
	defer sp.Free()

	// exp(A_log) — A_log is [Hv]; broadcasts over [B,S,Hv].
	expALog, err := mlx.Exp(aLog, stream)
	if err != nil {
		return nil, err
	}
	defer expALog.Free()

	// exp(A_log) * softplus(a + dt_bias)
	mul, err := mlx.Multiply(expALog, sp, stream)
	if err != nil {
		return nil, err
	}
	defer mul.Free()

	// -mul, then exp => g
	neg, err := mlx.Negative(mul, stream)
	if err != nil {
		return nil, err
	}
	defer neg.Free()
	return mlx.Exp(neg, stream)
}

// rmsNormGated computes out = rms_norm(y, w, eps) * silu(z), with the silu and
// the product evaluated in fp32, mirroring mlx-lm's Qwen3NextRMSNormGated
// (_precise_swiglu). y and z are [B, S, Hv, Dv]; w is [head_v_dim] and
// broadcasts over the Hv axis. The result is cast back to y's dtype.
func rmsNormGated(y, z, w *mlx.Array, stream *mlx.Stream) (*mlx.Array, error) {
	// x = rms_norm(y, w, eps) — weight [Dv] broadcasts over Hv.
	x, err := mlx.FastRMSNorm(y, w, 1e-6, stream)
	if err != nil {
		return nil, fmt.Errorf("rms_norm: %w", err)
	}
	defer x.Free()

	// gate = silu(z) computed in fp32.
	zF32, err := mlx.AsType(z, mlx.Float32, stream)
	if err != nil {
		return nil, fmt.Errorf("z->fp32: %w", err)
	}
	defer zF32.Free()
	gate, err := llm.SiLU(zF32, stream)
	if err != nil {
		return nil, fmt.Errorf("silu: %w", err)
	}
	defer gate.Free()

	// x in fp32, product, cast back to y's dtype.
	xF32, err := mlx.AsType(x, mlx.Float32, stream)
	if err != nil {
		return nil, fmt.Errorf("x->fp32: %w", err)
	}
	defer xF32.Free()
	prod, err := mlx.Multiply(gate, xF32, stream)
	if err != nil {
		return nil, fmt.Errorf("gated mul: %w", err)
	}
	defer prod.Free()
	return mlx.AsType(prod, y.Dtype(), stream)
}

// freeAll releases every array in ys. Safe on nil/empty slices.
func freeAll(ys []*mlx.Array) {
	for _, a := range ys {
		a.Free()
	}
}

// gatedDeltaUpdate implements the Gated DeltaNet recurrence over a full
// sequence. On Apple Silicon (Metal available) it dispatches to a single
// fused Metal kernel (fusedGatedDeltaUpdate) that scans the whole sequence in
// one launch; otherwise it falls back to the exact sequential per-step ops
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
func gatedDeltaUpdate(q, k, v, g, beta, state *mlx.Array, stream *mlx.Stream) (*mlx.Array, *mlx.Array, error) {
	// Fast path: one fused Metal kernel launch when available.
	if mlx.Available() {
		y, ns, err := fusedGatedDeltaUpdate(q, k, v, g, beta, state, stream)
		if err == nil {
			return y, ns, nil
		}
		// Fall through to the sequential ops loop on any kernel error
		// (uncompilable shape, Metal hiccup, etc.).
	}
	return gatedDeltaUpdateOps(q, k, v, g, beta, state, stream)
}

// gatedDeltaUpdateOps is the exact sequential per-step scan. It mirrors
// mlx-lm's gated_delta_ops reference implementation (matches the fused GPU
// kernel's math) and is used when Metal is unavailable.
func gatedDeltaUpdateOps(q, k, v, g, beta, state *mlx.Array, stream *mlx.Stream) (*mlx.Array, *mlx.Array, error) {
	qs := q.Shape()
	B, S, Hk, Dk := qs[0], qs[1], qs[2], qs[3]
	vs := v.Shape()
	Hv, Dv := vs[2], vs[3]

	// Broadcast q/k from Hk to Hv heads (repeat factor Hv/Hk), matching
	// gated_delta_ops.
	repeat := Hv / Hk
	var qR, kR *mlx.Array
	var err error
	if repeat > 1 {
		qR, err = mlx.RepeatAxis(q, repeat, 2, stream)
		if err != nil {
			return nil, nil, fmt.Errorf("repeat q: %w", err)
		}
		defer qR.Free()
		kR, err = mlx.RepeatAxis(k, repeat, 2, stream)
		if err != nil {
			return nil, nil, fmt.Errorf("repeat k: %w", err)
		}
		defer kR.Free()
	} else {
		qR, kR = q, k
	}

	var cur *mlx.Array
	curOwned := false
	if state != nil {
		cur = state // borrowed: caller owns it
	} else {
		// The recurrence accumulates; the MLX reference uses an fp32 state even
		// when q/k/v are bf16. A bf16 state would destroy delta-rule precision.
		cur, err = mlx.Zeros([]int{B, Hv, Dv, Dk}, mlx.Float32, stream)
		if err != nil {
			return nil, nil, fmt.Errorf("zero state: %w", err)
		}
		curOwned = true
	}

	ys := make([]*mlx.Array, 0, S)
	for t := 0; t < S; t++ {
		// qR/kR are [B, S, Hv, Dk] after the repeat — slice with Hv, not Hk.
		qt, err := sliceStep(qR, t, B, Hv, Dk, stream)
		if err != nil {
			freeAll(ys)
			if curOwned {
				cur.Free()
			}
			return nil, nil, err
		}
		kt, err := sliceStep(kR, t, B, Hv, Dk, stream)
		if err != nil {
			qt.Free()
			freeAll(ys)
			if curOwned {
				cur.Free()
			}
			return nil, nil, err
		}
		vt, err := sliceStep(v, t, B, Hv, Dv, stream)
		if err != nil {
			qt.Free()
			kt.Free()
			freeAll(ys)
			if curOwned {
				cur.Free()
			}
			return nil, nil, err
		}
		gt, err := sliceHead(g, t, B, Hv, stream)
		if err != nil {
			qt.Free()
			kt.Free()
			vt.Free()
			freeAll(ys)
			if curOwned {
				cur.Free()
			}
			return nil, nil, err
		}
		bt, err := sliceHead(beta, t, B, Hv, stream)
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

		yt, next, err := gatedDeltaStep(cur, qt, kt, vt, gt, bt, B, Hv, Dv, Dk, stream)
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
	y, err := mlx.ConcatenateAxis(ys, 1, stream)
	freeAll(ys)
	if err != nil {
		return nil, nil, fmt.Errorf("concat y: %w", err)
	}
	return y, cur, nil
}

// sliceStep slices a [B, S, H, D] tensor at position t → [B, 1, H, D].
func sliceStep(x *mlx.Array, t, B, H, D int, stream *mlx.Stream) (*mlx.Array, error) {
	return mlx.Slice(x, []int{0, t, 0, 0}, []int{B, t + 1, H, D}, []int{1, 1, 1, 1}, stream)
}

// sliceHead slices a [B, S, H] tensor at position t → [B, 1, H].
func sliceHead(x *mlx.Array, t, B, H int, stream *mlx.Stream) (*mlx.Array, error) {
	return mlx.Slice(x, []int{0, t, 0}, []int{B, t + 1, H}, []int{1, 1, 1}, stream)
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
func gatedDeltaStep(state, q, k, v, g, beta *mlx.Array, B, Hv, Dv, Dk int, stream *mlx.Stream) (*mlx.Array, *mlx.Array, error) {
	// g_t [B,1,Hv] → [B,1,Hv,1,1] to broadcast with state [B,Hv,Dv,Dk].
	g5, err := mlx.Reshape(g, []int{B, 1, Hv, 1, 1}, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("g reshape: %w", err)
	}
	defer g5.Free()

	// Reshape state to [B,1,Hv,Dv,Dk] and broadcast g5 over it.
	s5, err := mlx.Reshape(state, []int{B, 1, Hv, Dv, Dk}, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("state reshape: %w", err)
	}
	defer s5.Free()

	decayed, err := mlx.Multiply(s5, g5, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("decay mul: %w", err)
	}
	defer decayed.Free()

	// k_t [B,1,Hv,Dk] → [B,1,Hv,1,Dk]
	k5, err := mlx.Reshape(k, []int{B, 1, Hv, 1, Dk}, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("k reshape: %w", err)
	}
	defer k5.Free()

	// kv_mem = sum_dk(decayed * k_t) → [B,1,Hv,Dv]
	prod, err := mlx.Multiply(decayed, k5, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("kv_mem prod: %w", err)
	}
	defer prod.Free()
	kvMem, err := mlx.Sum(prod, []int{4}, false, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("kv_mem sum: %w", err)
	}
	defer kvMem.Free()

	// delta = (v_t - kv_mem) * beta_t → [B,1,Hv,Dv]
	diff, err := mlx.Subtract(v, kvMem, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("v - kv_mem: %w", err)
	}
	defer diff.Free()
	// beta [B,1,Hv] → [B,1,Hv,1]
	b4, err := mlx.Reshape(beta, []int{B, 1, Hv, 1}, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("beta reshape: %w", err)
	}
	defer b4.Free()
	delta, err := mlx.Multiply(diff, b4, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("delta mul: %w", err)
	}
	defer delta.Free()

	// state += k_t * delta → [B,1,Hv,Dv,Dk] (delta [B,1,Hv,Dv] → [B,1,Hv,Dv,1])
	d5, err := mlx.Reshape(delta, []int{B, 1, Hv, Dv, 1}, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("delta reshape: %w", err)
	}
	defer d5.Free()
	outer, err := mlx.Multiply(k5, d5, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("outer: %w", err)
	}
	defer outer.Free()
	next5, err := mlx.Add(decayed, outer, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("state add: %w", err)
	}
	defer next5.Free()

	// y = sum_dk(state * q_t) → [B,1,Hv,Dv]
	// q_t [B,1,Hv,Dk] → [B,1,Hv,1,Dk]
	q5, err := mlx.Reshape(q, []int{B, 1, Hv, 1, Dk}, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("q reshape: %w", err)
	}
	defer q5.Free()
	yProd, err := mlx.Multiply(next5, q5, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("y prod: %w", err)
	}
	defer yProd.Free()
	y, err := mlx.Sum(yProd, []int{4}, false, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("y sum: %w", err)
	}

	// The reference casts y back to q's dtype (bf16) before the gated norm
	// and quantized out_proj — y would otherwise stay fp32 (state dtype).
	yCast, err := mlx.AsType(y, q.Dtype(), stream)
	y.Free()
	if err != nil {
		return nil, nil, fmt.Errorf("y dtype: %w", err)
	}

	// next (a reshape of next5) and y are fresh arrays; the caller owns both.
	next, err := mlx.Reshape(next5, []int{B, Hv, Dv, Dk}, stream)
	if err != nil {
		yCast.Free()
		return nil, nil, fmt.Errorf("state unflatten: %w", err)
	}
	return yCast, next, nil
}
