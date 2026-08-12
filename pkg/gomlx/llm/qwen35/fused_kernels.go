//go:build darwin && arm64 && cgo

package qwen35

import (
	"fmt"
	"sync"

	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// ---------------------------------------------------------------------------
// Fused elementwise kernels — the hot paths that mlx-lm compiles with
// @mx.compile but that we dispatch as separate ops in Go. Each kernel below
// replaces 3–7 CGO calls + Metal kernel launches with one.
// ---------------------------------------------------------------------------

// preciseSwigluKernelSource: silu(gate) * x, computed in fp32 and cast back.
// Mirrors mlx-lm's _precise_swiglu:
//
//	gate = silu(gate.astype(fp32))
//	x = x.astype(fp32)
//	return (gate * x).astype(h.dtype)
//
// Inputs: h [any] (dtype reference for output cast), gate [any], x [any].
// All three must be the same shape (broadcast handled by MLX before launch).
const preciseSwigluKernelSource = `
    auto idx = thread_position_in_grid.z;
    auto g = static_cast<float>(gate[idx]);
    // silu(g) = g / (1 + exp(-g))
    auto silu_g = g / (1.0f + exp(-g));
    auto xv = static_cast<float>(x[idx]);
    out[idx] = static_cast<InT>(silu_g * xv);
`

// computeGKernelSource: g = exp(-exp(A_log) * softplus(a + dt_bias)).
// Mirrors mlx-lm's compute_g (@partial(mx.compile, shapeless=True)):
//
//	exp(-exp(A_log.astype(fp32)) * softplus(a + dt_bias))
//
// Inputs: a_log [Hv], a [B,S,Hv], dt_bias [Hv].
// a_log and dt_bias are [Hv]; a is [B,S,Hv]. We launch one thread per element
// of the output g [B,S,Hv].
const computeGKernelSource = `
    auto idx = thread_position_in_grid.z;
    auto a_val = static_cast<float>(a[idx]);
    auto bias_idx = idx % Hv;
    auto bias_val = static_cast<float>(dt_bias[bias_idx]);
    // softplus(x) = log1p(exp(x))
    auto sp = log1p(exp(a_val + bias_val));
    auto alog_val = static_cast<float>(a_log[bias_idx]);
    auto ex = exp(alog_val);
    out[idx] = static_cast<float>(exp(-ex * sp));
`

var (
	fusedKernelsMu sync.Mutex
	swigluKernel   *mlx.MetalKernel
	computeGKernel *mlx.MetalKernel
)

func getSwigluKernel(dtype mlx.Dtype) (*mlx.MetalKernel, error) {
	fusedKernelsMu.Lock()
	defer fusedKernelsMu.Unlock()
	if swigluKernel != nil {
		return swigluKernel, nil
	}
	k, err := mlx.NewMetalKernel(
		"precise_swiglu",
		[]string{"h", "gate", "x"},
		[]string{"out"},
		preciseSwigluKernelSource,
		true,
		false,
	)
	if err != nil {
		return nil, err
	}
	swigluKernel = k
	return k, nil
}

func getComputeGKernel(dtype mlx.Dtype) (*mlx.MetalKernel, error) {
	fusedKernelsMu.Lock()
	defer fusedKernelsMu.Unlock()
	if computeGKernel != nil {
		return computeGKernel, nil
	}
	k, err := mlx.NewMetalKernel(
		"compute_g",
		[]string{"a_log", "a", "dt_bias"},
		[]string{"out"},
		computeGKernelSource,
		true,
		false,
	)
	if err != nil {
		return nil, err
	}
	computeGKernel = k
	return k, nil
}

// fusedSwiglu replaces q.swiglu with a single Metal kernel launch.
// h is the dtype reference, gate and x are the inputs.
// Returns silu(gate_fp32) * x_fp32, cast back to h's dtype.
func fusedSwiglu(h, gate, xVal tensor.Array, backend tensor.Backend, stream tensor.Stream) (tensor.Array, error) {
	if !backend.Available() {
		return nil, fmt.Errorf("fused swiglu requires Metal")
	}
	mlxGate := gate.(*mlx.Array)
	mlxX := xVal.(*mlx.Array)
	mlxH := h.(*mlx.Array)
	mlxStream := stream.(*mlx.Stream)

	kernel, err := getSwigluKernel(mlxGate.Dtype())
	if err != nil {
		return nil, err
	}

	n := mlxGate.Size()

	cfg := mlx.NewMetalKernelConfig()
	defer cfg.Free()
	if err := cfg.AddOutputArg(mlxGate.Shape(), mlxH.Dtype()); err != nil {
		return nil, err
	}
	if err := cfg.SetGrid(1, 1, n); err != nil {
		return nil, err
	}
	if err := cfg.SetThreadGroup(1, 1, 256); err != nil {
		return nil, err
	}
	if err := cfg.AddTemplateArgDtype("InT", mlxH.Dtype()); err != nil {
		return nil, err
	}

	outs, err := kernel.Apply([]*mlx.Array{mlxH, mlxGate, mlxX}, cfg, mlxStream)
	if err != nil {
		return nil, fmt.Errorf("fused swiglu: %w", err)
	}
	if len(outs) != 1 {
		for _, o := range outs {
			o.Free()
		}
		return nil, fmt.Errorf("fused swiglu: expected 1 output got %d", len(outs))
	}
	return outs[0], nil
}

// fusedComputeG replaces decayGate with a single Metal kernel launch.
// Computes g = exp(-exp(A_log) * softplus(a + dt_bias)) in fp32.
func fusedComputeG(aLog, a, dtBias tensor.Array, backend tensor.Backend, stream tensor.Stream) (tensor.Array, error) {
	if !backend.Available() {
		return nil, fmt.Errorf("fused compute_g requires Metal")
	}
	mlxA := a.(*mlx.Array)
	mlxAlog := aLog.(*mlx.Array)
	mlxDtBias := dtBias.(*mlx.Array)
	mlxStream := stream.(*mlx.Stream)

	kernel, err := getComputeGKernel(mlxA.Dtype())
	if err != nil {
		return nil, err
	}

	n := mlxA.Size()
	hv := mlxDtBias.Size()

	cfg := mlx.NewMetalKernelConfig()
	defer cfg.Free()
	if err := cfg.AddOutputArg(mlxA.Shape(), mlx.Float32); err != nil {
		return nil, err
	}
	if err := cfg.SetGrid(1, 1, n); err != nil {
		return nil, err
	}
	if err := cfg.SetThreadGroup(1, 1, 256); err != nil {
		return nil, err
	}
	if err := cfg.AddTemplateArgDtype("InT", mlx.Float32); err != nil {
		return nil, err
	}
	if err := cfg.AddTemplateArgInt("Hv", hv); err != nil {
		return nil, err
	}

	outs, err := kernel.Apply([]*mlx.Array{mlxAlog, mlxA, mlxDtBias}, cfg, mlxStream)
	if err != nil {
		return nil, fmt.Errorf("fused compute_g: %w", err)
	}
	if len(outs) != 1 {
		for _, o := range outs {
			o.Free()
		}
		return nil, fmt.Errorf("fused compute_g: expected 1 output got %d", len(outs))
	}
	return outs[0], nil
}
