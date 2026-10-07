package tools

// Tests for the on-demand checkpoint tool and its ManageCheckpoints
// operations: list, create, and restore (with the restore-is-an-entry
// guarantee). They run against a seeded temp history and checkpoint store.

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/history"
)

// seedCheckpointToolStore redirects the history store to a temp dir and
// records one revision with a file change under a workdir. It returns the
// workdir (used as the tool's workspace, so the workspace-scoped checkpoint
// store is the isolated one). The process-wide checkpoint store is also
// redirected so a test that reaches the fallback stays isolated.
func seedCheckpointToolStore(t *testing.T) string {
	t.Helper()
	prevC, prevR := history.GetPathsForTesting()
	prevCp := history.GetCheckpointsDir()
	tmp := t.TempDir()
	workdir := t.TempDir()
	history.SetPathsForTesting(filepath.Join(tmp, "changes"), filepath.Join(tmp, "revisions"))
	history.SetCheckpointsDirForTesting(filepath.Join(tmp, "checkpoints"))
	t.Cleanup(func() {
		history.SetPathsForTesting(prevC, prevR)
		history.SetCheckpointsDirForTesting(prevCp)
	})

	if _, err := history.RecordBaseRevision("rev-1", "prompt", "response", nil); err != nil {
		t.Fatalf("RecordBaseRevision: %v", err)
	}
	path := filepath.Join(workdir, "a.go")
	if err := os.WriteFile(path, []byte("package a\n// changed\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := history.RecordChangeWithDetails("rev-1", path, "package a\n", "package a\n// changed\n", "edit", "", "", "", "m"); err != nil {
		t.Fatalf("RecordChangeWithDetails: %v", err)
	}
	return workdir
}

func TestCheckpointTool_DefinitionAndValidate(t *testing.T) {
	h := &checkpointHandler{}
	assert.Equal(t, "checkpoint", h.Name())
	def := h.Definition()
	assert.Equal(t, "checkpoint", def.Name)
	assert.NotEmpty(t, def.Description)
	assert.NoError(t, h.Validate(nil))
	assert.NoError(t, h.Validate(map[string]any{"action": "list", "confirm": true}))
	assert.Error(t, h.Validate(map[string]any{"action": 5}))
	assert.Error(t, h.Validate(map[string]any{"confirm": "yes"}))
}

func TestCheckpointTool_ListCreateRestore(t *testing.T) {
	workdir := seedCheckpointToolStore(t)
	h := &checkpointHandler{}
	env := ToolEnv{WorkspaceRoot: workdir}
	ctx := context.Background()

	// Create on demand.
	res, err := h.Execute(ctx, env, map[string]any{"action": "create", "summary": "manual point"})
	require.NoError(t, err)
	require.False(t, res.IsError, "output: %s", res.Output)
	meta, ok := res.StructuredOut.(map[string]interface{})
	require.True(t, ok, "StructuredOut must be a map")
	id, _ := meta["checkpoint_id"].(string)
	require.NotEmpty(t, id)
	assert.Equal(t, "rev-1", meta["revision_id"])

	// List shows it.
	res, err = h.Execute(ctx, env, map[string]any{"action": "list"})
	require.NoError(t, err)
	require.False(t, res.IsError)
	assert.Contains(t, res.Output, id)

	// Restore previews without confirm.
	res, err = h.Execute(ctx, env, map[string]any{"action": "restore", "checkpoint_id": id})
	require.NoError(t, err)
	assert.Contains(t, res.Output, "Would restore")

	// Restore with confirm reverts the file and records the restore.
	res, err = h.Execute(ctx, env, map[string]any{"action": "restore", "checkpoint_id": id, "confirm": true})
	require.NoError(t, err)
	require.False(t, res.IsError, "output: %s", res.Output)
	restoreMeta, ok := res.StructuredOut.(map[string]interface{})
	require.True(t, ok)
	restoreID, _ := restoreMeta["restore_entry_id"].(string)
	assert.NotEmpty(t, restoreID)

	got, _ := os.ReadFile(filepath.Join(workdir, "a.go"))
	assert.Equal(t, "package a\n", string(got))

	cps, err := history.ListCheckpointsInWorkspace(workdir, false)
	require.NoError(t, err)
	require.Len(t, cps, 2)
	assert.Equal(t, history.CheckpointRestore, cps[1].Origin)
}

func TestCheckpointTool_RestoreByRevision(t *testing.T) {
	seedCheckpointToolStore(t)
	h := &checkpointHandler{}
	ctx := context.Background()

	_, err := h.Execute(ctx, ToolEnv{}, map[string]any{"action": "create"})
	require.NoError(t, err)

	res, err := h.Execute(ctx, ToolEnv{}, map[string]any{"action": "restore", "revision_id": "rev-1"})
	require.NoError(t, err)
	assert.Contains(t, res.Output, "Would restore")
}

func TestCheckpointTool_UnknownActionAndRestore(t *testing.T) {
	seedCheckpointToolStore(t)
	h := &checkpointHandler{}
	ctx := context.Background()

	res, err := h.Execute(ctx, ToolEnv{}, map[string]any{"action": "bogus"})
	require.NoError(t, err)
	assert.True(t, res.IsError)

	res, err = h.Execute(ctx, ToolEnv{}, map[string]any{"action": "restore", "checkpoint_id": "nope"})
	require.NoError(t, err)
	assert.True(t, res.IsError)

	res, err = h.Execute(ctx, ToolEnv{}, map[string]any{"action": "restore"})
	require.NoError(t, err)
	assert.True(t, res.IsError)
}
