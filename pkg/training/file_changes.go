package training

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// ---------------------------------------------------------------------------
// Export options / result / example types
// ---------------------------------------------------------------------------

// FileChangeExportOptions configures a file-change diff-pair export run.
type FileChangeExportOptions struct {
	// Output is the destination file path (OpenAI JSONL).
	Output string

	// MaxSize is the maximum number of characters per file content (before
	// or after). Changes where either side exceeds this are skipped.
	// Defaults to 50000 when zero.
	MaxSize int

	// ExcludePaths is a list of absolute path prefixes. Working directories
	// starting with any of these paths are excluded from the export.
	ExcludePaths []string
}

// FileChangeExportResult contains statistics about a file-change export run.
type FileChangeExportResult struct {
	ChangesScanned  int    `json:"changes_scanned"`
	ChangesExported int    `json:"changes_exported"`
	ChangesFiltered int    `json:"changes_filtered"`
	OutputPath      string `json:"output_path"`
}

// FileChangeExample is one training example built from a file diff pair.
type FileChangeExample struct {
	Messages []OpenAIMessage             `json:"messages"`
	Metadata FileChangeExampleMetadata   `json:"metadata"`
}

// FileChangeExampleMetadata holds metadata about a single file-change example.
type FileChangeExampleMetadata struct {
	Source      string `json:"source"`
	Description string `json:"description"`
	Model       string `json:"model,omitempty"`
	File        string `json:"file"`
}

// ---------------------------------------------------------------------------
// Constants: filter sets
// ---------------------------------------------------------------------------

// fileChangeSystemPrompt is the system message used for every file-change
// training example.
const fileChangeSystemPrompt = "You are a code editing assistant. Apply the requested change to the file."

// allowedSourceCodeExts is the whitelist of file extensions that are
// eligible for export.
var allowedSourceCodeExts = map[string]bool{
	".go": true, ".ts": true, ".tsx": true, ".js": true, ".jsx": true,
	".py": true, ".swift": true, ".rs": true, ".java": true, ".kt": true,
	".css": true, ".scss": true, ".html": true, ".json": true,
	".yaml": true, ".yml": true, ".md": true, ".sh": true,
}

// blockedBinaryExts is the blacklist of binary/generated file extensions
// that are always skipped.
var blockedBinaryExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".pcm": true, ".dia": true,
	".etag": true, ".d": true, ".swiftdeps": true, ".xcscheme": true,
	".xcconfig": true, ".lock": true, ".bin": true, ".o": true,
	".a": true, ".dylib": true,
}

// blockedPathSegments is the list of path fragments that, if present in the
// filename, cause the change to be skipped.
var blockedPathSegments = []string{
	"Pods/", "node_modules/", ".git/", "vendor/", "build/",
	"DerivedData/", ".build/", "__pycache__/", ".swiftpm/",
}

// defaultMaxSize is the default per-file content limit.
const defaultMaxSize = 50000

// ---------------------------------------------------------------------------
// Internal: change metadata (mirrors history.ChangeMetadata)
// ---------------------------------------------------------------------------

// changeMetadata mirrors the on-disk metadata.json written by the
// history package. Only the fields needed for export are included.
type changeMetadata struct {
	Version      int    `json:"version"`
	Filename     string `json:"filename"`
	Description  string `json:"description"`
	LLMMessage   string `json:"llm_message,omitempty"`
	AgentModel   string `json:"agent_model,omitempty"`
	FileRevHash  string `json:"file_revision_hash"`
}

// ---------------------------------------------------------------------------
// Core export
// ---------------------------------------------------------------------------

// ExportFileChanges scans all .sprout/changes directories discovered via
// the session registry (agent.ListAllSessionsWithTimestamps), reads each
// file-change diff pair, filters out noise, and writes OpenAI JSONL
// training examples to opts.Output.
func ExportFileChanges(opts FileChangeExportOptions) (*FileChangeExportResult, error) {
	if strings.TrimSpace(opts.Output) == "" {
		return nil, fmt.Errorf("--output is required")
	}
	maxSize := opts.MaxSize
	if maxSize <= 0 {
		maxSize = defaultMaxSize
	}

	// Discover all .sprout/changes directories from session working dirs.
	changeDirs, err := discoverChangeDirs(opts.ExcludePaths)
	if err != nil {
		return nil, fmt.Errorf("failed to discover change directories: %w", err)
	}

	result := &FileChangeExportResult{
		OutputPath: opts.Output,
	}

	// Track seen file-revision hashes to avoid duplicates across dirs.
	seen := make(map[string]bool)
	var examples []FileChangeExample

	for _, dir := range changeDirs {
		exs, scanned, filtered := processChangeDir(dir, maxSize, seen)
		result.ChangesScanned += scanned
		result.ChangesFiltered += filtered
		examples = append(examples, exs...)
	}

	result.ChangesExported = len(examples)

	if err := writeFileChangeJSONL(examples, opts.Output); err != nil {
		return nil, fmt.Errorf("failed to write output: %w", err)
	}

	return result, nil
}

// ---------------------------------------------------------------------------
// Discovery: find all .sprout/changes directories
// ---------------------------------------------------------------------------

