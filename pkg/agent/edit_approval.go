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

	"github.com/pmezard/go-difflib/difflib"
	"github.com/sprout-foundry/sprout/pkg/clihooks"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
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
const (
	opEqual   byte = 'e'
	opReplace byte = 'r'
	opDelete  byte = 'd'
	opInsert  byte = 'i'
)

// DiffLine represents a single line in a unified diff hunk.
type DiffLine struct {
	Type    DiffLineType
	Content string
}

// Hunk represents a discrete change region in a unified diff.
type Hunk struct {
	ID       string
	OldStart int
	OldLines int
	NewStart int
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
	AcceptedHunks []string
}

// SplitIntoHunks computes the unified diff and splits it into discrete hunks with stable IDs.
func SplitIntoHunks(original, proposed string) []Hunk {
	origLines := splitLines(original)
	newLines := splitLines(proposed)

	groups := difflib.NewMatcher(origLines, newLines).GetGroupedOpCodes(3)

	var hunks []Hunk
	for hunkIdx, group := range groups {
		hunk := Hunk{
			ID: fmt.Sprintf("hunk-%d", hunkIdx),
		}

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

		hunk.OldStart++
		hunk.NewStart++

		hunks = append(hunks, hunk)
	}

	return hunks
}

// ApplyHunks reconstructs file content by applying only the accepted hunks.
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

// applySingleHunk finds the hunk's old-content region and replaces it with the new content.
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

// findSubslice finds the index of oldContent within lines, starting near startIdx.
func findSubslice(lines, oldContent []string, startIdx int) int {
	if len(oldContent) == 0 {
		if startIdx < 0 {
			return 0
		}
		if startIdx > len(lines) {
			return len(lines)
		}
		return startIdx
	}

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

// GenerateUnifiedDiff produces a standard unified-diff string from original and proposed content.
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
		return "", agenterrors.Wrap(err, fmt.Sprintf("generate diff for %s", path))
	}
	return result, nil
}

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
