//go:build darwin && arm64 && cgo

package localmodel

// local_provider_chat.go — the LocalProvider chat engine: the
// SendChatRequest / SendChatRequestStream entry points and the
// localFinishReason mapper. Split out of local_provider.go.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sprout-foundry/sinter/llm"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func (p *LocalProvider) SendChatRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	model, err := p.ensureLoaded()
	if err != nil {
		return nil, fmt.Errorf("local provider: %w", err)
	}
	TouchActivity()
	warmSystemPrefix(model, messages, tools)

	prompt := buildPrompt(model, messages, tools, !disableThinking)
	cfg := llm.DefaultGenerateConfig()
	// k=6 measured the best net on agent-style traffic: +30-50% tok/s on
	// echo-heavy generation (tool output, quoted files) vs k=4, ~5% cost on
	// novel prose from wasted candidate search.
	cfg.PromptLookupMaxDrafts = 6
	// Real output budget against the context window — see
	// localMaxOutputTokens. The 512 default truncated real turns
	// mid-thought while reporting a clean "stop".
	cfg.MaxTokens = localMaxOutputTokens(model, prompt)
	// MaxMTPDrafts is deliberately left disabled (0). It was enabled once
	// tonight after TestMTPParityLiveModel passed cleanly against a correct
	// (non-pipelined) baseline on 4 short synthetic prompts — but a real
	// `sprout commit` run on a real diff produced a commit message with
	// literal chat-template tokens ("assistant", "<|im_start|>user") and
	// duplicated text leaking into it. Root-caused one real bug in the MTP
	// decode loop (a stop token landing mid-batch only broke the inner
	// accumulation loop, not the outer one — fixed, see generateLocked's
	// mtpOuter label) but the corruption persisted after that fix on the
	// same real commit, and confirmed the model's EOSTokenID resolves
	// correctly (248046, matching tokenizer.json's <|im_end|>) so it isn't
	// simple EOS misconfiguration either. Short synthetic prompts (capped at
	// 24-40 tokens) apparently never exercise whatever the remaining failure
	// mode is — real, longer, natural-stopping generations do. Needs a live
	// reproduction with full SPROUT_LOCAL_DEBUG output before re-enabling.
	//
	// Greedy decoding: DefaultGenerateConfig's Temperature=0.6/RepetitionPenalty=1.1
	// disable the on-device GPU argmax path (see Model.generateLocked's
	// useGPUArgmax gate), forcing every decode step to transfer the full
	// vocab logits vector (250K+ floats) to the CPU for sampling — a fixed
	// per-token tax that made local decode 10-30x slower than mlx-lm's
	// greedy-by-default CLI on the same model. It also silently disabled
	// PromptLookupMaxDrafts above, which requires useGPUArgmax. Zeroing both
	// here restores the fast path; deterministic output is also simply
	// correct for tool-calling and commit-message generation.
	cfg.Temperature = 0
	cfg.RepetitionPenalty = 0

	logMLXMemory("chat-start")
	start := time.Now()
	// Capture the model's thinking trace (preserve-thinking families emit
	// one when thinking is enabled; the closed-cue families normally don't).
	// It lands on the response Message as ReasoningContent — the agent loop
	// persists it in history and buildPrompt replays it on later turns.
	var trace strings.Builder
	cfg.ReasoningFn = func(chunk string) { trace.WriteString(chunk) }
	text, err := model.GenerateText(ctx, prompt, cfg)
	if err != nil {
		return nil, fmt.Errorf("generation failed: %w", err)
	}
	elapsed := time.Since(start).Seconds()

	content, toolCalls := parseLocalToolCalls(p.model.Config().Arch, text)
	promptTokens := len(model.TokenizerEncode(prompt))
	logLocalExchange("chat", prompt, text, promptTokens, len(toolCalls))
	completionTokens := len(model.TokenizerEncode(text))
	p.recordTPS(completionTokens, elapsed)
	logLocalTiming("chat", promptTokens, completionTokens, elapsed)

	finishReason := localFinishReason(completionTokens, cfg.MaxTokens, toolCalls)

	resp := &api.ChatResponse{
		ID:     "chatcmpl-local",
		Object: "chat.completion",
		Model:  p.modelID,
	}
	resp.Choices = []api.Choice{{
		Index:        0,
		FinishReason: finishReason,
	}}
	resp.Choices[0].Message.Role = "assistant"
	resp.Choices[0].Message.Content = content
	resp.Choices[0].Message.ReasoningContent = strings.TrimSpace(trace.String())
	resp.Choices[0].Message.ToolCalls = toolCalls
	if len(toolCalls) > 0 {
		resp.Choices[0].Message.Meta = map[string]string{localRawContentMetaKey: text}
	}
	resp.Usage = api.ChatUsage{
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      promptTokens + completionTokens,
	}
	return resp, nil
}

