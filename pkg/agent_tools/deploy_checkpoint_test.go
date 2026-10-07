package tools

// Tests for the deploy tool's automatic checkpoint: a completed deploy
// leaves a restorable checkpoint of the shipped state under the workspace's
// .sprout/checkpoints/ store.

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/deploy"
	"github.com/sprout-foundry/sprout/pkg/history"
)

// TestDeploy_CapturesCheckpoint asserts that a successful deploy records a
// checkpoint of the shipped state in the workspace's checkpoint store.
func TestDeploy_CapturesCheckpoint(t *testing.T) {
	root := newDeployProject(t)
	installDeploySeams(t, deploy.NewFake())

	// Seed a history revision so the deploy checkpoint has a restorable
	// revision to capture, and pin that the history store used is the
	// process store (the agent's), not a stray one.
	prevC, prevR := history.GetPathsForTesting()
	tmp := t.TempDir()
	history.SetPathsForTesting(filepath.Join(tmp, "changes"), filepath.Join(tmp, "revisions"))
	t.Cleanup(func() { history.SetPathsForTesting(prevC, prevR) })
	_, err := history.RecordBaseRevision("deploy-rev", "prompt", "resp", nil)
	require.NoError(t, err)
	require.NoError(t, history.RecordChangeWithDetails("deploy-rev",
		filepath.Join(root, "index.html"), "<html/>", "<html>shipped</html>", "edit", "", "", "", "test-model"))

	res, err := (&deployHandler{}).Execute(context.Background(), deployToolEnv(root), map[string]any{})
	require.NoError(t, err)
	require.False(t, res.IsError, "output: %s", res.Output)

	// Read the workspace store directly, the same store the deploy path
	// writes to (not the process-wide store).
	cps, err := history.ListCheckpointsInWorkspace(root, false)
	require.NoError(t, err)
	require.Len(t, cps, 1)
	assert.Equal(t, history.CheckpointDeploy, cps[0].Origin)
	// The captured revision is the history revision the deploy built, a
	// restorable value — never d.Version (the opaque starter version).
	assert.Equal(t, "deploy-rev", cps[0].RevisionID)
}
