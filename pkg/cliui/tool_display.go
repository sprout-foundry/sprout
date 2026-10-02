//go:build !js

package cliui

// tool_display.go — the tool-line display layer: FormatToolStart/End/Run
// line builders, the diff-stat / compact-diff / result-size helpers, and the
// ToolRunState + tool-preview formatting. The todo-list display block lives
// in tool_display_todos.go; the edit/write diff + subagent / arg previews in
// tool_display_diff.go.

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
)

// ShellCommandLabel extracts the literal command from a shell tool's
// preview so the line reads "$ go test ./..." instead of
// "shell_command (go test…)". The preview wrapper (" (cmd)") comes from
// FormatToolArgPreview; trim it. Returns ok=false for non-shell tools
// or an empty command, leaving the caller on the default rendering.
func ShellCommandLabel(toolName, preview string) (string, bool) {
	if toolName != "shell_command" && toolName != "exec" {
		return "", false
	}
	cmd := strings.TrimSpace(preview)
	cmd = strings.TrimSuffix(strings.TrimPrefix(cmd, "("), ")")
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return "", false
	}
	return cmd, true
}

// FormatToolStartLine builds the activity-indicator line for a ToolStart
// event. At depth 0 it's byte-identical to the pre-SP-051 format
// ("  tool_name(preview)") so primary-agent tool calls render unchanged.
// At depth >= 1 it adds a depth indent and a colored "[persona]" badge.
// Shell commands render the literal command in place of the tool name.
func FormatToolStartLine(depth int, persona, toolName, preview string) string {
	indent := console.PersonaIndent(depth)
	badge := console.PersonaBadge(depth, persona)
	if cmd, ok := ShellCommandLabel(toolName, preview); ok {
		return fmt.Sprintf("%s  %s%s %s", indent, badge, console.GlyphShell.Prefix(), cmd)
	}
	return fmt.Sprintf("%s  %s%s%s", indent, badge, toolName, preview)
}

// FormatToolEndLine builds the activity-indicator replacement line for a
// ToolEnd event. Same depth/badge logic as FormatToolStartLine. The duration
// suffix is dimmed so glyph + tool name carry the visual weight. Shell
// commands render the literal command in place of the tool name.
func FormatToolEndLine(depth int, persona, icon, toolName, preview string, durationSec float64) string {
	indent := console.PersonaIndent(depth)
	badge := console.PersonaBadge(depth, persona)
	// icon is a Glyph.Prefix(), which carries its own trailing space.
	if cmd, ok := ShellCommandLabel(toolName, preview); ok {
		return fmt.Sprintf("%s  %s%s%s %s· %.1fs%s",
			indent, icon, badge, cmd, console.Esc(console.ColorDim), durationSec, console.Esc(console.ColorReset))
	}
	return fmt.Sprintf("%s  %s%s%s%s %s· %.1fs%s",
		indent, icon, badge, toolName, preview, console.Esc(console.ColorDim), durationSec, console.Esc(console.ColorReset))
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
// individual call was quick. Shell runs render with the shell glyph
// but keep the args trail so distinct commands stay visible.
func FormatToolRunLine(depth int, persona, icon, toolName string, count int, argsTrail []string, totalSec float64) string {
	indent := console.PersonaIndent(depth)
	badge := console.PersonaBadge(depth, persona)
	preview := ""
	if len(argsTrail) > 0 {
		preview = " (" + strings.Join(argsTrail, ", ") + ")"
	}
	return fmt.Sprintf("%s  %s%s%s × %d%s %s· %.1fs%s",
		indent, icon, badge, toolName, count, preview, console.Esc(console.ColorDim), totalSec, console.Esc(console.ColorReset))
}

// ToolEndGlyph picks the end-of-call glyph per the CLI display grammar:
// shell commands close with the shell prompt glyph, other completed tools
// close with the action arrow, and failures always close with the error
// cross. GlyphSuccess (✓) is reserved for outcomes — turn complete, todo
// completed, subagent results — so scanning for ✓ surfaces results, not
// routine activity.
func ToolEndGlyph(toolName, status string) console.Glyph {
	if status != "completed" {
		return console.GlyphError
	}
	if toolName == "shell_command" || toolName == "exec" {
		return console.GlyphShell
	}
	return console.GlyphAction
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
		return fmt.Sprintf("%s+%d -%d%s", console.Esc(console.ColorGreen), added, removed, console.Esc(console.ColorReset))
	case "write_file":
		content, _ := args["content"].(string)
		added := countLines(content)
		if added == 0 {
			return ""
		}
		return fmt.Sprintf("%s+%d%s", console.Esc(console.ColorGreen), added, console.Esc(console.ColorReset))
	case "write_structured_file":
		// content is in "data" field as structured JSON — count lines in the
		// serialized form for a rough size signal
		if data, ok := args["data"]; ok {
			jsonBytes, _ := json.Marshal(data)
			added := countLines(string(jsonBytes))
			if added > 0 {
				return fmt.Sprintf("%s+%d%s", console.Esc(console.ColorGreen), added, console.Esc(console.ColorReset))
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
