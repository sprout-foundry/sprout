//go:build darwin && arm64 && cgo

package localmodel

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// TestConcurrentConversations_NoCacheCorruption exercises the real,
// production concurrency path: sprout runs subagents in parallel goroutines
// (pkg/agent/subagent_runners.go RunParallel), and every one of them shares
// the single process-wide LocalProvider/*llm.Model. Model.Generate holds a
// mutex for its entire prefill+decode duration, so calls fully serialize —
// but the prefix-cache snapshot (m.prefixCache/m.prefixTokens) is mutated
// on every call, and interleaved DIFFERENT conversations must never let one
// conversation's turn N+1 mistakenly restore from a snapshot that belongs
// to a different conversation's history. This test drives several distinct,
// verifiable conversation threads concurrently and checks each one only
// ever sees its own content in its own replies — run with -race to also
// catch any unguarded access to the cache fields.
//
// Requires a real installed local model; set LOCAL_CONCURRENCY_TEST_MODEL
// to a model directory name (relative to DefaultModelsDir) to enable.
func TestConcurrentConversations_NoCacheCorruption(t *testing.T) {
	model := os.Getenv("LOCAL_CONCURRENCY_TEST_MODEL")
	if model == "" {
		t.Skip("LOCAL_CONCURRENCY_TEST_MODEL not set — skipping (requires a real installed local model)")
	}
	t.Setenv("SPROUT_LOCAL_MODEL", model)

	p := GetLocalProvider()
	if err := p.CheckConnection(); err != nil {
		t.Fatalf("model failed to load: %v", err)
	}

	const numConversations = 4
	const turnsPerConversation = 3

	tool := api.Tool{
		Type: "function",
		Function: api.ToolFunction{
			Name:        "noop",
			Description: "does nothing",
		},
	}

	var wg sync.WaitGroup
	errs := make([]error, numConversations)

	for c := range numConversations {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			marker := fmt.Sprintf("SECRET-%d-XYZ", id)
			messages := []api.Message{
				{Role: "system", Content: "You are a terse assistant."},
				{Role: "user", Content: fmt.Sprintf("Remember this exact token: %s. Reply with only that token and nothing else.", marker)},
			}
			for turn := range turnsPerConversation {
				resp, err := p.SendChatRequest(context.Background(), messages, []api.Tool{tool}, "", true)
				if err != nil {
					errs[id] = fmt.Errorf("conversation %d turn %d: %w", id, turn, err)
					return
				}
				assistantMsg := api.Message{
					Role:      "assistant",
					Content:   resp.Choices[0].Message.Content,
					ToolCalls: resp.Choices[0].Message.ToolCalls,
					Meta:      resp.Choices[0].Message.Meta,
				}
				messages = append(messages, assistantMsg)
				// Cross-check: no OTHER conversation's marker should ever
				// leak into this conversation's reply.
				for other := range numConversations {
					if other == id {
						continue
					}
					otherMarker := fmt.Sprintf("SECRET-%d-XYZ", other)
					if strings.Contains(resp.Choices[0].Message.Content, otherMarker) {
						errs[id] = fmt.Errorf("conversation %d turn %d leaked marker from conversation %d: %q",
							id, turn, other, resp.Choices[0].Message.Content)
						return
					}
				}
				messages = append(messages, api.Message{
					Role:    "user",
					Content: fmt.Sprintf("Good. Now say the token again: %s", marker),
				})
			}
		}(c)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("conversation %d failed: %v", i, err)
		}
	}
}
