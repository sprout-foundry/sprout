package embedding

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestTokenizerBasic(t *testing.T) {
	data, err := os.ReadFile("models/tokenizer.json")
	if err != nil {
		t.Skipf("tokenizer.json not found: %v", err)
	}

	tok, err := NewTokenizerJSON(data, 128)
	if err != nil {
		t.Fatalf("load tokenizer: %v", err)
	}

	ids, mask := tok.Tokenize("hello world")
	if len(ids) != 128 {
		t.Errorf("expected 128 token IDs, got %d", len(ids))
	}
	if len(mask) != 128 {
		t.Errorf("expected 128 mask values, got %d", len(mask))
	}

	// First token should be [CLS] = 101
	if ids[0] != 101 {
		t.Errorf("expected first token to be [CLS]=101, got %d", ids[0])
	}
	// Mask should be 1 for real tokens, 0 for padding
	if mask[0] != 1 {
		t.Errorf("expected mask[0]=1, got %d", mask[0])
	}
}

func TestTokenizerBatch(t *testing.T) {
	data, err := os.ReadFile("models/tokenizer.json")
	if err != nil {
		t.Skipf("tokenizer.json not found: %v", err)
	}

	tok, err := NewTokenizerJSON(data, 128)
	if err != nil {
		t.Fatalf("load tokenizer: %v", err)
	}

	texts := []string{"hello world", "read file from disk"}
	ids, mask, batch := tok.TokenizeBatch(texts)
	if batch != 2 {
		t.Errorf("expected batch=2, got %d", batch)
	}
	if len(ids) != 2*128 {
		t.Errorf("expected %d token IDs, got %d", 2*128, len(ids))
	}
	if len(mask) != 2*128 {
		t.Errorf("expected %d mask values, got %d", 2*128, len(mask))
	}
}

func TestBundledProviderEmbed(t *testing.T) {
	ortLib := os.Getenv("ONNXRUNTIME_LIB")
	if ortLib == "" {
		// Prefer cached (downloaded) library first, then try other locations.
		cacheDir := ortCacheDir()
		cached := filepath.Join(cacheDir, "libonnxruntime.so.1.25.1")
		if _, err := os.Stat(cached); err == nil {
			ortLib = cached
		}
	}
	if ortLib == "" {
		// Try common locations
		candidates := []string{
			"/tmp/onnxruntime-linux-aarch64-1.25.1/lib/libonnxruntime.so.1.25.1",
			"/tmp/onnxruntime-linux-aarch64-1.22.0/lib/libonnxruntime.so.1.22.0",
			"/tmp/onnxruntime-linux-aarch64-1.21.0/lib/libonnxruntime.so.1.21.0",
			"/usr/local/lib/libonnxruntime.so",
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				ortLib = c
				break
			}
		}
	}
	if ortLib == "" {
		t.Skip("ONNX Runtime shared library not found; set ONNXRUNTIME_LIB to run this test")
	}

	modelDir, _ := filepath.Abs("models")
	provider, err := NewBundledProvider(modelDir, ortLib)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	defer provider.Close()

	vec, err := provider.Embed(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("embed: %v", err)
	}

	if len(vec) != 384 {
		t.Errorf("expected 384-dim vector, got %d", len(vec))
	}

	// Verify L2 normalization: sum of squares should be ~1.0
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	if norm < 0.99 || norm > 1.01 {
		t.Errorf("expected L2 norm ~1.0, got %f", norm)
	}
}

func TestBundledProviderEmbedBatch(t *testing.T) {
	ortLib := os.Getenv("ONNXRUNTIME_LIB")
	if ortLib == "" {
		// Prefer cached (downloaded) library first, then try other locations.
		cacheDir := ortCacheDir()
		cached := filepath.Join(cacheDir, "libonnxruntime.so.1.25.1")
		if _, err := os.Stat(cached); err == nil {
			ortLib = cached
		}
	}
	if ortLib == "" {
		// Try common locations
		candidates := []string{
			"/tmp/onnxruntime-linux-aarch64-1.25.1/lib/libonnxruntime.so.1.25.1",
			"/tmp/onnxruntime-linux-aarch64-1.22.0/lib/libonnxruntime.so.1.22.0",
			"/tmp/onnxruntime-linux-aarch64-1.21.0/lib/libonnxruntime.so.1.21.0",
			"/usr/local/lib/libonnxruntime.so",
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				ortLib = c
				break
			}
		}
	}
	if ortLib == "" {
		t.Skip("ONNX Runtime shared library not found; set ONNXRUNTIME_LIB to run this test")
	}

	modelDir, _ := filepath.Abs("models")
	provider, err := NewBundledProvider(modelDir, ortLib)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	defer provider.Close()

	texts := []string{"read file from disk", "write data to output"}
	vecs, err := provider.EmbedBatch(context.Background(), texts)
	if err != nil {
		t.Fatalf("embed batch: %v", err)
	}

	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}
	for i, vec := range vecs {
		if len(vec) != 384 {
			t.Errorf("vec[%d]: expected 384-dim, got %d", i, len(vec))
		}
	}

	// Verify cosine similarity between semantically similar texts
	sim := CosineSimilarity(vecs[0], vecs[1])
	if sim < 0.3 {
		t.Errorf("expected some similarity between related texts, got %f", sim)
	}

	// Verify cosine similarity of a text with itself is 1.0
	selfSim := CosineSimilarity(vecs[0], vecs[0])
	if selfSim < 0.999 {
		t.Errorf("expected self-similarity ~1.0, got %f", selfSim)
	}
}

func TestBundledProviderDeterminism(t *testing.T) {
	ortLib := os.Getenv("ONNXRUNTIME_LIB")
	if ortLib == "" {
		// Prefer cached (downloaded) library first, then try other locations.
		cacheDir := ortCacheDir()
		cached := filepath.Join(cacheDir, "libonnxruntime.so.1.25.1")
		if _, err := os.Stat(cached); err == nil {
			ortLib = cached
		}
	}
	if ortLib == "" {
		// Try common locations
		candidates := []string{
			"/tmp/onnxruntime-linux-aarch64-1.25.1/lib/libonnxruntime.so.1.25.1",
			"/tmp/onnxruntime-linux-aarch64-1.22.0/lib/libonnxruntime.so.1.22.0",
			"/tmp/onnxruntime-linux-aarch64-1.21.0/lib/libonnxruntime.so.1.21.0",
			"/usr/local/lib/libonnxruntime.so",
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				ortLib = c
				break
			}
		}
	}
	if ortLib == "" {
		t.Skip("ONNX Runtime shared library not found; set ONNXRUNTIME_LIB to run this test")
	}

	modelDir, _ := filepath.Abs("models")
	provider, err := NewBundledProvider(modelDir, ortLib)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	defer provider.Close()

	// Same text should produce identical embeddings
	vec1, _ := provider.Embed(context.Background(), "hello world")
	vec2, _ := provider.Embed(context.Background(), "hello world")

	for i := range vec1 {
		if vec1[i] != vec2[i] {
			t.Errorf("embedding not deterministic at index %d: %f != %f", i, vec1[i], vec2[i])
			break
		}
	}
}
