package agent

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"

	"github.com/sprout-foundry/sprout/pkg/clihooks"
	"github.com/sprout-foundry/sprout/pkg/events"
)

var editApprovalTimeout = 30 * time.Minute

// editApprovalBroker tracks pending edit approval requests and their response channels.
// Package-level so any agent instance can resolve any request ID.
var editApprovalBroker = &editApprovalBrokerType{
	pending: make(map[string]chan EditDecision),
}

type editApprovalBrokerType struct {
	mu      sync.Mutex
	pending map[string]chan EditDecision
}

func (b *editApprovalBrokerType) register(requestID string) chan EditDecision {
	ch := make(chan EditDecision, 1)
	b.mu.Lock()
	b.pending[requestID] = ch
	b.mu.Unlock()
	return ch
}

func (b *editApprovalBrokerType) respond(requestID string, decision EditDecision) bool {
	b.mu.Lock()
	ch, ok := b.pending[requestID]
	b.mu.Unlock()
	if !ok {
		return false
	}
	select {
	case ch <- decision:
		return true
	default:
		return false
	}
}

func (b *editApprovalBrokerType) cleanup(requestID string) {
	b.mu.Lock()
	delete(b.pending, requestID)
	b.mu.Unlock()
}

var (
	editReqCounter int64
	editReqMu      sync.Mutex
)

func generateEditRequestID() string {
	editReqMu.Lock()
	defer editReqMu.Unlock()
	editReqCounter++
	return fmt.Sprintf("edit_%d", editReqCounter)
}

// RequestEditApproval builds a proposal, asks the approval broker for a
// decision, applies only accepted hunks, and returns the result.
func (a *Agent) RequestEditApproval(ctx context.Context, p EditProposal) (applied string, summary string, err error) {
	select {
	case <-ctx.Done():
		return "", "", ctx.Err()
	default:
	}

	if len(p.Hunks) == 0 {
		p.Hunks = SplitIntoHunks(p.Original, p.Proposed)
	}

	if len(p.Hunks) == 0 {
		return p.Original, fmt.Sprintf("no changes to %s", p.Path), nil
	}

	// WebUI path: if the event bus is wired and there are active browser clients.
	if a.HasActiveWebUIClients() && a.GetEventBus() != nil {
		decision, outcome := a.requestWebUIEditApproval(ctx, p)
		if outcome == approvalOutcomeResponded {
			return a.applyEditDecision(p, decision)
		}
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			log.Printf("[edit_approval] WebUI timed out and no TTY — auto-approving %s", p.Path)
			return a.applyEditDecision(p, EditDecision{
				Approved:      true,
				AcceptedHunks: hunkIDs(p.Hunks),
			})
		}
	}

	if a.isNonInteractive() {
		return a.applyEditDecision(p, EditDecision{
			Approved:      true,
			AcceptedHunks: hunkIDs(p.Hunks),
		})
	}

	if term.IsTerminal(int(os.Stdin.Fd())) {
		decision := a.requestCLIEditApproval(p)
		return a.applyEditDecision(p, decision)
	}

	return a.applyEditDecision(p, EditDecision{
		Approved:      true,
		AcceptedHunks: hunkIDs(p.Hunks),
	})
}

type approvalOutcome int

const (
	approvalOutcomeResponded approvalOutcome = iota
	approvalOutcomeTimedOut
	approvalOutcomeNoChannel
)

// requestWebUIEditApproval publishes an edit_approval_request event and blocks for a response.
func (a *Agent) requestWebUIEditApproval(ctx context.Context, p EditProposal) (EditDecision, approvalOutcome) {
	requestID := generateEditRequestID()
	ch := editApprovalBroker.register(requestID)
	defer editApprovalBroker.cleanup(requestID)

	unifiedDiff, _ := GenerateUnifiedDiff(p.Path, p.Original, p.Proposed)
	hunkPayloads := make([]map[string]interface{}, len(p.Hunks))
	for i, h := range p.Hunks {
		hunkPayloads[i] = hunkToPayload(h)
	}

	payload := events.EditApprovalRequestEvent(requestID, p.Path, unifiedDiff, hunkPayloads)
	a.publishEvent(events.EventTypeEditApprovalRequest, payload)
	a.publishEvent(events.EventTypeInputRequired, events.InputRequiredEvent("edit_approval", requestID))

	log.Printf("[edit_approval] request %s for %s — waiting up to %v for WebUI response",
		requestID, p.Path, editApprovalTimeout)

	timer := time.NewTimer(editApprovalTimeout)
	defer timer.Stop()

	select {
	case decision, ok := <-ch:
		if !ok {
			return EditDecision{}, approvalOutcomeNoChannel
		}
		return decision, approvalOutcomeResponded
	case <-ctx.Done():
		return EditDecision{}, approvalOutcomeNoChannel
	case <-timer.C:
		log.Printf("[edit_approval] request %s timed out after %v", requestID, editApprovalTimeout)
		return EditDecision{}, approvalOutcomeTimedOut
	}
}

