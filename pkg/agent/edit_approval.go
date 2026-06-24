package agent

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/pmezard/go-difflib/difflib"
	"github.com/sprout-foundry/sprout/pkg/events"
	"golang.org/x/term"
)

// DiffLineType identifies whether a diff line is context, added, or removed.
type DiffLineType string

const (
	DiffLineContext DiffLineType = "context"
	DiffLineAdd     DiffLineType = "add"
	DiffLineRemove  DiffLineType = "remove"
)

// go-difflib's OpCode.Tag is a raw byte ('e'/'r'/'d'/'i').
// The library doesn't export named constants, so we mirror
// the values here. See github.com/pmezard/go-difflib/difflib/match.go
const (
	opEqual   byte = 'e' // equal/match
	opReplace byte = 'r' // replace
	opDelete  byte = 'd' // delete
	opInsert  byte = 'i' // insert
)

// DiffLine represents a single line in a unified diff hunk.
type DiffLine struct {
	Type    DiffLineType
	Content string
}

// Hunk represents a discrete change region in a unified diff.
type Hunk struct {
	ID       string
	OldStart int // 1-based
	OldLines int
	NewStart int // 1-based
	NewLines int
	Lines    []DiffLine
}

// EditProposal describes a proposed file edit awaiting approval.
type EditProposal struct {
	Path     string
	Original string
	Proposed string
	Hunks    []Hunk
}

// EditDecision captures the user's per-hunk accept/reject choices.
type EditDecision struct {
	Approved      bool
	AcceptedHunks []string // hunk IDs; empty + Approved=false => reject all
}

// SplitIntoHunks computes the unified diff between original and proposed
// content and splits it into discrete hunks with stable IDs ("hunk-0",
// "hunk-1", …). Each hunk includes up to 3 lines of surrounding context.
func SplitIntoHunks(original, proposed string) []Hunk {
	origLines := splitLines(original)
	newLines := splitLines(proposed)

	// GetGroupedOpCodes(n) returns groups of opcodes, each group being
	// a hunk with up to n lines of context around changes.
	groups := difflib.NewMatcher(origLines, newLines).GetGroupedOpCodes(3)

	var hunks []Hunk
	for hunkIdx, group := range groups {
		hunk := Hunk{
			ID: fmt.Sprintf("hunk-%d", hunkIdx),
		}

		// Set start positions from the first opcode (0-based → convert to 1-based later).
		if len(group) > 0 {
			hunk.OldStart = group[0].I1
			hunk.NewStart = group[0].J1
		}

		for _, op := range group {
			switch op.Tag {
			case opEqual:
				for _, line := range origLines[op.I1:op.I2] {
					hunk.Lines = append(hunk.Lines, DiffLine{Type: DiffLineContext, Content: line})
				}
				hunk.OldLines += op.I2 - op.I1
				hunk.NewLines += op.J2 - op.J1
			case opInsert:
				for _, line := range newLines[op.J1:op.J2] {
					hunk.Lines = append(hunk.Lines, DiffLine{Type: DiffLineAdd, Content: line})
				}
				hunk.NewLines += op.J2 - op.J1
			case opDelete:
				for _, line := range origLines[op.I1:op.I2] {
					hunk.Lines = append(hunk.Lines, DiffLine{Type: DiffLineRemove, Content: line})
				}
				hunk.OldLines += op.I2 - op.I1
			case opReplace:
				for _, line := range origLines[op.I1:op.I2] {
					hunk.Lines = append(hunk.Lines, DiffLine{Type: DiffLineRemove, Content: line})
				}
				for _, line := range newLines[op.J1:op.J2] {
					hunk.Lines = append(hunk.Lines, DiffLine{Type: DiffLineAdd, Content: line})
				}
				hunk.OldLines += op.I2 - op.I1
				hunk.NewLines += op.J2 - op.J1
			}
		}

		// Convert to 1-based line numbers for display.
		hunk.OldStart++
		hunk.NewStart++

		hunks = append(hunks, hunk)
	}

	return hunks
}

// ApplyHunks reconstructs file content by applying only the accepted hunks.
// Rejected hunks leave the original lines unchanged. Hunks are applied in
// order; each hunk locates its context in the current result and patches it.
func ApplyHunks(original string, hunks []Hunk, acceptedIDs []string) string {
	accepted := make(map[string]bool, len(acceptedIDs))
	for _, id := range acceptedIDs {
		accepted[id] = true
	}

	result := splitLines(original)

	for _, hunk := range hunks {
		if !accepted[hunk.ID] {
			continue
		}
		result = applySingleHunk(result, hunk)
	}

	return strings.Join(result, "\n")
}

