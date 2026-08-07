//go:build darwin && arm64 && cgo && mlx

package local_llm

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/sprout-foundry/sprout/pkg/mlx"
)

type safetensorEntry struct {
	DType       string `json:"dtype"`
	Shape       []int  `json:"shape"`
	DataOffsets []int  `json:"data_offsets"`
}

// loadWeights loads all model weights from a safetensors file and converts
// them to fp32 MLX arrays. The weights are loaded into GPU memory once and
// reused for every forward pass.
func loadWeights(path string, cfg ModelConfig, s *mlx.Stream) (*weights, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open safetensors: %w", err)
	}
	defer f.Close()

	var headerLen uint64
	if err := binary.Read(f, binary.LittleEndian, &headerLen); err != nil {
		return nil, fmt.Errorf("read header length: %w", err)
	}
	if headerLen > 1<<30 {
		return nil, fmt.Errorf("safetensors header too large: %d bytes", headerLen)
	}

	headerBytes := make([]byte, headerLen)
	if _, err := io.ReadFull(f, headerBytes); err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}

	headerMap, err := parseSafetensorsHeader(headerBytes)
	if err != nil {
		return nil, err
	}

	dataStart := 8 + int64(headerLen)
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	dataSize := stat.Size() - dataStart
	rawData := make([]byte, dataSize)
	if _, err := io.ReadFull(f, rawData); err != nil {
		return nil, fmt.Errorf("read data blob: %w", err)
	}

	return buildWeights(headerMap, rawData, cfg, s)
}

func buildWeights(header map[string]safetensorEntry, rawData []byte, cfg ModelConfig, s *mlx.Stream) (*weights, error) {
	load := func(name string) (*mlx.Array, error) {
		return loadSafetensorBF16(header, rawData, name, s)
	}

	w := &weights{
		layers: make([]layerWeights, cfg.NumLayers),
	}

	var err error
	w.embedTokens, err = load("model.embed_tokens.weight")
	if err != nil {
		return nil, fmt.Errorf("load embed_tokens: %w", err)
	}
	w.normWeight, err = load("model.norm.weight")
	if err != nil {
		return nil, fmt.Errorf("load final norm: %w", err)
	}

	for i := 0; i < cfg.NumLayers; i++ {
		lw := &w.layers[i]
		p := fmt.Sprintf("model.layers.%d", i)

		lw.inputNorm, err = load(p + ".input_layernorm.weight")
		if err != nil {
			return nil, fmt.Errorf("load layer %d input_norm: %w", i, err)
		}
		lw.qProj, err = load(p + ".self_attn.q_proj.weight")
		if err != nil {
			return nil, fmt.Errorf("load layer %d q_proj: %w", i, err)
		}
		lw.kProj, err = load(p + ".self_attn.k_proj.weight")
		if err != nil {
			return nil, fmt.Errorf("load layer %d k_proj: %w", i, err)
		}
		lw.vProj, err = load(p + ".self_attn.v_proj.weight")
		if err != nil {
			return nil, fmt.Errorf("load layer %d v_proj: %w", i, err)
		}
		lw.oProj, err = load(p + ".self_attn.o_proj.weight")
		if err != nil {
			return nil, fmt.Errorf("load layer %d o_proj: %w", i, err)
		}
		lw.qNorm, err = load(p + ".self_attn.q_norm.weight")
		if err != nil {
			return nil, fmt.Errorf("load layer %d q_norm: %w", i, err)
		}
		lw.kNorm, err = load(p + ".self_attn.k_norm.weight")
		if err != nil {
			return nil, fmt.Errorf("load layer %d k_norm: %w", i, err)
		}
		lw.postNorm, err = load(p + ".post_attention_layernorm.weight")
		if err != nil {
			return nil, fmt.Errorf("load layer %d post_norm: %w", i, err)
		}
		lw.gateProj, err = load(p + ".mlp.gate_proj.weight")
		if err != nil {
			return nil, fmt.Errorf("load layer %d gate_proj: %w", i, err)
		}
		lw.upProj, err = load(p + ".mlp.up_proj.weight")
		if err != nil {
			return nil, fmt.Errorf("load layer %d up_proj: %w", i, err)
		}
		lw.downProj, err = load(p + ".mlp.down_proj.weight")
		if err != nil {
			return nil, fmt.Errorf("load layer %d down_proj: %w", i, err)
		}
	}

	return w, nil
}

// loadSafetensorBF16 loads a single tensor from the safetensors blob,
// converting bf16 → fp32. Qwen3-0.6B stores weights in bfloat16.
func loadSafetensorBF16(header map[string]safetensorEntry, rawData []byte, name string, s *mlx.Stream) (*mlx.Array, error) {
	entry, ok := header[name]
	if !ok {
		return nil, fmt.Errorf("safetensors: key %q not found", name)
	}
	if entry.DType != "BF16" && entry.DType != "F16" {
		return nil, fmt.Errorf("safetensors: %q has dtype %s, expected BF16 or F16", name, entry.DType)
	}

	start := entry.DataOffsets[0]
	end := entry.DataOffsets[1]
	data := rawData[start:end]

	nElements := 1
	for _, d := range entry.Shape {
		nElements *= d
	}

	float32Data := make([]float32, nElements)
	if entry.DType == "BF16" {
		for i := 0; i < nElements; i++ {
			bits := binary.LittleEndian.Uint16(data[i*2 : i*2+2])
			float32Data[i] = math.Float32frombits(uint32(bits) << 16)
		}
	} else {
		for i := 0; i < nElements; i++ {
			bits := binary.LittleEndian.Uint16(data[i*2 : i*2+2])
			float32Data[i] = float32from16(bits)
		}
	}

	return mlx.NewArrayFromFloat32(float32Data, entry.Shape)
}

func float32from16(bits uint16) float32 {
	s := uint32(bits&0x8000) << 16
	exp := uint32(bits&0x7C00) >> 10
	mant := uint32(bits & 0x03FF)

	switch exp {
	case 0:
		if mant == 0 {
			return math.Float32frombits(s)
		}
		for mant&0x0400 == 0 {
			mant <<= 1
			exp--
		}
		exp++
		mant &= 0x03FF
	case 0x1F:
		return math.Float32frombits(s | 0x7F800000 | (mant << 13))
	}

	exp = exp + 127 - 15
	return math.Float32frombits(s | (exp << 23) | (mant << 13))
}

func parseSafetensorsHeader(headerBytes []byte) (map[string]safetensorEntry, error) {
	result := make(map[string]safetensorEntry)
	if err := json.Unmarshal(headerBytes, &result); err != nil {
		return nil, fmt.Errorf("parse safetensors header: %w", err)
	}
	return result, nil
}

// freeWeights releases all MLX handles held by the weights struct.
func freeWeights(w *weights) {
	if w == nil {
		return
	}
	freeArr(w.embedTokens)
	freeArr(w.normWeight)
	for i := range w.layers {
		freeArr(w.layers[i].inputNorm)
		freeArr(w.layers[i].qProj)
		freeArr(w.layers[i].kProj)
		freeArr(w.layers[i].vProj)
		freeArr(w.layers[i].oProj)
		freeArr(w.layers[i].qNorm)
		freeArr(w.layers[i].kNorm)
		freeArr(w.layers[i].postNorm)
		freeArr(w.layers[i].gateProj)
		freeArr(w.layers[i].upProj)
		freeArr(w.layers[i].downProj)
	}
}

func freeArr(a *mlx.Array) {
	if a != nil {
		a.Free()
	}
}
