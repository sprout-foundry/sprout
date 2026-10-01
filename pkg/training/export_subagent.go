// Package training provides utilities for exporting session data into
// training-ready formats (ShareGPT, OpenAI fine-tuning JSONL, Alpaca).
package training

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// ---------------------------------------------------------------------------
// Export options / result
// ---------------------------------------------------------------------------

// export_subagent.go — subagent example extraction (single + parallel task
// specs, persona system prompts, tool-arg parsing), split out of export.go.

// minSubagentOutputLen is the minimum character length for a subagent's
// output to be considered a useful training example. Shorter outputs are
// usually error messages or "insufficient output" placeholders.
const minSubagentOutputLen = 50

// subagentToolNames are the tool-call function names that trigger subagent
// extraction.
var subagentToolNames = map[string]bool{
	"run_subagent":           true,
	"run_parallel_subagents": true,
}

// extractSubagentExamples walks a conversation's messages looking for
// assistant tool calls to run_subagent or run_parallel_subagents. For each
// match, it builds an OpenAI fine-tuning example from the subagent's
// task prompt (user) and output (assistant).
//
// For run_subagent, exactly one example is produced per call.
//
// For run_parallel_subagents, the tool result is a JSON object keyed by
// task ID (task-1, task-2, …); one example is produced per task whose
// output passes the minimum-length filter.
//
// Examples are filtered out when the output is empty or shorter than
// minSubagentOutputLen characters.
func extractSubagentExamples(state agent.ConversationState) []OpenAITrainingExample {
	var examples []OpenAITrainingExample

	// Build a lookup from tool-call ID → tool-result content for quick
	// resolution.
	resultByID := make(map[string]string)
	for _, m := range state.Messages {
		if m.Role == "tool" && m.ToolCallID != "" {
			resultByID[m.ToolCallID] = m.Content
		}
	}

	for _, m := range state.Messages {
		if m.Role != "assistant" {
			continue
		}
		for _, tc := range m.ToolCalls {
			name := tc.Function.Name
			if !subagentToolNames[name] {
				continue
			}
			result, ok := resultByID[tc.ID]
			if !ok {
				continue
			}
			args := tc.Function.Arguments

			switch name {
			case "run_subagent":
				if ex := subagentExampleFromSingle(args, result); ex != nil {
					examples = append(examples, *ex)
				}
			case "run_parallel_subagents":
				exs := subagentExamplesFromParallel(args, result)
				examples = append(examples, exs...)
			}
		}
	}

	return examples
}

// subagentExampleFromSingle builds one training example from a
// run_subagent tool call + result.
func subagentExampleFromSingle(argsJSON, result string) *OpenAITrainingExample {
	args := parseToolCallArgs(argsJSON)
	prompt := extractStringArg(args, "prompt")
	persona := extractStringArg(args, "persona")

	output := extractSubagentOutput(result)
	if len(strings.TrimSpace(output)) < minSubagentOutputLen {
		return nil
	}

	system := personaToSystemPrompt(persona)

	prompt = RedactContent(prompt)
	output = RedactContent(output)
	system = RedactContent(system)

	return &OpenAITrainingExample{
		Messages: []OpenAIMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: prompt},
			{Role: "assistant", Content: output},
		},
	}
}

// subagentExamplesFromParallel builds one or more training examples from
// a run_parallel_subagents tool call + result. The arguments may contain
// a "subagents", "tasks", or "prompts" array of strings or objects; the
// result is a JSON object keyed by task ID.
func subagentExamplesFromParallel(argsJSON, result string) []OpenAITrainingExample {
	args := parseToolCallArgs(argsJSON)

	// Collect task prompts and IDs in order. The args may use "subagents",
	// "tasks", or "prompts". Each element may be a string (auto-ID) or
	// an object with "id" and "prompt" fields.
	var specs []parallelTaskSpec
	for _, key := range []string{"subagents", "tasks", "prompts"} {
		if raw, ok := args[key]; ok {
			specs = extractTaskSpecs(raw)
			if len(specs) > 0 {
				break
			}
		}
	}

	// Parse the result into a task-ID → output map.
	taskOutputs := parseParallelSubagentResult(result)

	var examples []OpenAITrainingExample
	for _, spec := range specs {
		// Try the task's own ID, then fall back to "task-N", then
		// 0-based index.
		output, ok := taskOutputs[spec.id]
		if !ok {
			output, ok = taskOutputs[fmt.Sprintf("task-%d", len(examples)+1)]
		}
		if !ok {
			continue
		}

		if len(strings.TrimSpace(output)) < minSubagentOutputLen {
			continue
		}

		prompt := RedactContent(spec.prompt)
		output = RedactContent(output)

		examples = append(examples, OpenAITrainingExample{
			Messages: []OpenAIMessage{
				{Role: "system", Content: subagentDefaultSystem},
				{Role: "user", Content: prompt},
				{Role: "assistant", Content: output},
			},
		})
	}

	return examples
}

