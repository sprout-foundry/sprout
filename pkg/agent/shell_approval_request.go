package agent

// shell_approval_request.go — Agent wiring for shell approval, split out of
// shell_approval.go. RequestShellApproval dispatches a ShellProposal to the
// WebUI broker or the CLI per-part picker; requestShellApprovalViaWebUI
// publishes the event and blocks for the decision; kindRiskLabel renders the
// short risk-tier label.
import (
	"context"
	"log"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// ---------------------------------------------------------------------------
// Agent.RequestShellApproval
// ---------------------------------------------------------------------------

// RequestShellApproval asks the user (CLI or WebUI) to approve each part of
// the shell command individually. Returns a map from part ID to approved bool.
//
// Flow:
//  1. If no parts, returns empty map and nil error.
//  2. If the WebUI has an active surface, dispatch via the security
//     approval manager (the real WebUI per-part dialog is implemented
//     in requestShellApprovalViaWebUI).
//  3. Otherwise, call console.PromptShellApprovalParts (the CLI picker).
//
// Errors come from the picker (e.g. context cancelled); a per-part
// rejection does NOT return an error — it's encoded in the decisions map.
func (a *Agent) RequestShellApproval(ctx context.Context, p ShellProposal) (map[string]bool, error) {
	if len(p.Parts) == 0 {
		return map[string]bool{}, nil
	}

	// WebUI surface — publish a shell_approval_request event and block
	// until the user responds via the per-part dialog.
	// The WebUI IS the interactive surface — a webui-only service has no
	// TTY but still has live users who can answer per-part dialogs. The
	// isNonInteractive() check only applies to the CLI fallback below.
	isSubagent := a.IsSubagent()
	hasWebUI := !isSubagent && a.HasActiveWebUIClients()
	if hasWebUI {
		return a.requestShellApprovalViaWebUI(ctx, p)
	}

	// CLI surface — use the per-part picker.
	// Project ShellParts into console.ShellPartInfo (avoids cyclic import).
	parts := make([]console.ShellPartInfo, len(p.Parts))
	for i, part := range p.Parts {
		parts[i] = console.ShellPartInfo{
			ID:        part.ID,
			Text:      part.Text,
			Kind:      string(part.Kind),
			Semantic:  part.Semantic,
			RiskLabel: kindRiskLabel(part.Kind),
		}
	}
	return console.PromptShellApprovalParts(ctx, parts)
}

// shellApprovalTimeout is the maximum time a WebUI shell approval blocks
// waiting for the user. Shell approvals are less time-sensitive than
// password prompts, but keeping a bound prevents indefinite hangs if the
// browser disconnects mid-dialog.
var shellApprovalTimeout = 10 * time.Minute

// requestShellApprovalViaWebUI publishes a shell_approval_request event and
// blocks until the WebUI user responds (or the timeout fires). On timeout
// or cancellation, all parts are denied (safer than approving).
func (a *Agent) requestShellApprovalViaWebUI(ctx context.Context, p ShellProposal) (map[string]bool, error) {
	requestID := generateShellApprovalRequestID()
	ch := shellApprovalBroker.register(requestID)
	defer shellApprovalBroker.cleanup(requestID)

	// Build the per-part payload (events.ShellApprovalPartArg avoids an
	// import cycle — events can't import agent).
	parts := make([]events.ShellApprovalPartArg, len(p.Parts))
	for i, part := range p.Parts {
		parts[i] = events.ShellApprovalPartArg{
			ID:       part.ID,
			Text:     part.Text,
			Kind:     string(part.Kind),
			Semantic: part.Semantic,
			// Lowercase to match configuration.RiskLevel constants and
			// the frontend's defaultDecisionForRisk comparison.
			Risk: strings.ToLower(kindRiskLabel(part.Kind)),
		}
	}

	payload := events.ShellApprovalRequestEvent(
		requestID, p.Command, parts,
		events.BuildShellApprovalUnifiedView(parts),
		string(p.RiskLevel),
	)
	a.publishEvent(events.EventTypeShellApprovalRequest, payload)

	if a.debug {
		a.debugLog("[SHELL-PART] request %s — waiting up to %v for WebUI response (%d parts)\n",
			requestID, shellApprovalTimeout, len(p.Parts))
	}

	// denyAll is the safe fallback on timeout/cancellation — never approve
	// when we don't have an explicit user decision.
	denyAll := make(map[string]bool, len(p.Parts))
	for _, part := range p.Parts {
		denyAll[part.ID] = false
	}

	timer := time.NewTimer(shellApprovalTimeout)
	defer timer.Stop()

	select {
	case decisions, ok := <-ch:
		if !ok {
			log.Printf("[shell-approval] request %s — channel closed without response", requestID)
			return denyAll, nil
		}
		return decisions, nil
	case <-ctx.Done():
		log.Printf("[shell-approval] request %s — context cancelled (denying all parts)", requestID)
		return denyAll, nil
	case <-timer.C:
		log.Printf("[shell-approval] request %s — timed out after %v (denying all parts)", requestID, shellApprovalTimeout)
		return denyAll, nil
	}
}

// kindRiskLabel returns a short risk-tier label for CLI display.
func kindRiskLabel(kind CommandKind) string {
	switch kind {
	case CommandKindRm, CommandKindGitReset, CommandKindKubectl:
		return "CRITICAL"
	case CommandKindDocker, CommandKindGitPush:
		return "HIGH"
	case CommandKindChmod, CommandKindChown,
		CommandKindWriteRedirect, CommandKindHttpPost:
		return "MEDIUM"
	default:
		return "LOW"
	}
}
