//go:build darwin && arm64 && cgo && mlx

package llm

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
)

// safetensorEntry is the metadata for one tensor in a safetensors file.
type safetensorEntry struct {
	DType       string `json:"dtype"`
	Shape       []int  `json:"shape"`
	DataOffsets []int  `json:"data_offsets"`
}

// SafetensorsFile holds the parsed header and raw data from a safetensors file.
// Architecture implementations use it to load tensors by name.
type SafetensorsFile struct {
	header  map[string]safetensorEntry
	rawData []byte
}

// OpenSafetensors reads a safetensors file and returns its header + data.
// The caller can then call Get to extract individual tensors as MLX arrays.
func OpenSafetensors(path string) (*SafetensorsFile, error) {
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

	return &SafetensorsFile{header: headerMap, rawData: rawData}, nil
}

// Get loads a tensor by name, converting BF16 or F16 to fp32 MLX array.
func (sf *SafetensorsFile) Get(name string, s *mlx.Stream) (*mlx.Array, error) {
	entry, ok := sf.header[name]
	if !ok {
		return nil, fmt.Errorf("safetensors: key %q not found", name)
	}
	if entry.DType != "BF16" && entry.DType != "F16" {
		return nil, fmt.Errorf("safetensors: %q has dtype %s, expected BF16 or F16", name, entry.DType)
	}

	start := entry.DataOffsets[0]
	end := entry.DataOffsets[1]
	data := sf.rawData[start:end]

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

// Has reports whether a tensor exists in the file.
func (sf *SafetensorsFile) Has(name string) bool {
	_, ok := sf.header[name]
	return ok
}

// float32from16 converts IEEE 754 half-precision (float16) to float32.
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