// discoverChangeDirs returns a deduplicated list of .sprout/changes
// directories by scanning the working directories of all known sessions,
// plus the user's home directory. Directories matching any exclude prefix
// are skipped.
func discoverChangeDirs(excludePaths []string) ([]string, error) {
	seen := make(map[string]bool)
	var dirs []string

	addIfValid := func(base string) {
		if isExcludedDirectory(base, excludePaths) {
			return
		}
		changesDir := filepath.Join(base, ".sprout", "changes")
		if seen[changesDir] {
			return
		}
		if info, err := os.Stat(changesDir); err == nil && info.IsDir() {
			seen[changesDir] = true
			dirs = append(dirs, changesDir)
		}
	}

	// Scan all session working directories.
	sessions, err := agent.ListAllSessionsWithTimestamps()
	if err == nil {
		for _, s := range sessions {
			if s.WorkingDirectory != "" {
				addIfValid(s.WorkingDirectory)
			}
		}
	}

	// Always include the home directory as a fallback.
	if home, err := os.UserHomeDir(); err == nil {
		addIfValid(home)
	}

	// Also check the current working directory.
	if cwd, err := os.Getwd(); err == nil {
		addIfValid(cwd)
	}

	return dirs, nil
}

// ---------------------------------------------------------------------------
// Per-directory processing
// ---------------------------------------------------------------------------

// processChangeDir reads all change subdirectories under changesDir,
// building training examples for qualifying diffs. seen tracks
// file-revision hashes already exported (across all directories) to avoid
// duplicates.
func processChangeDir(changesDir string, maxSize int, seen map[string]bool) ([]FileChangeExample, int, int) {
	entries, err := os.ReadDir(changesDir)
	if err != nil {
		return nil, 0, 0
	}

	var examples []FileChangeExample
	scanned, filtered := 0, 0

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		scanned++

		changeDir := filepath.Join(changesDir, entry.Name())
		example, ok := buildChangeExample(changeDir, maxSize)
		if !ok {
			filtered++
			continue
		}

		// Deduplicate by file revision hash.
		if seen[example.Metadata.File] {
			filtered++
			continue
		}

		// Use a composite key for dedup to handle same file edited differently.
		dedupKey := entry.Name()
		if seen[dedupKey] {
			filtered++
			continue
		}
		seen[dedupKey] = true

		examples = append(examples, *example)
	}

	return examples, scanned, filtered
}

// buildChangeExample reads metadata.json, .original, and .updated from a
// single change directory, applies filters, and returns a training example
// if the change qualifies.
func buildChangeExample(changeDir string, maxSize int) (*FileChangeExample, bool) {
	// Read metadata.
	metaPath := filepath.Join(changeDir, "metadata.json")
	metaBytes, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, false
	}
	var meta changeMetadata
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return nil, false
	}

	// Filter: only include descriptions containing "edit".
	if !strings.Contains(strings.ToLower(meta.Description), "edit") {
		return nil, false
	}

	filename := meta.Filename

	// Filter: blocked path segments.
	if hasBlockedPathSegment(filename) {
		return nil, false
	}

	// Filter: must be an allowed source code extension.
	ext := strings.ToLower(filepath.Ext(filename))
	if !allowedSourceCodeExts[ext] {
		return nil, false
	}

	// Filter: skip blocked binary extensions (belt-and-suspenders — the
	// allowedSourceCodeExts whitelist should already exclude these, but
	// we keep the explicit check for clarity).
	if blockedBinaryExts[ext] {
		return nil, false
	}

	// Read and base64-decode .original and .updated.
	original, origOK := readBase64File(changeDir, filename, ".original")
	updated, updOK := readBase64File(changeDir, filename, ".updated")
	if !origOK || !updOK {
		return nil, false
	}

	// Filter: skip empty content on either side.
	if strings.TrimSpace(original) == "" || strings.TrimSpace(updated) == "" {
		return nil, false
	}

	// Filter: skip identical content.
	if original == updated {
		return nil, false
	}

	// Filter: skip content exceeding maxSize.
	if len(original) > maxSize || len(updated) > maxSize {
		return nil, false
	}

	// Apply credential redaction.
	original = RedactContent(original)
	updated = RedactContent(updated)

	// Build the training example. Redact PII in filename and metadata too.
	rFilename := RedactContent(filename)
	userContent := fmt.Sprintf("Edit the following file:\n\nFile: %s\n\n```%s```", rFilename, original)
	assistantContent := fmt.Sprintf("```%s```", updated)

	return &FileChangeExample{
		Messages: []OpenAIMessage{
			{Role: "system", Content: fileChangeSystemPrompt},
			{Role: "user", Content: userContent},
			{Role: "assistant", Content: assistantContent},
		},
		Metadata: FileChangeExampleMetadata{
			Source:      "file_change",
			Description: meta.Description,
			Model:       meta.AgentModel,
			File:        rFilename,
		},
	}, true
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// readBase64File reads a base64-encoded file from a change directory.
// The filename is sanitized (/ replaced with _) to match the on-disk naming.
func readBase64File(changeDir, filename, suffix string) (string, bool) {
	safe := strings.ReplaceAll(filename, "/", "_")
	safe = strings.ReplaceAll(safe, "\\", "_")
	path := filepath.Join(changeDir, safe+suffix)

	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}

	decoded, err := base64.StdEncoding.DecodeString(string(raw))
	if err != nil {
		// Some files may not be base64-encoded (older format). Fall back
		// to using the raw content directly.
		return string(raw), true
	}
	return string(decoded), true
}

// hasBlockedPathSegment reports whether the path contains any blocked
// path fragment.
func hasBlockedPathSegment(path string) bool {
	normalized := strings.ReplaceAll(path, "\\", "/")
	for _, seg := range blockedPathSegments {
		if strings.Contains(normalized, seg) {
			return true
		}
	}
	return false
}

// writeFileChangeJSONL writes file-change examples as JSONL (one JSON
// object per line), creating the output directory if needed.
func writeFileChangeJSONL(examples []FileChangeExample, path string) error {
	// Reuse the generic writeJSONArrayWithEncoder helper to avoid
	// duplicating the mkdir/create/encode loop.
	items := make([]interface{}, len(examples))
	for i := range examples {
		items[i] = examples[i]
	}
	return writeJSONLGeneric(items, path)
}
