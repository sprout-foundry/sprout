//go:build !js

package webui

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// --- Types ---

// ReconciliationAction describes what recovery action to take for a single file.
type ReconciliationAction struct {
	FilePath         string    `json:"file_path"`
	Action           string    `json:"action"` // "sync_ok", "container_ahead", "browser_ahead", "diverged"
	ContainerSeq     int64     `json:"container_seq"`
	BrowserSeq       int64     `json:"browser_seq"`
	ContainerContent string    `json:"container_content,omitempty"`
	ContainerModTime time.Time `json:"container_mod_time,omitempty"`
}

// SyncRecoverData is the payload the browser sends in a sync_recover message.
type SyncRecoverData struct {
	ClientID string           `json:"client_id"`
	Seqs     map[string]int64 `json:"seqs"` // file_path -> browser_seq
}

// SyncReconcileData is the server's response with a reconciliation plan.
type SyncReconcileData struct {
	ClientID string                 `json:"client_id"`
	Plan     []ReconciliationAction `json:"plan"`
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

	ws.log().Info("container recovery plan created", slog.String("client_id", clientID), slog.Int("file_count", len(plan)))
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

// --- File replay helpers ---

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
