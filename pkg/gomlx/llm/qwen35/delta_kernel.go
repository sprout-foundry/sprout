//go:build darwin && arm64 && cgo && mlx

package qwen35

import (
	"fmt"
	"sync"

	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
)

// gatedDeltaKernelSource is the fused Metal kernel for the Gated DeltaNet
// recurrence, ported from mlx-lm's gated_delta.py (_make_gated_delta_kernel,
// scalar-gate variant, no mask). One kernel launch scans the ENTIRE sequence
// with the recurrent state held in registers, instead of the per-step ops
// loop in gatedDeltaUpdate.
//
// The kernel processes B*Hv (batch, value-head) threads in the grid z-axis.
// Each thread group of 32 x-threads cooperates over the Dk key dimension
// (n_per_t = Dk/32 per thread) and uses simd_sum for the Dk reduction. q/k
// are [B,T,Hk,Dk] — the kernel broadcasts key heads to value heads internally
// via hk_idx = hv_idx / (Hv / Hk), so NO repeat is needed.
//
// Template params (bound at compile time): InT (q/k/v input dtype), StT
// (state dtype), Dk, Dv, Hk, Hv. Runtime inputs: q, k, v, g (decay), beta,
// state_in, T (scalar int32 sequence length). Outputs: y [B,T,Hv,Dv] and
// state_out [B,Hv,Dv,Dk].
const gatedDeltaKernelSource = `
    auto n = thread_position_in_grid.z;
    auto b_idx = n / Hv;
    auto hv_idx = n % Hv;
    auto hk_idx = hv_idx / (Hv / Hk);
    constexpr int n_per_t = Dk / 32;

    // q, k: [B, T, Hk, Dk]
    auto q_ = q + b_idx * T * Hk * Dk + hk_idx * Dk;
    auto k_ = k + b_idx * T * Hk * Dk + hk_idx * Dk;

    // v, y: [B, T, Hv, Dv]
    auto v_ = v + b_idx * T * Hv * Dv + hv_idx * Dv;
    y += b_idx * T * Hv * Dv + hv_idx * Dv;

    auto dk_idx = thread_position_in_threadgroup.x;
    auto dv_idx = thread_position_in_grid.y;

    // state_in, state_out: [B, Hv, Dv, Dk]
    auto i_state = state_in + (n * Dv + dv_idx) * Dk;
    auto o_state = state_out + (n * Dv + dv_idx) * Dk;

    float state[n_per_t];
    for (int i = 0; i < n_per_t; ++i) {
      auto s_idx = n_per_t * dk_idx + i;
      state[i] = static_cast<float>(i_state[s_idx]);
    }

    // g: [B, T, Hv] (scalar gate)
    auto g_ = g + b_idx * T * Hv;
    auto beta_ = beta + b_idx * T * Hv;

    for (int t = 0; t < T; ++t) {
      {
        float kv_mem = 0.0f;
        for (int i = 0; i < n_per_t; ++i) {
          auto s_idx = n_per_t * dk_idx + i;
          state[i] = state[i] * g_[hv_idx];
          kv_mem += state[i] * k_[s_idx];
        }
        kv_mem = simd_sum(kv_mem);

        auto delta = (v_[dv_idx] - kv_mem) * beta_[hv_idx];

        float out = 0.0f;
        for (int i = 0; i < n_per_t; ++i) {
          auto s_idx = n_per_t * dk_idx + i;
          state[i] = state[i] + k_[s_idx] * delta;
          out += state[i] * q_[s_idx];
        }
        out = simd_sum(out);
        if (thread_index_in_simdgroup == 0) {
          y[dv_idx] = static_cast<InT>(out);
        }
      }
      // Increment data pointers to next time step
      q_ += Hk * Dk;
      k_ += Hk * Dk;
      v_ += Hv * Dv;
      y += Hv * Dv;
      g_ += Hv;
      beta_ += Hv;
    }
    for (int i = 0; i < n_per_t; ++i) {
      auto s_idx = n_per_t * dk_idx + i;
      o_state[s_idx] = static_cast<StT>(state[i]);
    }
`

// deltaKernelKey identifies the compiled kernel variant. The template args
// (Dk, Dv, Hk, Hv) and the InT/StT dtypes are baked into the Metal
// instantiation, so each distinct combination needs its own kernel object.
// In practice a model has one DeltaNet shape, so this cache holds one entry.
type deltaKernelKey struct {
	dk, dv, hk, hv int
	inT, stT       mlx.Dtype
}

var (
	deltaKernelMu   sync.Mutex
	deltaKernels    = map[deltaKernelKey]*mlx.MetalKernel{}
)

