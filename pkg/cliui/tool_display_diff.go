//go:build !js

package cliui

// tool_display_diff.go — the diff + arg-preview display layer: the
// subagent / parallel-subagent / tool-arg previews, path abbreviation + arg
// sanitization, and ComputeEditDiff / ComputeWriteFileDiff. Split out of
// tool_display.go.

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
)

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
