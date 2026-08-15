//go:build darwin && arm64 && cgo && mlx

// Command llm_download fetches the recommended model for this machine's RAM
// from HuggingFace (mlx-community quantized releases) into
// ~/dev/llm-models/, using the same catalog the server auto-selects from.
// Run it once; afterwards `llm_server` with no -model flag picks it up.
package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
)

func main() {
	ram := mlx.TotalSystemRAM()
	rec := llm.RecommendModelForRAM(ram)
	if rec == nil {
		log.Fatalf("no model recommended for %d bytes RAM", ram)
	}

	modelsRoot := os.Getenv("HOME") + "/dev/llm-models"
	dest := filepath.Join(modelsRoot, rec.Dir)

	if _, err := os.Stat(dest); err == nil {
		log.Printf("model %s already installed at %s", rec.Name, dest)
		return
	}

	fmt.Printf("Machine has %.0f GB RAM — recommending %s (%s)\n",
		float64(ram)/1073741824, rec.Name, rec.HFRepo)

	// Prefer the `hf` CLI (huggingface_hub ≥ 1.0); fall back to the legacy
	// huggingface-cli if only that exists.
	bin := "hf"
	if _, err := exec.LookPath("hf"); err != nil {
		bin = "huggingface-cli"
	}
	if _, err := exec.LookPath(bin); err != nil {
		log.Fatalf("%s not found — install huggingface_hub: pip install -U huggingface_hub", bin)
	}

	args := []string{"download", rec.HFRepo, "--local-dir", dest}
	cmd := exec.Command(bin, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatalf("download failed: %v", err)
	}

	fmt.Printf("\nInstalled %s at %s. Start the server with:\n  llm_server  # auto-selects by RAM\n", rec.Name, dest)
}
