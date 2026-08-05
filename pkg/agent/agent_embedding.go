package agent

import (
	"encoding/json"
	"os"

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
		ei = &configuration.EmbeddingIndexConfig{}
		ei.SetEnabled(true)
		ei.SetAutoIndex(true)
		cfg.EmbeddingIndex = ei
	}
	ei.SetEnabled(true)
	ei.SetAutoIndex(true)

	workspaceRoot := a.GetWorkspaceRoot()
	if workspaceRoot == "" {
		return agenterrors.NewConfig("no workspace root available", nil)
	}

	// Shared per (index dir, workspace): the daemon builds an agent per chat
	// session and per workspace switch, and a manager per agent means a full
	// duplicate of the workspace's vectors plus a competing writer to the same
	// index. AutoBuildWhenReady is idempotent per manager, so the second and
	// later agents on a workspace attach to the in-flight build rather than
	// starting another.
	mgr := embedding.AcquireManager(ei, workspaceRoot)
	a.embeddingMu.Lock()
	previous := a.embeddingMgr
	a.embeddingMgr = mgr
	a.embeddingMu.Unlock()
	// Re-enabling over an existing manager would otherwise strand its
	// reference and leak the store it pins.
	if previous != nil && previous != mgr {
		embedding.ReleaseManager(previous)
	}
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
	// Release rather than Close: other agents on this workspace may still hold
	// the manager, and the last releaser closes it.
	embedding.ReleaseManager(mgr)

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

	// Never auto-index the home directory. WorkspaceConfigDir already returns
	// "" at $HOME so there is no config here to opt in, but this stays as a
	// second gate because the blast radius is the whole home directory and the
	// failure is silent: the daemon walks and AST-parses every file under ~,
	// which on macOS also trips TCC prompts for Documents/Desktop/Photos.
	//
	// An earlier revision removed this guard on the reasoning that the config
	// split made the collision impossible. That was wrong — the daemon writes
	// its own workspace layer, so "an explicit workspace.json exists" was not
	// evidence of user intent. Enabling for home remains possible as a
	// deliberate runtime action; it just must never happen automatically.
	if isHomeDirPath(workspaceRoot) {
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
// to the workspace config via the config manager so it persists across
// sessions and doesn't conflict with the manager's own save path (which
// would clobber a raw file write via its conflict-detection check).
func (a *Agent) persistEmbeddingIndexPreference(workspaceRoot string, enabled bool) {
	mgr := a.GetConfigManager()
	if mgr == nil {
		a.Logger().Warn("Cannot persist embedding index preference: no config manager")
		return
	}

	err := mgr.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.EmbeddingIndex == nil {
			cfg.EmbeddingIndex = &configuration.EmbeddingIndexConfig{}
		}
		cfg.EmbeddingIndex.SetEnabled(enabled)
		cfg.EmbeddingIndex.SetAutoIndex(enabled)
		return nil
	})
	if err != nil {
		a.Logger().Warn("Failed to persist embedding index preference: %v", err)
	}
}