// ---------------------------------------------------------------------------
// Subagent helper functions
// ---------------------------------------------------------------------------

// subagentDefaultSystem is the fallback system prompt when no persona is
// provided in the tool call arguments.
const subagentDefaultSystem = "You are a helpful coding assistant."

// personaToSystemPrompt converts a persona name into a system prompt. When
// the persona is empty or unknown, a generic default is returned.
func personaToSystemPrompt(persona string) string {
	persona = strings.TrimSpace(strings.ToLower(persona))
	switch persona {
	case "coder", "":
		return subagentDefaultSystem
	default:
		return fmt.Sprintf("You are a %s assistant. Complete the task thoroughly and report your results.", persona)
	}
}

// extractSubagentOutput parses a run_subagent result and extracts the
// subagent's stdout output. The result is a JSON object with a "stdout"
// field. If parsing fails, the raw content is returned as-is.
func extractSubagentOutput(result string) string {
	result = strings.TrimSpace(result)
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(result), &m); err != nil {
		return result
	}
	if stdout, ok := m["stdout"].(string); ok {
		return stdout
	}
	return result
}

// parseParallelSubagentResult parses a run_parallel_subagents result into
// a map of task-ID → extracted stdout output. The result is a JSON object
// keyed by task ID, each value being a nested object with a "stdout"
// field. Non-parseable results return an empty map.
func parseParallelSubagentResult(result string) map[string]string {
	result = strings.TrimSpace(result)
	out := make(map[string]string)

	var topLevel map[string]json.RawMessage
	if err := json.Unmarshal([]byte(result), &topLevel); err != nil {
		return out
	}

	for taskID, raw := range topLevel {
		var taskResult map[string]interface{}
		if err := json.Unmarshal(raw, &taskResult); err != nil {
			continue
		}
		if stdout, ok := taskResult["stdout"].(string); ok {
			out[taskID] = stdout
		}
	}

	return out
}

// parseToolCallArgs parses the JSON arguments string from a tool call into
// a map. Returns an empty map on parse failure.
func parseToolCallArgs(argsJSON string) map[string]interface{} {
	args := make(map[string]interface{})
	argsJSON = strings.TrimSpace(argsJSON)
	if argsJSON == "" {
		return args
	}
	// Best-effort parse — ignore errors.
	_ = json.Unmarshal([]byte(argsJSON), &args)
	return args
}

// extractStringArg safely extracts a string value from a parsed args map.
func extractStringArg(args map[string]interface{}, key string) string {
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	default:
		// Some providers may send non-string types; marshal to string.
		b, err := json.Marshal(val)
		if err != nil {
			return ""
		}
		return string(b)
	}
}

// extractTaskPrompts extracts task prompts from a "subagents"/"tasks"/
// "prompts" array value. Each element may be a simple string or an object
// with a "prompt" field.
func extractTaskPrompts(raw interface{}) []string {
	specs := extractTaskSpecs(raw)
	prompts := make([]string, len(specs))
	for i, s := range specs {
		prompts[i] = s.prompt
	}
	return prompts
}

// parallelTaskSpec holds an ID and prompt for one parallel subagent task.
type parallelTaskSpec struct {
	id     string
	prompt string
}

// extractTaskSpecs extracts task specs (ID + prompt) from a
// "subagents"/"tasks"/"prompts" array value. Each element may be a simple
// string (auto-generated ID "task-N") or an object with "id" and "prompt"
// fields.
func extractTaskSpecs(raw interface{}) []parallelTaskSpec {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	var specs []parallelTaskSpec
	for i, item := range arr {
		switch v := item.(type) {
		case string:
			specs = append(specs, parallelTaskSpec{
				id:     fmt.Sprintf("task-%d", i+1),
				prompt: v,
			})
		case map[string]interface{}:
			spec := parallelTaskSpec{
				id: fmt.Sprintf("task-%d", i+1),
			}
			if id, ok := v["id"].(string); ok && id != "" {
				spec.id = id
			}
			if p, ok := v["prompt"].(string); ok {
				spec.prompt = p
			}
			specs = append(specs, spec)
		}
	}
	return specs
}

// ---------------------------------------------------------------------------
// File writers
// ---------------------------------------------------------------------------
