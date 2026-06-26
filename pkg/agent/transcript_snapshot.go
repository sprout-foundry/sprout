package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// TranscriptSnapshotFormat identifies snapshot file shape. Bump when
// breaking shape changes land so older readers don't silently misparse.
const TranscriptSnapshotFormat = "sprout-transcript/v1"

const checkpointMarker = "Compacted earlier conversation state:"

// Retention caps for per-session snapshot directories. The two buckets
// are kept separately so frequent auto-compaction snapshots can't push
// out the user's manually requested /transcript captures. Each bucket
// is FIFO by filename (timestamp-prefixed, so lexicographic order
// equals chronological).
const (
	transcriptMaxAutoSnapshots   = 6
	transcriptMaxManualSnapshots = 20
)

// MessageSource tags how a message arrived in the live conversation. It
// is the single most useful diagnostic field in a snapshot: it tells a
// reader whether what the model sees at index i is the user's original
// turn, a turn collapsed to a rule-based heuristic bullet list, or a
// structural summary produced by seed's LLM summarizer.
type MessageSource string

const (
	MessageSourceOriginal      MessageSource = "original"
	MessageSourceLLMCheckpoint MessageSource = "llm_checkpoint"
)

// MessageAnnotation is the per-message diagnostic view. Index aligns
// 1:1 with TranscriptSnapshot.State.Messages.
type MessageAnnotation struct {
	Index         int           `json:"index"`
	Role          string        `json:"role"`
	Source        MessageSource `json:"source"`
	ContentChars  int           `json:"content_chars"`
	ToolCallCount int           `json:"tool_call_count,omitempty"`
	FirstLine     string        `json:"first_line,omitempty"`
}

// CompactPreview captures the would-be result of running /compact right
// now, without applying it. Populated only when CaptureTranscriptSnapshot
// is called with includePreview=true.
type CompactPreview struct {
	BeforeMessageCount   int              `json:"before_message_count"`
	AfterMessageCount    int              `json:"after_message_count"`
	WouldReduce          bool             `json:"would_reduce"`
	CompactedMessages    []api.Message    `json:"compacted_messages"`
	RemainingCheckpoints []TurnCheckpoint `json:"remaining_checkpoints"`
}

// TranscriptFileChange is the slim per-file projection embedded in a
// snapshot's top-level FileChanges field. It deliberately omits the
// full original/new file bodies that the ChangeTracker keeps for
// recovery — those can be multi-megabyte per file and would blow up
// snapshot size. The path, operation, and tool-call identifier give a
// reader enough to answer "what files were touched between snapshot A
// and snapshot B" without loading the bytes themselves.
//
// Source distinguishes changes the primary agent made directly
// ("primary") from rollups parsed out of subagent tool results
// ("subagent"). The subagent's [subagent files modified] block is the
// authoritative per-call manifest, so the parser is a deterministic
// text scan rather than heuristic prose extraction.
type TranscriptFileChange struct {
	Path      string    `json:"path"`
	Operation string    `json:"operation"`
	Source    string    `json:"source"`
	ToolCall  string    `json:"tool_call,omitempty"`
	Timestamp time.Time `json:"timestamp,omitempty"`
	BulkCount int       `json:"bulk_count,omitempty"`
}

const (
	transcriptFileChangeSourcePrimary  = "primary"
	transcriptFileChangeSourceSubagent = "subagent"

	subagentFilesHeader = "[subagent files modified]"
	subagentFilesFooter = "[/subagent files modified]"
)

// TranscriptSnapshot is the file shape written by /transcript and by
// the auto-capture path on compaction events. It is intentionally a
// superset of ConversationState so a reader can diff message lists,
// inspect checkpoint summaries, and compare snapshots across time.
type TranscriptSnapshot struct {
	Format             string                 `json:"format"`
	Timestamp          time.Time              `json:"timestamp"`
	Label              string                 `json:"label"`
	SessionID          string                 `json:"session_id"`
	WorkingDirectory   string                 `json:"working_directory"`
	State              *ConversationState     `json:"state"`
	MessageAnnotations []MessageAnnotation    `json:"message_annotations"`
	FileChanges        []TranscriptFileChange `json:"file_changes,omitempty"`
	ChangeTrackerRev   string                 `json:"change_tracker_revision,omitempty"`
	CompactPreview     *CompactPreview        `json:"compact_preview,omitempty"`
}

