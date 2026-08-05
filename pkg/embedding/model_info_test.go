package embedding

import "testing"

// The settings panel used to hardcode the model name, quantization and dims,
// and reported bge-base-en-v1.5 / INT8 / 256 long after the default moved to
// EmbeddingGemma-300M / Q4F16 / 768. ActiveModelInfo must track the config the
// provider is actually built from.
func TestActiveModelInfoMatchesProviderConfig(t *testing.T) {
	cfg := EmbeddingGemma300MConfig()
	got := ActiveModelInfo()

	if got.Name != cfg.Name {
		t.Errorf("Name = %q, want %q", got.Name, cfg.Name)
	}
	if got.Dims != cfg.Dims {
		t.Errorf("Dims = %d, want %d", got.Dims, cfg.Dims)
	}
	if got.FullDims != cfg.FullDims {
		t.Errorf("FullDims = %d, want %d", got.FullDims, cfg.FullDims)
	}
	if got.Truncated {
		t.Errorf("Truncated = true, but Dims(%d) == FullDims(%d)", got.Dims, got.FullDims)
	}
}

// Guards the specific stale values the panel used to show.
func TestActiveModelInfoIsNotTheStalePanelValues(t *testing.T) {
	got := ActiveModelInfo()
	if got.Name == "bge-base-en-v1.5" {
		t.Error("reporting the retired bge model name")
	}
	if got.Dims == 256 && got.FullDims == 256 {
		t.Error("reporting the retired 256-dim figure")
	}
	if got.Quantization == "unknown" {
		t.Errorf("quantization unresolved from filename %q", EmbeddingGemma300MConfig().ModelFilename)
	}
}

func TestQuantizationFromFilename(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"model_q4f16.onnx", "Q4F16"},
		{"model_q4.onnx", "Q4"},
		{"model_fp16.onnx", "FP16"},
		{"model.onnx", "unknown"},
		{"", "unknown"},
	} {
		if got := quantizationFromFilename(tc.in); got != tc.want {
			t.Errorf("quantizationFromFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A truncated (Matryoshka) config must set Truncated so the UI can say so.
func TestModelInfoFlagsTruncation(t *testing.T) {
	got := modelInfoFor(ModelConfig{Name: "x", ModelFilename: "model_q8.onnx", Dims: 256, FullDims: 768})
	if !got.Truncated {
		t.Error("Dims 256 < FullDims 768 should set Truncated")
	}
	if got.Quantization != "Q8" {
		t.Errorf("Quantization = %q, want Q8", got.Quantization)
	}
}
