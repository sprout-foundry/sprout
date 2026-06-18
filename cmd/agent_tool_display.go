//go:build !js

package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// formatToolStartLine builds the activity-indicator line for a ToolStart
// event. At depth 0 it's byte-identical to the pre-SP-051 format
// ("  tool_name(preview)") so primary-agent tool calls render unchanged.
// At depth >= 1 it adds a depth indent and a colored "[persona]" badge.
func formatToolStartLine(depth int, persona, toolName, preview string) string {
	indent := console.PersonaIndent(depth)
	badge := console.PersonaBadge(depth, persona)
	return fmt.Sprintf("%s  %s%s%s", indent, badge, toolName, preview)
}

// formatToolEndLine builds the activity-indicator replacement line for a
// ToolEnd event. Same depth/badge logic as formatToolStartLine.
func formatToolEndLine(depth int, persona, icon, toolName, preview string, durationSec float64) string {
	indent := console.PersonaIndent(depth)
	badge := console.PersonaBadge(depth, persona)
	return fmt.Sprintf("%s  %s %s%s%s · %.1fs", indent, icon, badge, toolName, preview, durationSec)
}

// formatToolRunLine renders a collapsed line for repeated calls of the
// same tool. Replaces N stacked "✓ read_file (foo.go) · 0.1s" entries
// with a single "✓ read_file × N (foo.go, bar.go, baz.go) · 0.3s" line
// updated in place via ActivityIndicator.ReplaceLastN.
//
// argsTrail holds the most recent up-to-3 arg previews so the user can
// still see what was touched without scrolling through identical
// entries. totalSec is the cumulative duration across all N calls so
// the line still surfaces "this batch took a moment" even when each
// individual call was quick.
func formatToolRunLine(depth int, persona, icon, toolName string, count int, argsTrail []string, totalSec float64) string {
	indent := console.PersonaIndent(depth)
	badge := console.PersonaBadge(depth, persona)
	preview := ""
	if len(argsTrail) > 0 {
		preview = " (" + strings.Join(argsTrail, ", ") + ")"
	}
	return fmt.Sprintf("%s  %s%s%s × %d%s · %.1fs",
		indent, icon, badge, toolName, count, preview, totalSec)
}

// toolRunState tracks a sequence of consecutive identical tool calls
// so the subscriber can collapse them into a single in-place row
// (Phase 3 of CLI ergonomics). A run is broken — set to nil — whenever
// any non-tool event would invalidate the row math: streaming
// assistant text, a different tool, or a user-prompt boundary.
type toolRunState struct {
	name      string
	depth     int
	persona   string
	count     int
	argsTrail []string // most recent up to maxArgsTrail entries
	totalMs   int64
	lastIcon  string
	lastEnd   time.Time
}

// maxArgsTrail caps the per-arg preview list shown in the collapsed
// line. The earliest entries get dropped — the user usually cares
// about the most recent few calls in a run.
const maxArgsTrail = 3

func (r *toolRunState) matches(name string, depth int, persona string) bool {
	return r != nil && r.name == name && r.depth == depth && r.persona == persona
}

func (r *toolRunState) appendArg(preview string) {
	// formatToolPreview returns its result already wrapped in " (...)"
	// so that the start/end lines render as "tool (arg)". For the
	// collapsed run line we re-wrap argsTrail as a single parenthesised
	// list ("(a, b, c)"), so strip the per-arg wrap here. Otherwise
	// the line read "tool × N ( (a),  (b))" with doubled parens.
	stripped := strings.TrimPrefix(preview, " (")
	stripped = strings.TrimSuffix(stripped, ")")
	stripped = strings.TrimSpace(stripped)
	if stripped == "" {
		// No useful arg captured — skip rather than append "" which
		// renders as a stray comma-space ("× N (, , foo)").
		return
	}
	r.argsTrail = append(r.argsTrail, stripped)
	if len(r.argsTrail) > maxArgsTrail {
		r.argsTrail = r.argsTrail[len(r.argsTrail)-maxArgsTrail:]
	}
}

// formatToolPreview produces a short, single-line preview of a tool call
// for the activity-indicator timeline. For subagent tools (run_subagent,
// run_parallel_subagents) it surfaces the persona and the resolved
// provider/model so users can see which subagent — and which underlying
// model, often a cheaper/faster one than the parent's — is doing the
// work. For everything else it falls through to formatToolArgPreview.
func formatToolPreview(chatAgent *agent.Agent, toolName, arguments string) string {
	switch toolName {
	case "run_subagent":
		return formatRunSubagentPreview(chatAgent, arguments)
	case "run_parallel_subagents":
		return formatRunParallelSubagentsPreview(arguments)
	case "TodoWrite", "todo_write":
		return formatTodoWritePreview(arguments)
	default:
		return formatToolArgPreview(toolName, arguments)
	}
}

