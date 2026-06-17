package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

// DiffLineType identifies whether a diff line is context, added, or removed.
type DiffLineType string

const (
	DiffLineContext DiffLineType = "context"
	DiffLineAdd     DiffLineType = "add"
	DiffLineRemove  DiffLineType = "remove"
)

// difflib OpCode tag values (go-difflib uses raw bytes).
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
func GenerateUnifiedDiff(path, original, proposed string) string {
	diff := difflib.UnifiedDiff{
		A:        splitLines(original),
		B:        splitLines(proposed),
		FromFile: path,
		ToFile:   path,
		Context:  3,
	}
	result, err := difflib.GetUnifiedDiffString(diff)
	if err != nil {
		return ""
	}
	return result
}

// RequestEditApproval builds a proposal, asks the approval broker for a
// decision, applies only accepted hunks, and returns the result.
//
// In this phase the broker interaction is a simple approve-all placeholder.
// The CLI and WebUI integration (SP-072-3, SP-072-4) will wire the real
// interactive delivery.
func (a *Agent) RequestEditApproval(ctx context.Context, p EditProposal) (applied string, summary string, err error) {
	// Ensure hunks are populated.
	if len(p.Hunks) == 0 {
		p.Hunks = SplitIntoHunks(p.Original, p.Proposed)
	}

	// No changes — return original as-is.
	if len(p.Hunks) == 0 {
		return p.Original, fmt.Sprintf("no changes to %s", p.Path), nil
	}

	// Placeholder broker: approve all hunks.
	// Real interactive approval is wired in SP-072-3 (CLI) and SP-072-4 (WebUI).
	decision := EditDecision{
		Approved:      true,
		AcceptedHunks: hunkIDs(p.Hunks),
	}

	// Apply only accepted hunks.
	applied = ApplyHunks(p.Original, p.Hunks, decision.AcceptedHunks)

	// Build summary for the tool result.
	acceptedCount := len(decision.AcceptedHunks)
	totalCount := len(p.Hunks)
	if acceptedCount == totalCount {
		summary = fmt.Sprintf("applied %d/%d hunks to %s", acceptedCount, totalCount, p.Path)
	} else {
		rejected := rejectedHunkList(p.Hunks, decision.AcceptedHunks)
		summary = fmt.Sprintf("applied %d/%d hunks to %s; rejected %s", acceptedCount, totalCount, p.Path, rejected)
	}

	return applied, summary, nil
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

// splitLines splits content into lines without a trailing empty string.
func splitLines(content string) []string {
	if content == "" {
		return []string{""}
	}
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
