//go:build darwin && arm64 && cgo

package localmodel

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// TestLiveQwen38ProviderReasoning drives the real LocalProvider against the
// downloaded qwen3.8-27b weights: thinking-on streams "reasoning" chunks and
// the response carries ReasoningContent; a follow-up turn replays the trace
// into the prompt (buildPrompt) and the model continues the conversation.
// Skips unless SPROUT_LIVE_QWEN38=1. Points SPROUT_LLM_MODELS_DIR at the
// legacy ~/dev/llm-models location where the weights live.
func TestLiveQwen38ProviderReasoning(t *testing.T) {
	if os.Getenv("SPROUT_LIVE_QWEN38") != "1" {
		t.Skip("SPROUT_LIVE_QWEN38 not set")
	}
	t.Setenv("SPROUT_LLM_MODELS_DIR", filepath.Join(os.Getenv("HOME"), "dev", "llm-models"))

	status, err := ResolveModelID("qwen3.8-27b")
	if err != nil {
		t.Fatalf("resolve qwen3.8-27b: %v", err)
	}
	if !status.Installed {
		t.Fatalf("qwen3.8-27b not installed at %s", status.Dir)
	}

	p := GetLocalProvider()
	if err := p.SetModel("qwen3.8-27b"); err != nil {
		t.Fatalf("SetModel: %v", err)
	}

	// ── Turn 1: thinking-on stream ─────────────────────────────────────
	var reasoning strings.Builder
	var content strings.Builder
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	resp, err := p.SendChatRequestStream(ctx, []api.Message{
		{Role: "user", Content: "What is 19 * 21? Think it through, then give just the number."},
	}, nil, "", false, func(chunk, ct string) {
		switch ct {
		case "reasoning":
			reasoning.WriteString(chunk)
		default:
			content.WriteString(chunk)
		}
	})
	if err != nil {
		t.Fatalf("turn 1: %v", err)
	}
	t.Logf("turn1: %d reasoning bytes, content=%q, resp.ReasoningContent=%d bytes",
		reasoning.Len(), strings.TrimSpace(content.String()), len(resp.Choices[0].Message.ReasoningContent))

	if reasoning.Len() == 0 {
		t.Error("turn 1 streamed no reasoning chunks (thinking did not engage)")
	}
	if resp.Choices[0].Message.ReasoningContent == "" {
		t.Error("turn 1 response missing ReasoningContent (history persistence will lose the trace)")
	}
	if !strings.Contains(content.String(), "399") {
		t.Errorf("turn 1 answer should contain 399, got %q", content.String())
	}

	// ── Turn 2: replay trace via ReasoningContent ──────────────────────
	history := []api.Message{
		{Role: "user", Content: "What is 19 * 21? Think it through, then give just the number."},
		{Role: "assistant", Content: strings.TrimSpace(content.String()), ReasoningContent: resp.Choices[0].Message.ReasoningContent},
		{Role: "user", Content: "Now subtract 100 and show just the result."},
	}
	var content2 strings.Builder
	resp2, err := p.SendChatRequestStream(ctx, history, nil, "", false, func(chunk, ct string) {
		if ct != "reasoning" {
			content2.WriteString(chunk)
		}
	})
	if err != nil {
		t.Fatalf("turn 2: %v", err)
	}
	t.Logf("turn2: content=%q, reasoning=%d bytes", strings.TrimSpace(content2.String()), len(resp2.Choices[0].Message.ReasoningContent))
	if !strings.Contains(content2.String(), "299") {
		t.Errorf("turn 2 answer should contain 299, got %q", content2.String())
	}
}