// formatTodoListBlock renders the multi-line todo block printed in the
// scroll region in response to EventTypeTodoUpdate. The header is a
// one-line summary (counts by status); the body is one row per item
// with a status-coded glyph (✓ done, → active, · pending, ⏹ cancelled).
// Truncates long lists to keep the terminal scannable.
func formatTodoListBlock(todosRaw []interface{}) string {
	type todoEntry struct {
		content string
		status  string
	}
	items := make([]todoEntry, 0, len(todosRaw))
	var pending, inProgress, completed, cancelled int
	for _, t := range todosRaw {
		m, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		content, _ := m["content"].(string)
		status, _ := m["status"].(string)
		items = append(items, todoEntry{content: content, status: status})
		switch status {
		case "pending":
			pending++
		case "in_progress":
			inProgress++
		case "completed":
			completed++
		case "cancelled":
			cancelled++
		}
	}

	var b strings.Builder
	parts := []string{fmt.Sprintf("%d total", len(items))}
	if completed > 0 {
		parts = append(parts, fmt.Sprintf("%d done", completed))
	}
	if inProgress > 0 {
		parts = append(parts, fmt.Sprintf("%d active", inProgress))
	}
	if pending > 0 {
		parts = append(parts, fmt.Sprintf("%d pending", pending))
	}
	if cancelled > 0 {
		parts = append(parts, fmt.Sprintf("%d cancelled", cancelled))
	}
	b.WriteString(console.GlyphInfo.Prefix() + "Todos · " + strings.Join(parts, " · "))

	const maxLines = 20
	const maxContentLen = 100
	shown := 0
	for _, it := range items {
		if shown >= maxLines {
			fmt.Fprintf(&b, "\n   %s…and %d more", console.GlyphDim.Prefix(), len(items)-shown)
			break
		}
		content := strings.TrimSpace(it.content)
		if content == "" {
			content = "<untitled>"
		}
		if len(content) > maxContentLen {
			content = content[:maxContentLen-1] + "…"
		}
		fmt.Fprintf(&b, "\n   %s%s", todoStatusGlyph(it.status), content)
		shown++
	}
	return b.String()
}

// todoStatusGlyph maps a todo status onto the shared CLI glyph palette.
// Mirrors the mapping used by pkg/agent/tool_executor_todo_events.go so
// the inline list and any other todo-status rendering stay visually
// consistent.
func todoStatusGlyph(status string) string {
	switch status {
	case "completed":
		return console.GlyphSuccess.Prefix()
	case "in_progress":
		return console.GlyphAction.Prefix()
	case "cancelled":
		return console.GlyphStopped.Prefix()
	default:
		return console.GlyphDim.Prefix()
	}
}

// formatTodoWritePreview produces the compact tail for the todo_write
// tool's spinner / collapse line — "(5 tasks · 1 active · 3 done)" —
// so the user sees the shape of the list at a glance without waiting
// for the full TodoUpdate block to land. Returns "" when the args
// are unparseable or empty, matching the contract of the other
// per-tool preview helpers.
func formatTodoWritePreview(arguments string) string {
	if arguments == "" {
		return ""
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return ""
	}
	todos, ok := args["todos"].([]interface{})
	if !ok || len(todos) == 0 {
		return ""
	}
	var inProgress, completed int
	for _, t := range todos {
		m, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		switch s, _ := m["status"].(string); s {
		case "in_progress":
			inProgress++
		case "completed":
			completed++
		}
	}
	parts := []string{fmt.Sprintf("%d tasks", len(todos))}
	if inProgress > 0 {
		parts = append(parts, fmt.Sprintf("%d active", inProgress))
	}
	if completed > 0 {
		parts = append(parts, fmt.Sprintf("%d done", completed))
	}
	return " (" + strings.Join(parts, " · ") + ")"
}

