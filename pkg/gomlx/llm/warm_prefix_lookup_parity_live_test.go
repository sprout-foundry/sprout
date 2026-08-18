//go:build darwin && arm64 && cgo

package llm_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	_ "github.com/sprout-foundry/sprout/pkg/gomlx/llm/gemma4"
)

// TestWarmPrefixLookupParityLiveModel decomposes the agent path: first turn
// = WarmSystemPrefix(system+tools) followed by Generate(full prompt) which
// restores the warmed slot and delta-prefills the user turn. Each factor
// (warm vs cold, lookup k=6 vs k=0) is compared against the cold+k=0
// baseline. Any divergence isolates which mechanism corrupts generation.
// Skips when SPROUT_MTP_PARITY_MODEL isn't set.
func TestWarmPrefixLookupParityLiveModel(t *testing.T) {
	dir := os.Getenv("SPROUT_MTP_PARITY_MODEL")
	if dir == "" {
		t.Skip("SPROUT_MTP_PARITY_MODEL not set")
	}

	const system = `<|turn>system
You are Sprout, a software engineering agent working in a repository.

## Workflow

- Read files before editing them.
- Run tests after changes.

## Tools

- shell_command: run a shell command.
- read_file: read a file.

## Code Style

` + "```" + `go
func Example() {
	fmt.Println("hello")
}
` + "```" + `

Use markdown headers (##) and code fences (` + "```" + `) in answers.
`

	model, err := llm.NewModel(dir)
	if err != nil {
		t.Fatalf("NewModel(%q): %v", dir, err)
	}
	defer model.Close()

	sysMsgs := []llm.ChatMessage{{Role: "system", Content: system}}
	warmPrefix := model.FormatChatPrefix(sysMsgs)

	fullPrompt := warmPrefix + "<|turn>user\ntell me about this codebase\n<turn|>\n<|turn>model\n"

	gen := func(warm bool, k int) string {
		if warm {
			if err := model.WarmSystemPrefix(warmPrefix); err != nil {
				t.Fatalf("WarmSystemPrefix: %v", err)
			}
		}
		cfg := llm.DefaultGenerateConfig()
		cfg.MaxTokens = 64
		cfg.Temperature = 0
		cfg.RepetitionPenalty = 0
		cfg.PromptLookupMaxDrafts = k
		out, err := model.GenerateText(context.Background(), fullPrompt, cfg)
		if err != nil {
			t.Fatalf("GenerateText(warm=%v,k=%d): %v", warm, k, err)
		}
		return out
	}

	baseline := gen(false, 0)
	t.Logf("baseline (cold, k=0): %q", baseline)

	for _, tc := range []struct {
		name string
		warm bool
		k    int
	}{
		{"cold, k=6", false, 6},
		{"warm, k=0", true, 0},
		{"warm, k=6", true, 6},
	} {
		out := gen(tc.warm, tc.k)
		if strings.TrimSpace(out) != strings.TrimSpace(baseline) {
			t.Errorf("DIVERGENCE %s:\n  got:      %q\n  baseline: %q", tc.name, out, baseline)
		} else {
			t.Logf("parity ok (%s): %q", tc.name, out)
		}
	}
}
