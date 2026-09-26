package agent

// transcript_snapshot.go — the transcript snapshot lifecycle: the
// TranscriptSnapshot / MessageAnnotation / CompactPreview / TranscriptFileChange
// types, the Agent.BuildTranscriptSnapshot / CaptureTranscriptSnapshot capture
// path, the snapshot-directory pruning, LoadTranscriptSnapshot /
// ListTranscriptSnapshots, message annotation, and the session-dir / label
// helpers. The diff + file-change-extraction helpers live in
// transcript_snapshot_diff.go / transcript_snapshot_filechanges.go.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/envutil"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"

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
		ContinuationNudges:      a.state.GetContinuationNudges(),
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
		return "", agenterrors.NewTool("transcript", "agent unavailable for transcript snapshot", nil)
	}
	dir, err := transcriptSessionDir(snap.SessionID, snap.WorkingDirectory)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", agenterrors.NewTool("transcript", "failed to create transcript dir", err)
	}
	cleanLabel := sanitizeLabel(label)
	filename := fmt.Sprintf("%s-%s.json", snap.Timestamp.Format("20060102T150405Z"), cleanLabel)
	path := filepath.Join(dir, filename)
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return "", agenterrors.NewTool("transcript", "failed to marshal transcript snapshot", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", agenterrors.NewTool("transcript", "failed to write transcript snapshot", err)
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
		return nil, agenterrors.NewTool("transcript", fmt.Sprintf("failed to read transcript snapshot %s", path), err)
	}
	var snap TranscriptSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, agenterrors.NewTool("transcript", fmt.Sprintf("failed to parse transcript snapshot %s", path), err)
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
		return nil, agenterrors.NewTool("transcript", fmt.Sprintf("failed to read transcript dir %s", dir), err)
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
	stateDir, err := envutil.StateDir()
	if err != nil {
		return "", agenterrors.NewTool("transcript", "failed to resolve state directory", err)
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
	return filepath.Join(stateDir, "transcripts", scope, sid), nil
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
