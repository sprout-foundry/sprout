//go:build (darwin || linux) && arm64 && cgo && (mlx || ggml)

package qwen35

import (
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// sparseMoeBlock implements the Mixture-of-Experts MLP for Qwen3.6-MoE.
// Each layer has N experts (K active per token) plus a shared expert.
// The router (gate) selects which K experts process each token.
//
// Expert weights are packed into 3D quantized tensors:
//
//	switch_mlp.gate_proj: [num_experts, intermediate, hidden_packed]
//	switch_mlp.up_proj:   [num_experts, intermediate, hidden_packed]
//	switch_mlp.down_proj: [num_experts, hidden, intermediate_packed]
//
// gather_qmm dispatches each token to its selected expert in one batched op.
type sparseMoeBlock struct {
	gate             *llm.Linear // [hidden, num_experts]
	switchGateProj   *llm.Linear // [num_experts, intermediate, hidden] packed
	switchUpProj     *llm.Linear
	switchDownProj   *llm.Linear
	sharedGateProj   *llm.Linear
	sharedUpProj     *llm.Linear
	sharedDownProj   *llm.Linear
	sharedExpertGate *llm.Linear // [hidden, 1]

	numExperts       int
	numExpertsPerTok int
	normTopkProb     bool
}

func (m *sparseMoeBlock) loadWeights(sf *llm.SafetensorsFile, prefix string, b tensor.Backend, s tensor.Stream, quant *llm.QuantConfig) error {
	var err error
	p := prefix + ".mlp"

	if m.gate, err = llm.LoadLinear(sf, p+".gate.weight", b, s, quant); err != nil {
		return fmt.Errorf("moe gate: %w", err)
	}
	if m.switchGateProj, err = llm.LoadLinear(sf, p+".switch_mlp.gate_proj.weight", b, s, quant); err != nil {
		return fmt.Errorf("moe switch gate_proj: %w", err)
	}
	if m.switchUpProj, err = llm.LoadLinear(sf, p+".switch_mlp.up_proj.weight", b, s, quant); err != nil {
		return fmt.Errorf("moe switch up_proj: %w", err)
	}
	if m.switchDownProj, err = llm.LoadLinear(sf, p+".switch_mlp.down_proj.weight", b, s, quant); err != nil {
		return fmt.Errorf("moe switch down_proj: %w", err)
	}
	if m.sharedGateProj, err = llm.LoadLinear(sf, p+".shared_expert.gate_proj.weight", b, s, quant); err != nil {
		return fmt.Errorf("moe shared gate_proj: %w", err)
	}
	if m.sharedUpProj, err = llm.LoadLinear(sf, p+".shared_expert.up_proj.weight", b, s, quant); err != nil {
		return fmt.Errorf("moe shared up_proj: %w", err)
	}
	if m.sharedDownProj, err = llm.LoadLinear(sf, p+".shared_expert.down_proj.weight", b, s, quant); err != nil {
		return fmt.Errorf("moe shared down_proj: %w", err)
	}
	if m.sharedExpertGate, err = llm.LoadLinear(sf, p+".shared_expert_gate.weight", b, s, quant); err != nil {
		return fmt.Errorf("moe shared_expert_gate: %w", err)
	}

	m.numExperts = m.switchGateProj.NumExperts()
	return nil
}

func (m *sparseMoeBlock) free() {
	if m == nil {
		return
	}
	if m.gate != nil {
		m.gate.Free()
	}
	if m.switchGateProj != nil {
		m.switchGateProj.Free()
	}
	if m.switchUpProj != nil {
		m.switchUpProj.Free()
	}
	if m.switchDownProj != nil {
		m.switchDownProj.Free()
	}
	if m.sharedGateProj != nil {
		m.sharedGateProj.Free()
	}
	if m.sharedUpProj != nil {
		m.sharedUpProj.Free()
	}
	if m.sharedDownProj != nil {
		m.sharedDownProj.Free()
	}
	if m.sharedExpertGate != nil {
		m.sharedExpertGate.Free()
	}
}

// forward runs the MoE MLP with proper top-k routing.
//
//  1. gates = softmax(gate(x))              [B, S, num_experts]
//  2. inds = argpartition(gates, -k)[-k:]   [B, S, k]
//  3. scores = take_along_axis(gates, inds) [B, S, k]
//  4. normalize scores if norm_topk_prob
//  5. y = switch_mlp(x, inds) * scores      [B, S, hidden]
//  6. shared_y = sigmoid(shared_expert_gate(x)) * shared_expert(x)
//  7. y = y + shared_y
func (m *sparseMoeBlock) forward(x tensor.Array, q *Qwen35) (tensor.Array, error) {
	s := q.stream
	b := q.backend
	k := m.numExpertsPerTok

	// Flatten x to [BS, H] for matmuls
	xShape := x.Shape()
	lastDim := xShape[len(xShape)-1]
	xFlat, err := b.Reshape(x, []int{-1, lastDim}, s)
	if err != nil {
		return nil, fmt.Errorf("moe reshape x: %w", err)
	}
	defer xFlat.Free()
	BS := xFlat.Shape()[0]

	// --- Router ---
	gates, err := m.gate.Forward(xFlat, b, s)
	if err != nil {
		return nil, fmt.Errorf("moe gate: %w", err)
	}
	defer gates.Free()

	gates, err = b.SoftmaxAxis(gates, -1, s)
	if err != nil {
		return nil, fmt.Errorf("moe softmax: %w", err)
	}
	defer gates.Free()

	// --- Top-k expert selection ---
	// argpartition with kth=-k puts the top-k elements at the end
	partIdx, err := b.ArgPartitionAxis(gates, -k, -1, s)
	if err != nil {
		return nil, fmt.Errorf("moe argpartition: %w", err)
	}
	defer partIdx.Free()

	// Slice last k indices: [..., -k:]
	inds, err := b.Slice(partIdx, []int{0, 0}, []int{BS, k}, []int{1, 1}, s)
	if err != nil {
		return nil, fmt.Errorf("moe slice indices: %w", err)
	}
	defer inds.Free()

	// Gather the corresponding scores
	scores, err := b.TakeAlongAxis(gates, inds, -1, s)
	if err != nil {
		return nil, fmt.Errorf("moe take_along_axis: %w", err)
	}
	defer scores.Free()

	// Normalize if needed
	if m.normTopkProb {
		scoreSum, err := b.Sum(scores, []int{-1}, true, s)
		if err != nil {
			return nil, fmt.Errorf("moe score sum: %w", err)
		}
		defer scoreSum.Free()
		scores, err = b.Divide(scores, scoreSum, s)
		if err != nil {
			return nil, fmt.Errorf("moe score norm: %w", err)
		}
		scores.Free()
		scores = scores // rebind — Divide returns new
	}

	// --- Shared expert (always runs) ---
	sharedGate, err := m.sharedGateProj.Forward(xFlat, b, s)
	if err != nil {
		return nil, fmt.Errorf("moe shared gate: %w", err)
	}
	defer sharedGate.Free()
	sharedUp, err := m.sharedUpProj.Forward(xFlat, b, s)
	if err != nil {
		return nil, fmt.Errorf("moe shared up: %w", err)
	}
	defer sharedUp.Free()
	sharedAct, err := llm.SwiGLU(sharedUp, sharedGate, b, s)
	if err != nil {
		return nil, fmt.Errorf("moe shared swiglu: %w", err)
	}
	defer sharedAct.Free()
	sharedOut, err := m.sharedDownProj.Forward(sharedAct, b, s)
	if err != nil {
		return nil, fmt.Errorf("moe shared down: %w", err)
	}
	defer sharedOut.Free()

	// Shared expert gate
	sharedGateLogit, err := m.sharedExpertGate.Forward(xFlat, b, s)
	if err != nil {
		return nil, fmt.Errorf("moe shared_expert_gate: %w", err)
	}
	defer sharedGateLogit.Free()
	sharedGateSigmoid, err := b.Sigmoid(sharedGateLogit, s)
	if err != nil {
		return nil, fmt.Errorf("moe shared sigmoid: %w", err)
	}
	defer sharedGateSigmoid.Free()
	sharedOut, err = b.Multiply(sharedGateSigmoid, sharedOut, s)
	if err != nil {
		return nil, fmt.Errorf("moe shared gate*out: %w", err)
	}

	// --- Routed experts via gather_qmm ---
	// x needs shape [BS, 1, H] for gather_qmm (adds a seq dimension)
	x3d, err := b.Reshape(xFlat, []int{BS, 1, lastDim}, s)
	if err != nil {
		return nil, fmt.Errorf("moe reshape x3d: %w", err)
	}
	defer x3d.Free()

	// inds is [BS, k] — these are the expert indices per token
	gateOut, err := b.GatherQuantizedMatMul(x3d, m.switchGateProj.QW(), m.switchGateProj.QScales(), m.switchGateProj.QBiases(), nil, inds, true, m.switchGateProj.QGroupSize(), m.switchGateProj.QBits(), m.switchGateProj.QMode(), false, s)
	if err != nil {
		return nil, fmt.Errorf("moe gather gate_proj: %w", err)
	}
	defer gateOut.Free()

	upOut, err := b.GatherQuantizedMatMul(x3d, m.switchUpProj.QW(), m.switchUpProj.QScales(), m.switchUpProj.QBiases(), nil, inds, true, m.switchUpProj.QGroupSize(), m.switchUpProj.QBits(), m.switchUpProj.QMode(), false, s)
	if err != nil {
		return nil, fmt.Errorf("moe gather up_proj: %w", err)
	}
	defer upOut.Free()

	// SwiGLU: silu(gate) * up
	actOut, err := llm.SwiGLU(upOut, gateOut, b, s)
	if err != nil {
		return nil, fmt.Errorf("moe switch swiglu: %w", err)
	}
	defer actOut.Free()

	downOut, err := b.GatherQuantizedMatMul(actOut, m.switchDownProj.QW(), m.switchDownProj.QScales(), m.switchDownProj.QBiases(), nil, inds, true, m.switchDownProj.QGroupSize(), m.switchDownProj.QBits(), m.switchDownProj.QMode(), false, s)
	if err != nil {
		return nil, fmt.Errorf("moe gather down_proj: %w", err)
	}
	defer downOut.Free()

	// downOut shape: [BS, k, H]
	// Weight by scores: scores [BS, k] → [BS, k, 1]
	scoreWeights, err := b.Reshape(scores, []int{BS, k, 1}, s)
	if err != nil {
		return nil, fmt.Errorf("moe reshape scores: %w", err)
	}
	defer scoreWeights.Free()

	weightedOut, err := b.Multiply(downOut, scoreWeights, s)
	if err != nil {
		return nil, fmt.Errorf("moe weight: %w", err)
	}
	defer weightedOut.Free()

	// Sum over experts: [BS, H]
	routedOut, err := b.Sum(weightedOut, []int{1}, false, s)
	if err != nil {
		return nil, fmt.Errorf("moe sum: %w", err)
	}
	defer routedOut.Free()

	// y = routed + shared
	result, err := b.Add(routedOut, sharedOut, s)
	if err != nil {
		return nil, fmt.Errorf("moe add shared: %w", err)
	}

	// Reshape back to original input shape
	if len(xShape) == 3 {
		return b.Reshape(result, xShape, s)
	}
	return result, nil
}
