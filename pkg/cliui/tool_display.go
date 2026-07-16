//go:build !js

package cliui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// FormatToolStartLine builds the activity-indicator line for a ToolStart
// event. At depth 0 it's byte-identical to the pre-SP-051 format
// ("  tool_name(preview)") so primary-agent tool calls render unchanged.
// At depth >= 1 it adds a depth indent and a colored "[persona]" badge.
func FormatToolStartLine(depth int, persona, toolName, preview string) string {
	indent := console.PersonaIndent(depth)
	badge := console.PersonaBadge(depth, persona)
	return fmt.Sprintf("%s  %s%s%s", indent, badge, toolName, preview)
}

// FormatToolEndLine builds the activity-indicator replacement line for a
// ToolEnd event. Same depth/badge logic as FormatToolStartLine.
func FormatToolEndLine(depth int, persona, icon, toolName, preview string, durationSec float64) string {
	indent := console.PersonaIndent(depth)
	badge := console.PersonaBadge(depth, persona)
	return fmt.Sprintf("%s  %s %s%s%s · %.1fs", indent, icon, badge, toolName, preview, durationSec)
}

// FormatToolRunLine renders a collapsed line for repeated calls of the
// same tool. Replaces N stacked "✓ read_file (foo.go) · 0.1s" entries
// with a single "✓ read_file × N (foo.go, bar.go, baz.go) · 0.3s" line
// updated in place via ActivityIndicator.ReplaceLastN.
//
// argsTrail holds the most recent up-to-3 arg previews so the user can
// still see what was touched without scrolling through identical
// entries. totalSec is the cumulative duration across all N calls so
// the line still surfaces "this batch took a moment" even when each
// individual call was quick.
func FormatToolRunLine(depth int, persona, icon, toolName string, count int, argsTrail []string, totalSec float64) string {
	indent := console.PersonaIndent(depth)
	badge := console.PersonaBadge(depth, persona)
	preview := ""
	if len(argsTrail) > 0 {
		preview = " (" + strings.Join(argsTrail, ", ") + ")"
	}
	return fmt.Sprintf("%s  %s%s%s × %d%s · %.1fs",
		indent, icon, badge, toolName, count, preview, totalSec)
}

// ComputeDiffStat produces a dim "+N -M" diffstat suffix for file-editing
// tools. For edit_file it counts lines in old_str vs new_str; for write_file
// it counts all lines as added (new file or full overwrite). Returns "" for
// non-file tools or when no useful diff can be computed. CLI-UX-3.
func ComputeDiffStat(toolName, arguments string) string {
	if arguments == "" {
		return ""
	}
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return ""
	}
	switch toolName {
	case "edit_file":
		oldStr, _ := args["old_str"].(string)
		newStr, _ := args["new_str"].(string)
		removed := countLines(oldStr)
		added := countLines(newStr)
		if added == 0 && removed == 0 {
			return ""
		}
		return fmt.Sprintf("%s+%d -%d%s", console.ColorGreen, added, removed, console.ColorReset)
	case "write_file":
		content, _ := args["content"].(string)
		added := countLines(content)
		if added == 0 {
			return ""
		}
		return fmt.Sprintf("%s+%d%s", console.ColorGreen, added, console.ColorReset)
	case "write_structured_file":
		// content is in "data" field as structured JSON — count lines in the
		// serialized form for a rough size signal
		if data, ok := args["data"]; ok {
			jsonBytes, _ := json.Marshal(data)
			added := countLines(string(jsonBytes))
			if added > 0 {
				return fmt.Sprintf("%s+%d%s", console.ColorGreen, added, console.ColorReset)
			}
		}
	}
	return ""
}

// FormatCompactDiffLine renders the minimal one-liner shown in compact mode
// for file edits: "edit_file (path.go) +12 -3". Extracts the path from args
// for context so the user knows which file changed.
func FormatCompactDiffLine(toolName, arguments, diffStat string) string {
	path := ""
	if arguments != "" {
		var args map[string]interface{}
		if json.Unmarshal([]byte(arguments), &args) == nil {
			if p, ok := args["path"].(string); ok {
				path = AbbreviatePath(p, 50)
			}
		}
	}
	if path != "" {
		return fmt.Sprintf("%s (%s) %s", toolName, path, diffStat)
	}
	return fmt.Sprintf("%s %s", toolName, diffStat)
}

