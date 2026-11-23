package agent

import (
	"strings"
	"testing"
)

func TestLoadMemoriesForPromptMode(t *testing.T) {
	setupMemoryDirForTest(t)
	if err := SaveMemory("alpha", "# Alpha\n\nPrefer table-driven tests.\nSecond line of the body."); err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}
	if err := SaveMemory("beta", "---\ntitle: beta\n---\n# Beta\nUse merge, never rebase."); err != nil {
		t.Fatalf("SaveMemory: %v", err)
	}

	full := LoadMemoriesForPromptMode("", 0)
	if full != LoadMemoriesForPrompt() {
		t.Fatalf("default mode differs from LoadMemoriesForPrompt")
	}
	if !strings.Contains(full, "### alpha\n") || !strings.Contains(full, "Second line of the body.") {
		t.Fatalf("full mode lacks a body:\n%s", full)
	}

	index := LoadMemoriesForPromptMode("index", 0)
	for _, want := range []string{"- `alpha`: Prefer table-driven tests.", "- `beta`: Use merge, never rebase.", "manage_memory"} {
		if !strings.Contains(index, want) {
			t.Fatalf("index mode lacks %q:\n%s", want, index)
		}
	}
	if strings.Contains(index, "Second line of the body.") {
		t.Fatalf("index mode carries a body:\n%s", index)
	}
	if len(index) >= len(full) {
		t.Fatalf("index (%d bytes) not smaller than full (%d)", len(index), len(full))
	}

	if got := LoadMemoriesForPromptMode("off", 0); got != "" {
		t.Fatalf("off mode = %q", got)
	}

	capped := LoadMemoriesForPromptMode("full", 60)
	if !strings.Contains(capped, "omitted") || strings.Contains(capped, "### beta") {
		t.Fatalf("byte cap not applied:\n%s", capped)
	}
}

func TestMemorySummary(t *testing.T) {
	long := strings.Repeat("x", memoryIndexSummaryChars+10)
	cases := map[string]string{
		"# Title\n\nfirst line\nsecond": "first line",
		"---\na: b\n---\n## H\nbody":    "body",
		"":                              "(no summary)",
		long:                            strings.Repeat("x", memoryIndexSummaryChars) + "…",
	}
	for in, want := range cases {
		if got := memorySummary(in); got != want {
			t.Errorf("memorySummary(%q) = %q, want %q", in, got, want)
		}
	}
}
