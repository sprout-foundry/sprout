// Package tools provides the interface-based tool system for the Sprout AI agent.
//
// This file defines the workspace sync model types used by the browser-primary
// synchronization protocol between OPFS (browser) and the container filesystem.
// See roadmap/SP-046-workspace-sync-model.md for the full specification.

package tools

import (
	"fmt"
	"sync"
	"time"
)

// FileMetadata tracks sync state for a single file between browser (OPFS) and
// container replicas. Both sides hold their own sequence counters; the last
// synced counters record what has been reconciled in each direction.
//
// @ts-generated — consumed by the frontend to generate a TypeScript interface.
type FileMetadata struct {
	// BrowserSeq is the latest browser-originated sequence number for this file.
	// Bumped each time the user makes an edit in the browser editor.
	BrowserSeq int64 `json:"browser_seq"`

	// ContainerSeq is the latest container-originated sequence number for this file.
	// Bumped each time the agent writes to this file via a tool call.
	ContainerSeq int64 `json:"container_seq"`

	// LastSyncedBrowser is the browser_seq value that the container has last
	// observed. When BrowserSeq > LastSyncedBrowser, the browser has unsynced
	// edits from the container's perspective.
	LastSyncedBrowser int64 `json:"last_synced_browser"`

	// LastSyncedContainer is the container_seq value that the browser has last
	// observed. When ContainerSeq > LastSyncedContainer, the container has
	// unsynced writes from the browser's perspective.
	LastSyncedContainer int64 `json:"last_synced_container"`

	// ModifiedAt is the last time any sync-relevant change occurred for this file.
	ModifiedAt time.Time `json:"modified_at"`
}

// PatchEvent represents a file change from one replica to the other.
//
// @ts-generated — consumed by the frontend to generate a TypeScript interface.
type PatchEvent struct {
	// Path is the workspace-relative file path (e.g. "pkg/foo/bar.go").
	Path string `json:"path"`

	// ContainerSeq is the new container sequence number for this file after the
	// agent's write.
	ContainerSeq int64 `json:"container_seq"`

	// Content is the full file content after the agent's write. For the first
	// pass, patches are whole-file replaces.
	Content string `json:"content"`

	// BaseBrowserSeq is the browser_seq value the container observed before
	// applying this write. Used for staleness detection.
	BaseBrowserSeq int64 `json:"base_browser_seq"`
}

// SyncState is the per-file metadata store, protected by a mutex.
// It lives in-process on the server side to track sequence numbers for each
// workspace file during a session.
type SyncState struct {
	mu    sync.RWMutex
	files map[string]*FileMetadata
}

// NewSyncState creates a new empty SyncState ready for use.
func NewSyncState() *SyncState {
	return &SyncState{
		files: make(map[string]*FileMetadata),
	}
}

// getOrCreate returns the FileMetadata for the given path, creating a new
// entry if none exists.
//
// LOCKS_REQUIRED: mu (write lock) — the caller must hold ss.mu locked for
// writing before calling this method. It performs read-then-write on the map
// and is not safe to call concurrently with any other map access.
func (ss *SyncState) getOrCreate(path string) *FileMetadata {
	m, ok := ss.files[path]
	if !ok {
		m = &FileMetadata{
			BrowserSeq:        0,
			ContainerSeq:      0,
			LastSyncedBrowser: 0,
			// LastSyncedContainer starts at 0 — meaning the browser has seen
			// nothing from the container yet.
			LastSyncedContainer: 0,
			ModifiedAt: time.Now(),
		}
		ss.files[path] = m
	}
	return m
}

// GetMetadata looks up metadata for a path. Returns nil and false if not found.
func (ss *SyncState) GetMetadata(path string) (*FileMetadata, bool) {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	m, ok := ss.files[path]
	if !ok {
		return nil, false
	}
	// Return a copy so the caller can read safely without holding the lock.
	cp := *m
	return &cp, true
}

// UpdateContainerPatch applies a container→browser patch event for the given path.
// This is called when the agent writes to a file and the server needs to notify
// the browser of the change.
//
// Returns an error if the browser has unsynced edits (BrowserSeq > LastSyncedBrowser),
// indicating a conflict that the caller must resolve (e.g., by surfacing a
// ".theirs" file to the user).
func (ss *SyncState) UpdateContainerPatch(path string, event *PatchEvent) (*FileMetadata, error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	m := ss.getOrCreate(path)

	// Conflict check: if the browser has edits the container hasn't seen yet,
	// refuse the write to avoid overwriting user work.
	if m.BrowserSeq > m.LastSyncedBrowser {
		cp := *m
		return &cp, fmt.Errorf(
			"user has unsynced edits to %s (browser_seq=%d > last_synced_browser=%d), ask before overwriting",
			path, m.BrowserSeq, m.LastSyncedBrowser,
		)
	}

	m.ContainerSeq = event.ContainerSeq
	m.LastSyncedContainer = event.ContainerSeq
	m.ModifiedAt = time.Now()

	cp := *m
	return &cp, nil
}

// ApplyBrowserOp applies a browser→container operation (user edit synced to the
// container). Bumps the browser sequence and acknowledges the container as
// current.
func (ss *SyncState) ApplyBrowserOp(path string, content string) (*FileMetadata, error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()

	m := ss.getOrCreate(path)

	m.BrowserSeq++
	m.LastSyncedBrowser = m.BrowserSeq
	// After applying the browser op, the container is considered up-to-date
	// with the browser (assuming immediate apply).
	m.ContainerSeq = m.BrowserSeq
	m.ModifiedAt = time.Now()

	cp := *m
	return &cp, nil
}

// GetAllMetadata returns a snapshot copy of all metadata entries. The returned
// map and its values are independent copies; mutations will not affect the
// internal state.
func (ss *SyncState) GetAllMetadata() map[string]*FileMetadata {
	ss.mu.RLock()
	defer ss.mu.RUnlock()

	result := make(map[string]*FileMetadata, len(ss.files))
	for path, m := range ss.files {
		cp := *m
		result[path] = &cp
	}
	return result
}