// countLines returns the number of newline-separated lines in s.
// A non-empty string with no newlines counts as 1 line.
func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// FormatResultSize renders a human-readable size string for the number
// of characters in a tool result. Used by verbose mode to append a dim
// "· 1.2KB" or "· 450 chars" suffix to tool-end lines. Returns "" for
// zero-length results so we don't clutter the line with "· 0 chars".
//
// Threshold: >=1000 chars switches to kilobytes (base-1024) with one
// decimal place; below that we show the raw char count.
func FormatResultSize(length int) string {
	if length <= 0 {
		return ""
	}
	if length >= 1000 {
		return fmt.Sprintf("%.1fKB", float64(length)/1024)
	}
	return fmt.Sprintf("%d chars", length)
}

// ToolRunState tracks a sequence of consecutive identical tool calls
// so the subscriber can collapse them into a single in-place row
// (Phase 3 of CLI ergonomics). A run is broken — set to nil — whenever
// any non-tool event would invalidate the row math: streaming
// assistant text, a different tool, or a user-prompt boundary.
type ToolRunState struct {
	Name      string
	Depth     int
	Persona   string
	Count     int
	ArgsTrail []string // most recent up to MaxArgsTrail entries
	TotalMs   int64
	LastIcon  string
	LastEnd   time.Time
}

// MaxArgsTrail caps the per-arg preview list shown in the collapsed
// line. The earliest entries get dropped — the user usually cares
// about the most recent few calls in a run.
const MaxArgsTrail = 3

// Matches reports whether the run matches the given tool call parameters.
func (r *ToolRunState) Matches(name string, depth int, persona string) bool {
	return r != nil && r.Name == name && r.Depth == depth && r.Persona == persona
}

// AppendArg adds an argument preview to the args trail, capping it at MaxArgsTrail.
func (r *ToolRunState) AppendArg(preview string) {
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
	r.ArgsTrail = append(r.ArgsTrail, stripped)
	if len(r.ArgsTrail) > MaxArgsTrail {
		r.ArgsTrail = r.ArgsTrail[len(r.ArgsTrail)-MaxArgsTrail:]
	}
}

// FormatToolPreview produces a short, single-line preview of a tool call
// for the activity-indicator timeline. For subagent tools (run_subagent,
// run_parallel_subagents) it surfaces the persona and the resolved
// provider/model so users can see which subagent — and which underlying
// model, often a cheaper/faster one than the parent's — is doing the
// work. For everything else it falls through to FormatToolArgPreview.
//
// maxArgLen overrides the per-tool truncation width when > 0 (verbose
// mode passes a higher value so power users see more of the path/command).
// Pass 0 to use the built-in per-tool defaults.
func FormatToolPreview(chatAgent *agent.Agent, toolName, arguments string, maxArgLen int) string {
	switch toolName {
	case "run_subagent":
		return FormatRunSubagentPreview(chatAgent, arguments)
	case "run_parallel_subagents":
		return FormatRunParallelSubagentsPreview(arguments)
	case "TodoWrite", "todo_write":
		return FormatTodoWritePreview(arguments)
	default:
		return FormatToolArgPreview(toolName, arguments, maxArgLen)
	}
}

// FormatTodoListBlock renders the multi-line todo block printed in the
// scroll region in response to EventTypeTodoUpdate. The header is a
// one-line summary (counts by status); the body is one row per item
// with a status-coded glyph (✓ done, → active, · pending, ⏹ cancelled).
// Truncates long lists to keep the terminal scannable.
func FormatTodoListBlock(todosRaw []interface{}) string {
	return formatTodoListBlockLocked(todosRaw)
}

// FormatTodoListPanel renders the todo list inside a box-drawing panel
// for stronger visual structure (CLI-UX-9). The panel header includes
// the status counts; the body is the same per-row content as
// formatTodoListBlock but wrapped in light-vertical borders.
func FormatTodoListPanel(todosRaw []interface{}) string {
	items, counts := CollectTodos(todosRaw)
	content := buildTodoPanelContent(items, counts)
	style := console.DefaultPanelStyle()
	style.MinWidth = 40
	style.MaxWidth = 100
	return console.Panel{
		Title:   buildTodoPanelTitle(counts),
		Content: content,
		Style:   style,
	}.Render()
}

// TodoEntry mirrors the internal struct used by both the inline block
// and the panel renderer so they stay in sync.
type TodoEntry struct {
	Content string
	Status  string
}

// CollectTodos parses the raw todo event payload into typed items and
// counts by status. Shared by formatTodoListBlock and formatTodoListPanel.
func CollectTodos(todosRaw []interface{}) ([]TodoEntry, map[string]int) {
	items := make([]TodoEntry, 0, len(todosRaw))
	counts := map[string]int{
		"pending":     0,
		"in_progress": 0,
		"completed":   0,
		"cancelled":   0,
	}
	for _, t := range todosRaw {
		m, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		content, _ := m["content"].(string)
		status, _ := m["status"].(string)
		items = append(items, TodoEntry{Content: content, Status: status})
		if _, tracked := counts[status]; tracked {
			counts[status]++
		} else {
			counts["pending"]++
		}
	}
	return items, counts
}

