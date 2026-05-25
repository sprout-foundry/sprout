package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// EnableEmbeddingIndex initializes the embedding manager and starts building
// the index in the background. Call this when the user explicitly enables
// indexing for the workspace (via /index command or UI toggle).
// It persists the preference to the workspace config so it survives restarts.
func (a *Agent) EnableEmbeddingIndex() error {
	cfg := a.GetConfig()
	if cfg == nil {
		return fmt.Errorf("no config available")
	}

	ei := cfg.EmbeddingIndex
	if ei == nil {
		ei = &configuration.EmbeddingIndexConfig{Enabled: true, AutoIndex: true}
		cfg.EmbeddingIndex = ei
	}
	ei.Enabled = true
	ei.AutoIndex = true

	workspaceRoot := a.GetWorkspaceRoot()
	if workspaceRoot == "" {
		return fmt.Errorf("no workspace root available")
	}

	a.embeddingMgr = embedding.NewEmbeddingManager(ei, workspaceRoot)
	go a.embeddingMgr.AutoBuildWhenReady()

	// Run one-time memory migration: embed all existing memories into conversation store
	go MigrateMemories(context.Background(), a.embeddingMgr)

	// Persist the preference to workspace config
	a.persistEmbeddingIndexPreference(workspaceRoot, true)

	return nil
}

// DisableEmbeddingIndex stops and cleans up the embedding manager.
// It persists the preference to the workspace config so it stays disabled on restart.
func (a *Agent) DisableEmbeddingIndex() {
	if a.embeddingMgr != nil {
		_ = a.embeddingMgr.Close()
		a.embeddingMgr = nil
	}

	// Persist the preference to workspace config
	workspaceRoot := a.GetWorkspaceRoot()
	if workspaceRoot != "" {
		a.persistEmbeddingIndexPreference(workspaceRoot, false)
	}
}

// IsEmbeddingIndexEnabled returns whether the embedding index is currently active.
func (a *Agent) IsEmbeddingIndexEnabled() bool {
	return a.embeddingMgr != nil
}

// RestoreEmbeddingIndex checks if indexing was previously enabled for this
// workspace and restores it. Called once during agent startup after workspace
// root is known.
func (a *Agent) RestoreEmbeddingIndex() {
	workspaceRoot := a.GetWorkspaceRoot()
	if workspaceRoot == "" {
		return
	}

	wsCfgPath := configuration.GetWorkspaceConfigPath(workspaceRoot)
	data, err := os.ReadFile(wsCfgPath)
	if err != nil {
		return // no workspace config = indexing not previously enabled
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}

	// Check if embedding_index.enabled is set to true in workspace config
	eiRaw, ok := raw["embedding_index"]
	if !ok {
		return
	}

	var eiConfig struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(eiRaw, &eiConfig); err != nil {
		return
	}

	if eiConfig.Enabled {
		_ = a.EnableEmbeddingIndex()
	}
}

// persistEmbeddingIndexPreference saves the indexing enabled/disabled state
// to the workspace config file so it persists across sessions.
func (a *Agent) persistEmbeddingIndexPreference(workspaceRoot string, enabled bool) {
	wsCfgPath := configuration.GetWorkspaceConfigPath(workspaceRoot)
	wsCfgDir := filepath.Dir(wsCfgPath)

	// Ensure the .sprout directory exists
	if err := os.MkdirAll(wsCfgDir, 0755); err != nil {
		a.Logger().Warn("Failed to create embedding index config directory %s: %v", wsCfgDir, err)
		return
	}

	// Read existing config or start fresh
	var existing map[string]interface{}
	if data, err := os.ReadFile(wsCfgPath); err == nil {
		_ = json.Unmarshal(data, &existing)
	}
	if existing == nil {
		existing = make(map[string]interface{})
	}

	// Update the embedding_index section
	eiMap, ok := existing["embedding_index"].(map[string]interface{})
	if !ok {
		eiMap = make(map[string]interface{})
	}
	eiMap["enabled"] = enabled
	eiMap["auto_index"] = enabled
	existing["embedding_index"] = eiMap

	// Write back
	if data, err := json.MarshalIndent(existing, "", "  "); err == nil {
		if err := os.WriteFile(wsCfgPath, data, 0600); err != nil {
			a.Logger().Warn("Failed to write embedding index config to %s: %v", wsCfgPath, err)
		}
	} else {
		a.Logger().Warn("Failed to marshal embedding index config: %v", err)
	}
}
