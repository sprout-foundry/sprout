//go:build darwin && arm64 && cgo

package main

import (
	"log"
	"os"
	"runtime"

	"github.com/sprout-foundry/sinter/llm"
	"github.com/sprout-foundry/sinter/llm/qwen35"
	"github.com/sprout-foundry/sinter/mlx"
	"github.com/sprout-foundry/sinter/tensor"
)

// Dumps the hidden state after each layer for a fixed prompt so the parity
// script (scripts/llm_parity.py) can compare layer-by-layer with mlx-lm.
// This isolates whether linear (DeltaNet) or full-attention layers diverge.
//
// Usage: llm_parity <model-dir> <dump-dir> ["prompt"]
func main() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if len(os.Args) < 3 {
		log.Fatalf("usage: %s <model-dir> <dump-dir> [prompt]", os.Args[0])
	}
	dir := os.Args[1]
	dumpDir := os.Args[2]
	prompt := "The capital of France is"
	if len(os.Args) > 3 {
		prompt = os.Args[3]
	}

	cfg, err := llm.LoadConfig(dir + "/config.json")
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	backend := tensor.DetectBackend()
	arch, err := qwen35.New(cfg, backend)
	if err != nil {
		log.Fatalf("arch: %v", err)
	}
	q := arch.(*qwen35.Qwen35)

	s, err := mlx.NewGPUStream()
	if err != nil {
		log.Fatal(err)
	}
	defer s.Free()

	// Load weights on the same thread/stream as the forward (thread-local).
	if err := q.InitWeights(dir+"/model.safetensors", s); err != nil {
		log.Fatalf("weights: %v", err)
	}
	defer q.FreeWeights()

	tok, err := llm.LoadTokenizer(dir + "/tokenizer.json")
	if err != nil {
		log.Fatalf("tokenizer: %v", err)
	}
	toks := tok.Encode(prompt)
	if len(toks) == 0 {
		log.Fatalf("empty prompt")
	}
	log.Printf("prompt %q -> %d tokens", prompt, len(toks))

	ids, err := idsArray(toks)
	if err != nil {
		log.Fatal(err)
	}
	defer ids.Free()

	cache := llm.NewKVCache(cfg.NumLayers, s, backend)
	defer cache.Free()

	if err := os.MkdirAll(dumpDir, 0o755); err != nil {
		log.Fatal(err)
	}
	if err := q.DebugDumpLayers(ids, len(toks), cache, s, dumpDir); err != nil {
		log.Fatalf("dump: %v", err)
	}
	log.Printf("dumped %d layers to %s", cfg.NumLayers, dumpDir)
}

func idsArray(toks []int) (*mlx.Array, error) {
	data := make([]int64, len(toks))
	for i, t := range toks {
		data[i] = int64(t)
	}
	return mlx.NewArrayFromInt64(data, []int{1, len(toks)})
}
