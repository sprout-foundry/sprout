//go:build !js

package cliui

// tool_display_todos.go — the todo-list display block: the todo panel /
// list formatters, CollectTodos, the panel title / content builders, and the
// todo status glyphs. Split out of tool_display.go.

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/console"
)

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
// Mirrors the mapping in pkg/agent_tools/todo_render.go so the inline
// list and any other todo-status rendering stay visually consistent.
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