// requestCLIEditApproval renders the diff to stderr and prompts the user per-hunk.
func (a *Agent) requestCLIEditApproval(p EditProposal) EditDecision {
	unifiedDiff, _ := GenerateUnifiedDiff(p.Path, p.Original, p.Proposed)

	var accepted []string
	clihooks.SuspendStreaming()
	defer clihooks.ResumeStreaming()
	err := clihooks.WithCookedStdin(func() error {
		fmt.Fprintf(os.Stderr, "\n%sEdit approval required for %s%s\n", "\x1b[1m", p.Path, "\x1b[0m")
		fmt.Fprintf(os.Stderr, "%s\n", unifiedDiff)
		fmt.Fprintf(os.Stderr, "\n%sReview each hunk:%s\n", "\x1b[1m", "\x1b[0m")

		scanner := bufio.NewScanner(os.Stdin)
		accepted = make([]string, 0, len(p.Hunks))
		for _, hunk := range p.Hunks {
			fmt.Fprintf(os.Stderr, "  %s (lines %d-%d, +%d/-%d) [Y/n]: ",
				hunk.ID, hunk.OldStart, hunk.OldStart+hunk.OldLines-1,
				countLinesByType(hunk.Lines, DiffLineAdd), countLinesByType(hunk.Lines, DiffLineRemove))

			var answer string
			if scanner.Scan() {
				answer = scanner.Text()
			} else if err := scanner.Err(); err != nil {
				return err
			}

			answer = strings.ToLower(strings.TrimSpace(answer))
			if answer == "" || answer == "y" || answer == "yes" {
				accepted = append(accepted, hunk.ID)
			}
		}
		return nil
	})

	if err != nil {
		return EditDecision{Approved: false, AcceptedHunks: nil}
	}

	return EditDecision{
		Approved:      len(accepted) > 0,
		AcceptedHunks: accepted,
	}
}

// RespondToEditApproval delivers a user decision to a pending edit approval request.
func (a *Agent) RespondToEditApproval(requestID string, decision EditDecision) bool {
	return editApprovalBroker.respond(requestID, decision)
}

// DeliverEditDecision delivers a user decision to a pending edit approval
// request without requiring an Agent instance. This is used by the WASM JS
// bridge so the webui can resolve edit approval requests in cloud mode.
func DeliverEditDecision(requestID string, decision EditDecision) bool {
	return editApprovalBroker.respond(requestID, decision)
}

// applyEditDecision applies the accepted hunks to the original content.
func (a *Agent) applyEditDecision(p EditProposal, decision EditDecision) (string, string, error) {
	applied := ApplyHunks(p.Original, p.Hunks, decision.AcceptedHunks)

	acceptedCount := len(decision.AcceptedHunks)
	totalCount := len(p.Hunks)
	if !decision.Approved && acceptedCount == 0 {
		summary := fmt.Sprintf("edit rejected — no hunks applied to %s", p.Path)
		return p.Original, summary, nil
	}
	if acceptedCount == totalCount {
		summary := fmt.Sprintf("applied %d/%d hunks to %s", acceptedCount, totalCount, p.Path)
		return applied, summary, nil
	}
	rejected := rejectedHunkList(p.Hunks, decision.AcceptedHunks)
	summary := fmt.Sprintf("applied %d/%d hunks to %s; rejected %s", acceptedCount, totalCount, p.Path, rejected)
	return applied, summary, nil
}

// hunkToPayload converts a Hunk to a JSON-serializable map for the event payload.
func hunkToPayload(h Hunk) map[string]interface{} {
	lines := make([]map[string]interface{}, len(h.Lines))
	for i, dl := range h.Lines {
		lines[i] = map[string]interface{}{
			"type":    string(dl.Type),
			"content": dl.Content,
		}
	}
	return map[string]interface{}{
		"id":        h.ID,
		"old_start": h.OldStart,
		"old_lines": h.OldLines,
		"new_start": h.NewStart,
		"new_lines": h.NewLines,
		"lines":     lines,
		"add_count": countLinesByType(h.Lines, DiffLineAdd),
		"del_count": countLinesByType(h.Lines, DiffLineRemove),
	}
}

func countLinesByType(lines []DiffLine, t DiffLineType) int {
	n := 0
	for _, dl := range lines {
		if dl.Type == t {
			n++
		}
	}
	return n
}

// SetEditApprovalTimeout overrides the default WebUI response timeout.
func SetEditApprovalTimeout(d time.Duration) {
	editApprovalTimeout = d
}

// ShouldGateEdit reports whether a write to the given path should be
// routed through the diff-approval gate based on the agent's config.
func (a *Agent) ShouldGateEdit(path string) bool {
	cfg := a.GetConfig()
	if cfg == nil || cfg.EditApproval == nil {
		return false
	}
	if a.isNonInteractive() {
		return false
	}
	return cfg.EditApproval.ShouldGate(path)
}

// isNonInteractive reports whether the agent is running in a mode where
// interactive prompts are suppressed or impossible.
func (a *Agent) isNonInteractive() bool {
	if strings.TrimSpace(os.Getenv("SPROUT_FORCE_INTERACTIVE")) == "1" {
		return false
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return true
	}
	if cfg := a.GetConfig(); cfg != nil && cfg.SkipPrompt {
		return true
	}
	return false
}

func hunkIDs(hunks []Hunk) []string {
	ids := make([]string, len(hunks))
	for i, h := range hunks {
		ids[i] = h.ID
	}
	return ids
}

// rejectedHunkList produces a human-readable description of rejected hunks.
func rejectedHunkList(hunks []Hunk, acceptedIDs []string) string {
	accepted := make(map[string]bool, len(acceptedIDs))
	for _, id := range acceptedIDs {
		accepted[id] = true
	}

	var rejected []string
	for _, h := range hunks {
		if !accepted[h.ID] {
			rejected = append(rejected, fmt.Sprintf("%s (lines %d-%d)", h.ID, h.OldStart, h.OldStart+h.OldLines-1))
		}
	}
	if len(rejected) == 0 {
		return "none"
	}
	return strings.Join(rejected, ", ")
}

// splitLines splits content into lines, preserving trailing empty elements.
func splitLines(content string) []string {
	if content == "" {
		return []string{""}
	}
	return strings.Split(content, "\n")
}
