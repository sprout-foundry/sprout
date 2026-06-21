// Persistence for the ChangeTracker's adaptive auto-skip set.
//
// The walker learns "fat directories" on first visit (those with more
// immediate child files than autoSkipFileCountThreshold) and skips
// them on subsequent walks within the same session. Without
// persistence, every new agent session re-learns from scratch —
// paying the first-walk cost over and over for the same dirs.
//
// This file persists the learned set to
// `~/.config/sprout/shell_skip_dirs.json`, keyed by absolute
// workspace root, so subsequent sessions in the same workspace
// inherit the learning. The file is best-effort: read failures fall
// back to an empty set (re-learn), write failures log a warning.
//
// To prevent unbounded growth across many workspaces, the file caps
// at maxPersistedWorkspaces entries — least-recently-used workspaces
// are evicted first when the cap is exceeded.
package agent

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// shellSkipDirsFilename is the on-disk name within the sprout config
// directory. One file holds all workspaces — keyed by workspace root
// inside the JSON. Easy to inspect / edit by hand to forget a
// previously-learned dir.
const shellSkipDirsFilename = "shell_skip_dirs.json"

// maxPersistedWorkspaces caps the number of workspaces tracked in
// shell_skip_dirs.json. 50 covers most users (a few dozen projects +
// some scratch dirs); the cap prevents the file from growing without
// bound when a user works across many projects over time. LRU
// eviction ensures frequently-used workspaces survive.
const maxPersistedWorkspaces = 50

// shellSkipDirsFileSchema is the on-disk shape. Versioned in case we
// want to evolve the file format. LastUsed is keyed by workspace root
// (same key as Workspaces) and stores a unix timestamp; missing
// entries are treated as epoch (oldest) so they evict first.
type shellSkipDirsFileSchema struct {
	Version    int                 `json:"version"`
	Workspaces map[string][]string `json:"workspaces"`
	LastUsed   map[string]int64    `json:"last_used,omitempty"`
}

// shellSkipDirsPersistMu serializes file I/O so concurrent subagents
// in the same process don't race on the read-modify-write pattern.
// One mutex covers all workspaces (the file is global); contention is
// negligible because saves only happen at walk-end when something new
// is learned.
var shellSkipDirsPersistMu sync.Mutex

// loadAutoSkipDirsFor returns the previously-persisted auto-skip set
// for the given workspace root. Returns an empty (non-nil) map on
// first run, missing config dir, or any I/O / parse error — we never
// surface persistence failures to the agent, just log and continue
// with an empty learned set (re-learning is cheap).
func loadAutoSkipDirsFor(workspaceRoot string) map[string]bool {
	result := make(map[string]bool)
	if workspaceRoot == "" {
		return result
	}
	path, err := shellSkipDirsFilePath()
	if err != nil {
		return result
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			// Quiet — persistence is best-effort.
		}
		return result
	}

	var schema shellSkipDirsFileSchema
	if jsonErr := json.Unmarshal(data, &schema); jsonErr != nil {
		return result
	}
	for _, dir := range schema.Workspaces[workspaceRoot] {
		result[dir] = true
	}

	return result
}

// saveAutoSkipDirsFor merges the given set into the persisted file
// under the workspace key. Read-modify-write under a process-level
// mutex; atomic via temp + rename so a crash never produces a partial
// JSON file.
//
// Enforces maxPersistedWorkspaces via LRU eviction: if the workspace
// count exceeds the cap, the least-recently-used entries (oldest
// LastUsed) are removed until count <= cap.
func saveAutoSkipDirsFor(workspaceRoot string, dirs map[string]bool) error {
	if workspaceRoot == "" || len(dirs) == 0 {
		return nil
	}
	path, err := shellSkipDirsFilePath()
	if err != nil {
		return err
	}

	shellSkipDirsPersistMu.Lock()
	defer shellSkipDirsPersistMu.Unlock()

	// Read current state (empty schema if file missing / unreadable).
	var schema shellSkipDirsFileSchema
	if data, readErr := os.ReadFile(path); readErr == nil {
		_ = json.Unmarshal(data, &schema)
	}
	if schema.Workspaces == nil {
		schema.Workspaces = make(map[string][]string)
	}
	if schema.LastUsed == nil {
		schema.LastUsed = make(map[string]int64)
	}
	schema.Version = 1

	// Merge: union of previously-saved + currently-known.
	existing := make(map[string]bool, len(schema.Workspaces[workspaceRoot]))
	for _, d := range schema.Workspaces[workspaceRoot] {
		existing[d] = true
	}
	for d := range dirs {
		existing[d] = true
	}
	merged := make([]string, 0, len(existing))
	for d := range existing {
		merged = append(merged, d)
	}
	sort.Strings(merged) // deterministic output for sane diffs
	schema.Workspaces[workspaceRoot] = merged

	// Bump LastUsed for the current workspace. If it was loaded this
	// process (or already has a future timestamp), use now; otherwise
	// preserve whatever was there (could be a newer timestamp from
	// another process).
	now := time.Now().Unix()
	if schema.LastUsed[workspaceRoot] < now {
		schema.LastUsed[workspaceRoot] = now
	}

	// LRU eviction: if we're over the cap, remove the oldest
	// workspaces until we're at the cap. Workspaces with no LastUsed
	// (missing/zero) evict first.
	if len(schema.Workspaces) > maxPersistedWorkspaces {
		evictOldestWorkspaces(&schema, len(schema.Workspaces)-maxPersistedWorkspaces)
	}

	out, marshalErr := json.MarshalIndent(&schema, "", "  ")
	if marshalErr != nil {
		return marshalErr
	}

	// Atomic write: temp file in same dir, then rename. Same pattern as
	// the embedding store's meta-file save.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".shell-skip-dirs-tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// evictOldestWorkspaces removes the n least-recently-used workspaces
// from schema.Workspaces and schema.LastUsed. Workspaces without a
// LastUsed (zero timestamp) evict first; ties broken by sorted
// workspace root for determinism.
func evictOldestWorkspaces(schema *shellSkipDirsFileSchema, n int) {
	if n <= 0 || len(schema.Workspaces) <= n {
		return
	}
	type entry struct {
		root     string
		lastUsed int64
	}
	entries := make([]entry, 0, len(schema.Workspaces))
	for root := range schema.Workspaces {
		entries = append(entries, entry{root: root, lastUsed: schema.LastUsed[root]})
	}
	// Sort ascending by lastUsed; ties broken by root name for
	// determinism (so eviction is reproducible across runs).
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].lastUsed != entries[j].lastUsed {
			return entries[i].lastUsed < entries[j].lastUsed
		}
		return entries[i].root < entries[j].root
	})
	for i := 0; i < n; i++ {
		delete(schema.Workspaces, entries[i].root)
		delete(schema.LastUsed, entries[i].root)
	}
}

// shellSkipDirsFilePath returns the absolute path of the
// shell_skip_dirs.json file inside the sprout config directory.
// Errors when no config directory can be determined (very rare —
// happens only when $HOME is unset and $XDG_CONFIG_HOME isn't either).
func shellSkipDirsFilePath() (string, error) {
	dir, err := configuration.GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, shellSkipDirsFilename), nil
}