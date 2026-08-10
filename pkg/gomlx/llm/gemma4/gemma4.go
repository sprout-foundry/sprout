//go:build darwin && arm64 && cgo && mlx

package gemma4

import (
	"fmt"
	"math"
	"runtime"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

func init() {
	llm.RegisterArchitecture("gemma4_text", New)
}

type Gemma4 struct {
	cfg        llm.ModelConfig
	backend    tensor.Backend
	stream     tensor.Stream
	weights    *weights
	layerTypes []string
	prevKVs    []int // previous_kvs: for each layer, which layer's KV to reuse

	// Precomputed scales
	embedScale             float32
	embedPerLayerScale     float32
	perLayerInputScale     float32
	perLayerProjectionScale float32
}

func New(cfg llm.ModelConfig, backend tensor.Backend) (llm.Architecture, error) {
	g := &Gemma4{cfg: cfg, backend: backend}

	g.embedScale = float32(math.Sqrt(float64(cfg.HiddenSize)))
	g.embedPerLayerScale = float32(math.Sqrt(float64(cfg.HiddenSizePerLayerInput)))
	g.perLayerInputScale = float32(math.Pow(2.0, -0.5))
	g.perLayerProjectionScale = float32(math.Pow(float64(cfg.HiddenSize), -0.5))

	// Copy layer types
	g.layerTypes = make([]string, cfg.NumLayers)
	if len(cfg.LayerTypes) == cfg.NumLayers {
		copy(g.layerTypes, cfg.LayerTypes)
	} else {
		for i := range g.layerTypes {
			if (i+1)%cfg.SlidingWindowPattern == 0 {
				g.layerTypes[i] = "full_attention"
			} else {
				g.layerTypes[i] = "sliding_attention"
			}
		}
	}

	// Compute KV sharing map
	g.prevKVs = make([]int, cfg.NumLayers)
	for i := range g.prevKVs {
		g.prevKVs[i] = i
	}
	if cfg.NumKVSharedLayers > 0 {
		N := cfg.NumLayers
		M := N - cfg.NumKVSharedLayers
		kvsByType := map[string]int{}
		for i := 0; i < M; i++ {
			kvsByType[g.layerTypes[i]] = i
		}
		for j := M; j < N; j++ {
			if idx, ok := kvsByType[g.layerTypes[j]]; ok {
				g.prevKVs[j] = idx
			}
		}
	}

	return g, nil
}

func (g *Gemma4) Config() llm.ModelConfig { return g.cfg }
func (g *Gemma4) SetStream(s tensor.Stream) { g.stream = s }

type weights struct {
	embed              *llm.Embedding
	embedPerLayer      *llm.Embedding
	norm               tensor.Array // final RMSNorm
	layers             []layerWeights
	perLayerProj       *llm.Linear // per_layer_model_projection
	perLayerProjNorm   tensor.Array // per_layer_projection_norm
}

type layerWeights struct {
	inputNorm     tensor.Array
	postAttnNorm  tensor.Array
	preFFNorm     tensor.Array
	postFFNorm    tensor.Array
	layerScalar   tensor.Array

	qProj *llm.Linear
	kProj *llm.Linear // nil for KV-shared layers
	vProj *llm.Linear // nil for KV-shared layers
	oProj *llm.Linear
	qNorm tensor.Array
	kNorm tensor.Array // nil for KV-shared layers

	gateProj *llm.Linear
	upProj   *llm.Linear
	downProj *llm.Linear

	// Per-layer input gating
	perLayerInputGate    *llm.Linear
	perLayerProjection   *llm.Linear
	postPerLayerInputNorm tensor.Array
}

func isFullAttention(layerIdx int, pattern int) bool {
	return (layerIdx+1)%pattern == 0
}

func (g *Gemma4) hasKV(layerIdx int) bool {
	return layerIdx < g.cfg.NumLayers-g.cfg.NumKVSharedLayers
}

func (g *Gemma4) InitWeights(path string, s tensor.Stream) error {
	sf, err := llm.OpenSafetensors(path)
	if err != nil {
		return err
	}
	defer sf.Release()

	prefix := g.cfg.WeightPrefix
	if prefix == "" {
		prefix = "language_model.model."
	}

	w := &weights{layers: make([]layerWeights, g.cfg.NumLayers)}

	// Load embeddings
	w.embed, err = llm.LoadEmbedding(sf, prefix+"embed_tokens.weight", g.backend, s, g.cfg.Quantization)
	if err != nil {
		return fmt.Errorf("load embed_tokens: %w", err)
	}

	if g.cfg.HiddenSizePerLayerInput > 0 {
		w.embedPerLayer, err = llm.LoadEmbedding(sf, prefix+"embed_tokens_per_layer.weight", g.backend, s, g.cfg.Quantization)
		if err != nil {
			return fmt.Errorf("load embed_tokens_per_layer: %w", err)
		}
		w.perLayerProj, err = llm.LoadLinear(sf, prefix+"per_layer_model_projection.weight", g.backend, s, g.cfg.Quantization)
		if err != nil {
			return fmt.Errorf("load per_layer_model_projection: %w", err)
		}
		w.perLayerProjNorm, err = sf.Get(prefix+"per_layer_projection_norm.weight", s)
		if err != nil {
			return fmt.Errorf("load per_layer_projection_norm: %w", err)
		}
	}

	w.norm, err = sf.Get(prefix+"norm.weight", s)
	if err != nil {
		return fmt.Errorf("load final norm: %w", err)
	}

	for i := 0; i < g.cfg.NumLayers; i++ {
		p := fmt.Sprintf("%slayers.%d", prefix, i)
		lw := &w.layers[i]
		isFull := isFullAttention(i, g.cfg.SlidingWindowPattern)
		hasKV := g.hasKV(i)
		_ = isFull // head dim selection happens at forward time

		// Norms
		lw.inputNorm, err = sf.Get(p+".input_layernorm.weight", s)
		if err != nil { return fmt.Errorf("layer %d input_norm: %w", i, err) }
		lw.postAttnNorm, err = sf.Get(p+".post_attention_layernorm.weight", s)
		if err != nil { return fmt.Errorf("layer %d post_attn_norm: %w", i, err) }
		lw.preFFNorm, err = sf.Get(p+".pre_feedforward_layernorm.weight", s)
		if err != nil { return fmt.Errorf("layer %d pre_ff_norm: %w", i, err) }
		lw.postFFNorm, err = sf.Get(p+".post_feedforward_layernorm.weight", s)
		if err != nil { return fmt.Errorf("layer %d post_ff_norm: %w", i, err) }

		// Layer scalar
		lw.layerScalar, err = sf.Get(p+".layer_scalar", s)
		if err != nil {
			lw.layerScalar = nil // may not exist; default to 1
		}

		// Attention
		lw.qProj, err = llm.LoadLinear(sf, p+".self_attn.q_proj.weight", g.backend, s, g.cfg.Quantization)
		if err != nil { return fmt.Errorf("layer %d q_proj: %w", i, err) }
		if hasKV {
			lw.kProj, err = llm.LoadLinear(sf, p+".self_attn.k_proj.weight", g.backend, s, g.cfg.Quantization)
			if err != nil { return fmt.Errorf("layer %d k_proj: %w", i, err) }
			lw.vProj, err = llm.LoadLinear(sf, p+".self_attn.v_proj.weight", g.backend, s, g.cfg.Quantization)
			if err != nil { return fmt.Errorf("layer %d v_proj: %w", i, err) }
		}
		lw.oProj, err = llm.LoadLinear(sf, p+".self_attn.o_proj.weight", g.backend, s, g.cfg.Quantization)
		if err != nil { return fmt.Errorf("layer %d o_proj: %w", i, err) }
		lw.qNorm, err = sf.Get(p+".self_attn.q_norm.weight", s)
		if err != nil { return fmt.Errorf("layer %d q_norm: %w", i, err) }
		if hasKV {
			lw.kNorm, err = sf.Get(p+".self_attn.k_norm.weight", s)
			if err != nil { return fmt.Errorf("layer %d k_norm: %w", i, err) }
		}

		// MLP
		intermSize := g.cfg.IntermediateSize
		if g.cfg.UseDoubleWideMLP && !hasKV {
			intermSize *= 2
		}
		_ = intermSize // size determined by weight shapes, not needed explicitly
		lw.gateProj, err = llm.LoadLinear(sf, p+".mlp.gate_proj.weight", g.backend, s, g.cfg.Quantization)
		if err != nil { return fmt.Errorf("layer %d gate_proj: %w", i, err) }
		lw.upProj, err = llm.LoadLinear(sf, p+".mlp.up_proj.weight", g.backend, s, g.cfg.Quantization)
		if err != nil { return fmt.Errorf("layer %d up_proj: %w", i, err) }
		lw.downProj, err = llm.LoadLinear(sf, p+".mlp.down_proj.weight", g.backend, s, g.cfg.Quantization)
		if err != nil { return fmt.Errorf("layer %d down_proj: %w", i, err) }

		// Per-layer input gating
		if g.cfg.HiddenSizePerLayerInput > 0 {
			lw.perLayerInputGate, err = llm.LoadLinear(sf, p+".per_layer_input_gate.weight", g.backend, s, g.cfg.Quantization)
			if err != nil { return fmt.Errorf("layer %d per_layer_input_gate: %w", i, err) }
			lw.perLayerProjection, err = llm.LoadLinear(sf, p+".per_layer_projection.weight", g.backend, s, g.cfg.Quantization)
			if err != nil { return fmt.Errorf("layer %d per_layer_projection: %w", i, err) }
			lw.postPerLayerInputNorm, err = sf.Get(p+".post_per_layer_input_norm.weight", s)
			if err != nil { return fmt.Errorf("layer %d post_per_layer_input_norm: %w", i, err) }
		}
	}

	g.weights = w
	sf.Release()
	runtime.GC()
	return nil
}

func (g *Gemma4) FreeWeights() {
	if g.weights == nil { return }
	g.weights.embed.Free()
	if g.weights.embedPerLayer != nil { g.weights.embedPerLayer.Free() }
	if g.weights.perLayerProj != nil { g.weights.perLayerProj.Free() }
	freeIfNotNil(g.weights.norm)
	freeIfNotNil(g.weights.perLayerProjNorm)
	for i := range g.weights.layers {
		lw := &g.weights.layers[i]
		freeIfNotNil(lw.inputNorm)
		freeIfNIL(lw.postAttnNorm)
		freeIfNIL(lw.preFFNorm)
		freeIfNIL(lw.postFFNorm)
		freeIfNIL(lw.layerScalar)
		freeIfNIL(lw.qNorm)
		freeIfNIL(lw.kNorm)
		freeIfNIL(lw.postPerLayerInputNorm)
		if lw.qProj != nil { lw.qProj.Free() }
		if lw.kProj != nil { lw.kProj.Free() }
		if lw.vProj != nil { lw.vProj.Free() }
		if lw.oProj != nil { lw.oProj.Free() }
		if lw.gateProj != nil { lw.gateProj.Free() }
		if lw.upProj != nil { lw.upProj.Free() }
		if lw.downProj != nil { lw.downProj.Free() }
		if lw.perLayerInputGate != nil { lw.perLayerInputGate.Free() }
		if lw.perLayerProjection != nil { lw.perLayerProjection.Free() }
	}
	g.weights = nil
}

func freeIfNotNil(a tensor.Array) {
	if a != nil { a.Free() }
}

// keep alias for consistency
var freeIfNIL = freeIfNotNil

// gemmaRMSNorm applies RMSNorm with weight (Gemma adds 1 to the weight).
func (g *Gemma4) gemmaRMSNorm(x, weight tensor.Array) (tensor.Array, error) {
	s := g.stream
	ones, err := g.backend.NewArrayFromFloat32([]float32{1}, []int{1})
	if err != nil { return nil, err }
	defer ones.Free()
	scale, err := g.backend.Add(weight, ones, s)
	if err != nil { return nil, err }
	defer scale.Free()
	return llm.RMSNorm(x, scale, g.cfg.RMSNormEPS, g.backend, s)
}

// rmsNormNoScale applies RMSNorm without a weight (for v_norm).
func rmsNormNoScale(x tensor.Array, eps float32, b tensor.Backend, s tensor.Stream) (tensor.Array, error) {
	return llm.RMSNorm(x, nil, eps, b, s)
}

// geluApprox is the tanh approximation of GELU.
func geluApprox(x tensor.Array, b tensor.Backend, s tensor.Stream) (tensor.Array, error) {
	mkF := func(v float32) tensor.Array {
		a, _ := b.NewArrayFromFloat32([]float32{v}, []int{1})
		return a
	}
	c := float32(math.Sqrt(2.0 / math.Pi))
	xCubed, err := b.Power(x, 3, s)
	if err != nil { return nil, err }
	defer xCubed.Free()
	c044 := mkF(0.044715)
	defer c044.Free()
	inner, err := b.Multiply(c044, xCubed, s)
	if err != nil { return nil, err }
	defer inner.Free()
	inner, err = b.Add(x, inner, s)
	if err != nil { return nil, err }
	defer inner.Free()
	cArr := mkF(c)
	defer cArr.Free()
	inner, err = b.Multiply(cArr, inner, s)
	if err != nil { return nil, err }
	defer inner.Free()
	tanh, err := b.Tanh(inner, s)
	if err != nil { return nil, err }
	defer tanh.Free()
	one := mkF(1.0)
	defer one.Free()
	onePlus, err := b.Add(one, tanh, s)
	if err != nil { return nil, err }
	defer onePlus.Free()
	half := mkF(0.5)
	defer half.Free()
	halfX, err := b.Multiply(half, x, s)
	if err != nil { return nil, err }
	defer halfX.Free()
	return b.Multiply(halfX, onePlus, s)
}
