//go:build darwin && arm64 && cgo

package localmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/gomlx/llm"
	_ "github.com/sprout-foundry/sprout/pkg/gomlx/llm/gemma4"
	_ "github.com/sprout-foundry/sprout/pkg/gomlx/llm/lfm2"
	_ "github.com/sprout-foundry/sprout/pkg/gomlx/llm/qwen3"
	_ "github.com/sprout-foundry/sprout/pkg/gomlx/llm/qwen35"
	"github.com/sprout-foundry/sprout/pkg/gomlx/mlx"
)

const localBackendMLX = "mlx"

// LocalProvider implements api.ClientInterface by calling the MLX model
// engine directly — no HTTP, no separate process, no serialization.
// The model is loaded once (lazy, on first request) and kept resident
// for subsequent requests. The idle reaper (in lifecycle.go) unloads
// weights to reclaim GPU memory after inactivity.
type LocalProvider struct {
	mu sync.Mutex

	model    *llm.Model
	modelDir string
	modelID  string
	backend  string
	debug    bool

	loadOnce sync.Once
	loadErr  error

	// TPS tracking (fixed-point: value * 1000, stored as uint64)
	lastTPS     atomic.Uint64
	avgTPS      atomic.Uint64
	totalTokens atomic.Uint64
	genCount    atomic.Uint64
}

var (
	globalProvider     *LocalProvider
	globalProviderOnce sync.Once
)

// GetLocalProvider returns the singleton in-process local provider.
// The model is loaded lazily on first use.
func GetLocalProvider() *LocalProvider {
	globalProviderOnce.Do(func() {
		globalProvider = &LocalProvider{backend: detectBackend()}
	})
	return globalProvider
}

func detectBackend() string {
	if runtime.GOOS == "darwin" && runtime.GOARCH == "arm64" && mlx.Available() {
		return localBackendMLX
	}
	return "none"
}

func (p *LocalProvider) loadModel() {
	p.loadOnce.Do(func() {
		if p.backend == "none" {
			p.loadErr = fmt.Errorf("no local LLM backend available (requires Apple Silicon with MLX)")
			return
		}

		dir, resolvedBackend, err := resolveModelForCurrentMachine()
		if err != nil {
			p.loadErr = err
			return
		}

		if err := llm.ApplyMemoryLimits(); err != nil {
			p.loadErr = fmt.Errorf("apply memory limits: %w", err)
			return
		}

		if localDebug() {
			log.Printf("local: resolved model dir=%s backend=%s", dir, resolvedBackend)
		}
		logMLXMemory("model-load-start")
		m, err := llm.NewModel(dir)
		logMLXMemory("model-load-end")
		if err != nil {
			p.loadErr = fmt.Errorf("load model from %s: %w", dir, err)
			return
		}

		p.model = m
		p.modelDir = dir
		cfg := m.Config()
		p.modelID = fmt.Sprintf("%s-local-%d-%d", cfg.Arch, cfg.HiddenSize, cfg.NumLayers)
	})
}

func (p *LocalProvider) ensureLoaded() (*llm.Model, error) {
	if p.model != nil {
		return p.model, nil
	}
	p.loadModel()
	if p.loadErr != nil {
		return nil, p.loadErr
	}
	if p.model == nil {
		return nil, fmt.Errorf("model failed to load")
	}
	return p.model, nil
}

// --- api.ClientInterface ---

// localDebug reports whether SPROUT_LOCAL_DEBUG=1 is set. When on, the
// provider logs prompt size and raw model output, which is the only way to
// see why a local model produced an unusable response inside the agent loop.
func localDebug() bool { return os.Getenv("SPROUT_LOCAL_DEBUG") == "1" }

func logLocalExchange(tag, prompt, raw string, promptTokens int, toolCalls int) {
	if !localDebug() {
		return
	}
	trunc := func(s string, n int) string {
		if len(s) <= n {
			return s
		}
		return s[:n] + fmt.Sprintf("...[+%d bytes]", len(s)-n)
	}
	log.Printf("local[%s]: prompt_tokens=%d prompt_bytes=%d raw_bytes=%d tool_calls=%d",
		tag, promptTokens, len(prompt), len(raw), toolCalls)
	log.Printf("local[%s]: PROMPT_TAIL=%q", tag, trunc(prompt[max(0, len(prompt)-1200):], 1200))
	log.Printf("local[%s]: RAW_OUTPUT=%q", tag, trunc(raw, 1200))
}

