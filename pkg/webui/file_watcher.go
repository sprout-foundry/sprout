package webui

import (
	"context"
	"log"
	"os"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/events"
	"github.com/fsnotify/fsnotify"
)

const (
	fileWatcherDebounceInterval = 2 * time.Second
	fileWatcherStaleThreshold   = 60 * time.Second
	fileWatcherCleanupInterval  = 30 * time.Second
)

// watchEntry holds bookkeeping for a single watched file path.
type watchEntry struct {
	lastSeen    time.Time
	displayPath string // relative path used in event publishing
}

// fileWatcher monitors open files for external changes using fsnotify and
// publishes file_content_changed events via the event bus so the WebUI can
// react immediately (in addition to the 3-second polling fallback).
type fileWatcher struct {
	eventBus  *events.EventBus
	fsWatcher *fsnotify.Watcher
	watches   map[string]watchEntry // canonical (absolute) path → entry
	debounces map[string]time.Time   // path → last event emit time
	mu        sync.Mutex
	cancel    context.CancelFunc
}

// newFileWatcher creates a file watcher that publishes events to the given bus.
func newFileWatcher(eventBus *events.EventBus) *fileWatcher {
	fw := &fileWatcher{
		eventBus:  eventBus,
		watches:   make(map[string]watchEntry),
		debounces: make(map[string]time.Time),
	}
	return fw
}

// start begins watching for filesystem events. It must be called exactly once.
func (fw *fileWatcher) start(ctx context.Context) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	if fw.cancel != nil {
		return // already started
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("[filewatcher] WARNING: cannot create fsnotify watcher, real-time change notifications disabled: %v", err)
		return
	}
	fw.fsWatcher = w

	ctx, cancel := context.WithCancel(ctx)
	fw.cancel = cancel

	// Drain the fsnotify error channel so it doesn't block.
	go func() {
		for err := range w.Errors {
			log.Printf("[filewatcher] fsnotify error: %v", err)
		}
	}()

	go fw.eventLoop(ctx)
	go fw.cleanupLoop(ctx)
}

// watch registers a file path for monitoring. Each call refreshes the
// registration timestamp; paths not re-registered within staleThreshold are
// automatically removed by the cleanup loop. The canonicalPath is used for
// fsnotify operations and map lookups; displayPath is used in published
// event data (typically the relative path the frontend uses).
func (fw *fileWatcher) watch(canonicalPath, displayPath string) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	if fw.fsWatcher == nil {
		return
	}

	now := time.Now()
	_, alreadyWatched := fw.watches[canonicalPath]
	fw.watches[canonicalPath] = watchEntry{lastSeen: now, displayPath: displayPath}

	if !alreadyWatched {
		if err := fw.fsWatcher.Add(canonicalPath); err != nil {
			log.Printf("[filewatcher] failed to add watch for %s: %v", canonicalPath, err)
		}
	}
}

// stop shuts down the watcher and releases all resources.
func (fw *fileWatcher) stop() {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	if fw.cancel != nil {
		fw.cancel()
		fw.cancel = nil
	}

	if fw.fsWatcher != nil {
		_ = fw.fsWatcher.Close()
		fw.fsWatcher = nil
	}
}

// watchedCount returns the number of currently registered watch paths.
func (fw *fileWatcher) watchedCount() int {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	return len(fw.watches)
}

// eventLoop reads fsnotify events, deduplicates them, and publishes
// file_content_changed events via the event bus.
func (fw *fileWatcher) eventLoop(ctx context.Context) {
	eventsCh := fw.fsWatcher.Events // captured before any potential close

	for {
		select {
		case <-ctx.Done():
			return
		case fse, ok := <-eventsCh:
			if !ok {
				return
			}

			// We only care about meaningful change events.
			if fse.Op&(fsnotify.Create|fsnotify.Write|fsnotify.Rename|fsnotify.Remove) == 0 {
				continue
			}

			fw.handleFileEvent(fse)
		}
	}
}

// handleFileEvent processes a single fsnotify event with debouncing.
func (fw *fileWatcher) handleFileEvent(fse fsnotify.Event) {
	path := fse.Name

	fw.mu.Lock()
	now := time.Now()
	lastEmit, wasDebounced := fw.debounces[path]
	if wasDebounced && now.Sub(lastEmit) < fileWatcherDebounceInterval {
		fw.mu.Unlock()
		return
	}
	fw.debounces[path] = now
	entry, exists := fw.watches[path]
	fw.mu.Unlock()

	// Use the display path (relative) for event data if available.
	publishPath := path
	if exists && entry.displayPath != "" {
		publishPath = entry.displayPath
	}

	if fse.Op&fsnotify.Remove != 0 {
		// File was removed — emit with zeroed metadata.
		data := events.FileContentChangedEvent(publishPath, 0, 0)
		data["deleted"] = true
		fw.eventBus.Publish(events.EventTypeFileContentChanged, data)
		return
	}

	// Stat the file to get current metadata.
	info, err := os.Stat(path)
	if err != nil {
		// File may have been removed or become inaccessible between the
		// fsnotify event and the stat call. Treat as deleted.
		data := events.FileContentChangedEvent(publishPath, 0, 0)
		data["deleted"] = true
		fw.eventBus.Publish(events.EventTypeFileContentChanged, data)
		return
	}

	if info.IsDir() {
		return
	}

	fw.eventBus.Publish(events.EventTypeFileContentChanged,
		events.FileContentChangedEvent(publishPath, info.ModTime().Unix(), info.Size()))
}

// cleanupLoop periodically removes fsnotify watches for paths that have not
// been refreshed (via watch()) within staleThreshold.
func (fw *fileWatcher) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(fileWatcherCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fw.cleanup()
		}
	}
}

// cleanup removes stale watches from the fsnotify watcher and the watches map.
func (fw *fileWatcher) cleanup() {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	if fw.fsWatcher == nil {
		return
	}

	now := time.Now()
	for path, entry := range fw.watches {
		if now.Sub(entry.lastSeen) > fileWatcherStaleThreshold {
			if err := fw.fsWatcher.Remove(path); err != nil {
				log.Printf("[filewatcher] failed to remove watch for %s: %v", path, err)
			}
			delete(fw.watches, path)
			delete(fw.debounces, path)
		}
	}
}
