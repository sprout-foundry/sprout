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

// TestTPSCrossoverSweep measures eager vs compiled decode tok/s across
// prompt lengths (1K..16K) on one model instance, to locate the context
// length where compiled decode stops winning (the staging cost scales with
// KV buffer size; the CPU graph-walk savings are constant). Raises the
// compiled context cutoff so compiled runs at every length; pins eager via
// the opt-out. Not part of the default test run's fast path — it loads the
// live model and takes minutes.
func TestTPSCrossoverSweep(t *testing.T) {
	dir := os.Getenv("SPROUT_MTP_PARITY_MODEL")
	if dir == "" {
		t.Skip("SPROUT_MTP_PARITY_MODEL not set")
	}
	os.Setenv("SPROUT_COMPILED_DECODE_CTX_LIMIT", "1000000")

	model, err := llm.NewModel(dir)
	if err != nil {
		t.Fatalf("NewModel: %v", err)
	}
	defer model.Close()

	words := []string{"func", "return", "error", "nil", "string", "int", "struct",
		"interface", "package", "import", "context", "time", "sync", "mutex",
		"append", "len", "make", "the", "and", "of", "to", "in", "a", "is"}
	r := rand.New(rand.NewSource(42))
	soup := make([]string, 16400)
	for i := range soup {
		soup[i] = words[r.Intn(len(words))]
	}

	run := func(nWords int) (toks int, tps float64) {
		prompt := "Write a very long, detailed short story about a robot exploring an alien planet, with lots of description. "
		for i := 0; i < nWords; i++ {
			prompt += soup[i] + " "
		}
		cfg := llm.DefaultGenerateConfig()
		cfg.MaxTokens = 40
		cfg.Temperature = 0
		cfg.RepetitionPenalty = 0
		var first, last time.Time
		count := 0
		if err := model.Generate(context.Background(), prompt, cfg, func(id int) {
			now := time.Now()
			if count == 0 {
				first = now
			}
			last = now
			count++
		}); err != nil {
			t.Fatalf("Generate: %v", err)
		}
		decode := count - 1
		el := last.Sub(first).Seconds()
		if decode > 0 && el > 0 {
			return count, float64(decode) / el
		}
		return count, 0
	}

	lengths := []int{1000, 2000, 4000, 8000, 12000, 16000}
	for _, n := range lengths {
		os.Setenv("SPROUT_COMPILED_DECODE", "0")
		_, eagerTPS := run(n)
		os.Setenv("SPROUT_COMPILED_DECODE", "1")
		_, compiledTPS := run(n)
		os.Unsetenv("SPROUT_COMPILED_DECODE")
		verdict := "compiled"
		if compiledTPS < eagerTPS {
			verdict = "eager"
		}
		fmt.Printf("ctx~%d: eager=%.1f tok/s compiled=%.1f tok/s -> %s wins\n",
			n, eagerTPS, compiledTPS, verdict)
	}
}
