package design

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The screen brief exists twice: the Go design_brief (BuildScreenBrief) and
// the webui workbench's client-side derivation (screenBrief.ts). Each side's
// own tests pass on its own fixtures, which pins nothing across the language
// boundary — the SP-140-8 acceptance criterion ("one truth, no second
// aggregation path") holds only if the two arms agree on the same tree.
//
// The fixture (testdata/webui-brief/screen-brief.json) is that shared tree
// plus the Go brief over it; webui/src/components/design/
// screenBrief.parity.test.ts re-derives the brief client-side from the same
// bytes and pins the projected fields equal. This file guards the Go half of
// that handshake: the fixture's goBrief must still be exactly what
// BuildScreenBrief produces for the fixture tree, so a Go-side contract
// change fails here instead of leaving the committed fixture — and
// therefore the webui pin — silently stale.

type briefParityArtifact struct {
	Description string            `json:"description"`
	Screen      string            `json:"screen"`
	Tree        map[string]string `json:"tree"`
	GoBrief     *ScreenBrief      `json:"goBrief"`
}

func loadBriefParityArtifact(t *testing.T) briefParityArtifact {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "webui-brief", "screen-brief.json"))
	require.NoError(t, err, "the screen-brief parity fixture must exist (regenerate it when the fixture tree changes)")
	var artifact briefParityArtifact
	require.NoError(t, json.Unmarshal(raw, &artifact))
	require.NotNil(t, artifact.GoBrief)
	require.NotEmpty(t, artifact.Tree)
	require.NotEmpty(t, artifact.Screen)
	return artifact
}

// TestScreenBriefParity_FixtureMatchesGoBrief re-runs the Go brief over the
// fixture's own tree and asserts the committed goBrief is current. It writes
// nothing outside t.TempDir.
func TestScreenBriefParity_FixtureMatchesGoBrief(t *testing.T) {
	artifact := loadBriefParityArtifact(t)

	root := t.TempDir()
	for rel, content := range artifact.Tree {
		p := filepath.Join(root, filepath.FromSlash(rel))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
	}

	brief, err := BuildScreenBrief(ScreenBriefInput{Root: root, ScreenName: artifact.Screen, Depth: BriefDepthFull})
	require.NoError(t, err)

	assert.Equal(t, artifact.GoBrief, brief,
		"the committed parity fixture is stale: BuildScreenBrief no longer produces it. "+
			"Regenerate the fixture (rerun the generator) and re-check the webui pin still agrees.")
}

// TestScreenBriefParity_FixtureExercisesTheContract keeps the fixture an
// honest parity probe: every §5g facet the two implementations share must be
// non-degenerate in it, or the cross-language pin would pass vacuously.
func TestScreenBriefParity_FixtureExercisesTheContract(t *testing.T) {
	artifact := loadBriefParityArtifact(t)
	brief := artifact.GoBrief

	assert.True(t, brief.Found)
	assert.True(t, brief.ListedInReadme)
	assert.NotEmpty(t, brief.Purpose)
	assert.NotEmpty(t, brief.Status)
	assert.True(t, brief.WireframeExists)
	assert.NotEmpty(t, brief.ScreenFile, "full depth names the delivered screen")
	assert.NotEmpty(t, brief.FlowsIn)
	assert.NotEmpty(t, brief.FlowsOut)
	var labelled bool
	for _, edge := range append(append([]BriefFlowEdge{}, brief.FlowsIn...), brief.FlowsOut...) {
		if edge.Trigger != "" {
			labelled = true
		}
	}
	assert.True(t, labelled, "at least one edge must carry a trigger label (the contract's headline field)")
	require.Len(t, brief.TokenPaths, 2)
	assert.True(t, brief.TokenPaths[0].Known, "one known token ref")
	assert.False(t, brief.TokenPaths[1].Known, "one unknown token ref")
	assert.True(t, brief.Feedback.Pending)
	assert.NotEmpty(t, brief.Feedback.Notes, "full depth carries the open annotation note")
}
