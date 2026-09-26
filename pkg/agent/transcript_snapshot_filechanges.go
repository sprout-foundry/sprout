package agent

// transcript_snapshot_filechanges.go — the transcript file-change
// extraction and merge helpers (ExtractFileChangesFromMessages, the
// subagent/compacted-block parsers, the op-letter round-trip, and the
// change merge/dedup), split out of transcript_snapshot.go.

import (
	"encoding/json"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// CompactedFilesHeader marks the file-change manifest block that
// `/compact` appends to its LLM-generated summary. Future compactions
// re-parse this block to keep the running file-change history visible
// across the summary boundary — without this, every `/compact` would
// lose the manifest of files touched in the summarized turns.
const CompactedFilesHeader = "Files modified during compacted segment:"

// fileWriteToolNames is the set of file-mutating tool names whose
// arguments carry a `path` field worth surfacing in the manifest.
// Mirrors the structured-file write surface in pkg/agent/file_*.go.
var fileWriteToolNames = map[string]string{
	"write_file":            "created",
	"edit_file":             "modified",
	"write_structured_file": "created",
	"patch_structured_file": "modified",
}

// ExtractFileChangesFromMessages walks the supplied message slice and
// returns a deduped manifest of files touched, drawn from three
// authoritative sources: (1) tool_calls on assistant messages whose
// function name is a known file-write tool, (2) `[subagent files
// modified]` blocks embedded by tool_handlers_subagent in subagent
// tool results, and (3) `Files modified during compacted segment:`
// blocks that this package writes when /compact substitutes a
// summary for prior turns. The third source is what carries the
// manifest forward across successive compactions.
func ExtractFileChangesFromMessages(messages []api.Message) []TranscriptFileChange {
	var out []TranscriptFileChange
	seen := make(map[string]struct{})
	add := func(c TranscriptFileChange) {
		if c.Path == "" {
			return
		}
		key := c.Source + "|" + c.Operation + "|" + c.Path
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, c)
	}

	for _, m := range messages {
		switch m.Role {
		case "assistant":
			for _, tc := range m.ToolCalls {
				name := strings.TrimSpace(tc.Function.Name)
				op, ok := fileWriteToolNames[name]
				if !ok {
					continue
				}
				path := extractPathFromToolArgs(tc.Function.Arguments)
				if path == "" {
					continue
				}
				add(TranscriptFileChange{
					Path:      path,
					Operation: op,
					Source:    transcriptFileChangeSourcePrimary,
					ToolCall:  name,
				})
			}
			for _, c := range parseCompactedFilesBlock(m.Content) {
				add(c)
			}
		case "tool":
			for _, c := range parseSubagentFilesBlock(m.Content) {
				add(c)
			}
		}
	}
	return out
}

