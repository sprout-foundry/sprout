// Package agent: payload and display-name helpers for seed tool events,
// secret source building, and TodoWrite event formatting. (split from
// seed_tool_registry.go)
package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

// extractDurationMs reads the duration_ms field from a tool_end payload.
// seed publishes this as either an int or a float depending on whether the
// duration is whole milliseconds — handle both.
func extractDurationMs(payload map[string]interface{}) time.Duration {
	switch v := payload["duration_ms"].(type) {
	case int:
		return time.Duration(v) * time.Millisecond
	case int64:
		return time.Duration(v) * time.Millisecond
	case float64:
		return time.Duration(v * float64(time.Millisecond))
	}
	return 0
}

// argsFromPayload returns the tool's argument map from a tool event
// payload. seed core publishes the args as a JSON string under
// "arguments" (see core.EventTypeToolStart), so the primary-arg keys
// buildDisplayName looks for ("command", "path", …) are NOT top-level —
// without decoding, every shell line rendered as a bare "shell_command".
// Falls back to a structured "args"/"input" map, then to the payload
// itself, so older event shapes still work.
func argsFromPayload(payload map[string]interface{}) map[string]interface{} {
	for _, k := range []string{"args", "input", "parameters"} {
		if m, ok := payload[k].(map[string]interface{}); ok {
			return m
		}
	}
	if s, ok := payload["arguments"].(string); ok && s != "" {
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(s), &m); err == nil {
			return m
		}
	}
	return payload
}

// buildDisplayName constructs a human-readable tool name by appending the
// primary argument to the tool name. For example:
//   - read_file /path/to/file → "read_file /path/to/file"
//   - shell_command ls -la → "shell_command ls -la"
//   - run_subagent with prompt → "run_subagent [task]"
func buildDisplayName(toolName string, payload map[string]interface{}) string {
	switch toolName {
	case "read_file", "write_file", "edit_file", "write_structured_file", "patch_structured_file":
		if path, ok := payload["path"].(string); ok && path != "" {
			return fmt.Sprintf("%s %s", toolName, path)
		}
	case "shell_command":
		if cmd, ok := payload["command"].(string); ok && cmd != "" {
			// Collapse newlines / whitespace runs so a multi-line command
			// renders as a single scannable line, then truncate.
			cmd = strings.Join(strings.Fields(cmd), " ")
			if len(cmd) > 80 {
				return fmt.Sprintf("%s %s...", toolName, cmd[:77])
			}
			return fmt.Sprintf("%s %s", toolName, cmd)
		}
	case "git":
		if op, ok := payload["operation"].(string); ok && op != "" {
			if args, ok2 := payload["args"].(string); ok2 && args != "" {
				return fmt.Sprintf("%s %s %s", toolName, op, args)
			}
			return fmt.Sprintf("%s %s", toolName, op)
		}
	case "commit":
		if msg, ok := payload["message"].(string); ok && msg != "" {
			if len(msg) > 80 {
				return fmt.Sprintf("%s %s...", toolName, msg[:77])
			}
			return fmt.Sprintf("%s %s", toolName, msg)
		}
		return toolName
	case "search_files":
		if pattern, ok := payload["search_pattern"].(string); ok && pattern != "" {
			return fmt.Sprintf("%s %s", toolName, pattern)
		}
	case "web_search", "semantic_search":
		if query, ok := payload["query"].(string); ok && query != "" {
			if len(query) > 80 {
				return fmt.Sprintf("%s %s...", toolName, query[:77])
			}
			return fmt.Sprintf("%s %s", toolName, query)
		}
	case "fetch_url", "browse_url":
		if url, ok := payload["url"].(string); ok && url != "" {
			// Truncate URLs for readability
			if len(url) > 80 {
				return fmt.Sprintf("%s %s...", toolName, url[:77])
			}
			return fmt.Sprintf("%s %s", toolName, url)
		}
	case "run_subagent":
		if prompt, ok := payload["prompt"].(string); ok && prompt != "" {
			if len(prompt) > 80 {
				return fmt.Sprintf("%s [task: %s...]", toolName, prompt[:77])
			}
			return fmt.Sprintf("%s [task: %s]", toolName, prompt)
		}
	case "run_parallel_subagents":
		if subagents, ok := payload["subagents"].([]interface{}); ok {
			return fmt.Sprintf("%s (%d subagents)", toolName, len(subagents))
		}
		return toolName
	case "ask_user":
		if question, ok := payload["question"].(string); ok && question != "" {
			if len(question) > 80 {
				return fmt.Sprintf("%s %s...", toolName, question[:77])
			}
			return fmt.Sprintf("%s %s", toolName, question)
		}
	case "TodoWrite":
		if todos, ok := payload["todos"].([]interface{}); ok {
			return fmt.Sprintf("%s (%d items)", toolName, len(todos))
		}
	case "embedding_index":
		if operation, ok := payload["operation"].(string); ok && operation != "" {
			return fmt.Sprintf("%s %s", toolName, operation)
		}
	case "activate_skill":
		if skillID, ok := payload["skill_id"].(string); ok && skillID != "" {
			return fmt.Sprintf("%s %s", toolName, skillID)
		}
	case "manage_memory":
		// Surface the affected memory name for add/read/delete; for list/search
		// fall through to the bare tool name.
		if name, ok := payload["name"].(string); ok && name != "" {
			return fmt.Sprintf("%s %s", toolName, name)
		}
	case "analyze_ui_screenshot":
		if imgPath, ok := payload["image_path"].(string); ok && imgPath != "" {
			return fmt.Sprintf("%s %s", toolName, imgPath)
		}
	case "analyze_image_content":
		if imgPath, ok := payload["image_path"].(string); ok && imgPath != "" {
			return fmt.Sprintf("%s %s", toolName, imgPath)
		}
	}

	return toolName
}

