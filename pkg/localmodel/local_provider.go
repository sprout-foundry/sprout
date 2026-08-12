//go:build darwin && arm64 && cgo

package localmodel

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
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

		dir, _, err := resolveModelForCurrentMachine()
		if err != nil {
			p.loadErr = err
			return
		}

		if err := llm.ApplyMemoryLimits(); err != nil {
			p.loadErr = fmt.Errorf("apply memory limits: %w", err)
			return
		}

		m, err := llm.NewModel(dir)
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

func (p *LocalProvider) SendChatRequest(ctx context.Context, messages []api.Message, tools []api.Tool, reasoning string, disableThinking bool) (*api.ChatResponse, error) {
	model, err := p.ensureLoaded()
	if err != nil {
		return nil, fmt.Errorf("local provider: %w", err)
	}
	TouchActivity()

	prompt := buildPrompt(model, messages, tools)
	cfg := llm.DefaultGenerateConfig()
	cfg.PromptLookupMaxDrafts = 4

	start := time.Now()
	text, err := model.GenerateText(ctx, prompt, cfg)
	if err != nil {
		return nil, fmt.Errorf("generation failed: %w", err)
	}
	elapsed := time.Since(start).Seconds()

	content, toolCalls := parseLocalToolCalls(p.model.Config().Arch, text)
	promptTokens := len(model.TokenizerEncode(prompt))
	completionTokens := len(model.TokenizerEncode(text))
	p.recordTPS(completionTokens, elapsed)

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

	prompt := buildPrompt(model, messages, tools)
	cfg := llm.DefaultGenerateConfig()
	cfg.PromptLookupMaxDrafts = 4

	hasTools := len(tools) > 0
	var outputBuf strings.Builder
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
		if content != "" && callback != nil {
			callback(content, "content")
		}
		p.recordTPS(len(model.TokenizerEncode(outputBuf.String())), elapsed)
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
	return model.Config().MaxPosition, nil
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
			msgs[i].Content = formatAssistantToolCalls(arch, m.ToolCalls)
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
			for k, v := range args {
				sb.WriteString("<parameter=")
				sb.WriteString(k)
				sb.WriteString(">\n")
				sb.WriteString(fmt.Sprintf("%v", v))
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
			var pairs []string
			for k, v := range args {
				pairs = append(pairs, fmt.Sprintf("%s=%s", k, lfm2FormatValue(v)))
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

func parseQwenToolCalls(text string) (string, []api.ToolCall) {
	if !strings.Contains(text, "<tool_call>") && !strings.Contains(text, "<function=") {
		return text, nil
	}

	lines := strings.Split(text, "\n")
	var contentLines []string
	var calls []api.ToolCall
	var currentCall *api.ToolCall
	var currentArgs map[string]interface{}
	var currentKey string
	inToolCall := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.Contains(trimmed, "<tool_call>"):
			inToolCall = true
			currentCall = &api.ToolCall{Type: "function"}
			currentArgs = make(map[string]interface{})
		case strings.Contains(trimmed, "</tool_call>"):
			if currentCall != nil {
				argsJSON, _ := json.Marshal(currentArgs)
				currentCall.Function.Arguments = string(argsJSON)
				calls = append(calls, *currentCall)
				currentCall = nil
			}
			inToolCall = false
		case strings.Contains(trimmed, "<function="):
			if currentCall != nil {
				start := strings.Index(trimmed, "<function=")
				if start >= 0 {
					rest := trimmed[start+len("<function="):]
					end := strings.Index(rest, ">")
					if end >= 0 {
						currentCall.Function.Name = rest[:end]
						currentCall.ID = fmt.Sprintf("call_%d_%d", time.Now().UnixNano(), len(calls))
					}
				}
			}
		case strings.Contains(trimmed, "<parameter="):
			if currentCall != nil {
				start := strings.Index(trimmed, "<parameter=")
				if start >= 0 {
					rest := trimmed[start+len("<parameter="):]
					end := strings.Index(rest, ">")
					if end >= 0 {
						currentKey = rest[:end]
						val := strings.TrimSpace(rest[end+1:])
						val = strings.TrimSuffix(val, "</parameter>")
						val = strings.TrimSpace(val)
						var parsed interface{}
						if json.Unmarshal([]byte(val), &parsed) == nil {
							currentArgs[currentKey] = parsed
						} else {
							currentArgs[currentKey] = val
						}
					}
				}
			}
		case !inToolCall:
			contentLines = append(contentLines, line)
		}
	}

	content := strings.TrimSpace(strings.Join(contentLines, "\n"))
	return content, calls
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
