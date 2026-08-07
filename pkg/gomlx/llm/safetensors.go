//go:build darwin && arm64 && cgo && mlx

package llm

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
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

// Release drops the raw file data. Call after every weight has been loaded via
// Get: the MLX arrays own copies of their buffers, so the (potentially
// multiple-hundred-MB) Go-side file blob can be garbage collected. Keeps the
// header so Has/Get still work for metadata-only access.
func (sf *SafetensorsFile) Release() {
	sf.rawData = nil
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

// Get loads a tensor by name as a native-dtype MLX array. BF16 and F16 weights
// are loaded directly without conversion to float32 — MLX Metal kernels handle
// these dtypes natively at full speed, keeping memory usage and bandwidth at
// the native (half-precision) level.
func (sf *SafetensorsFile) Get(name string, s *mlx.Stream) (*mlx.Array, error) {
	entry, ok := sf.header[name]
	if !ok {
		return nil, fmt.Errorf("safetensors: key %q not found", name)
	}

	start := entry.DataOffsets[0]
	end := entry.DataOffsets[1]
	rawBytes := sf.rawData[start:end]

	var dtype mlx.Dtype
	switch entry.DType {
	case "BF16":
		dtype = mlx.BFloat16
	case "F16":
		dtype = mlx.Float16
	case "F32":
		dtype = mlx.Float32
	default:
		return nil, fmt.Errorf("safetensors: %q has unsupported dtype %s", name, entry.DType)
	}

	return mlx.NewArrayFromBytes(rawBytes, entry.Shape, dtype)
}

// Has reports whether a tensor exists in the file.
func (sf *SafetensorsFile) Has(name string) bool {
	_, ok := sf.header[name]
	return ok
}

func parseSafetensorsHeader(headerBytes []byte) (map[string]safetensorEntry, error) {
	result := make(map[string]safetensorEntry)
	if err := json.Unmarshal(headerBytes, &result); err != nil {
		return nil, fmt.Errorf("parse safetensors header: %w", err)
	}
	return result, nil
}