// BuildTranscriptSnapshot constructs an in-memory snapshot of the
// agent's current conversation state plus diagnostic annotations. Pure
// read — does not mutate the agent or touch disk.
func (a *Agent) BuildTranscriptSnapshot(label string, includePreview bool) *TranscriptSnapshot {
	if a == nil {
		return nil
	}
	workingDir, _ := os.Getwd()
	cleanWorkingDir, err := normalizeWorkingDirectory(workingDir)
	if err != nil {
		cleanWorkingDir = workingDir
	}
	sessionID := a.GetSessionID()
	messages := a.GetMessages()
	checkpoints := a.copyTurnCheckpoints()

	state := &ConversationState{
		Messages:                append([]api.Message(nil), messages...),
		TurnCheckpoints:         checkpoints,
		TaskActions:             a.GetTaskActions(),
		TotalCost:               a.state.GetTotalCost(),
		TotalTokens:             a.state.GetTotalTokens(),
		PromptTokens:            a.state.GetPromptTokens(),
		CompletionTokens:        a.state.GetCompletionTokens(),
		EstimatedTokenResponses: a.state.GetEstimatedTokenResponses(),
		CachedTokens:            a.state.GetCachedTokens(),
		CachedCostSavings:       a.state.GetCachedCostSavings(),
		LastUpdated:             time.Now(),
		SessionID:               sessionID,
		Name:                    a.generateSessionName(),
		WorkingDirectory:        cleanWorkingDir,
		ConfigOverrides:         a.state.GetConfigOverrides(),
		SessionIntentEmbedding:  a.state.GetSessionIntentEmbedding(),
		LastProviderError:       a.state.GetLastProviderError(),
	}

	// File-change manifest: combine ChangeTracker's authoritative
	// primary record (which catches shell mutations the tool_calls
	// scan would miss) with the manifest extracted from message
	// content (which catches subagent rollups and prior-compaction
	// summary blocks). Dedupes on path+op+source so a primary write
	// reported by both sources collapses to one entry.
	var trackerChanges []TranscriptFileChange
	var trackerRev string
	if tracker := a.GetChangeTracker(); tracker != nil {
		trackerChanges = trackedChangesAsTranscript(tracker.GetChanges())
		trackerRev = tracker.GetRevisionID()
	}
	messageChanges := ExtractFileChangesFromMessages(messages)
	fileChanges := mergeFileChanges(trackerChanges, messageChanges)

	snap := &TranscriptSnapshot{
		Format:             TranscriptSnapshotFormat,
		Timestamp:          time.Now().UTC(),
		Label:              label,
		SessionID:          sessionID,
		WorkingDirectory:   cleanWorkingDir,
		State:              state,
		MessageAnnotations: annotateMessages(messages),
		FileChanges:        fileChanges,
		ChangeTrackerRev:   trackerRev,
	}

	if includePreview && len(checkpoints) > 0 {
		compacted, remaining := a.BuildCheckpointCompactedMessages(messages)
		snap.CompactPreview = &CompactPreview{
			BeforeMessageCount:   len(messages),
			AfterMessageCount:    len(compacted),
			WouldReduce:          len(compacted) < len(messages),
			CompactedMessages:    compacted,
			RemainingCheckpoints: remaining,
		}
	}

	return snap
}

// CaptureTranscriptSnapshot builds a snapshot and writes it to
// ~/.sprout/transcripts/<scope-hash>/<session-id>/<UTC-ts>-<label>.json.
// Returns the absolute path of the file written so callers can report
// it to the user or log it.
func (a *Agent) CaptureTranscriptSnapshot(label string, includePreview bool) (string, error) {
	snap := a.BuildTranscriptSnapshot(label, includePreview)
	if snap == nil {
		return "", fmt.Errorf("agent unavailable for transcript snapshot")
	}
	dir, err := transcriptSessionDir(snap.SessionID, snap.WorkingDirectory)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("failed to create transcript dir: %w", err)
	}
	cleanLabel := sanitizeLabel(label)
	filename := fmt.Sprintf("%s-%s.json", snap.Timestamp.Format("20060102T150405Z"), cleanLabel)
	path := filepath.Join(dir, filename)
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal transcript snapshot: %w", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", fmt.Errorf("failed to write transcript snapshot: %w", err)
	}
	pruneTranscriptDir(dir, transcriptMaxAutoSnapshots, transcriptMaxManualSnapshots)
	return path, nil
}

