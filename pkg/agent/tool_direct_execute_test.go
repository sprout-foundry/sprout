//go:build !js

package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExecuteToolByName_Success runs read_file on a temp file through the
// direct execution path, verifying the full pipeline: registry construction →
// seed Execute → handler dispatch → file read → result mapping.
func TestExecuteToolByName_Success(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	workDir := t.TempDir()
	a.SetWorkspaceRoot(workDir)

	filePath := filepath.Join(workDir, "hello.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("hello from tool"), 0o644))

	content, toolErr := a.ExecuteToolByName(context.Background(), "read_file",
		`{"path":`+`"`+filePath+`"}`)
	assert.Empty(t, toolErr, "read_file must not return an error")
	assert.Contains(t, content, "hello from tool", "read_file must return file contents")
}

// TestExecuteToolByName_UnknownTool verifies that calling an unknown tool
// returns an error string from the seed registry (not a Go error), matching
// the daemon ExecuteTool contract where tool failures travel in the return value.
func TestExecuteToolByName_UnknownTool(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	content, toolErr := a.ExecuteToolByName(context.Background(), "no_such_tool", `{}`)
	assert.Empty(t, content, "unknown tool must not return content")
	assert.NotEmpty(t, toolErr, "unknown tool must return an error string")
	assert.True(t, strings.Contains(toolErr, "unknown tool") || strings.Contains(toolErr, "no_such_tool"),
		"error must mention the unknown tool: %q", toolErr)
}