// FormatFileChangesForSummary renders a manifest into the canonical
// text block appended to a /compact summary. The format is chosen so
// parseCompactedFilesBlock can round-trip it back to TranscriptFileChange
// entries, preserving source / tool attribution across compaction
// boundaries. Returns the empty string when the manifest is empty so
// callers can skip appending altogether.
func FormatFileChangesForSummary(changes []TranscriptFileChange) string {
	if len(changes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(CompactedFilesHeader)
	b.WriteByte('\n')
	for _, c := range changes {
		b.WriteString("- ")
		b.WriteString(opLetterFor(c.Operation))
		b.WriteByte(' ')
		b.WriteString(c.Path)
		if c.Source != "" || c.ToolCall != "" {
			b.WriteString(" (")
			b.WriteString(c.Source)
			if c.ToolCall != "" {
				b.WriteString(": ")
				b.WriteString(c.ToolCall)
			}
			b.WriteString(")")
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func parseSubagentFilesBlock(content string) []TranscriptFileChange {
	startIdx := strings.Index(content, subagentFilesHeader)
	if startIdx < 0 {
		return nil
	}
	endIdx := strings.Index(content[startIdx:], subagentFilesFooter)
	if endIdx < 0 {
		return nil
	}
	body := content[startIdx+len(subagentFilesHeader) : startIdx+endIdx]
	var out []TranscriptFileChange
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Format: "<letter> <path>" — A/M/D/R.
		fields := strings.SplitN(line, " ", 2)
		if len(fields) != 2 {
			continue
		}
		op := opFromLetter(fields[0])
		if op == "" {
			continue
		}
		out = append(out, TranscriptFileChange{
			Path:      strings.TrimSpace(fields[1]),
			Operation: op,
			Source:    transcriptFileChangeSourceSubagent,
		})
	}
	return out
}

func parseCompactedFilesBlock(content string) []TranscriptFileChange {
	startIdx := strings.Index(content, CompactedFilesHeader)
	if startIdx < 0 {
		return nil
	}
	body := content[startIdx+len(CompactedFilesHeader):]
	var out []TranscriptFileChange
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Stop at the first non-manifest line — the summary block can
		// be followed by other content if the LLM kept appending.
		if !strings.HasPrefix(line, "- ") {
			break
		}
		// Format: "- <letter> <path> (<source>[: <tool>])"
		rest := strings.TrimPrefix(line, "- ")
		fields := strings.SplitN(rest, " ", 2)
		if len(fields) != 2 {
			continue
		}
		op := opFromLetter(fields[0])
		if op == "" {
			continue
		}
		pathAndAttr := fields[1]
		path := pathAndAttr
		source := transcriptFileChangeSourcePrimary
		toolCall := ""
		if open := strings.LastIndex(pathAndAttr, " ("); open >= 0 && strings.HasSuffix(pathAndAttr, ")") {
			path = strings.TrimSpace(pathAndAttr[:open])
			attr := pathAndAttr[open+2 : len(pathAndAttr)-1]
			if colon := strings.Index(attr, ": "); colon >= 0 {
				source = strings.TrimSpace(attr[:colon])
				toolCall = strings.TrimSpace(attr[colon+2:])
			} else {
				source = strings.TrimSpace(attr)
			}
		}
		out = append(out, TranscriptFileChange{
			Path:      path,
			Operation: op,
			Source:    source,
			ToolCall:  toolCall,
		})
	}
	return out
}

func extractPathFromToolArgs(args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return ""
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(args), &parsed); err != nil {
		return ""
	}
	for _, key := range []string{"path", "file_path", "filepath"} {
		if v, ok := parsed[key]; ok {
			if s, ok := v.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func opFromLetter(letter string) string {
	switch strings.ToUpper(strings.TrimSpace(letter)) {
	case "A":
		return "created"
	case "M":
		return "modified"
	case "D":
		return "deleted"
	case "R":
		return "renamed"
	}
	return ""
}

func opLetterFor(op string) string {
	switch op {
	case "created", "write", "create":
		return "A"
	case "modified", "edit":
		return "M"
	case "deleted", "delete":
		return "D"
	case "renamed", "rename":
		return "R"
	}
	return "?"
}

// trackedChangesAsTranscript projects the ChangeTracker's full
// TrackedFileChange records to the slim TranscriptFileChange shape and
// tags them as primary-source. The original/new bodies are intentionally
// dropped — they belong to the recovery flow, not to the diagnostic
// snapshot.
func trackedChangesAsTranscript(changes []TrackedFileChange) []TranscriptFileChange {
	if len(changes) == 0 {
		return nil
	}
	out := make([]TranscriptFileChange, 0, len(changes))
	for _, c := range changes {
		out = append(out, TranscriptFileChange{
			Path:      c.FilePath,
			Operation: normalizeTrackerOp(c.Operation),
			Source:    transcriptFileChangeSourcePrimary,
			ToolCall:  c.ToolCall,
			Timestamp: c.Timestamp,
			BulkCount: c.BulkCount,
		})
	}
	return out
}

func normalizeTrackerOp(op string) string {
	switch op {
	case "write", "create":
		return "created"
	case "edit":
		return "modified"
	case "delete":
		return "deleted"
	}
	return op
}

// fileChangesAdded returns the entries present in newer but not in
// older, keyed by source+op+path. Used by /transcript diff to highlight
// what touched the filesystem between two snapshots.
func fileChangesAdded(older, newer []TranscriptFileChange) []TranscriptFileChange {
	if len(newer) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(older))
	for _, c := range older {
		seen[c.Source+"|"+c.Operation+"|"+c.Path] = struct{}{}
	}
	var out []TranscriptFileChange
	for _, c := range newer {
		key := c.Source + "|" + c.Operation + "|" + c.Path
		if _, ok := seen[key]; ok {
			continue
		}
		out = append(out, c)
	}
	return out
}

func mergeFileChanges(primary, secondary []TranscriptFileChange) []TranscriptFileChange {
	if len(primary) == 0 && len(secondary) == 0 {
		return nil
	}
	out := make([]TranscriptFileChange, 0, len(primary)+len(secondary))
	seen := make(map[string]struct{}, len(primary)+len(secondary))
	add := func(c TranscriptFileChange) {
		if c.Path == "" {
			return
		}
		key := c.Source + "|" + c.Operation + "|" + c.Path
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, c)
	}
	for _, c := range primary {
		add(c)
	}
	for _, c := range secondary {
		add(c)
	}
	return out
}
