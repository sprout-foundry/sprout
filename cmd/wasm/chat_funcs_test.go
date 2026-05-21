//go:build js && wasm

// Tests for chat_funcs.go's pure-Go helpers. The js.Value-bound entries
// (runChatFunc, the streaming callback bridge to onChunk.Invoke, and
// asPromise's Promise resolution) require a live browser to validate
// meaningfully — `go_js_wasm_exec` under Node lacks the fetch
// ReadableStream behavior that's the whole point of the streaming path.
// Those entries are exercised by the in-browser integration tests once
// the platform proxy endpoint is reachable.
//
// What we can pin here: the wire-format mapping from the Go-side
// ChatResponse (seed/core.ChatResponse) to the JS-side payload that
// SproutWasm.runChat resolves with. This is the stable contract host
// pages bind to, so regressions here would silently break them.

package main

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestChatResponseToJS_EmptyChoices(t *testing.T) {
	resp := &api.ChatResponse{}
	got := chatResponseToJS(resp, "openai", "gpt-5")

	if got["content"] != "" {
		t.Errorf("content with empty Choices should be \"\", got %q", got["content"])
	}
	if got["reasoning_content"] != "" {
		t.Errorf("reasoning_content with empty Choices should be \"\", got %q", got["reasoning_content"])
	}
	if got["finish_reason"] != "" {
		t.Errorf("finish_reason with empty Choices should be \"\", got %q", got["finish_reason"])
	}
	if got["provider"] != "openai" {
		t.Errorf("provider = %v, want \"openai\"", got["provider"])
	}
	if got["model"] != "gpt-5" {
		t.Errorf("model = %v, want \"gpt-5\"", got["model"])
	}

	for _, key := range []string{"prompt_tokens", "completion_tokens", "total_tokens"} {
		if _, present := got[key]; present {
			t.Errorf("%q should be absent when Usage is zero, got %v", key, got[key])
		}
	}
}

func TestChatResponseToJS_FirstChoiceWins(t *testing.T) {
	resp := &api.ChatResponse{
		Choices: []api.ChatChoice{
			{
				Index: 0,
				Message: api.Message{
					Role:             "assistant",
					Content:          "first",
					ReasoningContent: "thinking-first",
				},
				FinishReason: "stop",
			},
			{
				Index: 1,
				Message: api.Message{
					Role:    "assistant",
					Content: "second",
				},
				FinishReason: "length",
			},
		},
	}
	got := chatResponseToJS(resp, "anthropic", "claude-4.7")

	if got["content"] != "first" {
		t.Errorf("content should pick Choices[0], got %q", got["content"])
	}
	if got["reasoning_content"] != "thinking-first" {
		t.Errorf("reasoning_content = %q", got["reasoning_content"])
	}
	if got["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %q", got["finish_reason"])
	}
}

func TestChatResponseToJS_UsageTokensIncludedWhenNonZero(t *testing.T) {
	resp := &api.ChatResponse{
		Choices: []api.ChatChoice{
			{Message: api.Message{Content: "ack"}, FinishReason: "stop"},
		},
		Usage: api.ChatUsage{
			PromptTokens:     42,
			CompletionTokens: 7,
			TotalTokens:      49,
		},
	}
	got := chatResponseToJS(resp, "openai", "gpt-5")

	if got["prompt_tokens"] != 42 {
		t.Errorf("prompt_tokens = %v, want 42", got["prompt_tokens"])
	}
	if got["completion_tokens"] != 7 {
		t.Errorf("completion_tokens = %v, want 7", got["completion_tokens"])
	}
	if got["total_tokens"] != 49 {
		t.Errorf("total_tokens = %v, want 49", got["total_tokens"])
	}
}

func TestChatResponseToJS_PartialUsageOmitsZeroFields(t *testing.T) {
	resp := &api.ChatResponse{
		Choices: []api.ChatChoice{
			{Message: api.Message{Content: "ack"}, FinishReason: "stop"},
		},
		Usage: api.ChatUsage{
			PromptTokens: 42,
			// CompletionTokens and TotalTokens deliberately zero — some
			// providers return only partial usage info on streaming responses.
		},
	}
	got := chatResponseToJS(resp, "openai", "gpt-5")

	if got["prompt_tokens"] != 42 {
		t.Errorf("prompt_tokens = %v, want 42", got["prompt_tokens"])
	}
	if _, present := got["completion_tokens"]; present {
		t.Errorf("completion_tokens should be absent when zero, got %v", got["completion_tokens"])
	}
	if _, present := got["total_tokens"]; present {
		t.Errorf("total_tokens should be absent when zero, got %v", got["total_tokens"])
	}
}

func TestChatJSFuncs_RegistersRunChat(t *testing.T) {
	funcs := chatJSFuncs()
	if _, ok := funcs["runChat"]; !ok {
		t.Error("chatJSFuncs() must register \"runChat\"")
	}
}

func TestAgentJSFuncs_RegistersAgentEntries(t *testing.T) {
	funcs := agentJSFuncs()
	for _, name := range []string{"runAgent", "runPlan"} {
		if _, ok := funcs[name]; !ok {
			t.Errorf("agentJSFuncs() must register %q", name)
		}
	}
}
