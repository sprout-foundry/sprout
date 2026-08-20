// Command llm_convert loads a raw HF model and produces an mlx-community-style
// quantized export. The output directory contains pre-quantized packed weights
// (weight + .scales + .biases triplets) that load instantly without GO_QUANTIZE
// and use the fast quantized matmul kernel at full speed.
//
//go:build darwin && arm64 && cgo && mlx

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sprout-foundry/sinter/llm"
	"github.com/sprout-foundry/sinter/tensor"

	_ "github.com/sprout-foundry/sinter/llm/qwen35"
)

func main() {
	input := flag.String("input", "", "path to raw HF model directory")
	output := flag.String("output", "", "path to output directory for quantized model")
	bits := flag.Int("bits", 6, "quantization bits (4, 6, or 8)")
	embedBits := flag.Int("embed-bits", 0, "bits for embed_tokens/lm_head (0 = same as -bits). Use 8 for q5 bodies: the loader must materialize a full fp32 embedding table for widths outside {2,3,4,6,8}, which costs several GB at large vocab sizes")
	groupSize := flag.Int("group-size", 64, "quantization group size")
	mode := flag.String("mode", "affine", "quantization mode")
	flag.Parse()

	if *input == "" || *output == "" {
		log.Fatal("usage: llm_convert -input <raw-hf-dir> -output <mlx-dir> [-bits 6] [-group-size 64]")
	}

	runtime.LockOSThread()
	backend := tensor.DetectBackend()

	quantCfg := &llm.QuantConfig{
		GroupSize: *groupSize,
		Bits:      *bits,
		Mode:      *mode,
	}
	embedQuantCfg := quantCfg
	if *embedBits != 0 && *embedBits != *bits {
		embedQuantCfg = &llm.QuantConfig{
			GroupSize: *groupSize,
			Bits:      *embedBits,
			Mode:      *mode,
		}
	}

	log.Printf("converting %s → %s (Q%d, embed Q%d, group=%d)", *input, *output, *bits, embedQuantCfg.Bits, *groupSize)

	if err := os.MkdirAll(*output, 0o755); err != nil {
		log.Fatalf("mkdir %s: %v", *output, err)
	}

	// Load config.json and inject quantization metadata.
	if err := copyConfigWithQuant(*input, *output, quantCfg); err != nil {
		log.Fatalf("config: %v", err)
	}

	// Copy non-weight files (tokenizer, etc).
	if err := copyAuxFiles(*input, *output); err != nil {
		log.Fatalf("aux files: %v", err)
	}

	// Open the raw safetensors and quantize each weight.
	stream, err := backend.DefaultStream()
	if err != nil {
		log.Fatalf("stream: %v", err)
	}

	sf, err := llm.OpenSafetensors(filepath.Join(*input, "model.safetensors"))

	keys := sfKeys(sf)
	log.Printf("found %d tensors", len(keys))

	outPath := filepath.Join(*output, "model.safetensors")
	w, err := os.Create(outPath)
	if err != nil {
		log.Fatalf("create output: %v", err)
	}
	defer w.Close()

	var queue []pending
	var totalSize int64

	for _, key := range keys {
		// Skip multimodal towers entirely: the language-model loaders probe
		// for language-model keys and never touch visual/audio tensors, so
		// quantizing them only bloats the output (a 2.5 GB vision tower on
		// the 9B) and some have dims indivisible by the group size, which
		// makes quantize fail outright.
		if isTowerTensor(key) {
			continue
		}
		arr, err := sf.Get(key, backend, stream)
		if err != nil {
			log.Printf("skip %s: %v", key, err)
			continue
		}

		shape := arr.Shape()
		shouldQuantize := false
		if len(shape) == 2 {
			base := strings.TrimSuffix(key, ".weight")
			if key == base+".weight" && !strings.Contains(key, "norm") &&
				!strings.Contains(key, "embed_tokens") &&
				!strings.Contains(key, "lm_head") {
				// Check if it's a projection weight (not a norm or embedding)
				if sf.Has(base + ".scales") {
					// Already quantized — just copy
				} else {
					shouldQuantize = true
				}
			}
		}

		if !shouldQuantize && !strings.Contains(key, "embed_tokens") && !strings.Contains(key, "lm_head") {
			queue = append(queue, pending{name: key, arr: arr})
			totalSize += int64(dataSize(arr))
			continue
		}
		// embed_tokens/lm_head fall through to the dedicated loop below, which
		// quantizes them (optionally at separate embed bits) — queueing the
		// raw copy here too would write both versions to the file: the
		// safetensors header keeps only the quantized triplet, but the raw
		// bytes still land in the file as dead weight (~4 GB on a 9B with
		// untied lm_head) and the RAM gate measures file size, not live
		// weights.
		if !shouldQuantize {
			arr.Free()
			continue
		}

		// Quantize this weight.
		parts, err := backend.Quantize(arr, *groupSize, *bits, *mode, stream)
		if err != nil {
			// Dims not divisible by the group size can't be quantized — keep
			// the tensor as-is rather than queueing a freed array.
			log.Printf("keep %s unquantized: %v", key, err)
			queue = append(queue, pending{name: key, arr: arr})
			totalSize += int64(dataSize(arr))
			continue
		}
		arr.Free()
		for _, p := range parts {
			if err := p.Eval(); err != nil {
				log.Fatalf("eval quantized %s: %v", key, err)
			}
		}

		base := strings.TrimSuffix(key, ".weight")
		queue = append(queue, pending{name: base + ".weight", arr: parts[0]})
		queue = append(queue, pending{name: base + ".scales", arr: parts[1]})
		if len(parts) > 2 {
			queue = append(queue, pending{name: base + ".biases", arr: parts[2]})
		}
		totalSize += int64(dataSize(parts[0])) + int64(dataSize(parts[1]))
	}

	// Also quantize embed_tokens (tied lm_head). Matches the same key set
	// the first loop defers here (contains "embed_tokens"/"lm_head") —
	// including gemma4's embed_tokens_per_layer — optionally at separate
	// embed bits.
	for _, key := range keys {
		if strings.Contains(key, "embed_tokens") || strings.Contains(key, "lm_head") {
			if !strings.HasSuffix(key, ".weight") {
				continue
			}
			arr, err := sf.Get(key, backend, stream)
			if err != nil {
				continue
			}
			base := strings.TrimSuffix(key, ".weight")
			if sf.Has(base + ".scales") {
				arr.Free()
				continue
			}
			parts, err := backend.Quantize(arr, embedQuantCfg.GroupSize, embedQuantCfg.Bits, embedQuantCfg.Mode, stream)
			if err != nil {
				log.Printf("quantize %s failed: %v", key, err)
				arr.Free()
				continue
			}
			arr.Free()
			for _, p := range parts {
				if err := p.Eval(); err != nil {
					log.Fatalf("eval quantized %s: %v", key, err)
				}
			}
			queue = append(queue, pending{name: base + ".weight", arr: parts[0]})
			queue = append(queue, pending{name: base + ".scales", arr: parts[1]})
			if len(parts) > 2 {
				queue = append(queue, pending{name: base + ".biases", arr: parts[2]})
			}
			totalSize += int64(dataSize(parts[0])) + int64(dataSize(parts[1]))
		}
	}

	log.Printf("writing %d tensors (%.1f MB) to %s", len(queue), float64(totalSize)/1e6, outPath)

	// Write safetensors file manually: header JSON + raw data.
	if err := writeSafetensors(w, queue, backend, stream); err != nil {
		log.Fatalf("write safetensors: %v", err)
	}

	log.Printf("done — %s", *output)
}

