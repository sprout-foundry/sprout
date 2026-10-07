package timeline

// timeline_summary.go — deterministic template summaries for timeline
// entries. Each template is a pure function of the entry's fields: no model
// call, no invented detail. This mirrors the deterministic template
// summaries in pkg/cliui and pkg/events: the optional summarizer-role
// summary layers on top of these and falls back to them.

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/deploy"
	"github.com/sprout-foundry/sprout/pkg/history"
)

// RevisionSummary is the parts a change set's template summary is built
// from: the sorted distinct files the revision touched and the summed
// insertions/deletions across them.
type RevisionSummary struct {
	// Text is the rendered one-line summary.
	Text string
	// Files is the sorted list of distinct files touched.
	Files []string
	// FilesTouched is len(Files).
	FilesTouched int
	// Insertions and Deletions are the summed line counts.
	Insertions int
	Deletions  int
}

// SummarizeRevision renders a history revision as a short deterministic
// template summary of its diff.
//
// The summary names the changed files (up to maxSummaryFiles of them,
// then "+N more") and the insertions/deletions, e.g.
//
//	changed 2 files: a.go, b.go (+12/-3)
//
// A revision with no file changes, or one whose files carry no textual
// change, renders a fixed "no file changes" line rather than an empty
// string — a timeline entry is never silent about a recorded revision.
func SummarizeRevision(group *history.RevisionGroup) RevisionSummary {
	if group == nil {
		return RevisionSummary{Text: noChangesSummary}
	}

	files, insertions, deletions := revisionDiffStats(group)
	if len(files) == 0 {
		return RevisionSummary{Text: noChangesSummary}
	}

	return RevisionSummary{
		Text:         formatChangeSummary(files, insertions, deletions),
		Files:        files,
		FilesTouched: len(files),
		Insertions:   insertions,
		Deletions:    deletions,
	}
}

const (
	noChangesSummary = "no file changes"
	// activeStatus mirrors pkg/history's active change status. A change
	// set counts only active changes; a reverted or restored change is no
	// longer in effect.
	activeStatus = "active"
	// maxSummaryFiles is how many file names a summary names before
	// collapsing the remainder into "+N more".
	maxSummaryFiles = 3
)

// revisionDiffStats collects the revision's distinct files (sorted) and the
// total insertions/deletions across its active changes. Content is available
// because revision groups are read via GetRevisionGroups, which reads the
// stored payloads.
//
// Only changes with active status contribute: a rolled-back or superseded
// change (Status "reverted"/"restored") is no longer in effect, so counting
// it would overstate the revision's diff — the rest of pkg/history filters
// the same way (getActiveChanges).
func revisionDiffStats(group *history.RevisionGroup) ([]string, int, int) {
	fileSet := make(map[string]struct{})
	var insertions, deletions int

	for i := range group.Changes {
		change := &group.Changes[i]
		if change.Filename == "" || change.Status != activeStatus {
			continue
		}
		fileSet[change.Filename] = struct{}{}
		ins, del := countDiffLines(change.OriginalCode, change.NewCode)
		insertions += ins
		deletions += del
	}

	files := make([]string, 0, len(fileSet))
	for name := range fileSet {
		files = append(files, name)
	}
	sort.Strings(files)
	return files, insertions, deletions
}

// countDiffLines counts added and removed lines between two payloads using
// a line-oriented diff. Identical payloads count as zero.
func countDiffLines(before, after string) (insertions, deletions int) {
	if before == after {
		return 0, 0
	}
	d := diffLines(before, after)
	for _, op := range d {
		switch op.kind {
		case opInsert:
			insertions += op.count
		case opDelete:
			deletions += op.count
		}
	}
	return insertions, deletions
}

func formatChangeSummary(files []string, insertions, deletions int) string {
	var b strings.Builder
	b.WriteString(fileCountNoun(len(files), "file"))

	named := files
	extra := 0
	if len(named) > maxSummaryFiles {
		extra = len(named) - maxSummaryFiles
		named = named[:maxSummaryFiles]
	}
	b.WriteString(": ")
	b.WriteString(strings.Join(named, ", "))
	if extra > 0 {
		fmt.Fprintf(&b, ", +%d more", extra)
	}

	if insertions > 0 || deletions > 0 {
		fmt.Fprintf(&b, " (+%d/-%d)", insertions, deletions)
	}
	return b.String()
}

func fileCountNoun(n int, noun string) string {
	switch n {
	case 1:
		return "changed 1 " + noun
	default:
		return fmt.Sprintf("changed %d %ss", n, noun)
	}
}

// SummarizeDeploy renders a deploy as a short deterministic
// template summary. A production deploy reads "deployed <version> to
// production"; a preview reads "preview deploy <id>". A rolled-back or
// failed deploy says so; an unknown status appends the raw status when it
// is non-empty. Version and project are included only when present.
func SummarizeDeploy(d deploy.Deployment) string {
	var b strings.Builder

	if d.Kind == deploy.KindProduction {
		b.WriteString("deployed")
	} else {
		b.WriteString("preview deploy")
	}

	if d.Version != "" {
		b.WriteString(" ")
		b.WriteString(d.Version)
	}

	switch d.Kind {
	case deploy.KindProduction:
		b.WriteString(" to production")
	case deploy.KindPreview:
		if d.ID != "" {
			b.WriteString(" ")
			b.WriteString(d.ID)
		}
	default:
		if d.Kind != "" {
			b.WriteString(" to ")
			b.WriteString(string(d.Kind))
		}
	}

	switch d.Status {
	case deploy.StatusRolledBack:
		b.WriteString(" (rolled back)")
	case deploy.StatusFailed:
		b.WriteString(" (failed)")
	case deploy.StatusQueued:
		b.WriteString(" (queued)")
	case deploy.StatusDeploying:
		b.WriteString(" (deploying)")
	case deploy.StatusReady, "":
		// Ready (or a target that reports no status) needs no suffix.
	default:
		b.WriteString(" (")
		b.WriteString(string(d.Status))
		b.WriteString(")")
	}

	return b.String()
}

// diffOp is a single line-oriented diff operation.
type diffOp struct {
	kind  diffOpKind
	count int
}

type diffOpKind int

const (
	opEqual diffOpKind = iota
	opDelete
	opInsert
)

// diffLines is a minimal common-prefix/suffix line diff: it strips the
// lines the two sides share at the head and tail and reports the remaining
// middle as a delete followed by an insert. It is deliberately O(n) and
// allocation-light — enough for a summary's insertion/deletion counts —
// where the full rendered diff is history.UnifiedDiffFor's job.
func diffLines(before, after string) []diffOp {
	b := splitLines(before)
	a := splitLines(after)

	prefix := 0
	for prefix < len(b) && prefix < len(a) && b[prefix] == a[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(b)-prefix && suffix < len(a)-prefix &&
		b[len(b)-1-suffix] == a[len(a)-1-suffix] {
		suffix++
	}

	var ops []diffOp
	if prefix > 0 {
		ops = append(ops, diffOp{kind: opEqual, count: prefix})
	}
	if del := len(b) - prefix - suffix; del > 0 {
		ops = append(ops, diffOp{kind: opDelete, count: del})
	}
	if ins := len(a) - prefix - suffix; ins > 0 {
		ops = append(ops, diffOp{kind: opInsert, count: ins})
	}
	if suffix > 0 {
		ops = append(ops, diffOp{kind: opEqual, count: suffix})
	}
	return ops
}

// splitLines splits s into lines, dropping a single trailing empty element
// so "a\nb\n" and "a\nb" both yield ["a", "b"].
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}
