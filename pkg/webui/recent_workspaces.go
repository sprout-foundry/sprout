//go:build !js

package webui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// RecentWorkspace tracks a workspace that was recently used.
type RecentWorkspace struct {
	Path         string    `json:"path"`
	Name         string    `json:"name"`
	LastUsed     time.Time `json:"last_used"`
	Markers      []string  `json:"markers,omitempty"`
	SessionCount int       `json:"session_count"`
}

const maxRecentWorkspaces = 10

// recentWorkspacesState holds the in-memory state for recent workspace tracking.
type recentWorkspacesState struct {
	mu         sync.RWMutex
	workspaces []RecentWorkspace
	filePath   string
}

var recentWorkspaces = &recentWorkspacesState{}

// initRecentWorkspaces loads recent workspaces from disk.
// Call this once during server startup.
//
// An already-configured path is left alone. Every ReactWebServer construction
// calls this, so unconditionally re-deriving the path from os.UserHomeDir()
// stomped the temp file that tests point the global at — which is how test
// workspace paths ended up in the user's real recent-workspaces file and
// evicted their actual projects from the picker.
func initRecentWorkspaces() {
	recentWorkspaces.mu.Lock()
	if recentWorkspaces.filePath == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			recentWorkspaces.mu.Unlock()
			return
		}
		recentWorkspaces.filePath = filepath.Join(homeDir, ".sprout", "recent_workspaces.json")
	}
	recentWorkspaces.mu.Unlock()

	recentWorkspaces.load()
}

// load reads the recent-workspace list from disk. It takes the lock itself —
// callers must not hold it. (save, by contrast, is only ever reached from
// RecordWorkspace with the lock already held.)
func (s *recentWorkspacesState) load() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.filePath == "" {
		return
	}
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, &s.workspaces); err != nil {
		s.workspaces = nil
	}
}

func (s *recentWorkspacesState) save() {
	if s.filePath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0700); err != nil {
		return
	}
	data, err := json.MarshalIndent(s.workspaces, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(s.filePath, data, 0600)
}

// RecordWorkspace records a workspace as recently used.
func RecordWorkspace(path string) {
	s := recentWorkspaces
	s.mu.Lock()
	defer s.mu.Unlock()

	path = filepath.Clean(path)
	name := filepath.Base(path)

	// Check if already tracked
	for i, w := range s.workspaces {
		if w.Path == path {
			s.workspaces[i].LastUsed = time.Now()
			s.workspaces[i].SessionCount++
			s.workspaces[i].Name = name
			s.save()
			return
		}
	}

	// Detect markers
	_, markers := IsProjectDirectory(path)

	// Add new entry
	ew := RecentWorkspace{
		Path:         path,
		Name:         name,
		LastUsed:     time.Now(),
		Markers:      markers,
		SessionCount: 1,
	}
	s.workspaces = append([]RecentWorkspace{ew}, s.workspaces...)

	// Trim to max
	if len(s.workspaces) > maxRecentWorkspaces {
		s.workspaces = s.workspaces[:maxRecentWorkspaces]
	}

	s.save()
}

// GetRecentWorkspaces returns up to 10 recently used workspaces that still
// exist on disk.
//
// Entries are dropped rather than shown as dead links: a workspace can be
// renamed, deleted, or (as happened in practice) recorded against a temp
// directory that no longer exists, and offering those in the picker gives the
// user a list they cannot act on and cannot distinguish from real projects.
func GetRecentWorkspaces() []RecentWorkspace {
	s := recentWorkspaces
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]RecentWorkspace, 0, len(s.workspaces))
	for _, w := range s.workspaces {
		if info, err := os.Stat(w.Path); err == nil && info.IsDir() {
			result = append(result, w)
		}
	}
	return result
}

// GetMostRecentWorkspace returns the most recently used workspace path, or "" if none.
func GetMostRecentWorkspace() string {
	s := recentWorkspaces
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.workspaces) == 0 {
		return ""
	}
	return s.workspaces[0].Path
}
