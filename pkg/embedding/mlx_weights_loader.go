//go:build darwin && arm64 && cgo && mlx

package embedding

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/sprout-foundry/sinter/mlx"
)

type safetensorEntry struct {
	DType       string `json:"dtype"`
	Shape       []int  `json:"shape"`
	DataOffsets []int  `json:"data_offsets"`
}

func loadJinaSafetensors(path string, s *mlx.Stream) (*jinaWeights, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
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

	return loadWeightsFromBlob(headerMap, rawData, s)
}

func loadWeightsFromBlob(header map[string]safetensorEntry, rawData []byte, s *mlx.Stream) (*jinaWeights, error) {
	w := &jinaWeights{}

	load := func(name string) (*mlx.Array, error) {
		return loadSafetensorF16(header, rawData, name, s)
	}

	var err error
	if w.wordEmb, err = load("embeddings.word_embeddings.weight"); err != nil {
		return nil, err
	}
	if w.tokEmb, err = load("embeddings.token_type_embeddings.weight"); err != nil {
		return nil, err
	}
	if w.embNormW, err = load("embeddings.LayerNorm.weight"); err != nil {
		return nil, err
	}
	if w.embNormB, err = load("embeddings.LayerNorm.bias"); err != nil {
		return nil, err
	}

	for i := 0; i < numJinaLayers; i++ {
		lw := &jinaLayerWeights{}
		p := fmt.Sprintf("encoder.layer.%d", i)

		lw.qProjW, err = load(p + ".attention.self.query.weight")
		if err != nil {
			return nil, err
		}
		lw.qProjB, err = load(p + ".attention.self.query.bias")
		if err != nil {
			return nil, err
		}
		lw.kProjW, err = load(p + ".attention.self.key.weight")
		if err != nil {
			return nil, err
		}
		lw.kProjB, err = load(p + ".attention.self.key.bias")
		if err != nil {
			return nil, err
		}
		lw.vProjW, err = load(p + ".attention.self.value.weight")
		if err != nil {
			return nil, err
		}
		lw.vProjB, err = load(p + ".attention.self.value.bias")
		if err != nil {
			return nil, err
		}
		lw.outProjW, err = load(p + ".attention.output.dense.weight")
		if err != nil {
			return nil, err
		}
		lw.outProjB, err = load(p + ".attention.output.dense.bias")
		if err != nil {
			return nil, err
		}
		lw.qLnW, err = load(p + ".attention.self.layer_norm_q.weight")
		if err != nil {
			return nil, err
		}
		lw.qLnB, err = load(p + ".attention.self.layer_norm_q.bias")
		if err != nil {
			return nil, err
		}
		lw.kLnW, err = load(p + ".attention.self.layer_norm_k.weight")
		if err != nil {
			return nil, err
		}
		lw.kLnB, err = load(p + ".attention.self.layer_norm_k.bias")
		if err != nil {
			return nil, err
		}
		lw.attnLnW, err = load(p + ".attention.output.LayerNorm.weight")
		if err != nil {
			return nil, err
		}
		lw.attnLnB, err = load(p + ".attention.output.LayerNorm.bias")
		if err != nil {
			return nil, err
		}
		lw.ln1W, err = load(p + ".layer_norm_1.weight")
		if err != nil {
			return nil, err
		}
		lw.ln1B, err = load(p + ".layer_norm_1.bias")
		if err != nil {
			return nil, err
		}
		lw.ln2W, err = load(p + ".layer_norm_2.weight")
		if err != nil {
			return nil, err
		}
		lw.ln2B, err = load(p + ".layer_norm_2.bias")
		if err != nil {
			return nil, err
		}
		lw.gateUpW, err = load(p + ".mlp.up_gated_layer.weight")
		if err != nil {
			return nil, err
		}
		lw.downW, err = load(p + ".mlp.down_layer.weight")
		if err != nil {
			return nil, err
		}
		lw.downB, err = load(p + ".mlp.down_layer.bias")
		if err != nil {
			return nil, err
		}

		w.layers[i] = lw
	}

	return w, nil
}

func loadSafetensorF16(header map[string]safetensorEntry, rawData []byte, name string, s *mlx.Stream) (*mlx.Array, error) {
	entry, ok := header[name]
	if !ok {
		return nil, fmt.Errorf("safetensors: key %q not found", name)
	}
	if entry.DType != "F16" && entry.DType != "BF16" {
		return nil, fmt.Errorf("safetensors: %q has dtype %s, expected F16", name, entry.DType)
	}

	start := entry.DataOffsets[0]
	end := entry.DataOffsets[1]
	data := rawData[start:end]

	nElements := 1
	for _, d := range entry.Shape {
		nElements *= d
	}

	float32Data := make([]float32, nElements)
	if entry.DType == "F16" {
		for i := 0; i < nElements; i++ {
			bits := binary.LittleEndian.Uint16(data[i*2 : i*2+2])
			float32Data[i] = float32from16(bits)
		}
	} else {
		for i := 0; i < nElements; i++ {
			bits := binary.LittleEndian.Uint16(data[i*2 : i*2+2])
			float32Data[i] = math.Float32frombits(uint32(bits) << 16)
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
