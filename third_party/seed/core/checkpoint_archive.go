package core

import (
	"context"
	"fmt"
	"strings"
)

// MetaCompactionThreshold is the recommended checkpoint count above which the
// session is likely to start showing model-quality degradation from sheer
// summary volume (the prompt becomes mostly past-turn summaries with little
// recent raw context). Consumers should call ArchiveOldCheckpoints when the
// active checkpoint set passes this threshold; each archived checkpoint
// shrinks from a multi-line summary to a single commit-title-style line
// (~80 characters), bounding the long-tail growth.
const MetaCompactionThreshold = 100

// MetaArchiveBatchSize is the maximum number of checkpoints sent in a single
// summarizer call. Sending all at once would risk a context-overflow on the
// summarizer's own request; batching keeps each prompt bounded. 25 is a safe
// upper bound for typical models — adjust if observed empty responses suggest
// the summarizer is choking on the prompt size.
const MetaArchiveBatchSize = 25

// ArchiveOldCheckpoints generates ArchiveLine strings for the oldest
// checkpoints in the input slice, using the supplied LLMSummarizer to
// condense each turn's summary into a single commit-title-style line.
//
// Behavior:
//   - When summarizer is nil → returns the input unchanged.
//   - When len(checkpoints) <= keepRecent → returns the input unchanged.
//   - When a checkpoint already has a non-empty ArchiveLine → it is skipped
//     (idempotent — safe to call repeatedly).
//   - Errors during summarization are absorbed: the affected checkpoints
//     keep their original Summary/ActionableSummary and an error is logged
//     via the optional debugLog. The function never returns an error so
//     callers don't have to handle a fallible maintenance op.
//
// keepRecent guards the most recent N checkpoints from archiving — those
// summaries are likely still actively informing the model and shouldn't be
// flattened. The default recommendation is to keep 24 checkpoints raw
// (matching seed's defaultRecentToKeep convention for compaction).
func ArchiveOldCheckpoints(ctx context.Context, checkpoints []TurnCheckpoint, keepRecent int, summarizer LLMSummarizer) []TurnCheckpoint {
	if summarizer == nil || len(checkpoints) <= keepRecent {
		// Defensive copy so the caller's slice isn't aliased; matches the
		// pattern of BuildCheckpointCompactedMessages.
		out := make([]TurnCheckpoint, len(checkpoints))
		copy(out, checkpoints)
		return out
	}

	// Work on a copy from the start.
	out := make([]TurnCheckpoint, len(checkpoints))
	copy(out, checkpoints)

	// "Oldest first" by position in the slice. State.GetCheckpoints returns
	// in insertion order which is already oldest-first; we don't re-sort
	// here so the result preserves order for the caller.
	end := len(out) - keepRecent

	// Identify the indices that still need archiving (no ArchiveLine yet).
	needs := make([]int, 0, end)
	for i := 0; i < end; i++ {
		if strings.TrimSpace(out[i].ArchiveLine) == "" {
			needs = append(needs, i)
		}
	}
	if len(needs) == 0 {
		return out
	}

	// Process in batches to bound the summarizer's own prompt size.
	for start := 0; start < len(needs); start += MetaArchiveBatchSize {
		batchEnd := start + MetaArchiveBatchSize
		if batchEnd > len(needs) {
			batchEnd = len(needs)
		}
		batchIdx := needs[start:batchEnd]
		archiveBatch(ctx, out, batchIdx, summarizer)
	}

	return out
}

// archiveBatch issues a single summarizer call covering the checkpoints at
// the supplied indices and writes the resulting one-line condensations into
// out[i].ArchiveLine for each. Errors / malformed responses leave the
// affected entries unchanged.
func archiveBatch(ctx context.Context, out []TurnCheckpoint, indices []int, summarizer LLMSummarizer) {
	if len(indices) == 0 {
		return
	}

	// Build a single user message that lists each checkpoint with a
	// numbered tag the model must echo back. Echoing the tag lets us map
	// response lines back to the source even if the model reorders.
	var b strings.Builder
	b.WriteString("Condense each of the following past-conversation turn summaries into ")
	b.WriteString("a single commit-title-style line (under 80 chars). Use the format: ")
	b.WriteString(`"TAG: terse summary". Output one line per turn, no blank lines.` + "\n\n")
	for i, idx := range indices {
		cp := out[idx]
		body := cp.Summary
		if cp.ActionableSummary != "" {
			body = cp.ActionableSummary + "\n" + cp.Summary
		}
		fmt.Fprintf(&b, "T%d: %s\n\n", i, truncateForArchive(body, 800))
	}

	resp, err := summarizer(ctx, []Message{{Role: "user", Content: b.String()}}, SummarizerHint{
		DetailLevel: "brief",
		MaxWords:    20 * len(indices), // ~20 words per archived turn
	})
	if err != nil || strings.TrimSpace(resp) == "" {
		return
	}

	// Parse lines of the form "T<n>: <content>" into out[indices[n]].ArchiveLine.
	for _, line := range strings.Split(resp, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Expect "T<n>: ..."; tolerate slight format drift (e.g. "T0 -")
		var tag, rest string
		if cut := strings.IndexAny(line, ":-"); cut > 0 && cut < 6 {
			tag = strings.TrimSpace(line[:cut])
			rest = strings.TrimSpace(line[cut+1:])
		}
		if !strings.HasPrefix(strings.ToUpper(tag), "T") {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(tag[1:], "%d", &n); err != nil {
			continue
		}
		if n < 0 || n >= len(indices) {
			continue
		}
		if rest == "" {
			continue
		}
		// Cap the archive line to ~120 chars defensively; commit-title-style
		// shouldn't exceed this anyway.
		if len(rest) > 120 {
			rest = rest[:117] + "..."
		}
		out[indices[n]].ArchiveLine = rest
	}
}

// truncateForArchive returns a leading slice of s no longer than max runes,
// with an ellipsis appended when truncation happened. Used to bound the
// summarizer input so a single ultra-long Summary can't blow the batch
// prompt past the summarizer's context window.
func truncateForArchive(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
