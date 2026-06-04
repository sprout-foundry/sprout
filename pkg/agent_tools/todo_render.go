package tools

import (
	"fmt"
	"io"
	"strings"
)

// RenderTodosForCLI writes a bar-wrapped block summarizing the todo list
// to w, so CLI users see progress without having to read the LLM's
// structured tool output. Mirrors the visual treatment used by the
// ask_user CLI prompt (renderCLIPrompt) so the two surfaces feel like
// one family. Safe to call with an empty list — prints a "cleared"
// marker so the user knows the agent intentionally wiped the list.
func RenderTodosForCLI(w io.Writer, todos []TodoItem) {
	if w == nil {
		return
	}
	const bar = "────────────────────────────────────────────────"
	fmt.Fprintln(w)
	fmt.Fprintln(w, bar)
	if len(todos) == 0 {
		fmt.Fprintln(w, "  Tasks cleared")
		fmt.Fprintln(w, bar)
		return
	}

	completed, inProgress, pending, cancelled := 0, 0, 0, 0
	for _, t := range todos {
		switch t.Status {
		case "completed":
			completed++
		case "in_progress":
			inProgress++
		case "pending":
			pending++
		case "cancelled":
			cancelled++
		}
	}
	total := len(todos)
	pct := 0
	if total > 0 {
		pct = (completed * 100) / total
	}

	summary := fmt.Sprintf("  Tasks · %d total · %d done (%d%%)", total, completed, pct)
	if inProgress > 0 {
		summary += fmt.Sprintf(" · %d active", inProgress)
	}
	if pending > 0 {
		summary += fmt.Sprintf(" · %d pending", pending)
	}
	if cancelled > 0 {
		summary += fmt.Sprintf(" · %d cancelled", cancelled)
	}
	fmt.Fprintln(w, summary)
	fmt.Fprintln(w, bar)

	for _, t := range todos {
		marker := todoStatusGlyph(t.Status)
		priorityHint := todoPriorityHint(t.Priority)
		text := strings.TrimSpace(t.Content)
		if t.Status == "in_progress" && strings.TrimSpace(t.ActiveForm) != "" {
			text = strings.TrimSpace(t.ActiveForm)
		}
		if len(text) > 96 {
			text = text[:96] + "..."
		}
		if priorityHint != "" {
			fmt.Fprintf(w, "  %s %s %s\n", marker, priorityHint, text)
		} else {
			fmt.Fprintf(w, "  %s %s\n", marker, text)
		}
	}
	fmt.Fprintln(w, bar)
}

func todoStatusGlyph(status string) string {
	switch status {
	case "completed":
		return "[x]"
	case "in_progress":
		return "[~]"
	case "cancelled":
		return "[-]"
	default:
		return "[ ]"
	}
}

func todoPriorityHint(priority string) string {
	switch strings.ToLower(strings.TrimSpace(priority)) {
	case "high":
		return "(!)"
	case "medium":
		return "(.)"
	case "low":
		return "( )"
	}
	return ""
}
