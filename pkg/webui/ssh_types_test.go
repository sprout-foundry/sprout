package webui

import (
	"os/exec"
	"testing"
)

// ---------------------------------------------------------------------------
// killProcess
// ---------------------------------------------------------------------------

func TestKillProcess_NilCmd(t *testing.T) {
	err := killProcess(nil)
	if err != nil {
		t.Errorf("killProcess(nil) = %v, want nil", err)
	}
}

func TestKillProcess_NilProcess(t *testing.T) {
	cmd := &exec.Cmd{}
	err := killProcess(cmd)
	if err != nil {
		t.Errorf("killProcess(cmd with nil Process) = %v, want nil", err)
	}
}

// ---------------------------------------------------------------------------
// sshLaunchError.Error()
// ---------------------------------------------------------------------------

func TestSSHLaunchError_Error_NilReceiver(t *testing.T) {
	var e *sshLaunchError
	if e.Error() != "" {
		t.Errorf("nil.Error() = %q, want empty string", e.Error())
	}
}

func TestSSHLaunchError_Error_EmptyMessage(t *testing.T) {
	e := &sshLaunchError{}
	got := e.Error()
	if got != "failed to open SSH workspace" {
		t.Errorf("Error() = %q, want %q", got, "failed to open SSH workspace")
	}
}

func TestSSHLaunchError_Error_WithStep(t *testing.T) {
	e := &sshLaunchError{
		Step:    "download",
		Message: "network timeout",
		Details: "connection reset",
	}
	got := e.Error()
	if got != "network timeout (download)" {
		t.Errorf("Error() = %q, want %q", got, "network timeout (download)")
	}
}

func TestSSHLaunchError_Error_WhitespaceStepTrimmed(t *testing.T) {
	e := &sshLaunchError{
		Step:    "  ",
		Message: "some error",
	}
	got := e.Error()
	if got != "some error" {
		t.Errorf("Error() = %q, want %q", got, "some error")
	}
}

func TestSSHLaunchError_Error_WhitespaceMessageTrimmed(t *testing.T) {
	e := &sshLaunchError{
		Message: "   actual error   ",
		Step:    "init",
	}
	got := e.Error()
	if got != "actual error (init)" {
		t.Errorf("Error() = %q, want %q", got, "actual error (init)")
	}
}

func TestSSHLaunchError_Error_OnlyStep(t *testing.T) {
	e := &sshLaunchError{
		Step:    "connect",
		Message: "",
	}
	got := e.Error()
	if got != "failed to open SSH workspace (connect)" {
		t.Errorf("Error() = %q, want %q", got, "failed to open SSH workspace (connect)")
	}
}

// ---------------------------------------------------------------------------
// sshLaunchLogger.Path()
// ---------------------------------------------------------------------------

func TestSSHLaunchLogger_Path_NilReceiver(t *testing.T) {
	var l *sshLaunchLogger
	if l.Path() != "" {
		t.Errorf("nil.Path() = %q, want empty string", l.Path())
	}
}

func TestSSHLaunchLogger_Path_EmptyLogger(t *testing.T) {
	l := &sshLaunchLogger{path: "/some/path"}
	got := l.Path()
	if got != "/some/path" {
		t.Errorf("Path() = %q, want %q", got, "/some/path")
	}
}

// ---------------------------------------------------------------------------
// sshLaunchLogger.Logf() nil receiver
// ---------------------------------------------------------------------------

func TestSSHLaunchLogger_Logf_NilReceiver(t *testing.T) {
	var l *sshLaunchLogger
	// Should not panic
	l.Logf("test %s", "arg")
}

func TestSSHLaunchLogger_Logf_NilLogger(t *testing.T) {
	l := &sshLaunchLogger{} // logger field is nil
	// Should not panic
	l.Logf("test %s", "arg")
}

// ---------------------------------------------------------------------------
// newSSHLaunchFailure
// ---------------------------------------------------------------------------

func TestNewSSHLaunchFailure_BasicFields(t *testing.T) {
	logger := &sshLaunchLogger{}
	err := newSSHLaunchFailure("download", "network timeout", "connection reset", logger)

	e, ok := err.(*sshLaunchError)
	if !ok {
		t.Fatalf("newSSHLaunchFailure did not return *sshLaunchError, got %T", err)
	}
	if e.Step != "download" {
		t.Errorf("Step = %q, want %q", e.Step, "download")
	}
	if e.Message != "network timeout" {
		t.Errorf("Message = %q, want %q", e.Message, "network timeout")
	}
	if e.Details != "connection reset" {
		t.Errorf("Details = %q, want %q", e.Details, "connection reset")
	}
}

func TestNewSSHLaunchFailure_NilLogger(t *testing.T) {
	err := newSSHLaunchFailure("step", "message", "details", nil)

	e, ok := err.(*sshLaunchError)
	if !ok {
		t.Fatalf("newSSHLaunchFailure(nil logger) did not return *sshLaunchError, got %T", err)
	}
	if e.LogPath != "" {
		t.Errorf("LogPath = %q, want empty string when logger is nil", e.LogPath)
	}
}

func TestNewSSHLaunchFailure_WhitespaceTrimmed(t *testing.T) {
	err := newSSHLaunchFailure("  step  ", "  message  ", "  details  ", nil)

	e, ok := err.(*sshLaunchError)
	if !ok {
		t.Fatalf("newSSHLaunchFailure did not return *sshLaunchError")
	}
	if e.Step != "step" {
		t.Errorf("Step = %q, want %q", e.Step, "step")
	}
	if e.Message != "message" {
		t.Errorf("Message = %q, want %q", e.Message, "message")
	}
	if e.Details != "details" {
		t.Errorf("Details = %q, want %q", e.Details, "details")
	}
}

func TestNewSSHLaunchFailure_LogPathFromLogger(t *testing.T) {
	logger := &sshLaunchLogger{path: "/some/path/workspace.log"}
	err := newSSHLaunchFailure("step", "msg", "details", logger)

	e, ok := err.(*sshLaunchError)
	if !ok {
		t.Fatalf("newSSHLaunchFailure did not return *sshLaunchError")
	}
	if e.LogPath != "/some/path/workspace.log" {
		t.Errorf("LogPath = %q, want %q", e.LogPath, "/some/path/workspace.log")
	}
}