func copyConfigWithQuant(input, output string, quant *llm.QuantConfig) error {
	raw, err := os.ReadFile(filepath.Join(input, "config.json"))
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(raw, &cfg); nil != err {
		return fmt.Errorf("parse config: %w", err)
	}
	cfg["quantization"] = map[string]interface{}{
		"group_size": quant.GroupSize,
		"bits":       quant.Bits,
		"mode":       quant.Mode,
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(filepath.Join(output, "config.json"), out, 0o644)
}

// isTowerTensor reports whether a tensor belongs to a multimodal tower that
// the language-model loaders never read (vision/audio encoders and their
// embedders/projectors).
func isTowerTensor(key string) bool {
	for _, p := range []string{
		"visual.", "vision_tower.", "audio_tower.", "embed_audio.", "embed_vision.",
		"vision_embedder.", "multi_modal_projector.",
	} {
		if strings.Contains(key, p) {
			return true
		}
	}
	return false
}

func copyAuxFiles(input, output string) error {
	entries, err := os.ReadDir(input)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if name == "config.json" || strings.HasSuffix(name, ".safetensors") ||
			strings.HasSuffix(name, ".safetensors.index.json") {
			continue
		}
		src := filepath.Join(input, name)
		dst := filepath.Join(output, name)
		data, err := os.ReadFile(src)
		if err != nil {
			log.Printf("skip aux %s: %v", name, err)
			continue
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			log.Printf("write aux %s: %v", name, err)
		}
	}
	return nil
}