// extractPersona extracts the persona from tool arguments if present.
func extractPersona(payload map[string]interface{}) string {
	if persona, ok := payload["persona"].(string); ok && persona != "" {
		return persona
	}
	return ""
}

// ---------------------------------------------------------------------------
// Post-processing helpers: inlined in handler closures (seed's PostExecuteHook
// only receives (name, result) — no agent, no args, no context).
// ---------------------------------------------------------------------------

// buildSecretSource constructs the source string used by the elevation gate
// to identify the origin of detected secrets.
func buildSecretSource(toolName string, args map[string]interface{}) string {
	switch toolName {
	case "shell_command":
		if cmd, ok := args["command"].(string); ok {
			if len(cmd) > 80 {
				return toolName + ": " + cmd[:77] + "..."
			}
			return toolName + ": " + cmd
		}
	case "read_file", "write_file", "edit_file", "write_structured_file", "patch_structured_file":
		if path, ok := args["path"].(string); ok && path != "" {
			return toolName + ": " + path
		}
	case "search_files":
		if pattern, ok := args["search_pattern"].(string); ok && pattern != "" {
			return toolName + ": " + pattern
		}
	}
	return toolName
}

// formatTodoItemsForEvent converts a []tools.TodoItem slice into the
// []map[string]interface{} format expected by PublishTodoUpdate.
// Includes optional activeForm and priority so the WebUI can surface
// the present-continuous phrasing and priority indicator.
func formatTodoItemsForEvent(todos []tools.TodoItem) []map[string]interface{} {
	result := make([]map[string]interface{}, len(todos))
	for i, t := range todos {
		entry := map[string]interface{}{
			"id":      t.ID,
			"content": t.Content,
			"status":  t.Status,
		}
		if t.ActiveForm != "" {
			entry["activeForm"] = t.ActiveForm
		}
		if t.Priority != "" {
			entry["priority"] = t.Priority
		}
		result[i] = entry
	}
	return result
}