// buildTodoPanelTitle assembles the header line shown in the panel's
// top border: "Todos · 8 total · 3 done · 1 active · 4 pending".
func buildTodoPanelTitle(counts map[string]int) string {
	total := 0
	for _, n := range counts {
		total += n
	}
	parts := []string{fmt.Sprintf("%d total", total)}
	if counts["completed"] > 0 {
		parts = append(parts, fmt.Sprintf("%d done", counts["completed"]))
	}
	if counts["in_progress"] > 0 {
		parts = append(parts, fmt.Sprintf("%d active", counts["in_progress"]))
	}
	if counts["pending"] > 0 {
		parts = append(parts, fmt.Sprintf("%d pending", counts["pending"]))
	}
	if counts["cancelled"] > 0 {
		parts = append(parts, fmt.Sprintf("%d cancelled", counts["cancelled"]))
	}
	return "Todos · " + strings.Join(parts, " · ")
}

// buildTodoPanelContent renders the per-row body of the todo panel.
// Each row is "✓ content" with a status-coded glyph. Truncates long
// lists to keep the terminal scannable.
func buildTodoPanelContent(items []TodoEntry, _ map[string]int) []string {
	const maxLines = 20
	const maxContentLen = 80
	rows := make([]string, 0, len(items))
	shown := 0
	for _, it := range items {
		if shown >= maxLines {
			rows = append(rows, fmt.Sprintf("%s…and %d more", console.GlyphDim.Prefix(), len(items)-shown))
			break
		}
		content := strings.TrimSpace(it.Content)
		if content == "" {
			content = "<untitled>"
		}
		if len(content) > maxContentLen {
			content = content[:maxContentLen-1] + "…"
		}
		rows = append(rows, fmt.Sprintf("%s %s", todoStatusGlyph(it.Status), content))
		shown++
	}
	return rows
}

// TodoBlockRowCount returns the number of terminal rows that
// fmt.Fprintln(os.Stdout, formatTodoListBlock(todosRaw)) will consume.
// The block string has a header row plus one row per item (each item
// prefixed by \n). fmt.Fprintln adds a final \n. So the visible rows
// = strings.Count(block, "\n") + 1.
func TodoBlockRowCount(todosRaw []interface{}) int {
	block := formatTodoListBlockLocked(todosRaw)
	return strings.Count(block, "\n") + 1
}

