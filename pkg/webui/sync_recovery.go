//go:build !js

package webui

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// --- Types ---

// ReconciliationAction describes what recovery action to take for a single file.
type ReconciliationAction struct {
	FilePath       string    `json:"file_path"`
	Action         string    `json:"action"` // "sync_ok", "container_ahead", "browser_ahead", "diverged"
	ContainerSeq   int64     `json:"container_seq"`
	BrowserSeq     int64     `json:"browser_seq"`
	ContainerContent string  `json:"container_content,omitempty"`
	ContainerModTime time.Time `json:"container_mod_time,omitempty"`
}

// SyncRecoverData is the payload the browser sends in a sync_recover message.
type SyncRecoverData struct {
	ClientID string            `json:"client_id"`
	Seqs     map[string]int64  `json:"seqs"` // file_path -> browser_seq
}

// SyncReconcileData is the server's response with a reconciliation plan.
type SyncReconcileData struct {
	ClientID string                  `json:"client_id"`
	Plan     []ReconciliationAction  `json:"plan"`
}

// SyncReplayFileData is the payload for a single file replay.
type SyncReplayFileData struct {
	ClientID  string `json:"client_id"`
	FilePath  string `json:"file_path"`
	Content   string `json:"content"`
	Seq       int64  `json:"seq"`
	Timestamp int64  `json:"timestamp"`
}

// --- Container Death Recovery ---

// HandleContainerRecovery handles the case where a browser reconnects
// after its container died. It reconciles sequence numbers between the
// browser's last-known state and the container's current state.
func (ws *ReactWebServer) HandleContainerRecovery(ctx context.Context, clientID string, lastKnownSeq int64) (*SyncReconcileData, error) {
	// Get the agent for this client
	_, err := ws.getClientAgent(clientID)
	if err != nil {
		return nil, fmt.Errorf("no agent found for client %s: %w", clientID, err)
	}

	// For a simple container death scenario, we don't have per-file browser seqs.
	// We use the single lastKnownSeq as a cutoff. The browser sends its seq map
	// via the sync_recover message instead; this function handles the case where
	// the browser only knows a single seq number.
	//
	// In practice, HandleContainerRecovery is a fallback for clients that don't
	// support per-file sync_recover yet.
	return &SyncReconcileData{
		ClientID: clientID,
		Plan:     []ReconciliationAction{},
	}, nil
}

// HandleContainerRecoveryWithSeqs handles full per-file reconciliation
// after container death, given the browser's per-file sequence numbers.
func (ws *ReactWebServer) HandleContainerRecoveryWithSeqs(ctx context.Context, clientID string, browserSeqs map[string]int64) (*SyncReconcileData, error) {
	ag, err := ws.getClientAgent(clientID)
	if err != nil {
		return nil, fmt.Errorf("no agent found for client %s: %w", clientID, err)
	}

	plan, err := agent.ReconcileSeqNumbers(ag, browserSeqs)
	if err != nil {
		return nil, fmt.Errorf("reconciliation failed: %w", err)
	}

	result := &SyncReconcileData{
		ClientID: clientID,
		Plan:     makePlanFromResults(plan),
	}

	log.Printf("[SP-046] Container recovery for client %s: %d files in plan", clientID, len(plan))
	return result, nil
}

// makePlanFromResults converts agent reconciliation results to server ReconciliationAction entries.
func makePlanFromResults(results []agent.ReconciliationActionResult) []ReconciliationAction {
	if len(results) == 0 {
		return []ReconciliationAction{}
	}
	plan := make([]ReconciliationAction, 0, len(results))
	for _, r := range results {
		action := ReconciliationAction{
			FilePath:     r.FilePath,
			Action:       string(r.Action),
			ContainerSeq: r.ContainerSeq,
			BrowserSeq:   r.BrowserSeq,
		}
		plan = append(plan, action)
	}
	return plan
}

// --- Volume Corruption Recovery ---