// applySingleHunk finds the hunk's old-content region within lines and
// replaces it with the new content. If the region cannot be located (the
// surrounding context changed due to an earlier hunk shift), the lines are
// returned unchanged (safety: never clobber).
func applySingleHunk(lines []string, hunk Hunk) []string {
	var oldContent, newContent []string
	for _, dl := range hunk.Lines {
		switch dl.Type {
		case DiffLineContext:
			oldContent = append(oldContent, dl.Content)
			newContent = append(newContent, dl.Content)
		case DiffLineRemove:
			oldContent = append(oldContent, dl.Content)
		case DiffLineAdd:
			newContent = append(newContent, dl.Content)
		}
	}

	startIdx := findSubslice(lines, oldContent, hunk.OldStart-1)
	if startIdx < 0 {
		return lines
	}

	out := make([]string, 0, len(lines)-len(oldContent)+len(newContent))
	out = append(out, lines[:startIdx]...)
	out = append(out, newContent...)
	out = append(out, lines[startIdx+len(oldContent):]...)
	return out
}

// findSubslice finds the index of oldContent within lines, starting near
// startIdx (0-based). Returns -1 if not found.
func findSubslice(lines, oldContent []string, startIdx int) int {
	if len(oldContent) == 0 {
		// Clamp to valid insertion range to prevent index-out-of-bounds.
		if startIdx < 0 {
			return 0
		}
		if startIdx > len(lines) {
			return len(lines)
		}
		return startIdx
	}

	// Try positions near startIdx first (±5 lines).
	for _, offset := range []int{0, 1, -1, 2, -2, 3, -3, 4, -4, 5, -5} {
		pos := startIdx + offset
		if pos < 0 || pos+len(oldContent) > len(lines) {
			continue
		}
		match := true
		for i, s := range oldContent {
			if lines[pos+i] != s {
				match = false
				break
			}
		}
		if match {
			return pos
		}
	}

	// Full scan fallback.
	for i := 0; i <= len(lines)-len(oldContent); i++ {
		match := true
		for j, s := range oldContent {
			if lines[i+j] != s {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}

	return -1
}

// GenerateUnifiedDiff produces a standard unified-diff string from original
// and proposed content, suitable for terminal display.
func GenerateUnifiedDiff(path, original, proposed string) (string, error) {
	diff := difflib.UnifiedDiff{
		A:        splitLines(original),
		B:        splitLines(proposed),
		FromFile: path,
		ToFile:   path,
		Context:  3,
	}
	result, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		return "", fmt.Errorf("generate diff for %s: %w", path, err)
	}
	return result, nil
}

// editApprovalTimeout is the maximum time a request blocks waiting for a
// WebUI response. Matches the security approval default generous window
// so a user who steps away can still return and approve.
var editApprovalTimeout = 30 * time.Minute

// editApprovalBroker tracks pending edit approval requests and their
// response channels. It mirrors the pattern used by
// security.ApprovalManager: the agent registers a request, publishes an
// event to the EventBus, then blocks on the channel until the WebUI
// POSTs a decision (or the timeout fires).
//
// Package-level so that any agent instance can resolve any request ID —
// essential in daemon mode where multiple chat agents exist and the
// decision handler only has access to an arbitrary agent instance.
var editApprovalBroker = &editApprovalBrokerType{
	pending: make(map[string]chan EditDecision),
}

type editApprovalBrokerType struct {
	mu      sync.Mutex
	pending map[string]chan EditDecision
}

// register creates a buffered response channel for the given request ID
// and returns it. The caller publishes the event, then blocks on the
// channel. cleanup removes the entry after the block resolves.
func (b *editApprovalBrokerType) register(requestID string) chan EditDecision {
	ch := make(chan EditDecision, 1)
	b.mu.Lock()
	b.pending[requestID] = ch
	b.mu.Unlock()
	return ch
}

// respond delivers the decision to the waiting goroutine. Returns false
// if the request was not found or already resolved.
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

// cleanup removes a pending entry after it resolves or times out.
func (b *editApprovalBrokerType) cleanup(requestID string) {
	b.mu.Lock()
	delete(b.pending, requestID)
	b.mu.Unlock()
}

// generateEditRequestID produces a unique ID for an edit approval request.
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
//
// Resolution path depends on the execution surface:
//
//  1. Non-interactive (--skip-prompt, automate, daemon, non-TTY stdin):
//     auto-approve all hunks. No one can answer a prompt, so blocking
//     would dead-end the run.
//
//  2. WebUI with active clients: publish an edit_approval_request event
//     to the EventBus and block on a response channel. The browser
//     renders a per-hunk diff review panel and POSTs the decision back.
//     On timeout, fall through to the terminal path.
//
//  3. CLI (TTY only, no WebUI): render the diff to stderr and prompt the
//     user per-hunk via stdin. Accepted hunks are applied; rejected
//     hunks keep the original lines.
func (a *Agent) RequestEditApproval(ctx context.Context, p EditProposal) (applied string, summary string, err error) {
	// Check for context cancellation up front.
	select {
	case <-ctx.Done():
		return "", "", ctx.Err()
	default:
	}

	// Ensure hunks are populated.
	if len(p.Hunks) == 0 {
		p.Hunks = SplitIntoHunks(p.Original, p.Proposed)
	}

	// No changes — return original as-is.
	if len(p.Hunks) == 0 {
		return p.Original, fmt.Sprintf("no changes to %s", p.Path), nil
	}

	// Non-interactive runs (--skip-prompt, automate, daemon) treat the
	// mode as approve-all (no silent hangs).
	if a.isNonInteractive() {
		return a.applyEditDecision(p, EditDecision{
			Approved:      true,
			AcceptedHunks: hunkIDs(p.Hunks),
		})
	}

	// Try the WebUI path: if the event bus is wired and there are active
	// browser clients, route the proposal through the diff review panel.
	if a.HasActiveWebUIClients() && a.GetEventBus() != nil {
		decision, outcome := a.requestWebUIEditApproval(ctx, p)
		if outcome == approvalOutcomeResponded {
			return a.applyEditDecision(p, decision)
		}
		// Timed out or no channel — fall through to terminal prompt if
		// we have a TTY. If non-interactive, auto-approve as a safe
		// fallback (the user can undo via recover_file / git).
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			log.Printf("[edit_approval] WebUI timed out and no TTY — auto-approving %s", p.Path)
			return a.applyEditDecision(p, EditDecision{
				Approved:      true,
				AcceptedHunks: hunkIDs(p.Hunks),
			})
		}
	}

	// CLI terminal path: render the diff and prompt per-hunk.
	if term.IsTerminal(int(os.Stdin.Fd())) {
		decision := a.requestCLIEditApproval(p)
		return a.applyEditDecision(p, decision)
	}

	// No interactive surface at all — auto-approve.
	return a.applyEditDecision(p, EditDecision{
		Approved:      true,
		AcceptedHunks: hunkIDs(p.Hunks),
	})
}

