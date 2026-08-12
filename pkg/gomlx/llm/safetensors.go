//go:build (darwin || linux) && arm64 && cgo && (mlx || ggml)

package llm

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/tensor"
)

// safetensorEntry is the metadata for one tensor in a safetensors file.
type safetensorEntry struct {
	DType       string `json:"dtype"`
	Shape       []int  `json:"shape"`
	DataOffsets []int  `json:"data_offsets"`
}

// safetensorsIndex is the model.safetensors.index.json shard mapping.
type safetensorsIndex struct {
	WeightMap map[string]string `json:"weight_map"`
}

// SafetensorsFile holds the parsed header and raw data from a safetensors file.
// Architecture implementations use it to load tensors by name.
//
// A SafetensorsFile is either a single file (header + rawData populated) or a
// sharded model (weightMap + shards populated, header/rawData empty). Get
// routes to the owning shard transparently.
type SafetensorsFile struct {
	header  map[string]safetensorEntry
	rawData []byte

	// Sharded-model fields (only set when the model spans multiple files).
	weightMap map[string]string           // tensor name -> shard filename
	shards    map[string]*SafetensorsFile // shard filename -> single-file reader
	shardDir  string
}

// Release drops the raw file data. Call after every weight has been loaded via
// Get: the MLX arrays own copies of their buffers, so the (potentially
// multiple-hundred-MB) Go-side file blob can be garbage collected. Keeps the
// header so Has/Get still work for metadata-only access.
func (sf *SafetensorsFile) Release() {
	if sf.rawData != nil {
		sf.rawData = nil
	}
	for _, shard := range sf.shards {
		shard.Release()
	}
}

// OpenSafetensors reads a safetensors model from a path. The path may point at
// a single model.safetensors file, or — when that file is missing and a
// model.safetensors.index.json shard index exists next to it — at a sharded
// model (model-00001-of-N.safetensors, ...). Sharded models route Get through
// the index weight_map.
func OpenSafetensors(path string) (*SafetensorsFile, error) {
	if fi, err := os.Stat(path); err == nil && !fi.IsDir() {
		return openSingleSafetensors(path)
	}

	// Single file missing — try the sharded layout: <dir>/model.safetensors.index.json
	indexPath := strings.TrimSuffix(path, filepath.Base(path)) + "model.safetensors.index.json"
	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
			return nil, fmt.Errorf("open safetensors: %w (no single file or shard index)", statErr)
		}
		return nil, fmt.Errorf("read shard index %s: %w", indexPath, err)
	}
	var idx safetensorsIndex
	if err := json.Unmarshal(indexData, &idx); err != nil {
		return nil, fmt.Errorf("parse shard index %s: %w", indexPath, err)
	}
	if len(idx.WeightMap) == 0 {
		return nil, fmt.Errorf("shard index %s has empty weight_map", indexPath)
	}

	dir := filepath.Dir(path)
	sf := &SafetensorsFile{
		weightMap: idx.WeightMap,
		shards:    make(map[string]*SafetensorsFile),
		shardDir:  dir,
	}
	for _, shardFile := range idx.WeightMap {
		if _, ok := sf.shards[shardFile]; ok {
			continue
		}
		shard, err := openSingleSafetensors(filepath.Join(dir, shardFile))
		if err != nil {
			return nil, fmt.Errorf("open shard %s: %w", shardFile, err)
		}
		sf.shards[shardFile] = shard
	}
	return sf, nil
}

func openSingleSafetensors(path string) (*SafetensorsFile, error) {
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

// Get loads a tensor by name as a native-dtype tensor.Array. BF16 and F16 weights
// are loaded directly without conversion to float32 — MLX Metal kernels handle
// these dtypes natively at full speed, keeping memory usage and bandwidth at
// the native (half-precision) level.
func (sf *SafetensorsFile) Get(name string, b tensor.Backend, s tensor.Stream) (tensor.Array, error) {
	if sf.weightMap != nil {
		shardFile, ok := sf.weightMap[name]
		if !ok {
			return nil, fmt.Errorf("safetensors: key %q not found in shard index", name)
		}
		shard, ok := sf.shards[shardFile]
		if !ok {
			return nil, fmt.Errorf("safetensors: shard %q for %q not loaded", shardFile, name)
		}
		return shard.Get(name, b, s)
	}

	entry, ok := sf.header[name]
	if !ok {
		return nil, fmt.Errorf("safetensors: key %q not found", name)
	}

	start := entry.DataOffsets[0]
	end := entry.DataOffsets[1]
	rawBytes := sf.rawData[start:end]

	var dtype tensor.Dtype
	switch entry.DType {
	case "BF16":
		dtype = tensor.BFloat16
	case "F16":
		dtype = tensor.Float16
	case "F32":
		dtype = tensor.Float32
	case "U32":
		dtype = tensor.UInt32
	case "I32":
		dtype = tensor.Int32
	default:
		return nil, fmt.Errorf("safetensors: %q has unsupported dtype %s", name, entry.DType)
	}

	return b.NewArrayFromBytes(rawBytes, entry.Shape, dtype)
}

// Has reports whether a tensor exists in the file (or any shard).
func (sf *SafetensorsFile) Has(name string) bool {
	if sf.weightMap != nil {
		_, ok := sf.weightMap[name]
		return ok
	}
	_, ok := sf.header[name]
	return ok
}

// Keys returns all tensor names in the safetensors file.
func (sf *SafetensorsFile) Keys() []string {
	if len(sf.header) > 0 {
		keys := make([]string, 0, len(sf.header))
		for k := range sf.header {
			keys = append(keys, k)
		}
		return keys
	}
	// Sharded model — keys come from the weight map.
	keys := make([]string, 0, len(sf.weightMap))
	for k := range sf.weightMap {
		keys = append(keys, k)
	}
	return keys
}

// Close releases the mmap'd data backing the safetensors file.
func (sf *SafetensorsFile) Close() error {
	if sf == nil {
		return nil
	}
	if sf.rawData != nil {
		sf.rawData = nil
	}
	return nil
}

// DetectWeightPrefix returns the safetensors key prefix the model actually
// uses. Different conversion pipelines store the same tensors under different
// prefixes:
//
//	raw HF Qwen3.5 checkpoint:     model.language_model.layers.N.*
//	mlx-community converted:       language_model.model.layers.N.*
//	qwen3 / single-stream models:  model.layers.N.*
//
// The first candidate whose `embed_tokens.weight` (or `norm.weight`, for
// models with tied embeddings) exists is returned. Candidates are probed in
// order; callers pass the arch hint (e.g. "model.language_model.") first so
// raw checkpoints match before the fallbacks.
func (sf *SafetensorsFile) DetectWeightPrefix(candidates []string) string {
	probes := []string{"embed_tokens.weight", "norm.weight", "layers.0.input_layernorm.weight"}
	for _, cand := range candidates {
		if cand == "" {
			cand = ""
		}
		for _, probe := range probes {
			if sf.Has(cand + probe) {
				return cand
			}
		}
	}
	return ""
}

func parseSafetensorsHeader(headerBytes []byte) (map[string]safetensorEntry, error) {
	result := make(map[string]safetensorEntry)
	if err := json.Unmarshal(headerBytes, &result); err != nil {
		return nil, fmt.Errorf("parse safetensors header: %w", err)
	}
	return result, nil
}
