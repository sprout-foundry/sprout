//go:build darwin && arm64 && cgo && mlx

package qwen35

import (
	"fmt"
	"math"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
)

// gatedDeltaNet implements the Gated DeltaNet linear-attention block used by
// Qwen3.5 hybrid layers. Unlike full attention, it maintains a fixed-size
// recurrent state (no KV cache growth with sequence length):
//
//	qkv = in_proj_qkv(x)                 [B, S, key_dim*2 + value_dim]
//	z   = in_proj_z(x)                   [B, S, value_dim]
//	b   = in_proj_b(x)                   [B, S, Hv]
//	a   = in_proj_a(x)                   [B, S, Hv]
//	conv = silu(conv1d(concat(conv_state, qkv)))   depthwise, kernel=4
//	q,k,v = split(conv)
//	q = rms_norm(q) * inv_scale^2 ; k = rms_norm(k) * inv_scale
//	beta = sigmoid(b)
//	g = exp(-exp(A_log) * softplus(a + dt_bias))
//	y, state = gated_delta_update(q, k, v, g, beta, state)
//	out = out_proj(rms_norm_gated(y, z))
type gatedDeltaNet struct {
	// Projections (shared llm.Linear: full-precision or quantized).
	inProjQKV *llm.Linear // [hidden, key_dim*2 + value_dim]
	inProjZ   *llm.Linear // [hidden, value_dim]
	inProjB   *llm.Linear // [hidden, Hv]
	inProjA   *llm.Linear // [hidden, Hv]
	outProj   *llm.Linear // [value_dim, hidden]
	conv1d    *mlx.Array  // [conv_dim, kernel, 1] (groups = conv_dim, depthwise)

	norm *mlx.Array // [head_v_dim] — RMSNorm weight for the gated output norm

	ALog   *mlx.Array // [Hv] — log decay constant (fp32, NOT quantized)
	DTBias *mlx.Array // [Hv] — decay bias (ones)

	keyDim   int
	valueDim int
	numK     int // linear_num_key_heads
	numV     int // linear_num_value_heads
	headK    int // linear_key_head_dim
	headV    int // linear_value_head_dim
	convDim  int
	convK    int // linear_conv_kernel_dim
}

func newGatedDeltaNet(cfg llm.ModelConfig) *gatedDeltaNet {
	keyDim := cfg.LinearNumKeyHeads * cfg.LinearKeyHeadDim
	valueDim := cfg.LinearNumValueHeads * cfg.LinearValueHeadDim
	return &gatedDeltaNet{
		keyDim:   keyDim,
		valueDim: valueDim,
		numK:     cfg.LinearNumKeyHeads,
		numV:     cfg.LinearNumValueHeads,
		headK:    cfg.LinearKeyHeadDim,
		headV:    cfg.LinearValueHeadDim,
		convDim:  keyDim*2 + valueDim,
		convK:    cfg.LinearConvKernelDim,
	}
}

// free releases all arrays owned by the block.
func (g *gatedDeltaNet) free() {
	g.inProjQKV.Free()
	g.inProjZ.Free()
	g.inProjB.Free()
	g.inProjA.Free()
	g.outProj.Free()
	g.conv1d.Free()
	g.norm.Free()
	g.ALog.Free()
	g.DTBias.Free()
}

