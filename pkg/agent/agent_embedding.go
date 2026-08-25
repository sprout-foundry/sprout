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
//
// Sets Experimental alongside Enabled: this is the one deliberate-action path
// that counts as informed opt-in for the experimental gate (see
// RestoreEmbeddingIndex and EmbeddingIndexConfig.Experimental). A user
// calling /index or the UI toggle today, after full-workspace auto-indexing
// was found to cause severe unbounded memory growth, is choosing it knowing
// the risk — unlike a pre-existing persisted "enabled: true" from before that
// finding, which must not silently carry the same weight.
func (a *Agent) EnableEmbeddingIndex() error {
	cfg := a.GetConfig()
	if cfg == nil {
		return agenterrors.NewConfig("no config available", nil)
	}

	ei := cfg.EmbeddingIndex
	if ei == nil {
		ei = &configuration.EmbeddingIndexConfig{}
		ei.SetEnabled(true)
		ei.SetExperimental(true)
		ei.SetAutoIndex(true)
		cfg.EmbeddingIndex = ei
	}
	ei.SetEnabled(true)
	ei.SetExperimental(true)
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
// Embeddings are EXPERIMENTAL and OPT-IN, not default-on. Full-workspace
// auto-indexing was found to cause severe, unbounded native-memory growth —
// multi-GB spikes outside what Go's own memory accounting or limits can see
// or bound (see pkg/embedding/index.go, and EmbeddingIndexConfig.Experimental
// in pkg/configuration). A workspace config persisted before the
// Experimental gate existed has no "experimental" key at all, so it decodes
// to false regardless of what "enabled" was — existing users who had it on
// must explicitly opt in again via /index or the UI toggle, which sets both.
// Enable it via any of:
//   - workspace config `embedding_index.enabled: true` AND `experimental: true`
//     (both set together by /index or the UI toggle), or
//   - env `SPROUT_EXPERIMENTAL_EMBEDDINGS=1` for default-on globally.
//
// `SPROUT_DISABLE_EMBEDDING_AUTOINDEX=1` always wins and hard-disables (used by
// the test suites — see cmd/main_test.go and pkg/agent's TestMain).
//
// Resolution order:
//  1. SPROUT_DISABLE_EMBEDDING_AUTOINDEX=1                          → skip (hard off).
//  2. Workspace config enabled: true AND experimental: true         → enable (explicit opt-in).
//  3. Workspace config enabled: false, or experimental missing/false → skip (opted out, or
//     never re-opted-in since this gate was added).
//  4. No section / no file / unreadable config                     → enable only if
//     SPROUT_EXPERIMENTAL_EMBEDDINGS=1, else skip (lazy/opt-in default).
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
	autoOptIn := os.Getenv("SPROUT_EXPERIMENTAL_EMBEDDINGS") == "1"
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
		Enabled      bool `json:"enabled"`
		Experimental bool `json:"experimental"`
	}
	if err := json.Unmarshal(eiRaw, &eiConfig); err != nil {
		// Malformed section — treat as no preference.
		enableDefault()
		return
	}

	if eiConfig.Enabled && eiConfig.Experimental {
		// Explicit per-workspace opt-in, made under the experimental gate.
		_ = a.EnableEmbeddingIndex()
	}
	// If explicitly false, or experimental was never (re-)set, skip — the
	// user opted out, or hasn't opted back in since this gate was added.
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
		// Persist Experimental alongside Enabled: this function is the
		// single write path behind the /index command and the UI toggle,
		// so "enabled" here always represents a deliberate user action —
		// see EnableEmbeddingIndex for why that action counts as informed
		// opt-in under the experimental gate.
		cfg.EmbeddingIndex.SetExperimental(enabled)
		cfg.EmbeddingIndex.SetAutoIndex(enabled)
		return nil
	})
	if err != nil {
		a.Logger().Warn("Failed to persist embedding index preference: %v", err)
	}
}
