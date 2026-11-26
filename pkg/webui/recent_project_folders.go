//go:build !js

package webui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// RecentProjectFolder tracks a project folder that was recently used.
type RecentProjectFolder struct {
	Path         string    `json:"path"`
	Name         string    `json:"name"`
	LastUsed     time.Time `json:"last_used"`
	Markers      []string  `json:"markers,omitempty"`
	SessionCount int       `json:"session_count"`
}

const maxRecentProjectFolders = 10

// recentProjectFoldersState holds the in-memory state for recent project folder tracking.
type recentProjectFoldersState struct {
	mu             sync.RWMutex
	projectFolders []RecentProjectFolder
	filePath       string
}

var recentProjectFolders = &recentProjectFoldersState{}

// initRecentProjectFolders loads recent project folders from disk.
// Call this once during server startup.
//
// An already-configured path is left alone. Every ReactWebServer construction
// calls this, so unconditionally re-deriving the path from os.UserHomeDir()
// stomped the temp file that tests point the global at — which is how test
// project folder paths ended up in the user's real recent-project-folders file and
// evicted their actual projects from the picker.
func initRecentProjectFolders() {
	recentProjectFolders.mu.Lock()
	if recentProjectFolders.filePath == "" {
		stateDir, err := envutil.StateDir()
		if err == nil {
			recentProjectFolders.filePath = filepath.Join(stateDir, "recent_project_folders.json")
		}
	}
	recentProjectFolders.mu.Unlock()

	recentProjectFolders.load()
}

// load reads the recent-project-folder list from disk. It takes the lock itself —
// callers must not hold it. (save, by contrast, is only ever reached from
// RecordProjectFolder with the lock already held.)
func (s *recentProjectFoldersState) load() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.filePath == "" {
		return
	}
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, &s.projectFolders); err != nil {
		s.projectFolders = nil
	}
}

func (s *recentProjectFoldersState) save() {
	if s.filePath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.filePath), 0700); err != nil {
		return
	}
	data, err := json.MarshalIndent(s.projectFolders, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(s.filePath, data, 0600)
}

// RecordProjectFolder records a project folder as recently used.
func RecordProjectFolder(path string) {
	s := recentProjectFolders
	s.mu.Lock()
	defer s.mu.Unlock()

	path = filepath.Clean(path)
	name := filepath.Base(path)

	// Check if already tracked
	for i, w := range s.projectFolders {
		if w.Path == path {
			s.projectFolders[i].LastUsed = time.Now()
			s.projectFolders[i].SessionCount++
			s.projectFolders[i].Name = name
			s.save()
			return
		}
	}

	// Detect markers
	_, markers := IsProjectDirectory(path)

	// Add new entry
	ew := RecentProjectFolder{
		Path:         path,
		Name:         name,
		LastUsed:     time.Now(),
		Markers:      markers,
		SessionCount: 1,
	}
	s.projectFolders = append([]RecentProjectFolder{ew}, s.projectFolders...)

	// Trim to max
	if len(s.projectFolders) > maxRecentProjectFolders {
		s.projectFolders = s.projectFolders[:maxRecentProjectFolders]
	}

	s.save()
}

// GetRecentProjectFolders returns up to 10 recently used project folders that still
// exist on disk.
//
// Entries are dropped rather than shown as dead links: a project folder can be
// renamed, deleted, or (as happened in practice) recorded against a temp
// directory that no longer exists, and offering those in the picker gives the
// user a list they cannot act on and cannot distinguish from real projects.
func GetRecentProjectFolders() []RecentProjectFolder {
	s := recentProjectFolders
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]RecentProjectFolder, 0, len(s.projectFolders))
	for _, w := range s.projectFolders {
		if info, err := os.Stat(w.Path); err == nil && info.IsDir() {
			result = append(result, w)
		}
	}
	return result
}

// GetMostRecentProjectFolder returns the most recently used project folder path, or "" if none.
func GetMostRecentProjectFolder() string {
	s := recentProjectFolders
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.projectFolders) == 0 {
		return ""
	}
	return s.projectFolders[0].Path
}
