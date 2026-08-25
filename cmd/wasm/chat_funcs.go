//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"syscall/js"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/factory"
)

// Tier 2b chat bridge. The companion to setPlatformEndpoint — exposes a
// minimal "make one chat completion call" JS function so host pages can
// validate that the proxy routing path (JS → Go → llmproxy → platform →
// upstream provider → streamed response → JS callback) actually works
// end-to-end before the full agent loop is wired up.
//
// This is intentionally not the full sprout agent. Tool execution, turn
// checkpointing, conversation persistence, MCP, etc. all stay on the
// Go-WASM side until those entry points exist. Once they do, runChat
// becomes a building block they compose with.

func chatJSFuncs() map[string]interface{} {
	return map[string]interface{}{
		"runChat": js.FuncOf(runChatFunc),
	}
}

// runChatFunc invokes a single chat completion. Inputs:
//
//	args[0] (string)   — provider name (e.g. "openai", "anthropic", "openrouter")
//	args[1] (string)   — model id (provider-specific; pass "" for the provider default)
//	args[2] (string)   — JSON-encoded []agent_api.Message
//	args[3] (object?)  — options: {reasoning?: string, disableThinking?: bool, vision?: bool}
//	args[4] (func?)    — onChunk(content, contentType) streaming callback. Omit for non-streaming.
//
// Returns a Promise resolving to:
//
//	{
//	  content:           string,
//	  reasoning_content: string,
//	  prompt_tokens:     number,
//	  completion_tokens: number,
//	  finish_reason:     string,
//	  ...
//	}
//
// Streaming: when args[4] is a function, runChat invokes it as
// onChunk(content, contentType) for each incremental piece, and resolves
// the promise with the FINAL complete response once the stream ends.
// Mirrors the SendChatRequestStream contract in pkg/agent_api so callers
// can swap between streaming and non-streaming without changing how they
// read the final result.
func runChatFunc(_ js.Value, args []js.Value) interface{} {
	provider := argString(args, 0, "")
	model := argString(args, 1, "")
	messagesJSON := argString(args, 2, "")

	var opts struct {
		Reasoning       string `json:"reasoning"`
		DisableThinking bool   `json:"disableThinking"`
		Vision          bool   `json:"vision"`
	}
	if len(args) > 3 && args[3].Type() == js.TypeObject {
		// Re-encode the JS object via JSON.stringify for a clean Go decode.
		// Avoids per-field js.Get plumbing.
		raw := js.Global().Get("JSON").Call("stringify", args[3]).String()
		_ = json.Unmarshal([]byte(raw), &opts)
	}

	var onChunk js.Value
	if len(args) > 4 && args[4].Type() == js.TypeFunction {
		onChunk = args[4]
	}

	return asPromiseWithTimeout(10*time.Minute, func(ctx context.Context) (interface{}, error) {
		if provider == "" {
			return nil, fmt.Errorf("provider is required (first arg)")
		}
		if messagesJSON == "" {
			return nil, fmt.Errorf("messages JSON is required (third arg)")
		}

		var messages []api.Message
		if err := json.Unmarshal([]byte(messagesJSON), &messages); err != nil {
			return nil, fmt.Errorf("parse messages: %w", err)
		}
		if len(messages) == 0 {
			return nil, fmt.Errorf("messages array must be non-empty")
		}

		client, err := factory.CreateProviderClient(api.ClientType(provider), model)
		if err != nil {
			return nil, fmt.Errorf("create client: %w", err)
		}
		injectWasmStreamingClient(client)

		// The factory may have substituted a default model when "" was passed.
		// Report it back so the host page can log/display it accurately.
		usedModel := client.GetModel()

		// Route through the streaming path only when JS provided a callback.
		// SendChatRequestStream calls the callback synchronously from the
		// Go goroutine that owns the stream — invoking a JS function from
		// Go-WASM is itself synchronous, so we don't need extra plumbing.
		var resp *api.ChatResponse
		if !onChunk.IsUndefined() && !onChunk.IsNull() {
			cb := api.StreamCallback(func(content, contentType string) {
				if onChunk.IsUndefined() || onChunk.IsNull() {
					return
				}
				onChunk.Invoke(content, contentType)
			})
			if opts.Vision {
				// Vision streaming isn't part of the ClientInterface today;
				// fall back to non-streaming + final delivery via the chunk
				// callback so callers don't have to special-case vision.
				vr, vErr := client.SendVisionRequest(ctx, messages, nil, opts.Reasoning, opts.DisableThinking)
				if vErr != nil {
					return nil, vErr
				}
				if len(vr.Choices) > 0 {
					cb(vr.Choices[0].Message.Content, "content")
				}
				resp = vr
			} else {
				resp, err = client.SendChatRequestStream(ctx, messages, nil, opts.Reasoning, opts.DisableThinking, cb)
				if err != nil {
					return nil, err
				}
			}
		} else {
			if opts.Vision {
				resp, err = client.SendVisionRequest(ctx, messages, nil, opts.Reasoning, opts.DisableThinking)
			} else {
				resp, err = client.SendChatRequest(ctx, messages, nil, opts.Reasoning, opts.DisableThinking)
			}
			if err != nil {
				return nil, err
			}
		}

		if resp == nil {
			return nil, fmt.Errorf("client returned nil response")
		}
		return chatResponseToJS(resp, provider, usedModel), nil
	})
}

// chatResponseToJS picks the subset of fields the JS caller actually
// needs. Avoids leaking provider-specific internal fields (timings,
// model-specific metadata) that aren't part of the stable contract.
func chatResponseToJS(r *api.ChatResponse, provider, model string) map[string]interface{} {
	var content, reasoningContent, finishReason string
	if len(r.Choices) > 0 {
		c := r.Choices[0]
		content = c.Message.Content
		reasoningContent = c.Message.ReasoningContent
		finishReason = c.FinishReason
	}
	out := map[string]interface{}{
		"content":           content,
		"reasoning_content": reasoningContent,
		"finish_reason":     finishReason,
		"provider":          provider,
		"model":             model,
	}
	if r.Usage.PromptTokens > 0 {
		out["prompt_tokens"] = r.Usage.PromptTokens
	}
	if r.Usage.CompletionTokens > 0 {
		out["completion_tokens"] = r.Usage.CompletionTokens
	}
	if r.Usage.TotalTokens > 0 {
		out["total_tokens"] = r.Usage.TotalTokens
	}
	return out
}