// formatTodoListBlockLocked is the internal implementation.
func formatTodoListBlockLocked(todosRaw []interface{}) string {
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

// FormatTodoWritePreview produces the compact tail for the todo_write
// tool's spinner / collapse line — "(5 tasks · 1 active · 3 done)" —
// so the user sees the shape of the list at a glance without waiting
// for the full TodoUpdate block to land. Returns "" when the args
// are unparseable or empty, matching the contract of the other
// per-tool preview helpers.
func FormatTodoWritePreview(arguments string) string {
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

// FormatRunSubagentPreview extracts the persona from args and looks up its
// effective provider/model via the agent's persona resolver. Format:
//
//	(coder · anthropic/claude-haiku-4-5)
//
// Falls back to just persona name (or empty) when the lookup fails.
func FormatRunSubagentPreview(chatAgent *agent.Agent, arguments string) string {
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

// FormatRunParallelSubagentsPreview shows the task count so the user
// knows how many subagents fanned out. No per-task persona since the
// parallel form doesn't accept per-task persona overrides today; users
// see the count and infer fan-out from the line.
func FormatRunParallelSubagentsPreview(arguments string) string {
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

// FormatToolArgPreview produces a short, single-line preview of a tool's
// arguments for the activity indicator. The arguments string is the raw
// JSON the model emitted; we extract whichever field is most informative
// for the tool at hand. Returns an empty string (no parens) when nothing
// useful is available. Best-effort — invalid JSON yields no preview.
//
// maxArgLen overrides the per-tool truncation widths when > 0 (used by
// verbose mode to show longer paths/commands). Pass 0 to use the built-in
// per-tool defaults documented below.
//
// Per-tool max widths and truncation strategies (when maxArgLen == 0):
//   - File paths use AbbreviatePath so the filename always survives even
//     when the directory prefix has to be dropped — "…/last/two/seg.go"
//     reads better than "webui/src/components/sett…" where the actual
//     file is lost.
//   - shell_command / exec preserve more context (80 chars) because the
//     suffix of a command is often the meaningful part (pipes, args).
//   - Everything else gets the conservative 70-char tail truncation.
func FormatToolArgPreview(toolName, arguments string, maxArgLen int) string {
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

	// Verbose override: bump the truncation width so power users see
	// more of the path/command without the "…" cut.
	if maxArgLen > 0 {
		maxLen = maxArgLen
	}

	preview = SanitizeArgForPreview(preview)
	if preview == "" {
		return ""
	}
	if isPath {
		preview = AbbreviatePath(preview, maxLen)
	} else if len(preview) > maxLen {
		preview = preview[:maxLen-1] + "…"
	}
	return " (" + preview + ")"
}

// AbbreviatePath shortens a path while preserving the filename. A path
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
func AbbreviatePath(p string, maxLen int) string {
	if len(p) <= maxLen {
		return p
	}
	slash := strings.LastIndex(p, "/")
	if slash < 0 {
		return p[:maxLen-1] + "…"
	}
	return "…/" + p[slash+1:]
}

// SanitizeArgForPreview collapses whitespace and strips control characters
// so the preview always renders on one line inside parentheses.
func SanitizeArgForPreview(s string) string {
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

// EditDiffMaxLines is the default number of diff lines to show in
// non-verbose mode. Keep it tight — the user wants a glance, not a wall.
const EditDiffMaxLines = 8

// ComputeEditDiff generates a compact unified diff from the old and new
// strings for display in the terminal after an edit_file operation.
//
// Shows removed lines in red (-) and added lines in green (+), with up to
// one context line before and after the changed block. Truncates to
// maxLines when > 0; pass 0 for unlimited (verbose mode).
func ComputeEditDiff(oldStr, newStr string, maxLines int) string {
	oldLines := strings.Split(oldStr, "\n")
	newLines := strings.Split(newStr, "\n")

	// Compute common prefix and suffix to isolate the changed block
	pre := 0
	for pre < len(oldLines) && pre < len(newLines) && oldLines[pre] == newLines[pre] {
		pre++
	}
	suf := 0
	for suf < len(oldLines)-pre && suf < len(newLines)-pre &&
		oldLines[len(oldLines)-1-suf] == newLines[len(newLines)-1-suf] {
		suf++
	}

	oldMid := oldLines[pre : len(oldLines)-suf]
	newMid := newLines[pre : len(newLines)-suf]

	// Show 1 line of context before and after when available
	ctxBefore := 0
	if pre > 0 {
		ctxBefore = 1
	}
	ctxAfter := 0
	if suf > 0 && len(oldLines)-suf < len(oldLines) {
		ctxAfter = 1
	}

	var b strings.Builder

	// Context before
	if ctxBefore > 0 {
		b.WriteString(fmt.Sprintf("  %s%s%s\n", console.ColorDim, oldLines[pre-1], console.ColorReset))
	}

	// Removed lines
	for _, l := range oldMid {
		b.WriteString(fmt.Sprintf("%s- %s%s\n", console.ColorRed, l, console.ColorReset))
	}

	// Added lines
	for _, l := range newMid {
		b.WriteString(fmt.Sprintf("%s+ %s%s\n", console.ColorGreen, l, console.ColorReset))
	}

	// Context after
	if ctxAfter > 0 {
		b.WriteString(fmt.Sprintf("  %s%s%s\n", console.ColorDim, oldLines[len(oldLines)-suf], console.ColorReset))
	}

	result := b.String()
	if result == "" {
		return ""
	}

	// Truncate if needed
	if maxLines > 0 {
		lines := strings.Split(strings.TrimSuffix(result, "\n"), "\n")
		if len(lines) > maxLines {
			visible := strings.Join(lines[:maxLines], "\n")
			return visible + fmt.Sprintf("\n  %s… %d more lines (use verbose mode for full diff)%s",
				console.ColorDim, len(lines)-maxLines, console.ColorReset)
		}
	}

	return result
}

// ComputeWriteFileDiff generates a preview of new file content written
// via write_file. Shows the first few lines with green (+) markers,
// truncated per maxLines (0 = unlimited, for verbose mode).
func ComputeWriteFileDiff(content string, maxLines int) string {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		return ""
	}
	if maxLines <= 0 {
		maxLines = len(lines)
	}
	var b strings.Builder
	for i, l := range lines {
		if i >= maxLines {
			b.WriteString(fmt.Sprintf("  %s… %d more lines (use verbose mode for full output)%s\n",
				console.ColorDim, len(lines)-maxLines, console.ColorReset))
			break
		}
		b.WriteString(fmt.Sprintf("%s+ %s%s\n", console.ColorGreen, l, console.ColorReset))
	}
	return b.String()
}