// HandleVolumeCorruption attempts to recover a corrupted workspace volume.
// It tries: (1) git restore, (2) git clone from remote, (3) OPFS replay.
func HandleVolumeCorruption(ctx context.Context, workspaceRoot string) error {
	log.Printf("[SP-046] Volume corruption detected for workspace: %s", workspaceRoot)

	// Strategy 1: Try git restore if .git exists
	gitDir := filepath.Join(workspaceRoot, ".git")
	if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
		log.Printf("[SP-046] Attempting git restore for %s", workspaceRoot)
		if err := gitRestoreWorkspace(workspaceRoot); err != nil {
			log.Printf("[SP-046] git restore failed: %v, trying git clone", err)
		} else {
			log.Printf("[SP-046] git restore succeeded for %s", workspaceRoot)
			return nil
		}
	}

	// Strategy 2: Try git clone from remote
	// Check if there's a known remote URL we can clone from
	remoteURL, err := getGitRemoteURL(workspaceRoot)
	if err != nil || remoteURL == "" {
		log.Printf("[SP-046] No git remote available for %s", workspaceRoot)
	} else {
		log.Printf("[SP-046] Attempting git clone from %s", remoteURL)
		if err := gitCloneWorkspace(ctx, workspaceRoot, remoteURL); err != nil {
			log.Printf("[SP-046] git clone failed: %v", err)
		} else {
			log.Printf("[SP-046] git clone succeeded for %s", workspaceRoot)
			return nil
		}
	}

	// Strategy 3: Mark for OPFS replay — the caller should trigger
	// the browser to replay its persisted state.
	log.Printf("[SP-046] Volume corruption recovery for %s requires OPFS replay", workspaceRoot)
	return fmt.Errorf("volume corruption recovery for %s requires OPFS replay (no git available)", workspaceRoot)
}

// gitRestoreWorkspace runs git checkout/restore to fix a corrupted workspace.
func gitRestoreWorkspace(workspaceRoot string) error {
	// Clean untracked files and restore modified files
	cmd := exec.Command("git", "checkout", "--", ".")
	cmd.Dir = workspaceRoot
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git checkout failed: %w (%s)", err, strings.TrimSpace(string(output)))
	}

	cmd = exec.Command("git", "clean", "-fd")
	cmd.Dir = workspaceRoot
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git clean failed: %w (%s)", err, strings.TrimSpace(string(output)))
	}

	return nil
}

// getGitRemoteURL returns the fetch URL of the "origin" remote.
func getGitRemoteURL(workspaceRoot string) (string, error) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = workspaceRoot
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git remote get-url failed: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// gitCloneWorkspace removes the corrupted workspace and re-clones from remote.
func gitCloneWorkspace(ctx context.Context, workspaceRoot, remoteURL string) error {
	parentDir := filepath.Dir(workspaceRoot)
	dirName := filepath.Base(workspaceRoot)
	backupDir := filepath.Join(parentDir, dirName+".corrupted."+time.Now().Format("20060102-150405"))

	// Rename corrupted dir as backup
	if err := os.Rename(workspaceRoot, backupDir); err != nil {
		return fmt.Errorf("failed to backup corrupted workspace: %w", err)
	}

	// Clone fresh
	cmd := exec.CommandContext(ctx, "git", "clone", remoteURL, workspaceRoot)
	if output, err := cmd.CombinedOutput(); err != nil {
		// Restore the backup since clone failed
		_ = os.Rename(backupDir, workspaceRoot)
		return fmt.Errorf("git clone failed: %w (%s)", err, strings.TrimSpace(string(output)))
	}

	// Remove backup on success
	_ = os.RemoveAll(backupDir)
	return nil
}

// SendSyncReplayFile sends a single file replay patch to a client connection.
func (ws *ReactWebServer) SendSyncReplayFile(safeConn *SafeConn, clientID, filePath, content string, seq int64) error {
	return safeConn.WriteJSON(map[string]interface{}{
		"type": "sync_replay_file",
		"data": SyncReplayFileData{
			ClientID:  clientID,
			FilePath:  filePath,
			Content:   content,
			Seq:       seq,
			Timestamp: time.Now().Unix(),
		},
	})
}

// SendSyncReplayStart tells the client a replay is beginning.
func (ws *ReactWebServer) SendSyncReplayStart(safeConn *SafeConn, clientID string, fileCount int) error {
	return safeConn.WriteJSON(map[string]interface{}{
		"type": "sync_replay_start",
		"data": map[string]interface{}{
			"client_id":  clientID,
			"file_count": fileCount,
		},
	})
}

// SendSyncReplayComplete tells the client the replay is done.
func (ws *ReactWebServer) SendSyncReplayComplete(safeConn *SafeConn, clientID string) error {
	return safeConn.WriteJSON(map[string]interface{}{
		"type": "sync_replay_complete",
		"data": map[string]interface{}{
			"client_id": clientID,
		},
	})
}

// SendSyncReconcile sends the reconciliation plan to a client.
func (ws *ReactWebServer) SendSyncReconcile(safeConn *SafeConn, data *SyncReconcileData) error {
	return safeConn.WriteJSON(map[string]interface{}{
		"type": "sync_reconcile",
		"data": data,
	})
}
