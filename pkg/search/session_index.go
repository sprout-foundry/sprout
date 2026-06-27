// Package search provides cross-session search indexing capabilities.
// It builds an index of session conversations for fast semantic lookup
// without importing pkg/agent to avoid circular dependencies.
package search

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SessionIndex is the top-level search index structure.
type SessionIndex struct {
	Version  int                          `json:"version"`
	BuiltAt  time.Time                    `json:"built_at"`
	Sessions map[string]SessionIndexEntry `json:"sessions"`
}

// SessionIndexEntry holds indexed data for a single session.
type SessionIndexEntry struct {
	SessionID    string              `json:"session_id"`
	Name         string              `json:"name"`
	WorkingDir   string              `json:"working_directory"`
	LastUpdated  time.Time           `json:"last_updated"`
	TotalCost    float64             `json:"total_cost"`
	MessageCount int                 `json:"message_count"`
	Tokens       map[string][]int    `json:"tokens"` // [start, end] byte offsets in Text
	Text         string              `json:"text"`   // Concatenated user/assistant messages, lowercased
}

// ---------------------------------------------------------------------------
// Minimal session JSON structure — mirrors the shape written by the agent's
// persistence layer without importing pkg/agent (avoiding import cycles).
// ---------------------------------------------------------------------------

type sessionJSON struct {
	SessionID        string       `json:"session_id"`
	Name             string       `json:"name"`
	WorkingDirectory string       `json:"working_directory"`
	TotalCost        float64      `json:"total_cost"`
	Messages         []messageRef `json:"messages"`
}

type messageRef struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

// DefaultIndexPath returns the default location for the search index file.
// It returns ~/.sprout/sessions/search-index.json.  If $HOME cannot be
// determined the function returns an empty string and the caller should
// handle the error appropriately.
func DefaultIndexPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".sprout", "sessions", "search-index.json")
}

// LoadIndex reads and parses the search index from the given path.
//
// If the file does not exist a zero-value SessionIndex with an
// initialised (non-nil) Sessions map is returned — not an error.
// Malformed JSON returns the underlying parse error.
func LoadIndex(path string) (*SessionIndex, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &SessionIndex{Sessions: make(map[string]SessionIndexEntry)}, nil
		}
		return nil, fmt.Errorf("read index file %q: %w", path, err)
	}

	var idx SessionIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, fmt.Errorf("parse index file %q: %w", path, err)
	}
	if idx.Sessions == nil {
		idx.Sessions = make(map[string]SessionIndexEntry)
	}
	return &idx, nil
}

// SaveIndex atomically writes the search index to disk.
//
// The index is first written to path+".tmp" with 0600 permissions, synced
// to disk via Close(), then renamed into place.  Parent directories are
// created if they do not exist.  idx.BuiltAt is updated to the current
// time before serialisation.
func SaveIndex(path string, idx *SessionIndex) error {
	idx.BuiltAt = time.Now()

	// Ensure parent directories exist.
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("create parent directories for %q: %w", path, err)
	}

	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write temp index %q: %w", tmp, err)
	}

	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %q → %q: %w", tmp, path, err)
	}
	return nil
}

// BuildIndex walks the sessions directory and builds (or updates) the
// SessionIndex for every session_*.json file it finds.
//
// The walk is recursive to discover scoped sub-directories under the
// sessions base (e.g. ~/.sprout/sessions/scoped/<hash>/).
//
// Incremental update: if idx already has an entry for a session whose
// LastUpdated timestamp matches the file's mtime, that entry is kept
// without re-parsing.  Entries whose session files no longer exist on
// disk are removed from the index.
//
// idx.Version is set to 1 and idx.BuiltAt is updated to now.
func BuildIndex(sessionsDir string, idx *SessionIndex) (*SessionIndex, error) {
	if idx == nil {
		idx = &SessionIndex{Sessions: make(map[string]SessionIndexEntry)}
	}
	if idx.Sessions == nil {
		idx.Sessions = make(map[string]SessionIndexEntry)
	}

	files, err := WalkSessions(sessionsDir)
	if err != nil {
		return nil, err
	}

	// Track which session IDs we actually saw on disk.
	seen := make(map[string]bool, len(files))

	for _, f := range files {
		sessionID := extractSessionID(f)

		// File metadata for mtime comparison.
		fi, err := os.Stat(f)
		if err != nil {
			continue // skip unreadable files
		}
		mtime := fi.ModTime()

		// Incremental skip: identical mtime → keep cached entry.
		if existing, ok := idx.Sessions[sessionID]; ok && existing.LastUpdated.Equal(mtime) {
			seen[sessionID] = true
			continue
		}

		// Parse and index.
		entry, err := indexSessionFile(f, sessionID, mtime)
		if err != nil {
			continue // log & skip; don't fail the whole build
		}
		idx.Sessions[sessionID] = entry
		seen[sessionID] = true
	}

	// Drop entries whose files no longer exist.
	for sid := range idx.Sessions {
		if !seen[sid] {
			delete(idx.Sessions, sid)
		}
	}

	idx.Version = 1
	idx.BuiltAt = time.Now()

	return idx, nil
}

// WalkSessions returns a sorted list of all session_*.json files found
// under sessionsDir (recursive).  If the directory does not exist an
// empty slice is returned (not an error).
func WalkSessions(sessionsDir string) ([]string, error) {
	if _, err := os.Stat(sessionsDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat sessions dir %q: %w", sessionsDir, err)
	}

	var files []string
	err := filepath.WalkDir(sessionsDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, "session_") && strings.HasSuffix(name, ".json") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk sessions dir %q: %w", sessionsDir, err)
	}

	sort.Strings(files)
	return files, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// extractSessionID returns the session identifier from a filename like
// "session_foo123.json" → "foo123".
func extractSessionID(path string) string {
	name := filepath.Base(path)
	name = strings.TrimPrefix(name, "session_")
	name = strings.TrimSuffix(name, ".json")
	return name
}

// indexSessionFile reads a single session JSON file and produces an
// indexed entry.  mtime is used as LastUpdated (not the JSON field).
func indexSessionFile(path, sessionID string, mtime time.Time) (SessionIndexEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return SessionIndexEntry{}, fmt.Errorf("read session %q: %w", path, err)
	}

	var s sessionJSON
	if err := json.Unmarshal(data, &s); err != nil {
		return SessionIndexEntry{}, fmt.Errorf("parse session %q: %w", path, err)
	}

	// Derive name.
	name := s.Name
	if name == "" {
		name = sessionID
	}

	// Build concatenated text and token offsets for user/assistant messages.
	// Newline is used as a separator between messages (not a trailing terminator).
	var (
		sb     strings.Builder
		tokens = make(map[string][]int)
		msgIdx int  // counter of user/assistant messages only
		first  bool = true
	)
	for _, m := range s.Messages {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		if !first {
			sb.WriteString("\n")
		}
		first = false

		start := sb.Len()
		sb.WriteString(m.Content)
		end := sb.Len()
		key := fmt.Sprintf("%s:%d", sessionID, msgIdx)
		tokens[key] = []int{start, end}
		msgIdx++
	}

	text := strings.ToLower(sb.String())

	return SessionIndexEntry{
		SessionID:    sessionID,
		Name:         name,
		WorkingDir:   s.WorkingDirectory,
		LastUpdated:  mtime,
		TotalCost:    s.TotalCost,
		MessageCount: msgIdx,
		Tokens:       tokens,
		Text:         text,
	}, nil
}
