package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// newNotEnabledEmbeddingEnv returns a ToolEnv whose hermetic config has no
// embedding_index opt-in, so IsEnabled() is false — the build/update gate.
func newNotEnabledEmbeddingEnv(t *testing.T) ToolEnv {
	t.Helper()
	return newTestEnv(t, t.TempDir())
}

// writeWorkspaceEmbeddingConfig writes the workspace-level config file read
// by configuration.WorkspaceEmbeddingIndexEnabled under a workspace root.
func writeWorkspaceEmbeddingConfig(t *testing.T, root, content string) {
	t.Helper()
	path := filepath.Join(root, ".sprout", "workspace.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// newEnabledEmbeddingEnv returns a ToolEnv whose workspace config file and
// config manager both opt into the embedding index (enabled && experimental)
// — the realistic enabled path for build/update. The agent-owned manager is
// backed by a mock provider so the background build never touches ONNX.
func newEnabledEmbeddingEnv(t *testing.T) (ToolEnv, *embedding.EmbeddingManager) {
	t.Helper()
	root := t.TempDir()
	writeWorkspaceEmbeddingConfig(t, root, `{"embedding_index":{"enabled":true,"experimental":true}}`)

	cfgDir := filepath.Join(t.TempDir(), ".sprout")
	require.NoError(t, os.MkdirAll(cfgDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "config.json"),
		[]byte(`{"embedding_index":{"enabled":true,"experimental":true}}`), 0o600))
	mgr, err := configuration.NewManagerWithDir(cfgDir)
	require.NoError(t, err)

	store, err := embedding.NewHNSWStore(filepath.Join(root, "index.hnsw"), "hash")
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	provider := &gateTestProvider{dims: 8}
	embMgr := embedding.NewEmbeddingManager(&configuration.EmbeddingIndexConfig{}, root)
	embMgr.SetForTesting(provider, store, embedding.NewIndexManager(provider, store, embedding.IndexOptions{}))

	env := newTestEnv(t, root)
	env.ConfigManager = mgr
	env.EmbeddingMgr = embMgr
	return env, embMgr
}

// TestEmbeddingIndexHandler_BuildNotEnabled verifies build reports the
// not-enabled gate instead of acquiring a manager when the workspace config
// has not opted in (SP-137 Phase 1).
func TestEmbeddingIndexHandler_BuildNotEnabled(t *testing.T) {
	t.Parallel()
	h := &embeddingIndexHandler{}
	env := newNotEnabledEmbeddingEnv(t)

	res, err := h.Execute(context.Background(), env, map[string]any{"operation": "build"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "Embedding index is not enabled for this workspace")
}

// TestEmbeddingIndexHandler_UpdateNotEnabled verifies update reports the
// not-enabled gate instead of acquiring a manager when the workspace config
// has not opted in (SP-137 Phase 1).
func TestEmbeddingIndexHandler_UpdateNotEnabled(t *testing.T) {
	t.Parallel()
	h := &embeddingIndexHandler{}
	env := newNotEnabledEmbeddingEnv(t)

	res, err := h.Execute(context.Background(), env, map[string]any{"operation": "update"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "Embedding index is not enabled for this workspace")
}

// TestEmbeddingIndexHandler_StatusNotGated verifies status keeps working
// without a manager or opt-in — the gate only applies to build/update.
func TestEmbeddingIndexHandler_StatusNotGated(t *testing.T) {
	t.Parallel()
	h := &embeddingIndexHandler{}
	env := newNotEnabledEmbeddingEnv(t)

	res, err := h.Execute(context.Background(), env, map[string]any{"operation": "status"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.Contains(t, res.Output, "Embedding Index Status")
	require.Contains(t, res.Output, "Enabled: false")
}

// TestEmbeddingIndexHandler_BuildEnabled verifies build proceeds past the
// gate when the workspace opted in: the output is a build-progress message,
// never the not-enabled message (SP-137 Phase 1 enabled-path regression).
func TestEmbeddingIndexHandler_BuildEnabled(t *testing.T) {
	t.Parallel()
	h := &embeddingIndexHandler{}
	env, embMgr := newEnabledEmbeddingEnv(t)

	res, err := h.Execute(context.Background(), env, map[string]any{"operation": "build"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	require.NotContains(t, res.Output, "Embedding index is not enabled for this workspace")

	// The agent-owned manager starts a background build against the mock;
	// wait for it so it cannot outlive the test's store cleanup.
	require.Eventually(t, func() bool { return !embMgr.IsBuilding() }, 10*time.Second, 50*time.Millisecond)
}