func (p *LocalProvider) SendChatRequestStream(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool, callback api.StreamCallback) (*api.ChatResponse, error) {
	model, err := p.ensureLoaded()
	if err != nil {
		return nil, fmt.Errorf("local provider: %w", err)
	}
	TouchActivity()
	warmSystemPrefix(model, messages, tools)

	prompt := buildPrompt(model, messages, tools, !disableThinking)
	cfg := llm.DefaultGenerateConfig()
	// k=6 measured the best net on agent-style traffic: +30-50% tok/s on
	// echo-heavy generation (tool output, quoted files) vs k=4, ~5% cost on
	// novel prose from wasted candidate search.
	cfg.PromptLookupMaxDrafts = 6
	// Real output budget against the context window — see
	// localMaxOutputTokens. The 512 default truncated real turns
	// mid-thought while reporting a clean "stop".
	cfg.MaxTokens = localMaxOutputTokens(model, prompt)
	// MaxMTPDrafts is deliberately left disabled — see the matching comment
	// in SendChatRequest (real commit-message output corrupted with leaked
	// chat-template tokens even after fixing a real bug in the MTP decode
	// loop's stop handling).
	cfg.Temperature = 0
	cfg.RepetitionPenalty = 0

	// Thinking-trace capture: preserve-thinking families emit a
	// <think>...</think> block before the answer when thinking is enabled.
	// Trace chunks stream to the caller under the "reasoning" content type
	// (the agent loop routes them to the reasoning buffer/UI) and land on
	// the response Message as ReasoningContent for history persistence.
	hasTools := len(tools) > 0
	var trace strings.Builder
	cfg.ReasoningFn = func(chunk string) {
		trace.WriteString(chunk)
		if !hasTools && callback != nil {
			callback(chunk, "reasoning")
		}
	}

	var outputBuf strings.Builder
	// generatedTokens counts every decoded token (including filtered
	// thinking/EOS markers) so cap exhaustion can be distinguished from a
	// natural stop: hitting cfg.MaxTokens without EOS is a truncation and
	// must be reported as finish_reason "length", not "stop".
	generatedTokens := 0
	logMLXMemory("stream-start")
	start := time.Now()

	err = model.Generate(ctx, prompt, cfg, func(tokenID int) {
		generatedTokens++
		tok := model.DecodeToken(tokenID)
		if hasTools {
			outputBuf.WriteString(tok)
			return
		}
		if callback != nil {
			callback(tok, "content")
		}
	})
	if err != nil {
		return nil, fmt.Errorf("generation failed: %w", err)
	}
	elapsed := time.Since(start).Seconds()

	if hasTools {
		content, toolCalls := parseLocalToolCalls(p.model.Config().Arch, outputBuf.String())
		promptTokens := len(model.TokenizerEncode(prompt))
		logLocalExchange("stream", prompt, outputBuf.String(), promptTokens, len(toolCalls))
		if content != "" && callback != nil {
			callback(content, "content")
		}
		completionTokens := len(model.TokenizerEncode(outputBuf.String()))
		p.recordTPS(completionTokens, elapsed)
		logLocalTiming("stream", promptTokens, completionTokens, elapsed)
		finishReason := localFinishReason(generatedTokens, cfg.MaxTokens, toolCalls)
		resp := &api.ChatResponse{
			ID:     "chatcmpl-local",
			Object: "chat.completion",
			Model:  p.modelID,
		}
		resp.Choices = []api.Choice{{Index: 0, FinishReason: finishReason}}
		resp.Choices[0].Message.Role = "assistant"
		resp.Choices[0].Message.Content = content
		resp.Choices[0].Message.ReasoningContent = strings.TrimSpace(trace.String())
		resp.Choices[0].Message.ToolCalls = toolCalls
		if len(toolCalls) > 0 {
			resp.Choices[0].Message.Meta = map[string]string{localRawContentMetaKey: outputBuf.String()}
		}
		return resp, nil
	}

	return &api.ChatResponse{
		ID:     "chatcmpl-local",
		Object: "chat.completion",
		Model:  p.modelID,
		Choices: []api.Choice{{
			Index:        0,
			FinishReason: localFinishReason(generatedTokens, cfg.MaxTokens, nil),
			Message: api.Message{
				Role:             "assistant",
				Content:          outputBuf.String(),
				ReasoningContent: strings.TrimSpace(trace.String()),
			},
		}},
	}, nil
}

// localFinishReason reports why generation ended. "tool_calls" when the
// model emitted tool calls; "length" when the output hit the MaxTokens
// budget without a natural stop (truncation — the agent loop and the user
// deserve to know the model ran out of room, not that it "finished");
// "stop" only for a genuine EOS termination.
func localFinishReason(generated, maxTokens int, toolCalls []api.ToolCall) string {
	if len(toolCalls) > 0 {
		return "tool_calls"
	}
	if maxTokens > 0 && generated >= maxTokens {
		return "length"
	}
	return "stop"
}
