//go:build darwin && arm64 && cgo && mlx

package lfm2

import (
	"fmt"
	"math"
	"runtime"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

func init() {
	llm.RegisterArchitecture("lfm2", New)
}

// LFM2 implements the Liquid AI LFM2 hybrid architecture: a mix of
// short-convolution layers (depthwise Conv1d + gating) and standard GQA
// full-attention layers. The architecture is simpler than Gemma4/Qwen3.5 —
// no per-layer gating, no KV sharing, no sliding window, no proportional
// RoPE, no DeltaNet.
type LFM2 struct {
	cfg         llm.ModelConfig
	backend     tensor.Backend
	stream      tensor.Stream
	weights     *weights
	isAttnLayer []bool
	headDim     int
	attnScale   float32
}

func New(cfg llm.ModelConfig, backend tensor.Backend) (llm.Architecture, error) {
	headDim := cfg.HeadDim
	if headDim == 0 {
		headDim = cfg.HiddenSize / cfg.NumHeads
	}

	// Determine which layers are attention layers.
	isAttn := make([]bool, cfg.NumLayers)
	if len(cfg.LayerTypes) == cfg.NumLayers {
		for i, lt := range cfg.LayerTypes {
			isAttn[i] = lt == "full_attention"
		}
	}

	return &LFM2{
		cfg:         cfg,
		backend:     backend,
		isAttnLayer: isAttn,
		headDim:     headDim,
		attnScale:   float32(1.0 / math.Sqrt(float64(headDim))),
	}, nil
}

func (l *LFM2) Config() llm.ModelConfig   { return l.cfg }
func (l *LFM2) SetStream(s tensor.Stream) { l.stream = s }

type weights struct {
	embed         *llm.Embedding
	embeddingNorm tensor.Array
	layers        []layerWeights
}

type layerWeights struct {
	operatorNorm tensor.Array
	ffnNorm      tensor.Array

	// Conv layer weights (nil for attention layers)
	convWeight tensor.Array // [hidden, kernel, 1]
	inProj     *llm.Linear
	outProj    *llm.Linear

	// Attention layer weights (nil for conv layers)
	qProj *llm.Linear
	kProj *llm.Linear
	vProj *llm.Linear
	oProj *llm.Linear
	qNorm tensor.Array // [head_dim]
	kNorm tensor.Array // [head_dim]

	// FFN weights (all layers)
	w1 *llm.Linear
	w3 *llm.Linear
	w2 *llm.Linear
}

func (l *LFM2) InitWeights(path string, s tensor.Stream) error {
	sf, err := llm.OpenSafetensors(path)
	if err != nil {
		return err
	}
	defer sf.Release()

	prefix := l.cfg.WeightPrefix

	w := &weights{layers: make([]layerWeights, l.cfg.NumLayers)}

	// Embedding (tied)
	w.embed, err = llm.LoadEmbedding(sf, prefix+"embed_tokens.weight", l.backend, s, l.cfg.Quantization)
	if err != nil {
		return fmt.Errorf("load embed_tokens: %w", err)
	}

	// Final norm
	w.embeddingNorm, err = sf.Get(prefix+"embedding_norm.weight", s)
	if err != nil {
		return fmt.Errorf("load embedding_norm: %w", err)
	}

	for i := 0; i < l.cfg.NumLayers; i++ {
		p := fmt.Sprintf("%slayers.%d", prefix, i)
		lw := &w.layers[i]

		lw.operatorNorm, err = sf.Get(p+".operator_norm.weight", s)
		if err != nil {
			return fmt.Errorf("layer %d operator_norm: %w", i, err)
		}
		lw.ffnNorm, err = sf.Get(p+".ffn_norm.weight", s)
		if err != nil {
			return fmt.Errorf("layer %d ffn_norm: %w", i, err)
		}

		// FFN (all layers)
		lw.w1, err = llm.LoadLinear(sf, p+".feed_forward.w1.weight", l.backend, s, l.cfg.Quantization)
		if err != nil {
			return fmt.Errorf("layer %d ff w1: %w", i, err)
		}
		lw.w3, err = llm.LoadLinear(sf, p+".feed_forward.w3.weight", l.backend, s, l.cfg.Quantization)
		if err != nil {
			return fmt.Errorf("layer %d ff w3: %w", i, err)
		}
		lw.w2, err = llm.LoadLinear(sf, p+".feed_forward.w2.weight", l.backend, s, l.cfg.Quantization)
		if err != nil {
			return fmt.Errorf("layer %d ff w2: %w", i, err)
		}

		if l.isAttnLayer[i] {
			// Attention layer
			lw.qProj, err = llm.LoadLinear(sf, p+".self_attn.q_proj.weight", l.backend, s, l.cfg.Quantization)
			if err != nil {
				return fmt.Errorf("layer %d q_proj: %w", i, err)
			}
			lw.kProj, err = llm.LoadLinear(sf, p+".self_attn.k_proj.weight", l.backend, s, l.cfg.Quantization)
			if err != nil {
				return fmt.Errorf("layer %d k_proj: %w", i, err)
			}
			lw.vProj, err = llm.LoadLinear(sf, p+".self_attn.v_proj.weight", l.backend, s, l.cfg.Quantization)
			if err != nil {
				return fmt.Errorf("layer %d v_proj: %w", i, err)
			}
			lw.oProj, err = llm.LoadLinear(sf, p+".self_attn.out_proj.weight", l.backend, s, l.cfg.Quantization)
			if err != nil {
				return fmt.Errorf("layer %d out_proj: %w", i, err)
			}
			lw.qNorm, err = sf.Get(p+".self_attn.q_layernorm.weight", s)
			if err != nil {
				return fmt.Errorf("layer %d q_layernorm: %w", i, err)
			}
			lw.kNorm, err = sf.Get(p+".self_attn.k_layernorm.weight", s)
			if err != nil {
				return fmt.Errorf("layer %d k_layernorm: %w", i, err)
			}
		} else {
			// Conv layer
			lw.convWeight, err = sf.Get(p+".conv.conv.weight", s)
			if err != nil {
				return fmt.Errorf("layer %d conv weight: %w", i, err)
			}
			// Sanitize: if shape[-1] > shape[1], transpose (PyTorch layout → MLX)
			cwShape := lw.convWeight.Shape()
			if len(cwShape) == 3 && cwShape[2] > cwShape[1] {
				transposed, err := l.backend.TransposeAxes(lw.convWeight, []int{0, 2, 1}, s)
				if err != nil {
					return fmt.Errorf("layer %d conv transpose: %w", i, err)
				}
				lw.convWeight.Free()
				lw.convWeight = transposed
			}

			lw.inProj, err = llm.LoadLinear(sf, p+".conv.in_proj.weight", l.backend, s, l.cfg.Quantization)
			if err != nil {
				return fmt.Errorf("layer %d conv in_proj: %w", i, err)
			}
			lw.outProj, err = llm.LoadLinear(sf, p+".conv.out_proj.weight", l.backend, s, l.cfg.Quantization)
			if err != nil {
				return fmt.Errorf("layer %d conv out_proj: %w", i, err)
			}
		}
	}

	l.weights = w
	sf.Release()
	runtime.GC()
	return nil
}

func (l *LFM2) FreeWeights() {
	if l.weights == nil {
		return
	}
	l.weights.embed.Free()
	freeIfNotNil(l.weights.embeddingNorm)
	for i := range l.weights.layers {
		lw := &l.weights.layers[i]
		freeIfNotNil(lw.operatorNorm)
		freeIfNotNil(lw.ffnNorm)
		freeIfNotNil(lw.convWeight)
		freeIfNotNil(lw.qNorm)
		freeIfNotNil(lw.kNorm)
		if lw.inProj != nil {
			lw.inProj.Free()
		}
		if lw.outProj != nil {
			lw.outProj.Free()
		}
		if lw.qProj != nil {
			lw.qProj.Free()
		}
		if lw.kProj != nil {
			lw.kProj.Free()
		}
		if lw.vProj != nil {
			lw.vProj.Free()
		}
		if lw.oProj != nil {
			lw.oProj.Free()
		}
		if lw.w1 != nil {
			lw.w1.Free()
		}
		if lw.w3 != nil {
			lw.w3.Free()
		}
		if lw.w2 != nil {
			lw.w2.Free()
		}
	}
	l.weights = nil
}

func freeIfNotNil(a tensor.Array) {
	if a != nil {
		a.Free()
	}
}

func (l *LFM2) rmsNorm(x, weight tensor.Array) (tensor.Array, error) {
	return llm.RMSNorm(x, weight, l.cfg.RMSNormEPS, l.backend, l.stream)
}
