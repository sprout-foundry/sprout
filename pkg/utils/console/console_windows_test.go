//go:build windows && !js

package console

import (
	"os/exec"
	"testing"

	"golang.org/x/sys/windows"
)

// PID 0 means "every process on this console" to GenerateConsoleCtrlEvent;
// actually sending it would kill the test runner along with the test.
func TestSendCtrlBreak_RefusesConsoleBroadcast(t *testing.T) {
	if err := SendCtrlBreak(0); err == nil {
		t.Error("SendCtrlBreak(0) must refuse instead of broadcasting to the console")
	}
	if err := SendCtrlBreak(-1); err == nil {
		t.Error("SendCtrlBreak(-1) must refuse")
	}
}

func TestSendCtrlBreak_NonExistentPID(t *testing.T) {
	if err := SendCtrlBreak(999999999); err == nil {
		t.Error("SendCtrlBreak(999999999) should error (no such process)")
	}
}

func TestSetNewProcessGroup_PreservesExistingFlags(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "exit 0")
	SetNewProcessGroup(cmd)
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
	SetNewProcessGroup(cmd)
	want := uint32(windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW)
	if cmd.SysProcAttr.CreationFlags != want {
		t.Errorf("CreationFlags = %#x, want %#x", cmd.SysProcAttr.CreationFlags, want)
	}
}
