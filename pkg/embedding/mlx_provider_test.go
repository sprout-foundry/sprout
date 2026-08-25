//go:build darwin && arm64 && cgo && mlx

package embedding

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestMLXProviderParity validates that the MLX Metal provider produces
// embeddings that match the ONNX CPU provider to within fp16→int8 drift
// (cosine > 0.98). It also profiles memory usage during batch inference.
//
// Skipped if either model file is not present (no network in CI).
func TestMLXProviderParity(t *testing.T) {
	modelDir := DefaultModelDir()

	// Paths
	jinaCfg := JinaCodeV2Config()
	mlxCfg := JinaCodeV2SafetensorsConfig()
	onnxModelPath := filepath.Join(modelDir, jinaCfg.Name, jinaCfg.ModelFilename)
	mlxModelPath := filepath.Join(modelDir, mlxCfg.Name, mlxCfg.ModelFilename)
	tokenizerPath := filepath.Join(modelDir, jinaCfg.Name, "tokenizer.json")

	// Check models exist
	if _, err := os.Stat(onnxModelPath); err != nil {
		t.Skipf("ONNX model not present at %s", onnxModelPath)
	}
	if _, err := os.Stat(mlxModelPath); err != nil {
		t.Skipf("MLX safetensors model not present at %s", mlxModelPath)
	}
	if _, err := os.Stat(tokenizerPath); err != nil {
		t.Skipf("tokenizer not present at %s", tokenizerPath)
	}
	if !mlxProviderAvailable() {
		t.Skip("MLX provider not available on this platform")
	}

	ctx := context.Background()

	// ── Load ONNX provider (baseline) ──
	onnxProvider, _, err := acquireSharedJinaProvider(ctx, modelDir, jinaCfg)
	if err != nil {
		t.Fatalf("ONNX provider init: %v", err)
	}
	defer onnxProvider.Close()

	// ── Load MLX provider ──
	mlxProvider, err := NewMLXEmbeddingProvider(ctx, mlxModelPath, tokenizerPath)
	if err != nil {
		t.Fatalf("MLX provider init: %v", err)
	}
	defer mlxProvider.Close()

	// ── Test texts (code snippets of varying length) ──
	texts := []string{
		"func Sum(values []int) int {\n\ttotal := 0\n\tfor _, v := range values {\n\t\ttotal += v\n\t}\n\treturn total\n}",
		"func Add(nums []int) int {\n\tsum := 0\n\tfor _, n := range nums {\n\t\tsum += n\n\t}\n\treturn sum\n}",
		"sort an array of integers in place",
		"read a file and return its lines as a slice",
		"func processItem(item *Item) error {\n\tif item == nil {\n\t\treturn fmt.Errorf(\"nil item\")\n\t}\n\titem.processed = true\n\treturn nil\n}",
		"retry a network request with exponential backoff",
	}

	// ── Single-embed parity ──
	t.Run("SingleEmbed", func(t *testing.T) {
		for i, text := range texts {
			onnxE, err := onnxProvider.Embed(ctx, text)
			if err != nil {
				t.Fatalf("ONNX embed %d: %v", i, err)
			}
			mlxE, err := mlxProvider.Embed(ctx, text)
			if err != nil {
				t.Fatalf("MLX embed %d: %v", i, err)
			}

			cos := mlxCosineSim(onnxE, mlxE)
			t.Logf("[%d] cos=%.6f len_onnx=%d len_mlx=%d", i, cos, len(onnxE), len(mlxE))

			if cos < 0.98 {
				t.Errorf("text %d: cosine %.6f < 0.98", i, cos)
			}
		}
	})

	// ── Batch parity ──
	t.Run("BatchEmbed", func(t *testing.T) {
		onnxBatch, err := onnxProvider.EmbedBatch(ctx, texts)
		if err != nil {
			t.Fatalf("ONNX batch: %v", err)
		}
		mlxBatch, err := mlxProvider.EmbedBatch(ctx, texts)
		if err != nil {
			t.Fatalf("MLX batch: %v", err)
		}

		for i := range texts {
			cos := mlxCosineSim(onnxBatch[i], mlxBatch[i])
			t.Logf("  batch[%d] cos=%.6f", i, cos)
			if cos < 0.98 {
				t.Errorf("batch %d: cosine %.6f < 0.98", i, cos)
			}
		}
	})

	// ── Memory profile during batch inference ──
	t.Run("MemoryProfile", func(t *testing.T) {
		var m1, m2 runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&m1)

		// Run 10 batches of 8 texts each
		for iter := 0; iter < 10; iter++ {
			_, err := mlxProvider.EmbedBatch(ctx, texts)
			if err != nil {
				t.Fatalf("batch iter %d: %v", iter, err)
			}
		}

		runtime.ReadMemStats(&m2)
		allocAfter10Batches := int64(m2.Alloc) - int64(m1.Alloc)
		t.Logf("Memory after 10 batches: alloc=%d bytes (%.1f MB), total_alloc=%d, sys=%d (%.1f MB)",
			allocAfter10Batches,
			float64(allocAfter10Batches)/1024/1024,
			m2.TotalAlloc,
			m2.Sys,
			float64(m2.Sys)/1024/1024)

		// Heap should not grow unboundedly — after 10 batches, heap in-use
		// should be under ~500 MB (weights are persistent, transient tensors
		// are freed per-batch).
		if m2.HeapInuse > 500*1024*1024 {
			t.Errorf("heap in-use after 10 batches: %.1f MB (>500 MB leak?)",
				float64(m2.HeapInuse)/1024/1024)
		}
	})

	// ── Throughput comparison ──
	t.Run("Throughput", func(t *testing.T) {
		// Warmup both providers
		for i := 0; i < 5; i++ {
			_, _ = onnxProvider.EmbedBatch(ctx, texts)
			_, _ = mlxProvider.EmbedBatch(ctx, texts)
		}

		// ONNX baseline
		start := time.Now()
		n := 50
		for i := 0; i < n; i++ {
			_, err := onnxProvider.EmbedBatch(ctx, texts)
			if err != nil {
				t.Fatalf("ONNX throughput: %v", err)
			}
		}
		onnxDuration := time.Since(start)
		onnxRate := float64(n*len(texts)) / onnxDuration.Seconds()

		// MLX
		start = time.Now()
		for i := 0; i < n; i++ {
			_, err := mlxProvider.EmbedBatch(ctx, texts)
			if err != nil {
				t.Fatalf("MLX throughput: %v", err)
			}
		}
		mlxDuration := time.Since(start)
		mlxRate := float64(n*len(texts)) / mlxDuration.Seconds()

		speedup := mlxRate / onnxRate
		t.Logf("ONNX CPU: %.1f units/s (%.1fms/batch)", onnxRate, float64(onnxDuration.Milliseconds())/float64(n))
		t.Logf("MLX Metal: %.1f units/s (%.1fms/batch)", mlxRate, float64(mlxDuration.Milliseconds())/float64(n))
		t.Logf("Speedup: %.2fx", speedup)
	})
}

func mlxCosineSim(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		af, bf := float64(a[i]), float64(b[i])
		dot += af * bf
		na += af * af
		nb += bf * bf
	}
	if na < 1e-12 || nb < 1e-12 {
		return 0
	}
	return dot / math.Sqrt(na*nb)
}
