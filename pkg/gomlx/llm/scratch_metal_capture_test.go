//go:build darwin && arm64 && cgo

package llm_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
)

// TestScratchMetalCapture captures ONLY the steady-state decode steps
// (capture starts after model load + warmup, which is where the earlier
// attempt crashed: warmup's kernel-cache churn trips the debug layer's
// pipeline-lifetime assertion). The .gputrace is opened in Instruments
// for per-kernel GPU times; compare against mlx-lm's capture of the same
// model to attribute the ~11ms context-scaled decode gap.
//
// SPROUT_CAPTURE_DIR sets the output dir; SPROUT_CAPTURE_EAGER=1 captures
// the eager path instead of compiled-default.
func TestScratchMetalCapture(t *testing.T) {
	dir := os.Getenv("SPROUT_MTP_PARITY_MODEL")
	capDir := os.Getenv("SPROUT_CAPTURE_DIR")
	if dir == "" || capDir == "" {
		t.Skip("SPROUT_MTP_PARITY_MODEL / SPROUT_CAPTURE_DIR not set")
	}

	model, err := llm.NewModel(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer model.Close()

	// Warmup decode (outside capture): also ensures the prompt-lookup or
	// compiled branch is fully initialized.
	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = 12
	cfg.Temperature = 0
	cfg.RepetitionPenalty = 0
	cfg.PromptLookupMaxDrafts = 0
	if _, err := model.GenerateText(context.Background(), "Warm up the decode path.", cfg); err != nil {
		t.Fatal(err)
	}

	name := "compiled.gputrace"
	if os.Getenv("SPROUT_CAPTURE_EAGER") == "1" {
		os.Setenv("SPROUT_COMPILED_DECODE", "0")
		name = "eager.gputrace"
	}

	if err := mlx.StartMetalCapture(capDir + "/" + name); err != nil {
		t.Fatalf("start capture: %v", err)
	}
	genErr := func() error {
		defer os.Unsetenv("SPROUT_CAPTURE_EAGER")
		_, err := model.GenerateText(context.Background(), "Write a paragraph about the sea.", cfg)
		return err
	}()
	stopErr := mlx.StopMetalCapture()
	if genErr != nil {
		t.Fatalf("generate under capture: %v", genErr)
	}
	if stopErr != nil {
		t.Fatalf("stop capture: %v", stopErr)
	}
	fmt.Printf("capture written: %s/%s\n", capDir, name)
}
