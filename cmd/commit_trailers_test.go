//go:build !js

package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCommitTrailerFlagRepeatable pins the repeatable --trailer flag on
// `sprout commit`: each occurrence contributes one trailer (no
// comma-splitting, so values may contain spaces and commas).
func TestCommitTrailerFlagRepeatable(t *testing.T) {
	f := commitCmd.Flags().Lookup("trailer")
	require.NotNil(t, f, "--trailer must be registered on sprout commit")

	orig := commitTrailers
	t.Cleanup(func() { commitTrailers = orig })
	commitTrailers = nil
	require.NoError(t, commitCmd.Flags().Set("trailer", "Assisted-by: Sprout (anthropic/claude-opus-5-5)"))
	require.NoError(t, commitCmd.Flags().Set("trailer", "Refs: #12, #13"))
	assert.Equal(t, []string{
		"Assisted-by: Sprout (anthropic/claude-opus-5-5)",
		"Refs: #12, #13",
	}, commitTrailers)
}

func TestBuildCommitArgs(t *testing.T) {
	assert.Empty(t, buildCommitArgs(false, false, false, nil))
	assert.Equal(t,
		[]string{"--yes", "--dry-run", "--allow-secrets", "--trailer", "Assisted-by: Sprout (m)", "--trailer", "Refs: #1"},
		buildCommitArgs(true, true, true, []string{"Assisted-by: Sprout (m)", "Refs: #1"}))
}