// logLocalTiming reports per-turn generation timing so the split between
// prefill (scales with prompt_tokens) and decode (scales with
// completion_tokens) is visible from a real agent session, not just an
// isolated benchmark harness.
func logLocalTiming(tag string, promptTokens, completionTokens int, elapsed float64) {
	if !localDebug() {
		return
	}
	tps := 0.0
	if elapsed > 0 {
		tps = float64(completionTokens) / elapsed
	}
	log.Printf("local[%s]: TIMING prompt_tokens=%d completion_tokens=%d elapsed=%.2fs tps=%.1f",
		tag, promptTokens, completionTokens, elapsed, tps)
	logMLXMemory(tag)
}

// logMLXMemory reports MLX's own allocator accounting (bytes held by live
// arrays, pooled/cached bytes, and the peak watermark since load) after
// every turn. Isolates whether growth is coming from MLX's own live-array
// footprint — a real reference leak in the Go/cgo layer — versus system-wide
// pressure (other processes, macOS accounting) that shows up in Activity
// Monitor but isn't actually MLX holding more memory.
func logMLXMemory(tag string) {
	stats, err := mlx.Snapshot()
	if err != nil {
		log.Printf("local[%s]: MLX_MEM snapshot failed: %v", tag, err)
		return
	}
	log.Printf("local[%s]: MLX_MEM active=%.1fMB cache=%.1fMB peak=%.1fMB limit=%.1fMB cacheLimit=%.1fMB",
		tag,
		float64(stats.Active)/1048576, float64(stats.Cache)/1048576, float64(stats.Peak)/1048576,
		float64(stats.Limit)/1048576, float64(stats.CacheLimit)/1048576)
}

func (p *LocalProvider) SendChatRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	model, err := p.ensureLoaded()
	if err != nil {
		return nil, fmt.Errorf("local provider: %w", err)
	}
	TouchActivity()
	warmSystemPrefix(model, messages, tools)

	prompt := buildPrompt(model, messages, tools)
	cfg := llm.DefaultGenerateConfig()
	cfg.PromptLookupMaxDrafts = 4
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

	finishReason := "stop"
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	}

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

	prompt := buildPrompt(model, messages, tools)
	cfg := llm.DefaultGenerateConfig()
	cfg.PromptLookupMaxDrafts = 4
	// MaxMTPDrafts is deliberately left disabled — see the matching comment
	// in SendChatRequest (real commit-message output corrupted with leaked
	// chat-template tokens even after fixing a real bug in the MTP decode
	// loop's stop handling).
	cfg.Temperature = 0
	cfg.RepetitionPenalty = 0

	hasTools := len(tools) > 0
	var outputBuf strings.Builder
	logMLXMemory("stream-start")
	start := time.Now()

	err = model.Generate(ctx, prompt, cfg, func(tokenID int) {
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
		finishReason := "stop"
		if len(toolCalls) > 0 {
			finishReason = "tool_calls"
		}
		resp := &api.ChatResponse{
			ID:     "chatcmpl-local",
			Object: "chat.completion",
			Model:  p.modelID,
		}
		resp.Choices = []api.Choice{{Index: 0, FinishReason: finishReason}}
		resp.Choices[0].Message.Role = "assistant"
		resp.Choices[0].Message.Content = content
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
			FinishReason: "stop",
		}},
	}, nil
}

func (p *LocalProvider) CheckConnection() error {
	_, err := p.ensureLoaded()
	return err
}

func (p *LocalProvider) SetDebug(debug bool) { p.debug = debug }

func (p *LocalProvider) SetModel(model string) error { return nil }

func (p *LocalProvider) GetModel() string {
	p.loadModel()
	if p.modelID != "" {
		return p.modelID
	}
	return "local"
}

func (p *LocalProvider) GetProvider() string { return "sprout-local" }

// isModelLoaded reports whether the model is currently in GPU memory.
func (p *LocalProvider) isModelLoaded() bool    { return p.model != nil }
func (p *LocalProvider) loadedModelDir() string { return p.modelDir }
func (p *LocalProvider) loadedModelID() string  { return p.modelID }

