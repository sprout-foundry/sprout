// Todo event publishing: detects changes in todo checklists after
// TodoWrite tool calls and publishes structured update events.
package agent

import (
	"fmt"
	"strings"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/console"
)

func (te *ToolExecutor) emitTodoChecklistUpdate(before, after []tools.TodoItem) {
	if te.agent == nil {
		return
	}

	type todoKey struct {
		ID      string
		Content string
	}
	getKey := func(t tools.TodoItem) todoKey {
		return todoKey{
			ID:      strings.TrimSpace(t.ID),
			Content: strings.TrimSpace(t.Content),
		}
	}
	statusBefore := make(map[todoKey]string, len(before))
	for _, t := range before {
		statusBefore[getKey(t)] = t.Status
	}

	var pending, inProgress, completed, cancelled int
	changed := make([]string, 0, len(after))

	for _, t := range after {
		switch t.Status {
		case "pending":
			pending++
		case "in_progress":
			inProgress++
		case "completed":
			completed++
		case "cancelled":
			cancelled++
		}

		key := getKey(t)
		prevStatus, existed := statusBefore[key]
		if !existed || prevStatus != t.Status {
			statusSymbol := todoStatusSymbol(t.Status)
			label := strings.TrimSpace(t.Content)
			if label == "" {
				label = "<untitled>"
			}
			if existed {
				changed = append(changed, fmt.Sprintf("%s%s (%s → %s)", statusSymbol, label, prevStatus, t.Status))
			} else {
				changed = append(changed, fmt.Sprintf("%s%s (new)", statusSymbol, label))
			}
		}
	}

	// Publish structured todo update event for WebUI
	var todoItems []map[string]interface{}
	for _, t := range after {
		todoItems = append(todoItems, map[string]interface{}{
			"id":      t.ID,
			"content": t.Content,
			"status":  t.Status,
		})
	}
	te.agent.PublishTodoUpdate(todoItems)

	// In streaming mode, skip text output — the WebUI receives structured
	// todo_update events and does not need the inline text trace.
	if !te.agent.IsStreamingEnabled() {
		te.agent.PrintLine("")
		te.agent.PrintLine(fmt.Sprintf("%sTodos: %d total · %s%d pending · %s%d in progress · %s%d done · %s%d cancelled",
			console.GlyphInfo.Prefix(),
			len(after),
			console.GlyphDim.Prefix(), pending,
			console.GlyphAction.Prefix(), inProgress,
			console.GlyphSuccess.Prefix(), completed,
			console.GlyphStopped.Prefix(), cancelled))

		if len(changed) == 0 {
			te.agent.PrintLine("   No checklist changes detected.")
			te.agent.PrintLine("")
			return
		}

		maxLines := 8
		for i, line := range changed {
			if i >= maxLines {
				te.agent.PrintLine(fmt.Sprintf("   ... and %d more changes", len(changed)-maxLines))
				break
			}
			te.agent.PrintLine("   " + line)
		}
		te.agent.PrintLine("")
	}
}

// todoStatusSymbol returns the glyph prefix for a todo item status.
// Maps onto the shared CLI glyph vocabulary so checklist rows match
// other status lines:
//
//	pending     → · dim
//	in_progress → → action
//	completed   → ✓ success
//	cancelled   → ⏹ stopped
func todoStatusSymbol(status string) string {
	switch status {
	case "pending":
		return console.GlyphDim.Prefix()
	case "in_progress":
		return console.GlyphAction.Prefix()
	case "completed":
		return console.GlyphSuccess.Prefix()
	case "cancelled":
		return console.GlyphStopped.Prefix()
	default:
		return console.GlyphInfo.Prefix()
	}
}
