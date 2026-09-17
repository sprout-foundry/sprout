package localmodel

import (
	"os"
	"path/filepath"
	"testing"
)

// TestValidModelConfig covers the corrupt-install guard: a truncated or
// empty config.json (observed after an interrupted download left a 2-byte
// "{}" behind) must fail validation so selection skips the directory
// instead of panicking inside sinter's LoadConfig at load time.
func TestValidModelConfig(t *testing.T) {
	cases := []struct {
		name    string
		content string // written to config.json; empty means no file
		want    bool
	}{
		{"valid top-level", `{"num_attention_heads": 16, "hidden_size": 2048}`, true},
		{"valid nested text_config", `{"text_config": {"num_attention_heads": 24, "hidden_size": 5120}}`, true},
		{"empty object (the corruption)", `{}`, false},
		{"truncated json", `{"num_attention_heads":`, false},
		{"heads zero", `{"num_attention_heads": 0, "hidden_size": 2048}`, false},
		{"hidden zero", `{"num_attention_heads": 16, "hidden_size": 0}`, false},
		{"heads only in zero text_config", `{"num_attention_heads": 0, "text_config": {"num_attention_heads": 16, "hidden_size": 4096}}`, true},
		{"missing file (tuned/gguf variants)", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.content != "" {
				if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(tc.content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if got := validModelConfig(dir); got != tc.want {
				t.Errorf("validModelConfig(%q) = %v, want %v", tc.content, got, tc.want)
			}
		})
	}
}

// TestListModelsSkipsCorruptInstall verifies the selection-level guard:
// a weights directory with an invalid config.json is not listed as
// installed, so auto-resolution falls through to healthy candidates.
func TestListModelsSkipsCorruptInstall(t *testing.T) {
	root := t.TempDir()

	corrupt := filepath.Join(root, "minicpm5-2b-mlx")
	if err := os.MkdirAll(corrupt, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(corrupt, "model.safetensors"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The corruption: config.json exists but is empty JSON.
	if err := os.WriteFile(filepath.Join(corrupt, "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	healthy := filepath.Join(root, "gemma-4-e2b-it-5bit")
	if err := os.MkdirAll(healthy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(healthy, "model.safetensors"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := `{"text_config": {"num_attention_heads": 8, "hidden_size": 1536}}`
	if err := os.WriteFile(filepath.Join(healthy, "config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	old := DefaultModelsDir
	DefaultModelsDir = root
	defer func() { DefaultModelsDir = old }()

	models := ListModels()
	for _, m := range models {
		if m.Dir == corrupt && m.Installed {
			t.Errorf("corrupt install %s still listed as installed", m.Dir)
		}
	}
	found := false
	for _, m := range models {
		if m.Dir == healthy && m.Installed {
			found = true
		}
	}
	if !found {
		t.Errorf("healthy install %s missing from ListModels installed set", healthy)
	}
}
