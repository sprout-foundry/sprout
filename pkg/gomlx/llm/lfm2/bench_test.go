//go:build darwin && arm64 && cgo && mlx

package lfm2

import (
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
	"github.com/sprout-foundry/sprout/pkg/tensor"
)

func BenchmarkLFM2Decode(b *testing.B) {
	modelDir := os.Getenv("HOME") + "/dev/llm-models/lfm2.5-2.6b-mlx/5bit"
	if _, err := os.Stat(modelDir + "/config.json"); err != nil {
		b.Skipf("model not found at %s", modelDir)
	}

	backend := tensor.DetectBackend()
	if backend == nil || !backend.Available() {
		b.Skip("no GPU backend")
	}

	runtime.LockOSThread()
	stream, err := mlx.DefaultGPUStream()
	if err != nil {
		b.Fatal(err)
	}

	cfg, err := llm.LoadConfig(modelDir + "/config.json")
	if err != nil {
		b.Fatal(err)
	}

	arch, err := llm.ArchFactory("lfm2")
	if err != nil {
		b.Fatal(err)
	}
	impl, err := arch(cfg, backend)
	if err != nil {
		b.Fatal(err)
	}
	impl.SetStream(stream)
	if err := impl.InitWeights(modelDir+"/model.safetensors", stream); err != nil {
		b.Fatal(err)
	}
	defer impl.FreeWeights()
	if err := llm.ApplyMemoryLimits(); err != nil {
		b.Logf("memory limits: %v", err)
	}

	// Enable MLX graph compilation (fuses element-wise ops)
	if err := backend.EnableCompile(); err != nil {
		b.Logf("enable_compile: %v", err)
	}

	promptIDs := []int{124894, 124899, 5922, 207, 2992, 355, 229, 26, 19, 26, 39, 124900, 207, 124899, 63514, 207, 124901}
	idData := make([]int64, len(promptIDs))
	for i, id := range promptIDs {
		idData[i] = int64(id)
	}
	idsArr, err := backend.NewArrayFromInt64(idData, []int{1, len(promptIDs)})
	if err != nil {
		b.Fatal(err)
	}

	cache := llm.NewKVCache(cfg.NumLayers, stream, backend)
	logits, err := impl.ForwardPrefill(idsArr, len(promptIDs), cache)
	if err != nil {
		b.Fatal(err)
	}
	bestIdx := 0
	bestVal := logits[0]
	for i, v := range logits {
		if v > bestVal {
			bestVal = v
			bestIdx = i
		}
	}

	greedy := impl.(llm.GreedyArchitecture)
	token := bestIdx

	// Warmup decode
	for i := 0; i < 5; i++ {
		token, err = greedy.ForwardDecodeArgmax(token, len(promptIDs)+i, cache)
		if err != nil {
			b.Fatal(err)
		}
	}

	// Benchmark
	b.ResetTimer()
	start := time.Now()
	numTokens := 100
	for i := 0; i < numTokens; i++ {
		token, err = greedy.ForwardDecodeArgmax(token, len(promptIDs)+5+i, cache)
		if err != nil {
			b.Fatal(err)
		}
	}
	if err := stream.Synchronize(); err != nil {
		b.Fatal(err)
	}
	elapsed := time.Since(start)
	tps := float64(numTokens) / elapsed.Seconds()

	b.ReportMetric(tps, "tok/s")
	fmt.Printf("\nLFM2 decode: %d tokens in %v = %.1f tok/s\n", numTokens, elapsed, tps)
}