// pruneTranscriptDir enforces per-bucket retention on a session's
// transcript directory. Snapshots whose filename contains "auto" land
// in the auto bucket; everything else is manual. Each bucket is sorted
// by filename (timestamp-prefixed) and entries beyond the cap, oldest
// first, are deleted along with any sidecar .md and .diff.json files.
// Errors are swallowed — retention is best-effort and must never block
// the snapshot write or cause /compact to error out.
func pruneTranscriptDir(dir string, maxAuto, maxManual int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var autoFiles, manualFiles []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".json") || strings.HasSuffix(name, ".diff.json") {
			continue
		}
		if strings.Contains(name, "auto") {
			autoFiles = append(autoFiles, name)
		} else {
			manualFiles = append(manualFiles, name)
		}
	}
	sort.Strings(autoFiles)
	sort.Strings(manualFiles)
	pruneBucket(dir, autoFiles, maxAuto)
	pruneBucket(dir, manualFiles, maxManual)
}

func pruneBucket(dir string, files []string, cap int) {
	if cap <= 0 || len(files) <= cap {
		return
	}
	excess := len(files) - cap
	for i := 0; i < excess; i++ {
		jsonPath := filepath.Join(dir, files[i])
		_ = os.Remove(jsonPath)
		base := strings.TrimSuffix(files[i], ".json")
		_ = os.Remove(filepath.Join(dir, base+".md"))
		_ = os.Remove(filepath.Join(dir, base+".diff.json"))
	}
}

// LoadTranscriptSnapshot reads a snapshot file back into memory.
func LoadTranscriptSnapshot(path string) (*TranscriptSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read transcript snapshot %s: %w", path, err)
	}
	var snap TranscriptSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("failed to parse transcript snapshot %s: %w", path, err)
	}
	return &snap, nil
}

