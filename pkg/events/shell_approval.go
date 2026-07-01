// Package events — shell approval event types and helpers.
package events

import (
	"fmt"
	"time"
)

// ShellApprovalPart represents a single logical segment of a shell
// command in the approval payload. Declared here (not in agent) to
// avoid an import cycle — agent imports events, so events cannot
// import agent.
type ShellApprovalPart struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Kind     string `json:"kind"`
	Semantic string `json:"semantic"`
	Risk     string `json:"risk"`
}

// ShellApprovalRequestPayload is the JSON shape of a shell_approval_request
// event. The frontend consumes this to render the per-part approval panel.
type ShellApprovalRequestPayload struct {
	RequestID   string              `json:"request_id"`
	Command     string              `json:"command"`
	Parts       []ShellApprovalPart `json:"parts"`
	UnifiedView string              `json:"unified_view"`
	RiskLevel   string              `json:"risk_level"`
	Timestamp   string              `json:"timestamp"`
}

// ShellApprovalResponsePayload is the JSON shape the WebUI POSTs back
// to /api/shell-approvals/{id}/decision. It maps each part ID to an
// approved boolean.
type ShellApprovalResponsePayload struct {
	RequestID string          `json:"request_id"`
	Decisions map[string]bool `json:"decisions"`
}

// ShellApprovalPartArg is a plain-struct mirror of agent.ShellPart so
// ShellApprovalRequestEvent can accept agent-side data without importing
// the agent package (which would create a cycle).
//
// Callers in pkg/agent construct the parts slice from ShellProposal.Parts:
//
//	parts := make([]events.ShellApprovalPartArg, len(proposal.Parts))
//	for i, p := range proposal.Parts {
//	    parts[i] = events.ShellApprovalPartArg{
//	        ID: p.ID, Text: p.Text,
//	        Kind: string(p.Kind), Semantic: p.Semantic,
//	        Risk: string(proposal.RiskLevel),
//	    }
//	}
type ShellApprovalPartArg struct {
	ID       string
	Text     string
	Kind     string
	Semantic string
	Risk     string
}

// ShellApprovalRequestEvent creates a shell_approval_request event payload.
// It mirrors the EditApprovalRequestEvent pattern: takes the agent-side
// data (passed as plain structs to avoid import cycles) and returns a
// flat map[string]interface{} for EventBus.Publish().
func ShellApprovalRequestEvent(requestID, command string, parts []ShellApprovalPartArg, unifiedView string, risk string) map[string]interface{} {
	partsPayload := make([]ShellApprovalPart, len(parts))
	for i, p := range parts {
		partsPayload[i] = ShellApprovalPart{
			ID:       p.ID,
			Text:     p.Text,
			Kind:     p.Kind,
			Semantic: p.Semantic,
			Risk:     p.Risk,
		}
	}
	return map[string]interface{}{
		"request_id":   requestID,
		"command":      command,
		"parts":        partsPayload,
		"unified_view": unifiedView,
		"risk_level":   risk,
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}
}

// BuildShellApprovalUnifiedView creates a human-readable unified view
// string from the shell parts, suitable for display in the approval panel.
func BuildShellApprovalUnifiedView(parts []ShellApprovalPartArg) string {
	if len(parts) == 0 {
		return ""
	}
	lines := make([]string, 0, len(parts))
	for _, p := range parts {
		label := fmt.Sprintf("[%s] %s", p.Kind, p.Text)
		if p.Semantic != "" {
			label += fmt.Sprintf("  # %s", p.Semantic)
		}
		lines = append(lines, label)
	}
	return joinLines(lines)
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	result := lines[0]
	for i := 1; i < len(lines); i++ {
		result += "\n" + lines[i]
	}
	return result
}
