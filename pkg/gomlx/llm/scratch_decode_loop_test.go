//go:build darwin && arm64 && cgo

package llm_test

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
)

// TestScratchDecodeLoop runs a 20K-context prefill then decodes for a fixed
// wall-clock duration (SPROUT_DECODE_SECONDS, default 10s), printing tok/s.
// Designed for GPU profiling under xctrace 'Metal System Trace': steady
// state decode, no warmup noise. SPROUT_PROFILE_EAGER=1 pins eager decode.
func TestScratchDecodeLoop(t *testing.T) {
	dir := os.Getenv("SPROUT_MTP_PARITY_MODEL")
	if dir == "" {
		t.Skip("SPROUT_MTP_PARITY_MODEL not set")
	}
	dur := 10
	if v := os.Getenv("SPROUT_DECODE_SECONDS"); v != "" {
		fmt.Sscanf(v, "%d", &dur)
	}

	model, err := llm.NewModel(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer model.Close()

	words := []string{"func", "return", "error", "nil", "string", "int", "struct",
		"interface", "package", "import", "context", "time", "sync", "mutex",
		"append", "len", "make", "the", "and", "of", "to", "in", "a", "is"}
	r := rand.New(rand.NewSource(42))
	var buf []byte
	buf = append(buf, "Continue this text.\n\n"...)
	for i := 0; i < 20000; i++ {
		buf = append(buf, words[r.Intn(len(words))]...)
		buf = append(buf, ' ')
	}

	cfg := llm.DefaultGenerateConfig()
	cfg.MaxTokens = 4096
	cfg.Temperature = 0
	cfg.RepetitionPenalty = 0
	cfg.PromptLookupMaxDrafts = 0
	if os.Getenv("SPROUT_PROFILE_EAGER") == "1" {
		os.Setenv("SPROUT_COMPILED_DECODE", "0")
	}

	var first, last time.Time
	count := 0
	var deadline time.Time
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err = model.Generate(ctx, string(buf), cfg, func(id int) {
		now := time.Now()
		if count == 0 {
			first = now
			deadline = now.Add(time.Duration(dur) * time.Second)
		} else if now.After(deadline) {
			cancel()
		}
		last = now
		count++
	})
	if err != nil && ctx.Err() == nil {
		t.Fatalf("Generate: %v", err)
	}
	if count > 1 {
		el := last.Sub(first).Seconds()
		fmt.Printf("DECODE_LOOP: %d tokens in %.2fs = %.1f tok/s\n", count-1, el, float64(count-1)/el)
	} else {
		fmt.Println("DECODE_LOOP: no tokens decoded (prefill too slow?)")
	}
}