// ListTranscriptSnapshots returns snapshot file paths for the given
// session within the current workspace scope, sorted oldest-first.
func ListTranscriptSnapshots(sessionID, workingDir string) ([]string, error) {
	dir, err := transcriptSessionDir(sessionID, workingDir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read transcript dir %s: %w", dir, err)
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	sort.Strings(out)
	return out, nil
}

// TranscriptDiff is a compact, human-friendly comparison of two
// snapshots. Used by `/transcript diff` to expose what compaction (or
// some other state mutation) changed between snapshots.
type TranscriptDiff struct {
	OlderPath              string                 `json:"older_path"`
	NewerPath              string                 `json:"newer_path"`
	OlderTimestamp         time.Time              `json:"older_timestamp"`
	NewerTimestamp         time.Time              `json:"newer_timestamp"`
	OlderMessageCount      int                    `json:"older_message_count"`
	NewerMessageCount      int                    `json:"newer_message_count"`
	OlderCheckpointCount   int                    `json:"older_checkpoint_count"`
	NewerCheckpointCount   int                    `json:"newer_checkpoint_count"`
	OlderTotalTokens       int                    `json:"older_total_tokens"`
	NewerTotalTokens       int                    `json:"newer_total_tokens"`
	OlderFileChangeCount   int                    `json:"older_file_change_count"`
	NewerFileChangeCount   int                    `json:"newer_file_change_count"`
	NewFileChanges         []TranscriptFileChange `json:"new_file_changes,omitempty"`
	MessagesDroppedAtTail  int                    `json:"messages_dropped_at_tail"`
	MessagesReplacedByRole map[string]int         `json:"messages_replaced_by_role,omitempty"`
	ChangedIndices         []TranscriptDiffEntry  `json:"changed_indices,omitempty"`
	Notes                  []string               `json:"notes,omitempty"`
}

// TranscriptDiffEntry is a single divergence in the per-index walk.
// Truncated content makes diffs scannable; full text is in the raw JSON.
type TranscriptDiffEntry struct {
	Index          int    `json:"index"`
	OlderRole      string `json:"older_role,omitempty"`
	NewerRole      string `json:"newer_role,omitempty"`
	OlderSource    string `json:"older_source,omitempty"`
	NewerSource    string `json:"newer_source,omitempty"`
	OlderFirstLine string `json:"older_first_line,omitempty"`
	NewerFirstLine string `json:"newer_first_line,omitempty"`
}

// DiffTranscriptSnapshots compares two snapshots and returns a
// human-readable diff structure. Older should be the chronologically
// earlier snapshot; the function does not re-sort.
func DiffTranscriptSnapshots(older, newer *TranscriptSnapshot) *TranscriptDiff {
	if older == nil || newer == nil || older.State == nil || newer.State == nil {
		return nil
	}
	diff := &TranscriptDiff{
		OlderTimestamp:       older.Timestamp,
		NewerTimestamp:       newer.Timestamp,
		OlderMessageCount:    len(older.State.Messages),
		NewerMessageCount:    len(newer.State.Messages),
		OlderCheckpointCount: len(older.State.TurnCheckpoints),
		NewerCheckpointCount: len(newer.State.TurnCheckpoints),
		OlderTotalTokens:     older.State.TotalTokens,
		NewerTotalTokens:     newer.State.TotalTokens,
		OlderFileChangeCount: len(older.FileChanges),
		NewerFileChangeCount: len(newer.FileChanges),
		NewFileChanges:       fileChangesAdded(older.FileChanges, newer.FileChanges),
	}

	olderMsgs := older.State.Messages
	newerMsgs := newer.State.Messages
	olderAnn := older.MessageAnnotations
	newerAnn := newer.MessageAnnotations

	if len(newerMsgs) < len(olderMsgs) {
		diff.MessagesDroppedAtTail = len(olderMsgs) - len(newerMsgs)
	}

	limit := len(olderMsgs)
	if len(newerMsgs) < limit {
		limit = len(newerMsgs)
	}
	replacedByRole := map[string]int{}
	for i := 0; i < limit; i++ {
		o := olderMsgs[i]
		n := newerMsgs[i]
		if o.Role == n.Role && o.Content == n.Content && len(o.ToolCalls) == len(n.ToolCalls) {
			continue
		}
		role := n.Role
		if role == "" {
			role = o.Role
		}
		replacedByRole[role]++
		entry := TranscriptDiffEntry{Index: i, OlderRole: o.Role, NewerRole: n.Role}
		if i < len(olderAnn) {
			entry.OlderSource = string(olderAnn[i].Source)
			entry.OlderFirstLine = olderAnn[i].FirstLine
		}
		if i < len(newerAnn) {
			entry.NewerSource = string(newerAnn[i].Source)
			entry.NewerFirstLine = newerAnn[i].FirstLine
		}
		diff.ChangedIndices = append(diff.ChangedIndices, entry)
	}
	if len(replacedByRole) > 0 {
		diff.MessagesReplacedByRole = replacedByRole
	}

	if diff.NewerCheckpointCount < diff.OlderCheckpointCount {
		diff.Notes = append(diff.Notes,
			fmt.Sprintf("turn checkpoints decreased by %d — likely consumed by /compact",
				diff.OlderCheckpointCount-diff.NewerCheckpointCount))
	}
	if diff.MessagesDroppedAtTail > 0 {
		diff.Notes = append(diff.Notes,
			fmt.Sprintf("%d trailing messages dropped — could indicate /clear or pruner activity",
				diff.MessagesDroppedAtTail))
	}
	if len(diff.ChangedIndices) > 0 && diff.MessagesDroppedAtTail == 0 {
		diff.Notes = append(diff.Notes,
			fmt.Sprintf("%d messages were replaced in place — checkpoint substitution or pruner rewrite",
				len(diff.ChangedIndices)))
	}

	return diff
}

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

func annotateMessages(messages []api.Message) []MessageAnnotation {
	out := make([]MessageAnnotation, 0, len(messages))
	for i, m := range messages {
		ann := MessageAnnotation{
			Index:         i,
			Role:          m.Role,
			Source:        classifyMessageSource(m),
			ContentChars:  len(m.Content),
			ToolCallCount: len(m.ToolCalls),
			FirstLine:     firstNonEmptyLine(m.Content),
		}
		out = append(out, ann)
	}
	return out
}

func classifyMessageSource(m api.Message) MessageSource {
	if strings.Contains(m.Content, checkpointMarker) {
		return MessageSourceLLMCheckpoint
	}
	return MessageSourceOriginal
}

func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > 140 {
			line = line[:137] + "..."
		}
		return line
	}
	return ""
}

func transcriptSessionDir(sessionID, workingDir string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve home directory: %w", err)
	}
	cleanWorkingDir, err := normalizeWorkingDirectory(workingDir)
	if err != nil {
		cleanWorkingDir = workingDir
	}
	scope := workingDirectoryScopeHash(cleanWorkingDir)
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		sid = "unknown-session"
	}
	return filepath.Join(home, ".sprout", "transcripts", scope, sid), nil
}

func sanitizeLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return "snapshot"
	}
	var b strings.Builder
	for _, r := range label {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ' || r == '/':
			b.WriteRune('-')
		}
	}
	cleaned := b.String()
	if cleaned == "" {
		return "snapshot"
	}
	if len(cleaned) > 40 {
		cleaned = cleaned[:40]
	}
	return cleaned
}
