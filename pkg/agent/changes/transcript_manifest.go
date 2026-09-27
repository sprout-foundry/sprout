// SP-141 phase 2: the pure transcript-manifest half of the old
// pkg/agent/transcript_snapshot.go. The snapshot/diff structs stay in
// pkg/agent (their State field is *agent.ConversationState, a core type
// that moves last); everything here is agent-free logic: file-change
// manifest extraction, compacted-segment block round-tripping,
// message-source annotation, snapshot directory layout + working-dir
// path normalization, retention pruning, and label sanitizing.
package changes

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/envutil"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

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

// CompactedFilesHeader marks the file-change manifest block that
// `/compact` appends to its LLM-generated summary. Future compactions
// re-parse this block to keep the running file-change history visible
// across the summary boundary — without this, every `/compact` would
// lose the manifest of files touched in the summarized turns. pkg/agent
// aliases this constant next to its snapshot methods.
const CompactedFilesHeader = "Files modified during compacted segment:"

// TranscriptSessionDir returns the per-session transcript directory for
// the given session within the current workspace scope:
// <state>/transcripts/<scope-hash>/<session-id>. Exported for
// pkg/agent's CaptureTranscriptSnapshot (SP-141 phase 2).
func TranscriptSessionDir(sessionID, workingDir string) (string, error) {
	return transcriptSessionDir(sessionID, workingDir)
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

// PruneTranscriptDir enforces the default per-bucket retention caps on
// a session's transcript directory. Exported for pkg/agent's
// CaptureTranscriptSnapshot (SP-141 phase 2).
func PruneTranscriptDir(dir string) {
	pruneTranscriptDir(dir, transcriptMaxAutoSnapshots, transcriptMaxManualSnapshots)
}

// SanitizeSnapshotLabel reduces a snapshot label to the canonical
// filename-safe form. Exported for pkg/agent's CaptureTranscriptSnapshot
// (SP-141 phase 2).
func SanitizeSnapshotLabel(label string) string {
	return sanitizeLabel(label)
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
// TrackedChangesAsTranscript projects tracker changes to the slim manifest
// shape (exported for pkg/agent's BuildTranscriptSnapshot).
func TrackedChangesAsTranscript(changes []TrackedFileChange) []TranscriptFileChange {
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
// FileChangesAdded returns manifest entries present in newer but not
// older (exported for pkg/agent's DiffTranscriptSnapshots).
func FileChangesAdded(older, newer []TranscriptFileChange) []TranscriptFileChange {
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

// MergeFileChanges dedupes and concatenates primary+secondary manifest
// entries (exported for pkg/agent's BuildTranscriptSnapshot).
func MergeFileChanges(primary, secondary []TranscriptFileChange) []TranscriptFileChange {
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

// AnnotateMessages builds the per-message diagnostic annotations
// (exported for pkg/agent's BuildTranscriptSnapshot).
func AnnotateMessages(messages []api.Message) []MessageAnnotation {
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

// NormalizeWorkingDirectory normalizes a working directory to a cleaned,
// symlink-resolved absolute path (empty resolves to the process CWD).
// Moved from pkg/agent/persistence_message.go in SP-141 phase 2; shared
// by transcript session-dir resolution and session persistence.
func NormalizeWorkingDirectory(workingDir string) (string, error) {
	trimmed := strings.TrimSpace(workingDir)
	if trimmed == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", agenterrors.Wrap(err, "failed to resolve current working directory")
		}
		trimmed = cwd
	}
	abs, err := filepath.Abs(trimmed)
	if err != nil {
		return "", agenterrors.Wrapf(err, "failed to resolve absolute working directory %q", trimmed)
	}
	// Resolve symlinks for consistent path comparison. On macOS,
	// /var → /private/var and os.Getwd() returns the resolved path,
	// while t.TempDir() returns the unresolved path.
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return filepath.Clean(resolved), nil
	}
	return filepath.Clean(abs), nil
}

// WorkingDirectoryScopeHash returns the short hash that scopes
// per-working-directory state directories (transcripts, scoped
// sessions). Moved from pkg/agent/persistence_message.go in SP-141
// phase 2.
func WorkingDirectoryScopeHash(workingDir string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(workingDir))))
	return hex.EncodeToString(sum[:8])
}

func transcriptSessionDir(sessionID, workingDir string) (string, error) {
	stateDir, err := envutil.StateDir()
	if err != nil {
		return "", agenterrors.NewTool("transcript", "failed to resolve state directory", err)
	}
	cleanWorkingDir, err := NormalizeWorkingDirectory(workingDir)
	if err != nil {
		cleanWorkingDir = workingDir
	}
	scope := WorkingDirectoryScopeHash(cleanWorkingDir)
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
