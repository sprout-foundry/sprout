package agent

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// EnableEmbeddingIndex initializes the embedding manager and starts building
// the index in the background. Call this when the user explicitly enables
// indexing for the workspace (via /index command or UI toggle).
// It persists the preference to the workspace config so it survives restarts.
func (a *Agent) EnableEmbeddingIndex() error {
	cfg := a.GetConfig()
	if cfg == nil {
		return agenterrors.NewConfig("no config available", nil)
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
		return agenterrors.NewConfig("no workspace root available", nil)
	}

	mgr := embedding.NewEmbeddingManager(ei, workspaceRoot)
	a.embeddingMu.Lock()
	a.embeddingMgr = mgr
	a.embeddingMu.Unlock()
	go mgr.AutoBuildWhenReady()

	// Snapshot the interrupt ctx before launching the goroutine so the field
	// isn't read from another goroutine without synchronization. The local
	// `mgr` already shadows the racy field for the goroutine's use.
	migrateCtx, _ := a.snapshotInterrupt()
	a.backgroundWg.Add(1)
	go func() {
		defer a.backgroundWg.Done()
		MigrateMemories(migrateCtx, mgr)
	}()

	// Persist the preference to workspace config
	a.persistEmbeddingIndexPreference(workspaceRoot, true)

	return nil
}

// DisableEmbeddingIndex stops and cleans up the embedding manager.
// It persists the preference to the workspace config so it stays disabled on restart.
func (a *Agent) DisableEmbeddingIndex() {
	a.embeddingMu.Lock()
	mgr := a.embeddingMgr
	a.embeddingMgr = nil
	a.embeddingMu.Unlock()
	if mgr != nil {
		_ = mgr.Close()
	}

	// Persist the preference to workspace config
	workspaceRoot := a.GetWorkspaceRoot()
	if workspaceRoot != "" {
		a.persistEmbeddingIndexPreference(workspaceRoot, false)
	}
}

// IsEmbeddingIndexEnabled returns whether the embedding index is currently active.
func (a *Agent) IsEmbeddingIndexEnabled() bool {
	a.embeddingMu.RLock()
	defer a.embeddingMu.RUnlock()
	return a.embeddingMgr != nil
}

// RestoreEmbeddingIndex enables the workspace embedding index only when the
// user has opted in. Called once during agent startup after workspace root is
// known.
//
// Embeddings are OPT-IN, not default-on. The index lazily loads a ~380MB ONNX
// model + in-memory HNSW store (it is ~80% of an agent's resident memory:
// ~486MB with it, ~103MB without — measured), and proactive-context runs it on
// every prompt. A fresh agent therefore stays lightweight unless semantic
// recall / duplicate detection is explicitly wanted. Enable it via any of:
//   - workspace config `embedding_index.enabled: true` (set by /index or the UI toggle), or
//   - env `SPROUT_ENABLE_EMBEDDING_AUTOINDEX=1` for default-on globally.
//
// `SPROUT_DISABLE_EMBEDDING_AUTOINDEX=1` always wins and hard-disables (used by
// the test suites — see cmd/main_test.go and pkg/agent's TestMain).
//
// Resolution order:
//  1. SPROUT_DISABLE_EMBEDDING_AUTOINDEX=1                 → skip (hard off).
//  2. Workspace config embedding_index.enabled: true      → enable (explicit opt-in).
//  3. Workspace config embedding_index.enabled: false     → skip (explicit opt-out).
//  4. No section / no file / unreadable config            → enable only if
//     SPROUT_ENABLE_EMBEDDING_AUTOINDEX=1, else skip (lazy/opt-in default).
func (a *Agent) RestoreEmbeddingIndex() {
	if os.Getenv("SPROUT_DISABLE_EMBEDDING_AUTOINDEX") == "1" {
		return
	}

	workspaceRoot := a.GetWorkspaceRoot()
	if workspaceRoot == "" {
		return
	}

	// Default (no explicit per-workspace preference): enable only if the user
	// opted into default-on embeddings globally.
	autoOptIn := os.Getenv("SPROUT_ENABLE_EMBEDDING_AUTOINDEX") == "1"
	enableDefault := func() {
		if autoOptIn {
			_ = a.EnableEmbeddingIndex()
		}
	}

	wsCfgPath := configuration.GetWorkspaceConfigPath(workspaceRoot)
	data, err := os.ReadFile(wsCfgPath)
	if err != nil {
		// No workspace config file — fresh workspace, use the default.
		enableDefault()
		return
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		// Unreadable config — treat as no preference.
		enableDefault()
		return
	}

	// Check if embedding_index section exists.
	eiRaw, ok := raw["embedding_index"]
	if !ok {
		// No embedding_index section — no preference, use the default.
		enableDefault()
		return
	}

	var eiConfig struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.Unmarshal(eiRaw, &eiConfig); err != nil {
		// Malformed section — treat as no preference.
		enableDefault()
		return
	}

	if eiConfig.Enabled {
		// Explicit per-workspace opt-in.
		_ = a.EnableEmbeddingIndex()
	}
	// If explicitly false, skip — user opted out.
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