// getDeltaKernel returns (and lazily compiles) the fused Metal kernel for the
// given shape/dtype configuration. It returns nil when Metal is unavailable
// or the shape is not kernel-friendly (Dk must be divisible by 32 for the
// thread-level split, matching the reference kernel's n_per_t).
func getDeltaKernel(dk, dv, hk, hv int, inT, stT mlx.Dtype) (*mlx.MetalKernel, error) {
	if dk%32 != 0 {
		return nil, fmt.Errorf("gated delta kernel requires Dk %% 32 == 0, got Dk=%d", dk)
	}
	if !mlx.Available() {
		return nil, fmt.Errorf("gated delta kernel requires Metal")
	}

	key := deltaKernelKey{dk: dk, dv: dv, hk: hk, hv: hv, inT: inT, stT: stT}
	deltaKernelMu.Lock()
	defer deltaKernelMu.Unlock()
	if k, ok := deltaKernels[key]; ok {
		return k, nil
	}

	k, err := mlx.NewMetalKernel(
		"gated_delta_step",
		[]string{"q", "k", "v", "g", "beta", "state_in", "T"},
		[]string{"y", "state_out"},
		gatedDeltaKernelSource,
		true, // ensureRowContiguous — kernels require row-contiguous inputs
		false,
	)
	if err != nil {
		return nil, err
	}
	deltaKernels[key] = k
	return k, nil
}

// fusedGatedDeltaUpdate runs the recurrence with one Metal kernel launch.
// It mirrors mlx-lm's gated_delta_kernel: q/k are UN-repeated [B,S,Hk,Dk]
// (the kernel broadcasts key heads to value heads internally), v is
// [B,S,Hv,Dv], g/beta are [B,S,Hv], state is [B,Hv,Dv,Dk] or nil.
//
// Returns y [B,S,Hv,Dv] (cast to q's dtype) and the final state
// [B,Hv,Dv,Dk] (cast to state's dtype — fp32 in practice).
func fusedGatedDeltaUpdate(q, k, v, g, beta, state *mlx.Array, stream *mlx.Stream) (*mlx.Array, *mlx.Array, error) {
	qs := q.Shape()
	B, S, Hk, Dk := qs[0], qs[1], qs[2], qs[3]
	vs := v.Shape()
	Hv, Dv := vs[2], vs[3]
	stT := mlx.Float32
	if state != nil {
		stT = state.Dtype()
	}

	kernel, err := getDeltaKernel(Dk, Dv, Hk, Hv, q.Dtype(), stT)
	if err != nil {
		return nil, nil, err
	}

	// State: fp32 zeros when nil (matches the sequential path).
	cur := state
	if cur == nil {
		cur, err = mlx.Zeros([]int{B, Hv, Dv, Dk}, mlx.Float32, stream)
		if err != nil {
			return nil, nil, fmt.Errorf("zero state: %w", err)
		}
		// The apply graph retains its own reference to cur; the Go handle is
		// released once the kernel launch has consumed it.
		defer cur.Free()
	}

	// T is a scalar int32 sequence length (matches the reference's Python-int
	// scalar input; MLX treats 0-dim int32 arrays as by-value kernel args).
	tArr, err := mlx.NewScalarInt32(S)
	if err != nil {
		return nil, nil, fmt.Errorf("T scalar: %w", err)
	}
	defer tArr.Free()

	cfg := mlx.NewMetalKernelConfig()
	defer cfg.Free()
	if err := cfg.AddOutputArg([]int{B, S, Hv, Dv}, q.Dtype()); err != nil {
		return nil, nil, err
	}
	if err := cfg.AddOutputArg([]int{B, Hv, Dv, Dk}, stT); err != nil {
		return nil, nil, err
	}
	if err := cfg.SetGrid(32, Dv, B*Hv); err != nil {
		return nil, nil, err
	}
	if err := cfg.SetThreadGroup(32, 4, 1); err != nil {
		return nil, nil, err
	}
	for name, val := range map[string]mlx.Dtype{"InT": q.Dtype(), "StT": stT} {
		if err := cfg.AddTemplateArgDtype(name, val); err != nil {
			return nil, nil, err
		}
	}
	for name, val := range map[string]int{"Dk": Dk, "Dv": Dv, "Hk": Hk, "Hv": Hv} {
		if err := cfg.AddTemplateArgInt(name, val); err != nil {
			return nil, nil, err
		}
	}

	outs, err := kernel.Apply([]*mlx.Array{q, k, v, g, beta, cur, tArr}, cfg, stream)
	if err != nil {
		return nil, nil, fmt.Errorf("gated delta kernel: %w", err)
	}
	if len(outs) != 2 {
		for _, o := range outs {
			o.Free()
		}
		return nil, nil, fmt.Errorf("gated delta kernel: expected 2 outputs got %d", len(outs))
	}
	return outs[0], outs[1], nil
}
