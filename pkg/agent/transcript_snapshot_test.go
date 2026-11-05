package agent

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBuildTranscriptSnapshot_RecordsProviderModel pins that a transcript
// records the provider/model serving the session, so a reader (a resumed
// session, a subagent, a post-mortem) can attribute git commits to the model
// that actually did the work.
func TestBuildTranscriptSnapshot_RecordsProviderModel(t *testing.T) {
	a := newTestAgent(t)
	a.AddMessage(api.Message{Role: "user", Content: "hello"})

	snap := a.BuildTranscriptSnapshot("manual", false)
	require.NotNil(t, snap)
	assert.Equal(t, a.GetProvider(), snap.Provider)
	assert.Equal(t, a.GetModel(), snap.Model)
	assert.NotEmpty(t, snap.Provider)
	assert.NotEmpty(t, snap.Model)

	// The pair survives the JSON round trip a reader performs.
	path, err := a.CaptureTranscriptSnapshot("manual", false)
	require.NoError(t, err)
	require.NotEmpty(t, path)

	loaded, err := LoadTranscriptSnapshot(path)
	require.NoError(t, err)
	assert.Equal(t, snap.Provider, loaded.Provider)
	assert.Equal(t, snap.Model, loaded.Model)
}