// loadWeights loads the linear_attn.* tensors for a layer. Name is the
// per-layer prefix WITHOUT the trailing ".linear_attn" (e.g.
// "model.language_model.layers.0").
func (g *gatedDeltaNet) loadWeights(sf *llm.SafetensorsFile, name string, s *mlx.Stream, quant *llm.QuantConfig) error {
	// name is the full linear_attn key prefix (e.g.
	// "language_model.model.layers.0.linear_attn").
	var err error
	if g.inProjQKV, err = llm.LoadLinear(sf, name+".in_proj_qkv.weight", s, quant); err != nil {
		return fmt.Errorf("in_proj_qkv: %w", err)
	}
	if g.inProjZ, err = llm.LoadLinear(sf, name+".in_proj_z.weight", s, quant); err != nil {
		return fmt.Errorf("in_proj_z: %w", err)
	}
	if g.inProjB, err = llm.LoadLinear(sf, name+".in_proj_b.weight", s, quant); err != nil {
		return fmt.Errorf("in_proj_b: %w", err)
	}
	if g.inProjA, err = llm.LoadLinear(sf, name+".in_proj_a.weight", s, quant); err != nil {
		return fmt.Errorf("in_proj_a: %w", err)
	}
	if g.outProj, err = llm.LoadLinear(sf, name+".out_proj.weight", s, quant); err != nil {
		return fmt.Errorf("out_proj: %w", err)
	}

	// conv1d.weight: [conv_dim, kernel, 1] (sanitized MLX layout, depthwise).
	if g.conv1d, err = sf.Get(name+".conv1d.weight", s); err != nil {
		return fmt.Errorf("conv1d: %w", err)
	}
	if err := g.conv1d.Eval(); err != nil {
		return fmt.Errorf("eval conv1d: %w", err)
	}

	// norm.weight: [head_v_dim] — the gated output RMSNorm.
	if g.norm, err = sf.Get(name+".norm.weight", s); err != nil {
		return fmt.Errorf("norm: %w", err)
	}

	// A_log and dt_bias are tiny [Hv] buffers; never quantized, always fp32.
	if g.ALog, err = sf.Get(name+".A_log", s); err != nil {
		return fmt.Errorf("A_log: %w", err)
	}
	if err := g.ALog.Eval(); err != nil {
		return fmt.Errorf("eval A_log: %w", err)
	}
	if g.DTBias, err = sf.Get(name+".dt_bias", s); err != nil {
		return fmt.Errorf("dt_bias: %w", err)
	}
	if err := g.DTBias.Eval(); err != nil {
		return fmt.Errorf("eval dt_bias: %w", err)
	}
	return nil
}

