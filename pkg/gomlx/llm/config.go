//go:build darwin && arm64 && cgo && mlx

package llm

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// hfConfig is the raw HuggingFace config.json structure. Only fields used by
// supported architectures are included; unknown fields are silently ignored.
type hfConfig struct {
	Architectures     []string     `json:"architectures"`
	ModelType         string       `json:"model_type"`
	HiddenSize        int          `json:"hidden_size"`
	IntermediateSize  int          `json:"intermediate_size"`
	NumHiddenLayers   int          `json:"num_hidden_layers"`
	NumAttentionHeads int          `json:"num_attention_heads"`
	NumKVHeads        int          `json:"num_key_value_heads"`
	HeadDim           int          `json:"head_dim"`
	RMSNormEPS        float64      `json:"rms_norm_eps"`
	RopeTheta         float64      `json:"rope_theta"`
	VocabSize         int          `json:"vocab_size"`
	BOSTokenID        int          `json:"bos_token_id"`
	EOSTokenID        int          `json:"eos_token_id"`
	TieWordEmbeddings bool         `json:"tie_word_embeddings"`
	AttentionBias     bool         `json:"attention_bias"`
	MaxPositionEmbeds int          `json:"max_position_embeddings"`
	Quantization      *quantConfig `json:"quantization"`

	// Hybrid linear-attention fields (Qwen3.5 / Qwen3-Next style).
	FullAttentionInterval int         `json:"full_attention_interval"`
	LinearNumKeyHeads     int         `json:"linear_num_key_heads"`
	LinearNumValueHeads   int         `json:"linear_num_value_heads"`
	LinearKeyHeadDim      int         `json:"linear_key_head_dim"`
	LinearValueHeadDim    int         `json:"linear_value_head_dim"`
	LinearConvKernelDim   int         `json:"linear_conv_kernel_dim"`
	AttnOutputGate        bool        `json:"attn_output_gate"`
	MTPNumHiddenLayers    int         `json:"mtp_num_hidden_layers"`
	MTPUseDedicatedEmbeds bool        `json:"mtp_use_dedicated_embeddings"`
	LayerTypes            []string    `json:"layer_types"`
	RopeParameters        *ropeParams `json:"rope_parameters"`

	// TextConfig carries the nested text-model config for multimodal
	// wrappers (e.g. qwen3_5 wraps qwen3_5_text).
	TextConfig *json.RawMessage `json:"text_config"`
}

// ropeParams is the `rope_parameters` section of Qwen3.5 configs.
type ropeParams struct {
	PartialRotaryFactor float32 `json:"partial_rotary_factor"`
	RopeTheta           float64 `json:"rope_theta"`
	MRopeSection        []int   `json:"mrope_section"`
	MRopeInterleaved    bool    `json:"mrope_interleaved"`
	RopeType            string  `json:"rope_type"`
}

// quantConfig is the mlx-lm quantization section of config.json (present on
// pre-quantized models like mlx-community/Qwen3-0.6B-4bit).
type quantConfig struct {
	GroupSize int    `json:"group_size"`
	Bits      int    `json:"bits"`
	Mode      string `json:"mode"`
}