func dataSize(arr tensor.Array) int {
	shape := arr.Shape()
	dt := arr.Dtype()
	var elemSize int
	switch dt {
	case tensor.Float32, tensor.Float64, tensor.Int32, tensor.UInt32:
		elemSize = 4
	case tensor.Float16, tensor.BFloat16, tensor.Int16, tensor.UInt16:
		elemSize = 2
	case tensor.Int8, tensor.UInt8, tensor.Bool:
		elemSize = 1
	case tensor.Int64, tensor.UInt64:
		elemSize = 8
	default:
		elemSize = 4
	}
	n := 1
	for _, d := range shape {
		n *= d
	}
	return n * elemSize
}

// writeSafetensors writes a safetensors file with the queued tensors.
func writeSafetensors(f *os.File, queue []pending, b tensor.Backend, s tensor.Stream) error {
	type entry struct {
		dtype      string
		shape      []int
		dataOffset [2]uint64
	}

	header := make(map[string]interface{})
	var offset uint64

	for _, p := range queue {
		dt := dtypeToString(p.arr.Dtype())
		shape := p.arr.Shape()
		// Convert []int to []interface{} for JSON
		shapeIface := make([]interface{}, len(shape))
		for i, d := range shape {
			shapeIface[i] = d
		}

		sz := dataSize(p.arr)
		header[p.name] = map[string]interface{}{
			"dtype":        dt,
			"shape":        shapeIface,
			"data_offsets": []uint64{offset, offset + uint64(sz)},
		}
		offset += uint64(sz)
	}

	// Evaluate all arrays
	for _, p := range queue {
		if err := p.arr.Eval(); err != nil {
			return fmt.Errorf("eval %s: %w", p.name, err)
		}
	}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return fmt.Errorf("marshal header: %w", err)
	}

	// Pad header to 8-byte boundary with spaces (not nulls, which break JSON parsers)
	pad := (8 - len(headerJSON)%8) % 8
	if pad > 0 {
		headerJSON = append(headerJSON, make([]byte, pad)...)
		for i := len(headerJSON) - pad; i < len(headerJSON); i++ {
			headerJSON[i] = ' '
		}
	}

	// Write header length + header
	hdrLen := uint64(len(headerJSON))
	if _, err := f.Write([]byte{byte(hdrLen), byte(hdrLen >> 8), byte(hdrLen >> 16), byte(hdrLen >> 24),
		byte(hdrLen >> 32), byte(hdrLen >> 40), byte(hdrLen >> 48), byte(hdrLen >> 56)}); err != nil {
		return fmt.Errorf("write header len: %w", err)
	}
	if _, err := f.Write(headerJSON); err != nil {
		return fmt.Errorf("write header: %w", err)
	}

	// Write data — read each array's raw bytes and write to file
	for _, p := range queue {
		data, err := readArrayBytes(p.arr, b, s)
		if err != nil {
			return fmt.Errorf("read %s: %w", p.name, err)
		}
		if _, err := f.Write(data); err != nil {
			return fmt.Errorf("write %s: %w", p.name, err)
		}
	}

	return nil
}

// sfKeys returns all tensor keys in a SafetensorsFile.
func sfKeys(sf *llm.SafetensorsFile) []string {
	keys := sf.Keys()
	out := keys[:0]
	for _, k := range keys {
		// __metadata__ is a safetensors file-level metadata record, not a
		// tensor — it has no data_offsets and panics the reader.
		if k == "__metadata__" {
			continue
		}
		out = append(out, k)
	}
	return out
}

type pending struct {
	name string
	arr  tensor.Array
}

func dtypeToString(dt tensor.Dtype) string {
	switch dt {
	case tensor.Float32:
		return "F32"
	case tensor.Float16:
		return "F16"
	case tensor.BFloat16:
		return "BF16"
	case tensor.Int32:
		return "I32"
	case tensor.UInt32:
		return "U32"
	case tensor.Int8:
		return "I8"
	case tensor.UInt8:
		return "U8"
	case tensor.Bool:
		return "BOOL"
	case tensor.Int64:
		return "I64"
	case tensor.UInt64:
		return "U64"
	case tensor.Int16:
		return "I16"
	case tensor.UInt16:
		return "U16"
	case tensor.Float64:
		return "F64"
	default:
		return "F32"
	}
}

func readArrayBytes(arr tensor.Array, b tensor.Backend, s tensor.Stream) ([]byte, error) {
	return arr.RawBytes()
}