// formatRunSubagentPreview extracts the persona from args and looks up its
// effective provider/model via the agent's persona resolver. Format:
//
//	 (coder · anthropic/claude-haiku-4-5)
//
// Falls back to just persona name (or empty) when the lookup fails.
func formatRunSubagentPreview(chatAgent *agent.Agent, arguments string) string {
	if arguments == "" || chatAgent == nil {
		return ""
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return ""
	}
	persona, _ := args["persona"].(string)
	persona = strings.TrimSpace(persona)
	if persona == "" {
		return ""
	}
	provider, model, err := chatAgent.GetPersonaProviderModel(persona)
	if err != nil || (provider == "" && model == "") {
		return fmt.Sprintf(" (%s)", persona)
	}
	return fmt.Sprintf(" (%s · %s/%s)", persona, provider, model)
}

// formatRunParallelSubagentsPreview shows the task count so the user
// knows how many subagents fanned out. No per-task persona since the
// parallel form doesn't accept per-task persona overrides today; users
// see the count and infer fan-out from the line.
func formatRunParallelSubagentsPreview(arguments string) string {
	if arguments == "" {
		return ""
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return ""
	}
	if tasks, ok := args["subagents"].([]interface{}); ok && len(tasks) > 0 {
		return fmt.Sprintf(" (%d tasks)", len(tasks))
	}
	return ""
}

// formatToolArgPreview produces a short, single-line preview of a tool's
// arguments for the activity indicator. The arguments string is the raw
// JSON the model emitted; we extract whichever field is most informative
// for the tool at hand. Returns an empty string (no parens) when nothing
// useful is available. Best-effort — invalid JSON yields no preview.
//
// Per-tool max widths and truncation strategies:
//   - File paths use abbreviatePath so the filename always survives even
//     when the directory prefix has to be dropped — "…/last/two/seg.go"
//     reads better than "webui/src/components/sett…" where the actual
//     file is lost.
//   - shell_command / exec preserve more context (80 chars) because the
//     suffix of a command is often the meaningful part (pipes, args).
//   - Everything else gets the conservative 70-char tail truncation.
func formatToolArgPreview(toolName, arguments string) string {
	if arguments == "" {
		return ""
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil || len(args) == 0 {
		return ""
	}

	pick := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := args[k].(string); ok && v != "" {
				return v
			}
		}
		return ""
	}

	var preview string
	var maxLen int
	isPath := false
	switch toolName {
	case "read_file", "write_file", "edit_file", "write_structured_file", "patch_structured_file":
		preview = pick("path", "file_path", "filename")
		maxLen = 70
		isPath = true
	case "shell_command", "exec":
		preview = pick("command", "cmd")
		maxLen = 80
	case "search_files", "grep":
		preview = pick("pattern", "query", "search")
		maxLen = 70
	case "fetch_url":
		preview = pick("url")
		maxLen = 70
	default:
		// Generic fallback: surface the first short string value.
		for _, v := range args {
			if s, ok := v.(string); ok && len(s) > 0 && len(s) < 120 {
				preview = s
				break
			}
		}
		maxLen = 70
	}

	preview = sanitizeArgForPreview(preview)
	if preview == "" {
		return ""
	}
	if isPath {
		preview = abbreviatePath(preview, maxLen)
	} else if len(preview) > maxLen {
		preview = preview[:maxLen-1] + "…"
	}
	return " (" + preview + ")"
}

// abbreviatePath shortens a path while preserving the filename. A path
// like "webui/src/components/settings/ProviderSettingsTab.tsx" that
// exceeds maxLen renders as "…/ProviderSettingsTab.tsx" — the user
// almost always cares about the file at the tail more than the
// directory chain.
//
// When the path has a separator we always prefer "…/basename" even if
// the basename itself is still over maxLen: the alternative (tail-
// truncating the basename) drops the suffix that usually identifies
// the file type, which is worse than overshooting maxLen by a few
// chars on a pathological filename. The only path with no separator
// falls back to a plain tail-truncate.
func abbreviatePath(p string, maxLen int) string {
	if len(p) <= maxLen {
		return p
	}
	slash := strings.LastIndex(p, "/")
	if slash < 0 {
		return p[:maxLen-1] + "…"
	}
	return "…/" + p[slash+1:]
}

// sanitizeArgForPreview collapses whitespace and strips control characters
// so the preview always renders on one line inside parentheses.
func sanitizeArgForPreview(s string) string {
	out := make([]rune, 0, len(s))
	prevSpace := false
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			if !prevSpace {
				out = append(out, ' ')
				prevSpace = true
			}
			continue
		}
		if r < 32 {
			continue
		}
		out = append(out, r)
		prevSpace = r == ' '
	}
	return strings.TrimSpace(string(out))
}