// LoadConfig reads a HuggingFace config.json and returns a ModelConfig.
// The architecture is inferred from the model_type field. Multimodal
// wrappers (qwen3_5 etc.) are unwrapped to their nested text_config.
func LoadConfig(path string) (ModelConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ModelConfig{}, fmt.Errorf("read config: %w", err)
	}

	var raw hfConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return ModelConfig{}, fmt.Errorf("parse config: %w", err)
	}

	// Unwrap multimodal wrapper: qwen3_5 -> text_config (qwen3_5_text).
	if raw.TextConfig != nil {
		var text hfConfig
		if err := json.Unmarshal(*raw.TextConfig, &text); err != nil {
			return ModelConfig{}, fmt.Errorf("parse text_config: %w", err)
		}
		text.Quantization = raw.Quantization // outer wrapper may carry it
		text.TieWordEmbeddings = raw.TieWordEmbeddings
		if text.ModelType == "" {
			text.ModelType = raw.ModelType
		}
		raw = text
	}

	cfg := ModelConfig{
		Arch:              raw.ModelType,
		VocabSize:         raw.VocabSize,
		HiddenSize:        raw.HiddenSize,
		IntermediateSize:  raw.IntermediateSize,
		NumLayers:         raw.NumHiddenLayers,
		NumHeads:          raw.NumAttentionHeads,
		NumKVHeads:        raw.NumKVHeads,
		HeadDim:           raw.HeadDim,
		RMSNormEPS:        float32(raw.RMSNormEPS),
		RopeTheta:         raw.RopeTheta,
		BOSTokenID:        raw.BOSTokenID,
		EOSTokenID:        raw.EOSTokenID,
		UseAttentionBias:  raw.AttentionBias,
		UseTiedEmbeddings: raw.TieWordEmbeddings,
		MaxPosition:       raw.MaxPositionEmbeds,

		FullAttentionInterval: raw.FullAttentionInterval,
		LinearNumKeyHeads:     raw.LinearNumKeyHeads,
		LinearNumValueHeads:   raw.LinearNumValueHeads,
		LinearKeyHeadDim:      raw.LinearKeyHeadDim,
		LinearValueHeadDim:    raw.LinearValueHeadDim,
		LinearConvKernelDim:   raw.LinearConvKernelDim,
		AttnOutputGate:        raw.AttnOutputGate,
		MTPNumHiddenLayers:    raw.MTPNumHiddenLayers,
		MTPUseDedicatedEmbeds: raw.MTPUseDedicatedEmbeds,
		LayerTypes:            raw.LayerTypes,
	}

	// mRoPE parameters from the rope_parameters section.
	if raw.RopeParameters != nil {
		if raw.RopeParameters.PartialRotaryFactor > 0 {
			cfg.PartialRotaryFactor = raw.RopeParameters.PartialRotaryFactor
		}
		if raw.RopeParameters.RopeTheta > 0 {
			cfg.RopeTheta = raw.RopeParameters.RopeTheta
		}
		cfg.MRopeSection = raw.RopeParameters.MRopeSection
		cfg.MRopeInterleaved = raw.RopeParameters.MRopeInterleaved
	}

	// Quantization section from a pre-quantized model (mlx-community style).
	if raw.Quantization != nil {
		cfg.Quantization = &QuantConfig{
			GroupSize: raw.Quantization.GroupSize,
			Bits:      raw.Quantization.Bits,
			Mode:      raw.Quantization.Mode,
		}
		if cfg.Quantization.Mode == "" {
			cfg.Quantization.Mode = "affine"
		}
		if cfg.Quantization.GroupSize == 0 {
			cfg.Quantization.GroupSize = 64
		}
	}

	// Architecture-specific inference
	switch raw.ModelType {
	case "qwen3":
		cfg.UseQKNorm = true
		cfg.WeightPrefix = "model."
	case "qwen3_5_text":
		cfg.UseQKNorm = true
		cfg.WeightPrefix = "model.language_model."
	default:
		cfg.WeightPrefix = "model."
	}

	// Defaults for optional fields
	if cfg.HeadDim == 0 {
		cfg.HeadDim = cfg.HiddenSize / cfg.NumHeads
	}
	if cfg.NumKVHeads == 0 {
		cfg.NumKVHeads = cfg.NumHeads // MHA fallback
	}
	if cfg.RopeTheta == 0 {
		cfg.RopeTheta = 10000.0
	}
	if cfg.RMSNormEPS == 0 {
		cfg.RMSNormEPS = 1e-6
	}
	if cfg.PartialRotaryFactor == 0 {
		cfg.PartialRotaryFactor = 1.0 // full rotation (qwen3-style)
	}
	if cfg.FullAttentionInterval == 0 {
		cfg.FullAttentionInterval = 1 // all layers full attention
	}
	if cfg.LinearConvKernelDim == 0 {
		cfg.LinearConvKernelDim = 4
	}

	return cfg, nil
}

func (c ModelConfig) String() string {
	kind := "full-attn"
	if c.HybridLinearAttn() {
		kind = fmt.Sprintf("hybrid(%d:1)", c.FullAttentionInterval)
	}
	return fmt.Sprintf("%s(%s, hidden=%d, layers=%d, heads=%d, kv_heads=%d, head_dim=%d, vocab=%d)",
		c.Arch, kind, c.HiddenSize, c.NumLayers, c.NumHeads, c.NumKVHeads, c.HeadDim, c.VocabSize)
}

// freeArr releases a tensor array if non-nil (nil-safe, matching Array.Free).
func freeArr(a tensor.Array) {
	if a != nil {
		a.Free()
	}
}
