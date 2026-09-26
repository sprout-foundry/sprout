//go:build darwin && arm64 && cgo

package localmodel

// local_provider_prompt.go — the LocalProvider prompt-building layer: the
// raw-content meta key, buildPrompt + the leading-system / static-prefix /
// warm-system-prefix helpers, the tool-response / assistant-tool-call
// converters, and the Qwen / LFM2 tool-call formatters. Split out of
// local_provider.go.

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/sprout-foundry/sinter/llm"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

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
// specific tool prompt formatting. enableThinking selects the generation
// cue on preserve-thinking template families (Qwen3.8: open <think>\n cue
// vs the closed empty block); other families render identically either way.
func buildPrompt(model *llm.Model, messages []api.Message, tools []api.Tool, enableThinking bool) string {
	cfg := model.Config()
	arch := cfg.Arch
	isGemma := arch == gemmaArch

	// Gemma native format: tool responses are named — resolve each tool
	// message's ToolCallID to the function name from the preceding
	// assistant message's ToolCalls.
	var callNames map[string]string
	if isGemma {
		callNames = map[string]string{}
		for _, m := range messages {
			for _, tc := range m.ToolCalls {
				callNames[tc.ID] = tc.Function.Name
			}
		}
	}

	msgs := make([]llm.ChatMessage, len(messages))
	for i, m := range messages {
		msgs[i] = llm.ChatMessage{Role: m.Role, Content: m.Content, ReasoningContent: m.ReasoningContent}
		if m.Role == "tool" {
			if isGemma {
				msgs[i] = llm.ChatMessage{Role: "tool", Content: gemmaFormatToolResponse(callNames[m.ToolCallID], m.Content)}
				continue
			}
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
	if isGemma {
		// Gemma native: tool declarations live inside the system turn
		// (FormatChat renders it), not in a prepended Qwen block. The
		// canonical template opens a system turn whenever tools exist —
		// synthesize one if the conversation lacks it.
		if len(tools) > 0 {
			hasSystem := false
			for i := range msgs {
				if msgs[i].Role == "system" {
					msgs[i].Content += gemmaToolDeclarations(tools)
					hasSystem = true
					break
				}
			}
			if !hasSystem {
				msgs = append([]llm.ChatMessage{{Role: "system", Content: gemmaToolDeclarations(tools)}}, msgs...)
			}
		}
		return model.FormatChat(msgs)
	}
	prompt := model.FormatChatThinking(msgs, enableThinking)
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
	if arch == gemmaArch {
		// Mirror buildPrompt's gemma branch: declarations append to the
		// system message inside the template (no prepended Qwen block).
		// No system message + tools can't happen here (leadingSystemMessages
		// returns empty and warmSystemPrefix bails), so no synthesis.
		if len(tools) > 0 {
			for i := range msgs {
				if msgs[i].Role == "system" {
					msgs[i].Content += gemmaToolDeclarations(tools)
					break
				}
			}
		}
		return model.FormatChatPrefix(msgs)
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
	case gemmaArch:
		return gemmaFormatAssistantToolCalls(toolCalls)
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