// approvalOutcome mirrors security.ApprovalOutcome without the import
// dependency. The WebUI path returns responded/timedOut/noChannel so
// RequestEditApproval can decide whether to fall through to the CLI.
type approvalOutcome int

const (
	approvalOutcomeResponded approvalOutcome = iota
	approvalOutcomeTimedOut
	approvalOutcomeNoChannel
)

// requestWebUIEditApproval publishes an edit_approval_request event to
// the EventBus and blocks until the WebUI responds or the timeout fires.
func (a *Agent) requestWebUIEditApproval(ctx context.Context, p EditProposal) (EditDecision, approvalOutcome) {
	requestID := generateEditRequestID()
	ch := editApprovalBroker.register(requestID)
	defer editApprovalBroker.cleanup(requestID)

	// Build the event payload.
	unifiedDiff, _ := GenerateUnifiedDiff(p.Path, p.Original, p.Proposed)
	hunkPayloads := make([]map[string]interface{}, len(p.Hunks))
	for i, h := range p.Hunks {
		hunkPayloads[i] = hunkToPayload(h)
	}

	payload := events.EditApprovalRequestEvent(requestID, p.Path, unifiedDiff, hunkPayloads)
	a.publishEvent(events.EventTypeEditApprovalRequest, payload)
	// Notify input-required subscribers (CLI bell, browser notification).
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

// requestCLIEditApproval renders the diff to stderr and prompts the user
// to accept or reject each hunk via stdin. Each hunk is shown with its
// line range and a y/n prompt. Defaults to accept (y) on empty input.
func (a *Agent) requestCLIEditApproval(p EditProposal) EditDecision {
	unifiedDiff, _ := GenerateUnifiedDiff(p.Path, p.Original, p.Proposed)

	fmt.Fprintf(os.Stderr, "\n%sEdit approval required for %s%s\n", "\x1b[1m", p.Path, "\x1b[0m")
	fmt.Fprintf(os.Stderr, "%s\n", unifiedDiff)
	fmt.Fprintf(os.Stderr, "\n%sReview each hunk:%s\n", "\x1b[1m", "\x1b[0m")

	accepted := make([]string, 0, len(p.Hunks))
	for _, hunk := range p.Hunks {
		fmt.Fprintf(os.Stderr, "  %s (lines %d-%d, +%d/-%d) [Y/n]: ",
			hunk.ID, hunk.OldStart, hunk.OldStart+hunk.OldLines-1,
			countLinesByType(hunk.Lines, DiffLineAdd), countLinesByType(hunk.Lines, DiffLineRemove))

		var answer string
		fmt.Fscanln(os.Stdin, &answer)

		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer == "" || answer == "y" || answer == "yes" {
			accepted = append(accepted, hunk.ID)
		}
	}

	return EditDecision{
		Approved:      len(accepted) > 0,
		AcceptedHunks: accepted,
	}
}

// RespondToEditApproval delivers a user decision to a pending edit
// approval request. Called by the WebUI handler (POST /api/edits/{id}/decision)
// when the user submits their per-hunk accept/reject choices.
//
// Returns true if the request was found and the decision was delivered.
func (a *Agent) RespondToEditApproval(requestID string, decision EditDecision) bool {
	return editApprovalBroker.respond(requestID, decision)
}

// applyEditDecision applies the accepted hunks to the original content
// and builds a human-readable summary for the tool result.
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

// hunkToPayload converts a Hunk to a JSON-serializable map for the
// event payload, including per-line change type for the frontend.
func hunkToPayload(h Hunk) map[string]interface{} {
	lines := make([]map[string]interface{}, len(h.Lines))
	for i, dl := range h.Lines {
		lines[i] = map[string]interface{}{
			"type":    string(dl.Type),
			"content": dl.Content,
		}
	}
	return map[string]interface{}{
		"id":         h.ID,
		"old_start":  h.OldStart,
		"old_lines":  h.OldLines,
		"new_start":  h.NewStart,
		"new_lines":  h.NewLines,
		"lines":      lines,
		"add_count":  countLinesByType(h.Lines, DiffLineAdd),
		"del_count":  countLinesByType(h.Lines, DiffLineRemove),
	}
}

// countLinesByType counts how many lines in a slice have the given type.
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
// Intended for tests; production code uses the 30-minute default.
func SetEditApprovalTimeout(d time.Duration) {
	editApprovalTimeout = d
}

// ShouldGateEdit reports whether a write to the given path should be
// routed through the diff-approval gate based on the agent's config.
// Returns false when edit_approval mode is "off" (default), when the
// run is non-interactive (--skip-prompt / daemon / automate), or when
// mode is "paths" and the path doesn't match any configured glob.
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
// interactive prompts are suppressed or impossible. This is the single
// authoritative check used by the security system to decide between the
// interactive gating path (full profiles, prompts, long approval timeout)
// and the non-interactive path (permissive-by-default, Critical-only
// blocks, fast-fail).
//
// True when ANY of:
//   - stdin is not a TTY (daemon, CI, piped input)
//   - cfg.SkipPrompt is set (--skip-prompt, automate, daemon flag)
//
// Both conditions must lead to the same behavior because either one means
// there is no live user at a terminal to answer an approval prompt.
func (a *Agent) isNonInteractive() bool {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return true
	}
	if cfg := a.GetConfig(); cfg != nil && cfg.SkipPrompt {
		return true
	}
	return false
}

// hunkIDs returns the IDs of all hunks.
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

// splitLines splits content into lines, preserving trailing empty elements
// so that strings.Join(result, "\n") preserves trailing newlines.
func splitLines(content string) []string {
	if content == "" {
		return []string{""}
	}
	return strings.Split(content, "\n")
}
