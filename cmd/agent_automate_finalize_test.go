//go:build !js

package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/automate"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeDetachedSessionRecord seeds a launch-time session record exactly as
// the detach launcher writes it, returning the record path the child would
// have been passed via --automate-session-file.
func writeDetachedSessionRecord(t *testing.T, sproutDir, sessionID string, pid int) string {
	t.Helper()
	require.NoError(t, automate.WriteSessionFile(sproutDir, sessionID, &automate.AutomateSessionInfo{
		Workflow:       "wf.json",
		PID:            pid,
		StartedAt:      time.Now().Add(-time.Minute),
		Kind:           "automate",
		OutputFilePath: filepath.Join(sproutDir, "automate", "logs", sessionID+".log"),
	}))
	return filepath.Join(sproutDir, "automate", sessionID+".json")
}

// TestFinalizeAutomateSession_Success pins the child-side finalizer's nil-run
// mapping: exit 0 + status "success", PID zeroed, EndedAt set.
func TestFinalizeAutomateSession_Success(t *testing.T) {
	sproutDir := filepath.Join(t.TempDir(), ".sprout")
	path := writeDetachedSessionRecord(t, sproutDir, "cli-automate-success", 4242)

	finalizeAutomateSession(path, nil)

	info, err := automate.ReadSessionFile(sproutDir, "cli-automate-success")
	require.NoError(t, err)
	assert.Equal(t, "success", info.Status)
	require.NotNil(t, info.ExitCode)
	assert.Equal(t, 0, *info.ExitCode)
	require.NotNil(t, info.EndedAt)
	assert.Zero(t, info.PID, "PID must be zeroed so IsProcessAlive never matches a recycled PID")
}

// TestFinalizeAutomateSession_Error pins the error mapping: any non-nil
// RunAgent error finalizes with exit 1 + status "error" (cobra exits 1 on a
// returned error, so the record matches the child's real process exit).
func TestFinalizeAutomateSession_Error(t *testing.T) {
	sproutDir := filepath.Join(t.TempDir(), ".sprout")
	path := writeDetachedSessionRecord(t, sproutDir, "cli-automate-error", 4243)

	finalizeAutomateSession(path, errors.New("workflow failed"))

	info, err := automate.ReadSessionFile(sproutDir, "cli-automate-error")
	require.NoError(t, err)
	assert.Equal(t, "error", info.Status)
	require.NotNil(t, info.ExitCode)
	assert.Equal(t, 1, *info.ExitCode)
	require.NotNil(t, info.EndedAt)
	assert.Zero(t, info.PID)
}

// TestFinalizeAutomateSession_EmptyPathIsNoOp pins that normal interactive
// runs (no --automate-session-file) never touch any session record.
func TestFinalizeAutomateSession_EmptyPathIsNoOp(t *testing.T) {
	sproutDir := filepath.Join(t.TempDir(), ".sprout")
	writeDetachedSessionRecord(t, sproutDir, "cli-automate-noop", 4244)

	finalizeAutomateSession("", errors.New("ignored"))

	info, err := automate.ReadSessionFile(sproutDir, "cli-automate-noop")
	require.NoError(t, err)
	assert.Equal(t, "running", info.Status)
	assert.Nil(t, info.EndedAt)
	assert.Nil(t, info.ExitCode)
	assert.Equal(t, 4244, info.PID)
}

// TestFinalizeAutomateSession_MissingRecordIsNonFatal pins that a child whose
// record vanished (launcher's WriteSessionFile warned and failed, or
// `automate stop` already removed it) exits without creating one — the
// finalizer must never resurrect or invent a session record.
func TestFinalizeAutomateSession_MissingRecordIsNonFatal(t *testing.T) {
	dir := t.TempDir()
	absent := filepath.Join(dir, "automate", "gone.json")

	finalizeAutomateSession(absent, nil)

	_, err := os.Stat(absent)
	assert.True(t, os.IsNotExist(err), "finalizer must not create a missing record, got err=%v", err)
}

// runAgentCommandForTest invokes the real agent command body with the
// --automate-session-file global set, restoring every global it touches.
// SPROUT_DAEMON=1 suppresses the test-hostile side effects the command would
// otherwise trigger (background daemon auto-start, embedding socket factory).
func runAgentCommandForTest(t *testing.T, sessionFilePath, systemPromptFile string) error {
	t.Helper()
	savedSessionFile := agentAutomateSessionFile
	savedSystemPromptFile := agentSystemPromptFile
	savedWorkflowConfig := agentWorkflowConfig
	savedMockLLM := agentMockLLM
	t.Cleanup(func() {
		agentAutomateSessionFile = savedSessionFile
		agentSystemPromptFile = savedSystemPromptFile
		agentWorkflowConfig = savedWorkflowConfig
		agentMockLLM = savedMockLLM
	})

	agentAutomateSessionFile = sessionFilePath
	agentSystemPromptFile = systemPromptFile
	agentWorkflowConfig = ""
	agentMockLLM = true // bypass provider resolution entirely
	t.Setenv("SPROUT_DAEMON", "1")

	return agentCmd.RunE(agentCmd, []string{})
}

// TestAgentCommand_SelfFinalizesDetachedSession exercises the real child-side
// wiring end to end: with --automate-session-file set, an agent command that
// completes without error must finalize its own record (exit 0 / "success",
// PID zeroed). The command reaches RunAgent's direct-mode "no query" return,
// so every other deferred cleanup runs before the finalizer.
func TestAgentCommand_SelfFinalizesDetachedSession(t *testing.T) {
	sproutDir := filepath.Join(t.TempDir(), ".sprout")
	path := writeDetachedSessionRecord(t, sproutDir, "cli-automate-clean", 4245)

	require.NoError(t, runAgentCommandForTest(t, path, ""))

	info, err := automate.ReadSessionFile(sproutDir, "cli-automate-clean")
	require.NoError(t, err)
	assert.Equal(t, "success", info.Status)
	require.NotNil(t, info.ExitCode)
	assert.Equal(t, 0, *info.ExitCode)
	assert.Zero(t, info.PID, "PID must be zeroed on self-finalization")
}

// TestAgentCommand_SelfFinalizesDetachedSessionOnError pins the error leg —
// including failures that happen before RunAgent is ever entered. An
// unreadable --system-prompt file makes createChatAgent fail, which is the
// detached child's most common death (no usable provider); the deferred
// finalizer must still record exit 1 / "error" rather than leaving the
// record stuck at "running" forever.
func TestAgentCommand_SelfFinalizesDetachedSessionOnError(t *testing.T) {
	sproutDir := filepath.Join(t.TempDir(), ".sprout")
	path := writeDetachedSessionRecord(t, sproutDir, "cli-automate-fail", 4246)

	err := runAgentCommandForTest(t, path, filepath.Join(t.TempDir(), "no-such-prompt.md"))
	require.Error(t, err)

	info, readErr := automate.ReadSessionFile(sproutDir, "cli-automate-fail")
	require.NoError(t, readErr)
	assert.Equal(t, "error", info.Status)
	require.NotNil(t, info.ExitCode)
	assert.Equal(t, 1, *info.ExitCode)
	assert.Zero(t, info.PID)
}