// forward runs the DeltaNet block over a sequence of hidden states.
// cache holds the fixed-size recurrent state (State [B,Hv,Dv,Dk] and
// ConvState [B, convK-1, convDim]); it is populated on prefill and updated
// in place per token on decode. When cache is nil the block is stateless
// (used by the parity path for full-sequence re-encode).
func (g *gatedDeltaNet) forward(x *mlx.Array, cache *llm.KVCache, layerIdx, seqLen int, s *mlx.Stream) (*mlx.Array, error) {
	shape := x.Shape()
	B, S := shape[0], shape[1]
	if S == 0 || B == 0 {
		return nil, fmt.Errorf("delta_net: bad input shape %v", x.Shape())
	}

	qkv, err := g.inProjQKV.Forward(x, s)
	if err != nil {
		return nil, fmt.Errorf("in_proj_qkv: %w", err)
	}
	defer qkv.Free()

	z, err := g.inProjZ.Forward(x, s)
	if err != nil {
		return nil, fmt.Errorf("in_proj_z: %w", err)
	}
	defer z.Free()

	b, err := g.inProjB.Forward(x, s)
	if err != nil {
		return nil, fmt.Errorf("in_proj_b: %w", err)
	}
	defer b.Free()

	a, err := g.inProjA.Forward(x, s)
	if err != nil {
		return nil, fmt.Errorf("in_proj_a: %w", err)
	}
	defer a.Free()

	// Convolution: concat cached conv rows with qkv, run depthwise silu-conv.
	var convInput *mlx.Array
	var state, convState *mlx.Array
	if cache != nil {
		state, convState, err = cache.GetState(layerIdx)
		if err != nil {
			return nil, err
		}
	}

	if convState != nil {
		convInput, err = mlx.ConcatenateAxis([]*mlx.Array{convState, qkv}, 1, s)
		if err != nil {
			return nil, fmt.Errorf("conv concat: %w", err)
		}
		defer convInput.Free()
	} else {
		// First call: pad the conv window with zeros.
		pad, err := mlx.Zeros([]int{B, g.convK - 1, g.convDim}, qkv.Dtype(), s)
		if err != nil {
			return nil, fmt.Errorf("conv pad: %w", err)
		}
		defer pad.Free()
		convInput, err = mlx.ConcatenateAxis([]*mlx.Array{pad, qkv}, 1, s)
		if err != nil {
			return nil, fmt.Errorf("conv pad concat: %w", err)
		}
		defer convInput.Free()
	}

	convOut, err := mlx.Conv1D(convInput, g.conv1d, 1, 0, 1, g.convDim, s)
	if err != nil {
		return nil, fmt.Errorf("conv1d: %w", err)
	}
	defer convOut.Free()

	// silu(conv) — the conv output is the "conv feature" gating.
	convOut, err = llm.SiLU(convOut, s)
	if err != nil {
		return nil, fmt.Errorf("conv silu: %w", err)
	}
	defer convOut.Free()

	// Split conv output into q, k, v: q/k have numK heads, v has numV heads.
	// conv_out is [B, S, conv_dim]; key_dim is the first chunk.
	qkvSplit, err := mlx.SplitAxis(convOut, []int{g.keyDim, 2 * g.keyDim}, 2, s)
	if err != nil {
		return nil, fmt.Errorf("conv split: %w", err)
	}
	defer func() {
		for _, a := range qkvSplit {
			a.Free()
		}
	}()
	if len(qkvSplit) != 3 {
		return nil, fmt.Errorf("conv split: expected 3 parts got %d", len(qkvSplit))
	}

	// q/k/v: reshape to [B, S, H, D]
	q2d, err := mlx.Reshape(qkvSplit[0], []int{B, S, g.numK, g.headK}, s)
	if err != nil {
		return nil, fmt.Errorf("q reshape: %w", err)
	}
	defer q2d.Free()
	k2d, err := mlx.Reshape(qkvSplit[1], []int{B, S, g.numK, g.headK}, s)
	if err != nil {
		return nil, fmt.Errorf("k reshape: %w", err)
	}
	defer k2d.Free()
	v2d, err := mlx.Reshape(qkvSplit[2], []int{B, S, g.numV, g.headV}, s)
	if err != nil {
		return nil, fmt.Errorf("v reshape: %w", err)
	}
	defer v2d.Free()

	// Scale-normalized q/k (rms_norm with no weight, then scale).
	invScale := 1.0 / sqrt(float64(g.headK))
	q2d, err = scaleRMSNorm(q2d, float32(invScale*invScale), s)
	if err != nil {
		return nil, fmt.Errorf("q rmsnorm: %w", err)
	}
	defer q2d.Free()
	k2d, err = scaleRMSNorm(k2d, float32(invScale), s)
	if err != nil {
		return nil, fmt.Errorf("k rmsnorm: %w", err)
	}
	defer k2d.Free()

	// beta = sigmoid(b); g = exp(-exp(A_log) * softplus(a + dt_bias))
	beta, err := mlx.Sigmoid(b, s)
	if err != nil {
		return nil, fmt.Errorf("beta sigmoid: %w", err)
	}
	defer beta.Free()

	decay, err := decayGate(g.ALog, a, g.DTBias, s)
	if err != nil {
		return nil, fmt.Errorf("decay gate: %w", err)
	}
	defer decay.Free()

	// Recurrent update.
	y, newState, err := gatedDeltaUpdate(q2d, k2d, v2d, decay, beta, state, s)
	if err != nil {
		return nil, fmt.Errorf("gated delta update: %w", err)
	}
	defer y.Free()

	if cache != nil {
		// Persist the new recurrent state + trailing conv rows. The cache
		// takes ownership of both arrays — do not Free them here.
		nKeep := g.convK - 1
		convTail, err := mlx.Slice(convInput, []int{0, S, 0}, []int{B, S + nKeep, g.convDim}, []int{1, 1, 1}, s)
		if err != nil {
			return nil, fmt.Errorf("conv tail: %w", err)
		}
		if err := cache.StoreState(layerIdx, newState, convTail); err != nil {
			convTail.Free()
			newState.Free()
			return nil, err
		}
	} else {
		// No cache (e.g. single-shot forward) — release the state we own.
		newState.Free()
	}

	// Gated output norm: out = rms_norm(y) * silu(z), then project.
	zReshaped, err := mlx.Reshape(z, []int{B, S, g.numV, g.headV}, s)
	if err != nil {
		return nil, fmt.Errorf("z reshape: %w", err)
	}
	defer zReshaped.Free()

	out, err := rmsNormGated(y, zReshaped, g.norm, s)
	if err != nil {
		return nil, fmt.Errorf("norm gated: %w", err)
	}
	defer out.Free()

	outFlat, err := mlx.Reshape(out, []int{B, S, g.valueDim}, s)
	if err != nil {
		return nil, fmt.Errorf("out flatten: %w", err)
	}
	defer outFlat.Free()

	return g.outProj.Forward(outFlat, s)
}

func sqrt(x float64) float64 { return math.Sqrt(x) }