func (p *LocalProvider) GetModelContextLimit() (int, error) {
	model, err := p.ensureLoaded()
	if err != nil {
		return 0, err
	}
	// ContextLength(), not Config().MaxPosition directly: MaxPosition is the
	// model's raw native window (e.g. 262K for Qwen3.5), which a small
	// quantized local model goes stale/cogency-degrades long before
	// reaching. ContextLength applies the cap sprout's context-profile
	// auto-detection relies on to drop into Low-Context Mode at a sane
	// point.
	return model.ContextLength(), nil
}

func (p *LocalProvider) ListModels(ctx context.Context) ([]api.ModelInfo, error) {
	return []api.ModelInfo{{ID: p.GetModel()}}, nil
}

func (p *LocalProvider) SupportsVision() bool               { return false }
func (p *LocalProvider) SupportsConversationalVision() bool { return false }
func (p *LocalProvider) VisionCapabilities() api.VisionCapabilities {
	return api.VisionCapabilities{}
}
func (p *LocalProvider) GetVisionModel() string { return "" }
func (p *LocalProvider) SendVisionRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	return nil, fmt.Errorf("local provider does not support vision")
}

func (p *LocalProvider) GetLastTPS() float64    { return float64(p.lastTPS.Load()) / 1000.0 }
func (p *LocalProvider) GetAverageTPS() float64 { return float64(p.avgTPS.Load()) / 1000.0 }
func (p *LocalProvider) GetTPSStats() map[string]float64 {
	return map[string]float64{
		"last":         p.GetLastTPS(),
		"average":      p.GetAverageTPS(),
		"total_tokens": float64(p.totalTokens.Load()),
	}
}
func (p *LocalProvider) ResetTPSStats() {
	p.lastTPS.Store(0)
	p.avgTPS.Store(0)
	p.totalTokens.Store(0)
	p.genCount.Store(0)
}

func (p *LocalProvider) recordTPS(tokens int, elapsed float64) {
	if elapsed <= 0 || tokens <= 0 {
		return
	}
	tps := float64(tokens) / elapsed
	p.lastTPS.Store(uint64(tps * 1000))
	p.totalTokens.Add(uint64(tokens))
	count := p.genCount.Add(1)
	oldAvg := float64(p.avgTPS.Load()) / 1000.0
	newAvg := (oldAvg*float64(count-1) + tps) / float64(count)
	p.avgTPS.Store(uint64(newAvg * 1000))
}

// Close unloads the model and releases GPU memory.
func (p *LocalProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.model != nil {
		_ = p.model.Close()
		p.model = nil
	}
	return nil
}

// --- prompt building and tool-call parsing ---

// localRawContentMetaKey stashes the model's verbatim generated text on the
// response Message (via the provider-private Meta field). buildPrompt
// replays it byte-for-byte on the next turn instead of re-synthesizing the
// tool-call text from parsed ToolCalls — the reconstructed form doesn't
// match the model's own (variable) formatting, which broke the KV prefix
// cache on every multi-turn exchange. Falls back to reconstruction when
// Meta wasn't carried through (e.g. a session resumed from persisted disk
// state, where Meta — tagged json:"-" — doesn't survive).
const localRawContentMetaKey = "sprout_local_raw"

// buildPrompt constructs the full prompt from messages and tools, using
// the model's native chat template (via FormatChat) and architecture-
// specific tool prompt formatting.
func buildPrompt(model *llm.Model, messages []api.Message, tools []api.Tool) string {
	arch := model.Config().Arch
	msgs := make([]llm.ChatMessage, len(messages))
	for i, m := range messages {
		msgs[i] = llm.ChatMessage{Role: m.Role, Content: m.Content}
		if m.Role == "tool" {
			msgs[i] = convertToolResponse(arch, m.Content)
		}
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			if raw, ok := m.Meta[localRawContentMetaKey]; ok {
				msgs[i].Content = raw
			} else {
				msgs[i].Content = formatAssistantToolCalls(arch, m.ToolCalls)
			}
		}
	}
	prompt := model.FormatChat(msgs)
	if len(tools) > 0 {
		// Some architectures (e.g. LFM2) embed tools into the system
		// prompt via the chat template; for those, FormatChat already
		// handles it if we pass tools in the message content. Qwen-based
		// models need explicit tool-prompt injection before the conversation.
		if toolPrompt := formatToolsPrompt(arch, tools); toolPrompt != "" {
			prompt = toolPrompt + prompt
		}
	}
	return prompt
}

