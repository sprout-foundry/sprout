//go:build darwin && arm64 && cgo && mlx

package llm

import (
	"encoding/json"
	"fmt"
	"os"
)

// hfConfig is the raw HuggingFace config.json structure. Only fields used by
// supported architectures are included; unknown fields are silently ignored.
type hfConfig struct {
	Architectures      []string `json:"architectures"`
	ModelType          string   `json:"model_type"`
	HiddenSize         int      `json:"hidden_size"`
	IntermediateSize   int      `json:"intermediate_size"`
	NumHiddenLayers    int      `json:"num_hidden_layers"`
	NumAttentionHeads  int      `json:"num_attention_heads"`
	NumKVHeads         int      `json:"num_key_value_heads"`
	HeadDim            int      `json:"head_dim"`
	RMSNormEPS         float64  `json:"rms_norm_eps"`
	RopeTheta          float64  `json:"rope_theta"`
	VocabSize          int      `json:"vocab_size"`
	BOSTokenID         int      `json:"bos_token_id"`
	EOSTokenID         int      `json:"eos_token_id"`
	TieWordEmbeddings  bool     `json:"tie_word_embeddings"`
	AttentionBias      bool     `json:"attention_bias"`
	MaxPositionEmbeds  int      `json:"max_position_embeddings"`
}

// LoadConfig reads a HuggingFace config.json and returns a ModelConfig.
// The architecture is inferred from the model_type field.
func LoadConfig(path string) (ModelConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ModelConfig{}, fmt.Errorf("read config: %w", err)
	}

	var raw hfConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return ModelConfig{}, fmt.Errorf("parse config: %w", err)
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
	}

	// Architecture-specific inference
	switch raw.ModelType {
	case "qwen3":
		cfg.UseQKNorm = true
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

	return cfg, nil
}

func (c ModelConfig) String() string {
	return fmt.Sprintf("%s(hidden=%d, layers=%d, heads=%d, kv_heads=%d, head_dim=%d, vocab=%d)",
		c.Arch, c.HiddenSize, c.NumLayers, c.NumHeads, c.NumKVHeads, c.HeadDim, c.VocabSize)
}
