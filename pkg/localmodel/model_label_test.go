package localmodel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// Tier rows whose ResolveModelID pick is an installed sprout-tuned variant
// load those weights, not the plain catalog download. TestTierRowLabel
// guards that such a row visibly says so — without the annotation the tuned
// build hides behind the bare catalog name with no trace in the picker.
func TestTierRowLabel(t *testing.T) {
	root := t.TempDir()
	// qwen3.5-4b's tuned variant: same param size, quantized (passes the
	// RAM gate), so ResolveModelID("qwen3.5-4b") resolves to it.
	tuned := filepath.Join(root, "qwen35-4b-sprout-tuned-mlx-q5")
	if err := os.MkdirAll(tuned, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tuned, "model.safetensors"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	old := DefaultModelsDir
	DefaultModelsDir = root
	defer func() { DefaultModelsDir = old }()

	infos := TieredModelInfos(16 * 1024 * 1024 * 1024)
	var tier *api.ModelInfo
	for i := range infos {
		if infos[i].ID == "qwen3.5-4b" {
			tier = &infos[i]
		}
	}
	if tier == nil {
		t.Fatal("qwen3.5-4b tier row missing from TieredModelInfos output")
	}
	if want := "qwen3.5-4b (sprout-tuned mlx-q5)"; tier.Name != want {
		t.Fatalf("tier row Name = %q, want %q", tier.Name, want)
	}
	if !taggedWith(tier.Tags, "sprout-tuned") {
		t.Fatalf("tier row Tags = %v, want to contain sprout-tuned", tier.Tags)
	}
}

// The raw-bf16 tuned build (no quant suffix) must not produce a label with
// a stray trailing space — the old direct Sprintf produced
// "gemma4-12b-sprout-tuned (sprout-tuned )". Uses a 12b build: no catalog
// tier matches that param size, forcing the beyond-tier explicit-row path.
func TestBeyondTierRowLabelNoTrailingSpace(t *testing.T) {
	root := t.TempDir()
	raw := filepath.Join(root, "gemma4-12b-sprout-tuned")
	if err := os.MkdirAll(raw, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(raw, "model.safetensors"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	old := DefaultModelsDir
	DefaultModelsDir = root
	defer func() { DefaultModelsDir = old }()

	infos := TieredModelInfos(16 * 1024 * 1024 * 1024)
	var row *api.ModelInfo
	for i := range infos {
		if infos[i].ID == "gemma4-12b-sprout-tuned" {
			row = &infos[i]
		}
	}
	if row == nil {
		t.Fatal("beyond-tier explicit row for the raw tuned build missing")
	}
	if want := "gemma4-12b-sprout-tuned (sprout-tuned)"; row.Name != want {
		t.Fatalf("explicit row Name = %q, want %q", row.Name, want)
	}
	if strings.Contains(row.Name, " )") {
		t.Fatalf("explicit row Name = %q, contains stray trailing space", row.Name)
	}
}

// Unannotated tier rows — when no tuned variant is installed, the bare
// catalog name and no sprout-tuned tag — must be unchanged.
func TestTierRowLabelNoTunedInstalled(t *testing.T) {
	root := t.TempDir()
	// Nothing installed; every tier row stays plain.
	old := DefaultModelsDir
	DefaultModelsDir = root
	defer func() { DefaultModelsDir = old }()

	infos := TieredModelInfos(16 * 1024 * 1024 * 1024)
	var tier *api.ModelInfo
	for i := range infos {
		if infos[i].ID == "qwen3.5-4b" {
			tier = &infos[i]
		}
	}
	if tier == nil {
		t.Fatal("qwen3.5-4b tier row missing")
	}
	if tier.Name != "qwen3.5-4b" {
		t.Fatalf("tier row Name = %q, want bare catalog name", tier.Name)
	}
	if taggedWith(tier.Tags, "sprout-tuned") {
		t.Fatalf("tier row Tags = %v, must not contain sprout-tuned", tier.Tags)
	}
}

func taggedWith(tags []string, s string) bool {
	for _, v := range tags {
		if v == s {
			return true
		}
	}
	return false
}
