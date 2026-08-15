//go:build darwin && arm64 && cgo

package qwen35

import (
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// compiledDeltaLayer mirrors gatedDeltaNet.forward at S=1 with the recurrent
// state as explicit I/O and the ops-mirror (non-fused) math paths.
func (q *Qwen35) compiledDeltaLayer(x tensor.Array, g *gatedDeltaNet, stateIn []*mlx.Array) (tensor.Array, []*mlx.Array, error) {
	s := q.stream
	b := q.backend

	state := tensor.Array(stateIn[0])
	convState := tensor.Array(stateIn[1])

	B, S := 1, 1

	qkv, err := g.inProjQKV.Forward(x, b, s)
	if err != nil {
		return nil, nil, fmt.Errorf("in_proj_qkv: %w", err)
	}
	defer qkv.Free()

	z, err := g.inProjZ.Forward(x, b, s)
	if err != nil {
		return nil, nil, fmt.Errorf("in_proj_z: %w", err)
	}
	defer z.Free()

	bOut, err := g.inProjB.Forward(x, b, s)
	if err != nil {
		return nil, nil, fmt.Errorf("in_proj_b: %w", err)
	}
	defer bOut.Free()

	a, err := g.inProjA.Forward(x, b, s)
	if err != nil {
		return nil, nil, fmt.Errorf("in_proj_a: %w", err)
	}
	defer a.Free()

	// Convolution over the cached rows + this token.
	convInput, err := b.ConcatenateAxis([]tensor.Array{convState, qkv}, 1, s)
	if err != nil {
		return nil, nil, fmt.Errorf("conv concat: %w", err)
	}
	defer convInput.Free()

	convOut, err := b.Conv1D(convInput, g.conv1d, 1, 0, 1, g.convDim, s)
	if err != nil {
		return nil, nil, fmt.Errorf("conv1d: %w", err)
	}
	defer convOut.Free()

	convOut, err = llm.SiLU(convOut, b, s)
	if err != nil {
		return nil, nil, fmt.Errorf("conv silu: %w", err)
	}
	defer convOut.Free()

	// Constant-bounded Slices, NOT SplitAxis: the compiled (shapeless) trace
	// runs on shape-undefined placeholders, and Split infers output shapes
	// from the input's axis length — undefined there, so tracing fails
	// ("Split cannot infer output shapes"). Slices with constant bounds
	// derive output shapes from the constants.
	qPart, err := b.Slice(convOut, []int{0, 0, 0}, []int{B, S, g.keyDim}, []int{1, 1, 1}, s)
	if err != nil {
		return nil, nil, fmt.Errorf("conv slice q: %w", err)
	}
	defer qPart.Free()
	kPart, err := b.Slice(convOut, []int{0, 0, g.keyDim}, []int{B, S, 2 * g.keyDim}, []int{1, 1, 1}, s)
	if err != nil {
		return nil, nil, fmt.Errorf("conv slice k: %w", err)
	}
	defer kPart.Free()
	vPart, err := b.Slice(convOut, []int{0, 0, 2 * g.keyDim}, []int{B, S, g.convDim}, []int{1, 1, 1}, s)
	if err != nil {
		return nil, nil, fmt.Errorf("conv slice v: %w", err)
	}
	defer vPart.Free()

	q2d, err := b.Reshape(qPart, []int{B, S, g.numK, g.headDim}, s)
	if err != nil {
		return nil, nil, fmt.Errorf("q reshape: %w", err)
	}
	defer q2d.Free()
	k2d, err := b.Reshape(kPart, []int{B, S, g.numK, g.headDim}, s)
	if err != nil {
		return nil, nil, fmt.Errorf("k reshape: %w", err)
	}
	defer k2d.Free()
	v2d, err := b.Reshape(vPart, []int{B, S, g.numV, g.headV}, s)
	if err != nil {
		return nil, nil, fmt.Errorf("v reshape: %w", err)
	}
	defer v2d.Free()

	invScale := 1.0 / sqrt(float64(g.headDim))
	q2d, err = scaleRMSNorm(q2d, float32(invScale*invScale), b, s)
	if err != nil {
		return nil, nil, fmt.Errorf("q rmsnorm: %w", err)
	}
	defer q2d.Free()
	k2d, err = scaleRMSNorm(k2d, float32(invScale), b, s)
	if err != nil {
		return nil, nil, fmt.Errorf("k rmsnorm: %w", err)
	}
	defer k2d.Free()

	beta, err := b.Sigmoid(bOut, s)
	if err != nil {
		return nil, nil, fmt.Errorf("beta sigmoid: %w", err)
	}
	defer beta.Free()

	decay, err := decayGate(g.ALog, a, g.DTBias, b, s)
	if err != nil {
		return nil, nil, fmt.Errorf("decay gate: %w", err)
	}
	defer decay.Free()

	y, newState, err := gatedDeltaUpdate(q2d, k2d, v2d, decay, beta, state, b, s)
	if err != nil {
		return nil, nil, fmt.Errorf("gated delta update: %w", err)
	}
	defer y.Free()

	// Trailing conv rows for the next step: last convK-1 rows of convInput.
	nKeep := g.convK - 1
	convTail, err := b.Slice(convInput, []int{0, S, 0}, []int{B, S + nKeep, g.convDim}, []int{1, 1, 1}, s)
	if err != nil {
		newState.Free()
		return nil, nil, fmt.Errorf("conv tail: %w", err)
	}

	zReshaped, err := b.Reshape(z, []int{B, S, g.numV, g.headV}, s)
	if err != nil {
		convTail.Free()
		newState.Free()
		return nil, nil, fmt.Errorf("z reshape: %w", err)
	}
	defer zReshaped.Free()

	out, err := rmsNormGated(y, zReshaped, g.norm, b, s)
	if err != nil {
		convTail.Free()
		newState.Free()
		return nil, nil, fmt.Errorf("norm gated: %w", err)
	}
	defer out.Free()

	outFlat, err := b.Reshape(out, []int{B, S, g.valueDim}, s)
	if err != nil {
		convTail.Free()
		newState.Free()
		return nil, nil, fmt.Errorf("out flatten: %w", err)
	}
	defer outFlat.Free()

	projOut, err := g.outProj.Forward(outFlat, b, s)
	if err != nil {
		convTail.Free()
		newState.Free()
		return nil, nil, fmt.Errorf("out proj: %w", err)
	}
	return projOut, []*mlx.Array{newState.(*mlx.Array), convTail.(*mlx.Array)}, nil
}