// leadingSystemMessages returns the leading run of system-role messages —
// the part of a conversation that's identical across otherwise-unrelated
// conversations sharing the same system prompt (main agent + subagents; see
// warmSystemPrefix).
func leadingSystemMessages(messages []api.Message) []api.Message {
	i := 0
	for i < len(messages) && messages[i].Role == "system" {
		i++
	}
	return messages[:i]
}

// staticPromptPrefix mirrors buildPrompt's construction for a leading run
// of messages, producing a string guaranteed to be an exact prefix of
// buildPrompt's output for any messages/tools sharing this same leading
// sequence and tool list (system messages never go through buildPrompt's
// tool/assistant content rewrites, so no divergence there).
func staticPromptPrefix(model *llm.Model, sysMsgs []api.Message, tools []api.Tool) string {
	arch := model.Config().Arch
	msgs := make([]llm.ChatMessage, len(sysMsgs))
	for i, m := range sysMsgs {
		msgs[i] = llm.ChatMessage{Role: m.Role, Content: m.Content}
	}
	prefix := model.FormatChatPrefix(msgs)
	if len(tools) > 0 {
		if toolPrompt := formatToolsPrompt(arch, tools); toolPrompt != "" {
			prefix = toolPrompt + prefix
		}
	}
	return prefix
}

// warmSystemPrefix pre-caches the system prompt + tool definitions shared
// by many otherwise-unrelated conversations (main agent + subagents), so
// each one's first turn can delta-prefill instead of paying a full prefill
// for identical boilerplate. Cheap no-op once warmed — safe to call on
// every request. Errors are logged, not propagated: this is an optimization,
// not required for correctness (Generate's own per-conversation caching
// works fine without it).
func warmSystemPrefix(model *llm.Model, messages []api.Message, tools []api.Tool) {
	sysMsgs := leadingSystemMessages(messages)
	if len(sysMsgs) == 0 {
		return
	}
	if err := model.WarmSystemPrefix(staticPromptPrefix(model, sysMsgs, tools)); err != nil && localDebug() {
		log.Printf("local: warm system prefix: %v", err)
	}
}

// convertToolResponse formats a tool result message for the given architecture.
func convertToolResponse(arch, content string) llm.ChatMessage {
	switch arch {
	case "lfm2":
		return llm.ChatMessage{Role: "user", Content: content}
	default:
		return llm.ChatMessage{Role: "user", Content: "<tool_response>\n" + content + "\n</tool_response>"}
	}
}

// formatAssistantToolCalls converts prior assistant tool_calls into the
// model's native text format for conversation history.
func formatAssistantToolCalls(arch string, toolCalls []api.ToolCall) string {
	switch arch {
	case "lfm2":
		return formatLFM2AssistantToolCalls(toolCalls)
	default:
		return formatQwenAssistantToolCalls(toolCalls)
	}
}

func formatQwenAssistantToolCalls(toolCalls []api.ToolCall) string {
	var sb strings.Builder
	for _, tc := range toolCalls {
		sb.WriteString("<tool_call>\n<function=")
		sb.WriteString(tc.Function.Name)
		sb.WriteString(">\n")
		var args map[string]interface{}
		if json.Unmarshal([]byte(tc.Function.Arguments), &args) == nil {
			keys := make([]string, 0, len(args))
			for k := range args {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				sb.WriteString("<parameter=")
				sb.WriteString(k)
				sb.WriteString(">\n")
				sb.WriteString(fmt.Sprintf("%v", args[k]))
				sb.WriteString("\n</parameter>\n")
			}
		}
		sb.WriteString("</function>\n</tool_call>\n")
	}
	return sb.String()
}

