//go:build js && wasm

package main

import (
	"encoding/json"
	"sync"
	"syscall/js"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// replicaFileEntry wraps WorkspaceFileMetadata with the file path and
// optional base64-encoded content so the replica is a fully self-contained
// mirror of OPFS state.
type replicaFileEntry struct {
	Path     string // file path (key in the map)
	Metadata agent.WorkspaceFileMetadata
	Size     int64  // file size in bytes
	Content  string // base64-encoded content (optional)
}

var (
	opfsReplicaMu       sync.Mutex
	opfsReplicaFiles    = make(map[string]*replicaFileEntry)
	opfsReplicaLastSync time.Time
)

// HydrateProgressState tracks the progress of an in-flight cold hydration.
type HydrateProgressState struct {
	TotalFiles       int64     `json:"total_files"`
	TotalSize        int64     `json:"total_size"`
	FilesReceived    int64     `json:"files_received"`
	BytesReceived    int64     `json:"bytes_received"`
	EstimatedSeconds int64     `json:"estimated_seconds"`
	Completed        bool      `json:"completed"`
	StartTime        time.Time `json:"start_time,omitempty"`
}

var (
	hydrateProgress   HydrateProgressState
	hydrateProgressMu sync.Mutex
)

// ─── Registration ────────────────────────────────────────────────────────

// opfsReplicaJSFuncs returns the OPFS-replica JS bridge functions so
// sync_funcs.go can merge them into the shared export map.
func opfsReplicaJSFuncs() map[string]interface{} {
	return map[string]interface{}{
		"initOPFSReplica":        js.FuncOf(initOPFSReplicaFunc),
		"getOPFSReplicaStatus":   js.FuncOf(getOPFSReplicaStatusFunc),
		"syncOPFSReplica":        js.FuncOf(syncOPFSReplicaFunc),
		"getOPFSFile":            js.FuncOf(getOPFSFileFunc),
		"storeReplicaMetadata":   js.FuncOf(storeReplicaMetadataFunc),
		"processHydrateManifest": js.FuncOf(processHydrateManifestFunc),
		"processHydrateFile":     js.FuncOf(processHydrateFileFunc),
		"processHydrateComplete": js.FuncOf(processHydrateCompleteFunc),
		"getHydrateProgress":     js.FuncOf(getHydrateProgressFunc),

		// SP-046 sync recovery (server-side failure recovery paths)
		"initSyncRecovery":        js.FuncOf(initSyncRecoveryFunc),
		"handleSyncReconcile":     js.FuncOf(handleSyncReconcileFunc),
		"recoverFromBrowserCrash": js.FuncOf(recoverFromBrowserCrashFunc),
	}
}

// ─── initOPFSReplica ────────────────────────────────────────────────────

// manifestEntry represents a single entry in the browser-provided manifest.
// The browser serialises the path alongside WorkspaceFileMetadata because
// the struct itself carries no path.
type manifestEntry struct {
	Path string `json:"path"`
	Size int64  `json:"size,omitempty"`
	agent.WorkspaceFileMetadata
}

// initOPFSReplicaFunc initialises the replica from a JSON manifest produced
// by the browser-side IndexedDB store.  The manifest is a JSON array of
// objects, each containing a "path" field and WorkspaceFileMetadata fields.
//
// Signature: initOPFSReplica(manifestJSON: string): {ok, fileCount, totalSize}
func initOPFSReplicaFunc(_ js.Value, args []js.Value) interface{} {
	if len(args) < 1 {
		return map[string]interface{}{"error": "missing manifest argument"}
	}
	manifestJSON := args[0].String()
	if manifestJSON == "" {
		return map[string]interface{}{"error": "empty manifest"}
	}

	var manifest []manifestEntry
	if err := json.Unmarshal([]byte(manifestJSON), &manifest); err != nil {
		return map[string]interface{}{"error": "invalid manifest JSON: " + err.Error()}
	}

	opfsReplicaMu.Lock()
	defer opfsReplicaMu.Unlock()

	opfsReplicaFiles = make(map[string]*replicaFileEntry, len(manifest))
	var totalSize int64
	for i := range manifest {
		e := manifest[i]
		entry := &replicaFileEntry{
			Path:     e.Path,
			Metadata: e.WorkspaceFileMetadata,
			Size:     e.Size,
		}
		opfsReplicaFiles[e.Path] = entry
		totalSize += e.Size
	}
	opfsReplicaLastSync = time.Now()

	return map[string]interface{}{
		"ok":        true,
		"fileCount": len(manifest),
		"totalSize": totalSize,
	}
}

// ─── getOPFSReplicaStatus ───────────────────────────────────────────────

// getOPFSReplicaStatusFunc returns high-level replica statistics.
//
// Signature: getOPFSReplicaStatus(): {ok, fileCount, totalSize, lastSyncTimestamp}
func getOPFSReplicaStatusFunc(_ js.Value, args []js.Value) interface{} {
	opfsReplicaMu.Lock()
	defer opfsReplicaMu.Unlock()

	var totalSize int64
	for _, e := range opfsReplicaFiles {
		totalSize += e.Size
	}

	// A replica that has never synced has no timestamp, not year 1.
	lastSync := ""
	if !opfsReplicaLastSync.IsZero() {
		lastSync = opfsReplicaLastSync.Format(time.RFC3339)
	}
	return map[string]interface{}{
		"ok":                true,
		"fileCount":         len(opfsReplicaFiles),
		"totalSize":         totalSize,
		"lastSyncTimestamp": lastSync,
	}
}

// ─── syncOPFSReplica ────────────────────────────────────────────────────

// patchEvent represents a single workspace-patch event pushed by the browser.
type patchEvent struct {
	Op            string                       `json:"op"`
	Path          string                       `json:"path"`
	ContentBase64 string                       `json:"content_base64,omitempty"`
	Metadata      *agent.WorkspaceFileMetadata `json:"metadata,omitempty"`
}
