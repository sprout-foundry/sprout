// Package search provides debounced, incremental index updates triggered
// by session saves to avoid unnecessary disk thrash.

package search

import (
	"log"
	"sync"
	"time"
)

// IndexUpdater debounces index writes to avoid disk thrash.
type IndexUpdater struct {
	mu          sync.Mutex
	indexPath   string
	sessionsDir string
	pending     map[string]bool // sessionIDs needing update
	lastSaveAt  time.Time
	debounce    time.Duration   // default 5s
	timer       *time.Timer
	stopCh      chan struct{}
}

// NewIndexUpdater creates an updater that writes to indexPath from sessionsDir.
func NewIndexUpdater(indexPath, sessionsDir string) *IndexUpdater {
	return &IndexUpdater{
		indexPath:   indexPath,
		sessionsDir: sessionsDir,
		pending:     make(map[string]bool),
		debounce:    5 * time.Second,
		stopCh:      make(chan struct{}),
	}
}

// MarkDirty marks a session ID as needing an index update.
// Coalesces with previous uncommitted marks.
// Schedules a debounced rebuild.
func (u *IndexUpdater) MarkDirty(sessionID string) {
	u.mu.Lock()
	defer u.mu.Unlock()

	u.pending[sessionID] = true

	if u.timer == nil {
		u.timer = time.NewTimer(u.debounce)
		// Recreate stopCh — it may have been closed by a prior Stop/Flush.
		u.stopCh = make(chan struct{})
		go u.watchTimer()
	} else {
		u.timer.Reset(u.debounce)
	}
}

// watchTimer waits for the debounce timer to fire (or cancellation) then
// snapshots the pending set and runs the actual rebuild outside the lock.
//
// Channel references are captured under the lock to avoid racing with
// Flush/Stop. If Flush/Stop already ran (timer is nil), we return early
// since the rebuild was already handled.
func (u *IndexUpdater) watchTimer() {
	u.mu.Lock()

	// If timer is nil, Flush/Stop already ran and handled everything.
	if u.timer == nil {
		u.mu.Unlock()
		return
	}

	timerC := u.timer.C
	stopC := u.stopCh
	u.mu.Unlock()

	select {
	case <-timerC:
		u.mu.Lock()
		// Snapshot pending & clear.
		pending := make([]string, 0, len(u.pending))
		for sid := range u.pending {
			pending = append(pending, sid)
		}
		u.pending = make(map[string]bool)
		u.timer = nil
		u.mu.Unlock()

		// Do the actual rebuild+save outside the lock.
		if err := u.rebuildAndSave(); err != nil {
			log.Printf("search: incremental rebuild failed: %v", err)
		}

	case <-stopC:
		// Cancelled — do nothing.
	}
}

// rebuildAndSave loads the index, builds it, then saves it back.
func (u *IndexUpdater) rebuildAndSave() error {
	idx, err := LoadIndex(u.indexPath)
	if err != nil {
		idx = &SessionIndex{Sessions: make(map[string]SessionIndexEntry)}
	}
	_, err = BuildIndex(u.sessionsDir, idx)
	if err != nil {
		return err
	}
	if err = SaveIndex(u.indexPath, idx); err != nil {
		return err
	}
	u.mu.Lock()
	u.lastSaveAt = time.Now()
	u.mu.Unlock()
	return nil
}

// Flush forces an immediate rebuild + save (e.g. on shutdown).
func (u *IndexUpdater) Flush() error {
	u.mu.Lock()
	// Stop the timer to prevent the goroutine from also running.
	if u.timer != nil {
		u.timer.Stop()
		u.timer = nil
	}
	// Snapshot pending & clear.
	pending := make([]string, 0, len(u.pending))
	for sid := range u.pending {
		pending = append(pending, sid)
	}
	u.pending = make(map[string]bool)
	// Signal the goroutine to stop (if it's still waiting).
	if u.stopCh != nil {
		close(u.stopCh)
		u.stopCh = nil
	}
	u.mu.Unlock()

	if len(pending) == 0 {
		return nil
	}

	return u.rebuildAndSave()
}

// Stop cancels any pending timer.
func (u *IndexUpdater) Stop() {
	u.mu.Lock()
	defer u.mu.Unlock()

	if u.timer != nil {
		u.timer.Stop()
		u.timer = nil
	}
	if u.stopCh != nil {
		close(u.stopCh)
		u.stopCh = nil
	}
}

// ---------------------------------------------------------------------------
// Global accessor (sync.Once)
// ---------------------------------------------------------------------------

var (
	globalUpdaterOnce sync.Once
	GlobalUpdater     *IndexUpdater
)

// InitGlobalUpdater lazily initializes the process-global IndexUpdater.
// Subsequent calls are no-ops (sync.Once).
func InitGlobalUpdater(indexPath, sessionsDir string) {
	globalUpdaterOnce.Do(func() {
		GlobalUpdater = NewIndexUpdater(indexPath, sessionsDir)
	})
}

// MarkSessionDirty is a convenience wrapper that calls GlobalUpdater.MarkDirty.
// If the global updater hasn't been initialized yet, it does nothing (safe no-op).
func MarkSessionDirty(sessionID string) {
	if GlobalUpdater != nil {
		GlobalUpdater.MarkDirty(sessionID)
	}
}