func formatLFM2AssistantToolCalls(toolCalls []api.ToolCall) string {
	var calls []string
	for _, tc := range toolCalls {
		var args map[string]interface{}
		if json.Unmarshal([]byte(tc.Function.Arguments), &args) == nil {
			keys := make([]string, 0, len(args))
			for k := range args {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			var pairs []string
			for _, k := range keys {
				pairs = append(pairs, fmt.Sprintf("%s=%s", k, lfm2FormatValue(args[k])))
			}
			calls = append(calls, fmt.Sprintf("%s(%s)", tc.Function.Name, strings.Join(pairs, ", ")))
		} else {
			calls = append(calls, tc.Function.Name+"()")
		}
	}
	return "<|tool_call_start|>[" + strings.Join(calls, ", ") + "]<|tool_call_end|>"
}

func lfm2FormatValue(v interface{}) string {
	switch val := v.(type) {
	case string:
		return "'" + strings.ReplaceAll(val, "'", "\\'") + "'"
	default:
		return fmt.Sprintf("%v", val)
	}
}

// parseLocalToolCalls parses tool calls from model output using the
// architecture-appropriate parser. Returns content with tool calls
// stripped, and the parsed tool calls in OpenAI format.
func parseLocalToolCalls(arch, text string) (string, []api.ToolCall) {
	switch arch {
	case "lfm2":
		calls, remaining, ok := api.RecoverLFM2ToolCalls(text)
		if !ok {
			return text, nil
		}
		return remaining, calls
	default:
		return parseQwenToolCalls(text)
	}
}

// parseQwenToolCalls extracts Qwen-style tool calls from model output.
//
// The format is XML-ish and models are inconsistent about whitespace: a call
// may be spread over several lines or emitted entirely on one line, and the
// closing </parameter>/</function>/</tool_call> tags may share a line with a
// parameter value. So this scans the raw text rather than going line by line —
// a line-oriented parser drops parameters whenever a value shares a line with
// a closing tag, and misses one-line calls entirely.
func parseQwenToolCalls(text string) (string, []api.ToolCall) {
	if !strings.Contains(text, "<tool_call>") && !strings.Contains(text, "<function=") {
		return text, nil
	}

	var calls []api.ToolCall
	var content strings.Builder
	rest := text

	for {
		start := strings.Index(rest, "<tool_call>")
		if start < 0 {
			break
		}
		content.WriteString(rest[:start])
		rest = rest[start+len("<tool_call>"):]

		body := rest
		if end := strings.Index(rest, "</tool_call>"); end >= 0 {
			body = rest[:end]
			rest = rest[end+len("</tool_call>"):]
		} else {
			rest = "" // unterminated: treat the remainder as the call body
		}
		if call, ok := parseToolCallBody(body, len(calls)); ok {
			calls = append(calls, call)
		}
	}

	// Some models emit <function=...> without the surrounding <tool_call>.
	if len(calls) == 0 && strings.Contains(rest, "<function=") {
		if call, ok := parseToolCallBody(rest, 0); ok {
			calls = append(calls, call)
			rest = ""
		}
	}
	content.WriteString(rest)

	return strings.TrimSpace(content.String()), calls
}

// parseToolCallBody parses a single "<function=name>...<parameter=k>v..." body.
func parseToolCallBody(body string, idx int) (api.ToolCall, bool) {
	fnStart := strings.Index(body, "<function=")
	if fnStart < 0 {
		return api.ToolCall{}, false
	}
	r := body[fnStart+len("<function="):]
	gt := strings.Index(r, ">")
	if gt < 0 {
		return api.ToolCall{}, false
	}
	name := strings.TrimSpace(r[:gt])
	if name == "" {
		return api.ToolCall{}, false
	}

	args := make(map[string]interface{})
	p := r[gt+1:]
	for {
		ps := strings.Index(p, "<parameter=")
		if ps < 0 {
			break
		}
		p = p[ps+len("<parameter="):]
		pe := strings.Index(p, ">")
		if pe < 0 {
			break
		}
		key := strings.TrimSpace(p[:pe])
		p = p[pe+1:]

		// The value runs to </parameter>, or to the next <parameter= when the
		// model forgets to close, or to </function>, or to the end.
		valEnd := len(p)
		for _, marker := range []string{"</parameter>", "<parameter=", "</function>"} {
			if i := strings.Index(p, marker); i >= 0 && i < valEnd {
				valEnd = i
			}
		}
		val := strings.TrimSpace(p[:valEnd])
		if key != "" {
			var parsed interface{}
			if json.Unmarshal([]byte(val), &parsed) == nil {
				args[key] = parsed
			} else {
				args[key] = val
			}
		}
		p = p[valEnd:]
	}

	// Gemma-family models frequently emit parameters as <key=value> instead of
	// the Qwen <parameter=key>value</parameter> form. Fall back to that shape
	// only when no standard parameters were found, so this can't misparse a
	// well-formed call.
	if len(args) == 0 {
		parseInlineParams(body[fnStart:], args)
	}

	argsJSON, _ := json.Marshal(args)
	return api.ToolCall{
		ID:   fmt.Sprintf("call_%d_%d", time.Now().UnixNano(), idx),
		Type: "function",
		Function: api.ToolCallFunction{
			Name:      name,
			Arguments: string(argsJSON),
		},
	}, true
}

// parseInlineParams extracts <key=value> parameters, where the value ends at
// </key> when present and at the closing > otherwise. The leading
// <function=...> tag is skipped by the caller's slicing.
func parseInlineParams(body string, args map[string]interface{}) {
	p := body
	if i := strings.Index(p, ">"); i >= 0 {
		p = p[i+1:] // skip past <function=name>
	}
	for {
		lt := strings.Index(p, "<")
		if lt < 0 {
			return
		}
		p = p[lt+1:]
		eq := strings.Index(p, "=")
		gt := strings.Index(p, ">")
		if eq < 0 || (gt >= 0 && gt < eq) {
			continue // not a key=value tag
		}
		key := strings.TrimSpace(p[:eq])
		if key == "" || !isSimpleIdent(key) {
			continue
		}
		rest := p[eq+1:]

		end := len(rest)
		if close := strings.Index(rest, "</"+key+">"); close >= 0 {
			end = close
		} else if g := strings.Index(rest, ">"); g >= 0 {
			end = g
		}
		val := strings.TrimSpace(rest[:end])
		if _, seen := args[key]; !seen && val != "" {
			args[key] = val
		}
		p = rest[end:]
	}
}

// isSimpleIdent reports whether s looks like a parameter name rather than
// arbitrary markup, so stray angle brackets in prose aren't treated as params.
func isSimpleIdent(s string) bool {
	for _, r := range s {
		if !(r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return len(s) > 0
}

// formatToolsPrompt builds the tool-calling system prompt for the given
// architecture. Returns empty string for architectures that handle tools
// via the chat template itself (e.g. LFM2 embeds tools in the system prompt).
func formatToolsPrompt(arch string, tools []api.Tool) string {
	switch arch {
	case "lfm2":
		// LFM2 tools are injected into the system prompt as JSON.
		// The chat template's {% if tools %} block handles formatting.
		// We prepend the tool list as part of the system message.
		var toolJSONs []string
		for _, tool := range tools {
			j, _ := json.Marshal(tool)
			toolJSONs = append(toolJSONs, string(j))
		}
		return "<|im_start|>system\nList of tools: [" + strings.Join(toolJSONs, ", ") + "]<|im_end|>\n"
	default:
		return formatQwenToolsPrompt(tools)
	}
}

func formatQwenToolsPrompt(tools []api.Tool) string {
	var sb strings.Builder
	sb.WriteString("<|im_start|>system\n# Tools\n\nYou have access to the following functions:\n\n<tools>")
	for _, tool := range tools {
		j, _ := json.Marshal(tool)
		sb.WriteString("\n")
		sb.Write(j)
	}
	sb.WriteString("\n</tools>")
	sb.WriteString("\n\nIf you choose to call a function ONLY reply in the following format with NO suffix:\n\n")
	sb.WriteString("<tool_call>\n<function=example_function_name>\n<parameter=example_parameter_1>\nvalue_1\n</parameter>\n<parameter=example_parameter_2>\nThis is the value for the second parameter\nthat can span\nmultiple lines\n</parameter>\n</function>\n</tool_call>\n\n")
	sb.WriteString("<IMPORTANT>\nReminder:\n- Function calls MUST follow the specified format: an inner <function=...></function> block must be nested within <tool_call></tool_call> XML tags\n- Required parameters MUST be specified\n- You may provide optional reasoning for your function call in natural language BEFORE the function call, but NOT after\n- If there is no function call available, answer the question like normal with your current knowledge and do not tell the user about function calls\n</IMPORTANT>")
	sb.WriteString("<|im_end|>\n")
	return sb.String()
}
