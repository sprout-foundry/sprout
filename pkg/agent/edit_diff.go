package agent

// edit_diff.go — the pure diff/hunk engine (split from edit_approval.go):
// the DiffLineType/Hunk/DiffLine types, SplitIntoHunks, ApplyHunks,
// applySingleHunk, findSubslice, and GenerateUnifiedDiff. These are free
// functions (no *Agent receiver); the approval-flow methods and the
// approval broker stay in edit_approval.go.
import (
	"fmt"
	"strings"

	"github.com/pmezard/go-difflib/difflib"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
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
